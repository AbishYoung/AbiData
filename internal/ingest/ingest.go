// Package ingest moves data in and out of a workspace: the import wizard's
// staging databases (upload, parse, infer, map, validate, commit), exports as
// operations, and the staging scratch databases behind the Lua `staging`
// module. Bulk writes go through one merge path that reads staging in Go and
// writes small chunks as background transactions so interactive writers are
// served first (brief <ingest>).
package ingest

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"abidata/internal/artifacts"
	"abidata/internal/catalog"
	"abidata/internal/fault"
	"abidata/internal/ids"
	"abidata/internal/ops"
	"abidata/internal/query"
	"abidata/internal/records"
	"abidata/internal/sqlitex"
	"abidata/internal/workspace"
)

// Limits (brief <ingest>).
const (
	MaxUploadBytes = 100 << 20
	MaxRows        = 1_000_000
	MaxColumns     = 256
	PreviewRows    = 50
	InferSample    = 1000
	ChunkRows      = 2000
	MaxRowErrors   = 100
	// AllOrNothingWarnRows is the size above which one big transaction is warned about.
	AllOrNothingWarnRows = 100_000
	// ReplaceChunkedAbove is the row count above which `replace` is no longer one
	// transaction (delete and the first chunk commit together, later chunks follow).
	ReplaceChunkedAbove = 50_000

	maxFieldBytes  = 1 << 20
	maxRecordBytes = 4 << 20
	maxJSONDepth   = 32
	stagingTTL     = 24 * time.Hour
)

// Modes and transaction styles.
const (
	ModeAppend  = "append"
	ModeUpsert  = "upsert"
	ModeReplace = "replace"

	TxChunked      = "chunked"
	TxAllOrNothing = "all_or_nothing"
)

// Import statuses.
const (
	StatusParsed     = "parsed"
	StatusMapped     = "mapped"
	StatusCommitting = "committing"
	StatusFailed     = "failed"
)

// Service implements imports, exports and run staging.
type Service struct {
	DataDir    string // <data>; staging lives in <data>/staging/<ws>
	Workspaces *workspace.Manager
	Schemas    *catalog.Cache
	Catalog    *catalog.Engine
	Records    *records.Service
	Queries    *query.Service
	Ops        *ops.Service
	Artifacts  *artifacts.Store
	// Now is a clock for tests.
	Now func() time.Time
}

func (s *Service) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

// stagingDir returns (and creates) <data>/staging/<ws>.
func (s *Service) stagingDir(ws ids.WorkspaceID) (string, error) {
	if !ids.Valid(string(ws)) {
		return "", fault.E(fault.NotFound)
	}
	dir, err := ids.SafeJoin(filepath.Join(s.DataDir, "staging"), string(ws))
	if err != nil {
		return "", fault.E(fault.NotFound)
	}
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return "", err
	}
	return dir, nil
}

var errImportNotFound = fault.New(fault.NotFound, "Import not found. It may have been committed, deleted or expired.")

// importPaths returns the staging database and source file paths of an import.
// Anything that is not a canonical ULID is NOT_FOUND, so no id can traverse.
func (s *Service) importPaths(ws ids.WorkspaceID, id string) (db string, err error) {
	if !ids.Valid(id) {
		return "", errImportNotFound
	}
	dir, err := s.stagingDir(ws)
	if err != nil {
		return "", err
	}
	db, err = ids.SafeJoin(dir, id+".db")
	if err != nil {
		return "", errImportNotFound
	}
	if _, err := os.Stat(db); err != nil {
		return "", errImportNotFound
	}
	return db, nil
}

// removeFiles deletes an import's staging database and uploaded source.
func (s *Service) removeFiles(ws ids.WorkspaceID, id string) {
	if !ids.Valid(id) {
		return
	}
	dir, err := s.stagingDir(ws)
	if err != nil {
		return
	}
	matches, _ := filepath.Glob(filepath.Join(dir, id+".*"))
	for _, m := range matches {
		_ = os.Remove(m)
	}
}

// Delete removes an import and its files.
func (s *Service) Delete(ctx context.Context, ws ids.WorkspaceID, id string) error {
	if _, err := s.importPaths(ws, id); err != nil {
		return err
	}
	m, err := s.loadMeta(ctx, ws, id)
	if err == nil && m.Status == StatusCommitting {
		return fault.New(fault.BadRequest, "The import is being committed; cancel the operation first.")
	}
	s.removeFiles(ws, id)
	return nil
}

// Sweep deletes staging files older than maxAge in every workspace directory
// (janitor; brief: staging files are deleted after 24 h).
func (s *Service) Sweep(maxAge time.Duration, now time.Time) (removed int, err error) {
	root := filepath.Join(s.DataDir, "staging")
	wss, err := os.ReadDir(root)
	if errors.Is(err, fs.ErrNotExist) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	if maxAge <= 0 {
		maxAge = stagingTTL
	}
	for _, w := range wss {
		if !w.IsDir() {
			continue
		}
		files, err := os.ReadDir(filepath.Join(root, w.Name()))
		if err != nil {
			continue
		}
		for _, f := range files {
			fi, err := f.Info()
			if err != nil || f.IsDir() || now.Sub(fi.ModTime()) < maxAge {
				continue
			}
			// Backup snapshots (bk-*) belong to the backup service, which removes them itself.
			if strings.HasPrefix(f.Name(), "bk-") {
				continue
			}
			if os.Remove(filepath.Join(root, w.Name(), f.Name())) == nil {
				removed++
			}
		}
	}
	return removed, nil
}

// openStaging opens an existing staging database.
func openStaging(ctx context.Context, path string) (*sqlitex.Conn, error) {
	if _, err := os.Stat(path); err != nil {
		return nil, errImportNotFound
	}
	return sqlitex.Open(ctx, path, sqlitex.Options{Kind: sqlitex.Staging})
}

func stagingCol(i int) string { return fmt.Sprintf("c%d", i+1) }
