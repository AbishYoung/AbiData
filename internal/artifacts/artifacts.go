// Package artifacts stores files produced by job runs and export operations
// under <data>/artifacts/<workspace>/<owner>/<name> (brief layout), with a
// per-workspace byte quota.
package artifacts

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"mime"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"abidata/internal/fault"
	"abidata/internal/ids"
)

// DefaultQuota is the per-workspace artifact byte limit when the quota does not set one.
const DefaultQuota = 1 << 30

// MaxPerRun bounds the number of artifacts one owner (run or operation) may create.
const MaxPerRun = 100

var nameRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._ -]{0,127}$`)

// ValidName reports whether name is an acceptable artifact file name.
func ValidName(name string) bool {
	return nameRe.MatchString(name) && !strings.Contains(name, "..") && !strings.HasSuffix(name, ".meta.json")
}

// Info describes a stored artifact.
type Info struct {
	Name    string    `json:"name"`
	Size    int64     `json:"size"`
	MIME    string    `json:"mime"`
	Created time.Time `json:"created_at"`
}

// Store manages artifact files.
type Store struct {
	Dir string // <data>/artifacts
	// Quota returns a workspace's byte limit (0 → DefaultQuota).
	Quota func(ws ids.WorkspaceID) int64

	mu sync.Mutex // serializes quota accounting per process
}

func (s *Store) ownerDir(ws ids.WorkspaceID, owner string) (string, error) {
	if !ids.Valid(string(ws)) || !ids.Valid(owner) {
		return "", fault.New(fault.BadRequest, "Invalid artifact owner.")
	}
	return ids.SafeJoin(s.Dir, string(ws), owner)
}

// Usage returns the bytes stored for a workspace.
func (s *Store) Usage(ws ids.WorkspaceID) (int64, error) {
	root, err := ids.SafeJoin(s.Dir, string(ws))
	if err != nil {
		return 0, err
	}
	var n int64
	err = filepath.WalkDir(root, func(_ string, d fs.DirEntry, err error) error {
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				return nil
			}
			return err
		}
		if d.Type().IsRegular() && !strings.HasSuffix(d.Name(), ".meta.json") {
			if fi, err := d.Info(); err == nil {
				n += fi.Size()
			}
		}
		return nil
	})
	return n, err
}

// Save writes r as artifact name of owner (a run or operation id). The quota is
// enforced while streaming; a partial file is removed on any error.
func (s *Store) Save(ws ids.WorkspaceID, owner, name, mimeType string, r io.Reader) (Info, error) {
	if !ValidName(name) {
		return Info{}, fault.Newf(fault.BadRequest, "Invalid artifact name %q: use letters, digits, spaces, '.', '_' or '-' (at most 128 characters).", name)
	}
	dir, err := s.ownerDir(ws, owner)
	if err != nil {
		return Info{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	used, err := s.Usage(ws)
	if err != nil {
		return Info{}, err
	}
	limit := int64(DefaultQuota)
	if s.Quota != nil {
		if q := s.Quota(ws); q > 0 {
			limit = q
		}
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return Info{}, err
	}
	if entries, _ := os.ReadDir(dir); len(entries)/2 >= MaxPerRun {
		return Info{}, fault.Newf(fault.QuotaExceeded, "A run may save at most %d artifacts.", MaxPerRun)
	}
	path := filepath.Join(dir, name)
	old := int64(0)
	if fi, err := os.Stat(path); err == nil {
		old = fi.Size()
	}
	room := limit - used + old
	if room <= 0 {
		return Info{}, fault.New(fault.QuotaExceeded, "The workspace's artifact storage is full.")
	}
	tmp, err := os.CreateTemp(dir, ".tmp-*")
	if err != nil {
		return Info{}, err
	}
	n, err := io.Copy(tmp, io.LimitReader(r, room+1))
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err == nil && n > room {
		err = fault.New(fault.QuotaExceeded, "The workspace's artifact storage is full.")
	}
	if err != nil {
		_ = os.Remove(tmp.Name())
		return Info{}, err
	}
	if mimeType == "" {
		mimeType = mime.TypeByExtension(filepath.Ext(name))
	}
	if mimeType == "" {
		mimeType = "application/octet-stream"
	}
	info := Info{Name: name, Size: n, MIME: mimeType, Created: time.Now().UTC()}
	meta, _ := json.Marshal(info)
	if err := os.WriteFile(path+".meta.json", meta, 0o600); err != nil {
		_ = os.Remove(tmp.Name())
		return Info{}, err
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		_ = os.Remove(tmp.Name())
		return Info{}, err
	}
	return info, nil
}

// List returns owner's artifacts sorted by name.
func (s *Store) List(ws ids.WorkspaceID, owner string) ([]Info, error) {
	dir, err := s.ownerDir(ws, owner)
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return []Info{}, nil
	}
	if err != nil {
		return nil, err
	}
	out := []Info{}
	for _, e := range entries {
		name := e.Name()
		if !e.Type().IsRegular() || !ValidName(name) {
			continue
		}
		if info, err := s.stat(dir, name); err == nil {
			out = append(out, info)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

func (s *Store) stat(dir, name string) (Info, error) {
	fi, err := os.Stat(filepath.Join(dir, name))
	if err != nil {
		return Info{}, err
	}
	info := Info{Name: name, Size: fi.Size(), MIME: "application/octet-stream", Created: fi.ModTime().UTC()}
	if b, err := os.ReadFile(filepath.Join(dir, name+".meta.json")); err == nil { //nolint:gosec // dir from SafeJoin, name passed ValidName
		var m Info
		if json.Unmarshal(b, &m) == nil && m.MIME != "" {
			info.MIME, info.Created = m.MIME, m.Created
		}
	}
	return info, nil
}

// Open opens an artifact for reading. The caller closes the file.
func (s *Store) Open(ws ids.WorkspaceID, owner, name string) (*os.File, Info, error) {
	if !ValidName(name) {
		return nil, Info{}, fault.New(fault.NotFound, "Artifact not found.")
	}
	dir, err := s.ownerDir(ws, owner)
	if err != nil {
		return nil, Info{}, fault.New(fault.NotFound, "Artifact not found.")
	}
	info, err := s.stat(dir, name)
	if err != nil {
		return nil, Info{}, fault.New(fault.NotFound, "Artifact not found.")
	}
	f, err := os.Open(filepath.Join(dir, name)) //nolint:gosec // dir from SafeJoin, name passed ValidName
	if err != nil {
		return nil, Info{}, fault.New(fault.NotFound, "Artifact not found.")
	}
	return f, info, nil
}

// Sweep removes owner directories older than maxAge (janitor retention).
func (s *Store) Sweep(maxAge time.Duration, now time.Time) (removed int, err error) {
	wss, err := os.ReadDir(s.Dir)
	if errors.Is(err, fs.ErrNotExist) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	for _, w := range wss {
		if !w.IsDir() || !ids.Valid(w.Name()) {
			continue
		}
		owners, err := os.ReadDir(filepath.Join(s.Dir, w.Name()))
		if err != nil {
			continue
		}
		for _, o := range owners {
			fi, err := o.Info()
			if err != nil || !o.IsDir() || now.Sub(fi.ModTime()) < maxAge {
				continue
			}
			if err := os.RemoveAll(filepath.Join(s.Dir, w.Name(), o.Name())); err == nil {
				removed++
			}
		}
	}
	return removed, nil
}

// DeleteWorkspace removes all of a workspace's artifacts.
func (s *Store) DeleteWorkspace(ws ids.WorkspaceID) error {
	root, err := ids.SafeJoin(s.Dir, string(ws))
	if err != nil {
		return err
	}
	if err := os.RemoveAll(root); err != nil {
		return fmt.Errorf("artifacts: %w", err)
	}
	return nil
}
