// Package admin owns the admin database (system/admin.db): users, sessions,
// workspaces, memberships, settings, the derived job index, backup records and
// the platform audit log.
package admin

import (
	"context"
	"embed"
	"fmt"
	"io/fs"
	"slices"
	"strings"
	"sync"

	"abidata/internal/dbx"
	"abidata/internal/sqlitex"
)

//go:embed migrations/sqlite/*.sql
var migrationFS embed.FS

// ApplicationID marks admin DB files ("ABAD").
const ApplicationID = 0x41424144

// Store is the admin DB. Writes are serialised on one writer connection; reads
// use a small pool of query_only connections.
type Store struct {
	path    string
	wmu     sync.Mutex
	writer  *sqlitex.Conn
	readers chan *sqlitex.Conn
}

const readerCount = 2

// Open opens (creating if needed) and migrates the admin DB at path.
func Open(ctx context.Context, path string, synchronous string) (*Store, error) {
	w, err := sqlitex.Open(ctx, path, sqlitex.Options{Kind: sqlitex.Writer, Synchronous: synchronous})
	if err != nil {
		return nil, err
	}
	s := &Store{path: path, writer: w, readers: make(chan *sqlitex.Conn, readerCount)}
	if err := s.migrate(ctx); err != nil {
		_ = w.Close()
		return nil, err
	}
	for range readerCount {
		r, err := sqlitex.Open(ctx, path, sqlitex.Options{Kind: sqlitex.Reader})
		if err != nil {
			_ = s.Close()
			return nil, err
		}
		s.readers <- r
	}
	return s, nil
}

func (s *Store) migrate(ctx context.Context) error {
	var appID, version int
	if err := s.writer.QueryRow(ctx, sqlMigrate.SQL).Scan(&appID); err != nil {
		return err
	}
	if err := s.writer.QueryRow(ctx, sqlMigrate2.SQL).Scan(&version); err != nil {
		return err
	}
	if appID != 0 && appID != ApplicationID {
		return fmt.Errorf("admin: %s is not an AbiData admin database", s.path)
	}
	names, err := fs.Glob(migrationFS, "migrations/sqlite/*.sql")
	if err != nil {
		return err
	}
	slices.Sort(names)
	if version > len(names) {
		return fmt.Errorf("admin: database version %d is newer than this server (%d)", version, len(names))
	}
	for i := version; i < len(names); i++ {
		body, err := migrationFS.ReadFile(names[i])
		if err != nil {
			return err
		}
		script := fmt.Sprintf("BEGIN IMMEDIATE;\n%s\nPRAGMA application_id=%d;\nPRAGMA user_version=%d;\nCOMMIT;", body, ApplicationID, i+1)
		if err := s.writer.ExecScript(ctx, script); err != nil {
			_ = s.writer.ExecScript(context.Background(), "ROLLBACK")
			return fmt.Errorf("admin: migration %s: %w", strings.TrimPrefix(names[i], "migrations/sqlite/"), err)
		}
	}
	return nil
}

// Read runs fn on a reader connection.
func (s *Store) Read(ctx context.Context, fn func(q dbx.Querier) error) error {
	select {
	case c := <-s.readers:
		defer func() {
			if c.Broken() {
				_ = c.Close()
				if nc, err := sqlitex.Open(context.Background(), s.path, sqlitex.Options{Kind: sqlitex.Reader}); err == nil {
					c = nc
				}
			}
			s.readers <- c
		}()
		return fn(c)
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Write runs fn in a BEGIN IMMEDIATE transaction on the writer.
func (s *Store) Write(ctx context.Context, fn func(tx dbx.Tx) error) error {
	s.wmu.Lock()
	defer s.wmu.Unlock()
	return s.writer.Immediate(ctx, func(tx *sqlitex.Tx) error { return fn(tx) })
}

// Ping verifies the DB answers (readyz).
func (s *Store) Ping(ctx context.Context) error {
	return s.Read(ctx, func(c dbx.Querier) error {
		var one int
		return c.QueryRow(ctx, sqlPing.SQL).Scan(&one)
	})
}

// Close closes all connections.
func (s *Store) Close() error {
	s.wmu.Lock()
	defer s.wmu.Unlock()
	close(s.readers)
	for r := range s.readers {
		_ = r.Close()
	}
	if s.writer != nil {
		_, _ = s.writer.Exec(context.Background(), sqlClose.SQL)
		err := s.writer.Close()
		s.writer = nil
		return err
	}
	return nil
}
