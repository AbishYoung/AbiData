// Package files stores workspace file attachments (ADR 0011): a per-workspace,
// content-addressed bucket next to the workspace database, the _sys_files
// metadata rows, thumbnails, upload limits and the janitor that purges files
// nothing refers to any more. References from rows are maintained by SQLite
// triggers (see catalog), so every write path keeps them consistent.
package files

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"

	"abidata/internal/dbx"
	"abidata/internal/fault"
	"abidata/internal/ids"
	"abidata/internal/workspace"
)

// Defaults.
const (
	// DefaultMaxFileBytes is the per-file size limit (ABIDATA_MAX_FILE_BYTES).
	DefaultMaxFileBytes = 25 << 20
	// DefaultQuota is a workspace's total file bytes when its quota sets none.
	DefaultQuota = 5 << 30
	// PendingTTL is how long an uploaded file may wait to be attached.
	PendingTTL = 24 * time.Hour
	// UnreferencedGrace is how long a file that lost its last reference stays,
	// so undo and row history can still show it.
	UnreferencedGrace = 7 * 24 * time.Hour

	maxConcurrentUploads = 4
	uploadsPerMinute     = 120
)

// Meta is a file's metadata.
type Meta struct {
	ID           string    `json:"id"`
	SHA256       string    `json:"sha256"`
	Size         int64     `json:"size"`
	Name         string    `json:"name" doc:"The original file name, sanitised."`
	ContentType  string    `json:"content_type" doc:"Detected from the content, never taken from the client."`
	Width        *int      `json:"width,omitempty"`
	Height       *int      `json:"height,omitempty"`
	State        string    `json:"state" enum:"pending,attached" doc:"pending until a row refers to the file."`
	HasThumbnail bool      `json:"has_thumbnail"`
	CreatedAt    time.Time `json:"created_at"`
	CreatedBy    string    `json:"created_by,omitempty"`
}

// HasThumbnail reports whether a thumbnail can be made for a content type.
func hasThumbnail(ct string) bool {
	switch ct {
	case "image/png", "image/jpeg", "image/gif":
		return true
	}
	return false
}

// Service stores and serves files.
type Service struct {
	DataDir      string
	MaxFileBytes int64 // per file; 0 = DefaultMaxFileBytes
	// Quota returns a workspace's total file byte limit (0 = DefaultQuota).
	Quota func(ws ids.WorkspaceID) int64
	Now   func() time.Time
	// OnUpload and OnPurge feed metrics (nil: ignored).
	OnUpload func(bytes int64)
	OnPurge  func(files int, bytes int64)

	mu        sync.Mutex
	gates     map[ids.WorkspaceID]*uploadGate
	locks     sync.Map // ids.WorkspaceID → *sync.RWMutex
	thumbMu   sync.Map // sha256 → *sync.Mutex
	lastScan  map[ids.WorkspaceID]time.Time
	scanEvery time.Duration
}

type uploadGate struct {
	inflight int
	tokens   float64
	last     time.Time
}

func (s *Service) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

// MaxFile returns the effective per-file size limit.
func (s *Service) MaxFile() int64 {
	if s.MaxFileBytes > 0 {
		return s.MaxFileBytes
	}
	return DefaultMaxFileBytes
}

// BucketFor returns the bucket directory handle of workspace ws.
func (s *Service) BucketFor(ws ids.WorkspaceID) (Bucket, error) {
	d, err := BucketDir(s.DataDir, ws)
	return Bucket{Dir: d}, err
}

// BucketDir is <data>/workspaces/<id>.files.
func BucketDir(dataDir string, ws ids.WorkspaceID) (string, error) {
	if !ids.Valid(string(ws)) {
		return "", ids.ErrInvalidID
	}
	return ids.SafeJoin(dataDir, "workspaces", string(ws)+".files")
}

// RemoveBucket deletes a workspace's whole bucket (hard delete).
func (s *Service) RemoveBucket(ws ids.WorkspaceID) error {
	b, err := s.BucketFor(ws)
	if err != nil {
		return err
	}
	return os.RemoveAll(b.Dir)
}

func (s *Service) wsLock(ws ids.WorkspaceID) *sync.RWMutex {
	v, _ := s.locks.LoadOrStore(ws, &sync.RWMutex{})
	return v.(*sync.RWMutex)
}

// Hold blocks the janitor from deleting blobs of ws until the returned
// function is called. Backups hold it while they copy files.
func (s *Service) Hold(ws ids.WorkspaceID) (release func()) {
	m := s.wsLock(ws)
	m.RLock()
	return m.RUnlock
}

// acquire admits one upload for ws: at most maxConcurrentUploads at a time and
// uploadsPerMinute on average, so one client cannot monopolise disk and writer.
func (s *Service) acquire(ws ids.WorkspaceID) (func(), error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.gates == nil {
		s.gates = map[ids.WorkspaceID]*uploadGate{}
	}
	g := s.gates[ws]
	now := s.now()
	if g == nil {
		g = &uploadGate{tokens: uploadsPerMinute, last: now}
		s.gates[ws] = g
	}
	g.tokens = min(uploadsPerMinute, g.tokens+now.Sub(g.last).Seconds()*uploadsPerMinute/60)
	g.last = now
	if g.inflight >= maxConcurrentUploads || g.tokens < 1 {
		return nil, &fault.Error{Code: fault.RateLimited, RetryAfter: 2 * time.Second,
			Message: "Too many uploads to this workspace at once; try again in a moment."}
	}
	g.tokens--
	g.inflight++
	return func() {
		s.mu.Lock()
		g.inflight--
		s.mu.Unlock()
	}, nil
}

// ---- upload -----------------------------------------------------------------

type ctxReader struct {
	ctx context.Context
	r   io.Reader
}

func (c ctxReader) Read(p []byte) (int, error) {
	if err := c.ctx.Err(); err != nil {
		return 0, err
	}
	return c.r.Read(p)
}

// Upload stores the bytes of body as a new pending file in workspace l. The
// size limit is enforced while streaming, the SHA-256 is computed on the way
// in, and the content type is sniffed from the bytes. The file stays pending
// until a row refers to it.
func (s *Service) Upload(ctx context.Context, l *workspace.Lease, actor, name string, body io.Reader) (Meta, error) {
	release, err := s.acquire(l.ID())
	if err != nil {
		return Meta{}, err
	}
	defer release()
	bk, err := s.BucketFor(l.ID())
	if err != nil {
		return Meta{}, err
	}
	limit := s.MaxFile()
	tmp, err := bk.NewTemp()
	if err != nil {
		return Meta{}, err
	}
	tmpName := tmp.Name()
	placed := false
	defer func() {
		_ = tmp.Close()
		if !placed {
			_ = os.Remove(tmpName)
		}
	}()

	h := sha256.New()
	head := make([]byte, 0, sniffLen)
	buf := make([]byte, 64<<10)
	var total int64
	src := ctxReader{ctx, body}
	for {
		n, rerr := src.Read(buf)
		if n > 0 {
			total += int64(n)
			if total > limit {
				return Meta{}, fault.Newf(fault.BadRequest, "The file is larger than the %s limit.", humanBytes(limit))
			}
			if len(head) < sniffLen {
				head = append(head, buf[:min(n, sniffLen-len(head))]...)
			}
			h.Write(buf[:n])
			if _, werr := tmp.Write(buf[:n]); werr != nil {
				return Meta{}, werr
			}
		}
		if errors.Is(rerr, io.EOF) {
			break
		}
		if rerr != nil {
			if ctx.Err() != nil {
				return Meta{}, ctx.Err()
			}
			return Meta{}, fault.New(fault.BadRequest, "The upload was interrupted before the whole file arrived.")
		}
	}
	if total == 0 {
		return Meta{}, fault.New(fault.BadRequest, "The file is empty.")
	}
	if err := tmp.Sync(); err != nil {
		return Meta{}, err
	}

	name = SanitizeName(name)
	ct := SniffType(name, head)
	m := Meta{ID: ids.New(), SHA256: hex.EncodeToString(h.Sum(nil)), Size: total, Name: name, ContentType: ct,
		State: "pending", HasThumbnail: hasThumbnail(ct), CreatedAt: s.now().UTC().Truncate(time.Millisecond), CreatedBy: actor}
	if IsImage(ct) {
		if _, err := tmp.Seek(0, io.SeekStart); err != nil {
			return Meta{}, err
		}
		w, hh, err := ImageSize(ct, tmp)
		if err != nil {
			return Meta{}, err
		}
		m.Width, m.Height = &w, &hh
	}
	if err := tmp.Close(); err != nil {
		return Meta{}, err
	}

	// Place the blob and insert the row in one writer transaction, so the
	// janitor (which runs on the same writer) can never delete a blob that is
	// being adopted by a new row.
	err = l.Write(ctx, workspace.Interactive, func(tx dbx.Tx) error {
		var exists bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM _sys_files WHERE sha256 = ?)`, m.SHA256).Scan(&exists); err != nil {
			return err
		}
		if !exists {
			var used int64
			if err := tx.QueryRow(ctx, `SELECT COALESCE(SUM(size), 0) FROM (SELECT size FROM _sys_files GROUP BY sha256)`).Scan(&used); err != nil {
				return err
			}
			if q := s.quota(l.ID()); used+m.Size > q {
				return fault.Newf(fault.QuotaExceeded, "This workspace's file storage is full (%s of %s used). Nothing was uploaded.", humanBytes(used), humanBytes(q))
			}
		}
		created, err := bk.Place(tmpName, m.SHA256, m.Size)
		if err != nil {
			return err
		}
		placed = true
		var w, hh any
		if m.Width != nil {
			w, hh = *m.Width, *m.Height
		}
		var by any
		if actor != "" {
			by = actor
		}
		if _, err := tx.Exec(ctx, `INSERT INTO _sys_files(id, sha256, size, name, content_type, width, height, created_at, created_by, state)
			VALUES (?,?,?,?,?,?,?,?,?, 'pending')`, m.ID, m.SHA256, m.Size, m.Name, m.ContentType, w, hh, dbx.FormatTime(m.CreatedAt), by); err != nil {
			if created {
				_ = bk.Remove(m.SHA256)
			}
			return err
		}
		return nil
	})
	if err != nil {
		return Meta{}, err
	}
	if s.OnUpload != nil {
		s.OnUpload(total)
	}
	return m, nil
}

// Create stores generated content (Lua files.create) like an upload.
func (s *Service) Create(ctx context.Context, l *workspace.Lease, actor, name string, content []byte) (Meta, error) {
	return s.Upload(ctx, l, actor, name, bytes.NewReader(content))
}

func (s *Service) quota(ws ids.WorkspaceID) int64 {
	if s.Quota != nil {
		if q := s.Quota(ws); q > 0 {
			return q
		}
	}
	return DefaultQuota
}

// ---- reads ------------------------------------------------------------------

const metaCols = `id, sha256, size, name, content_type, width, height, state, created_at, created_by`

// ErrFileNotFound is the NOT_FOUND returned for unknown, malformed or other
// workspaces' file ids.
func errNotFound() error { return fault.New(fault.NotFound, "File not found.") }

func scanMeta(row interface{ Scan(...any) error }) (Meta, error) {
	var m Meta
	var w, h *int64
	var at string
	var by *string
	if err := row.Scan(&m.ID, &m.SHA256, &m.Size, &m.Name, &m.ContentType, &w, &h, &m.State, &at, &by); err != nil {
		return Meta{}, err
	}
	if w != nil && h != nil {
		wi, hi := int(*w), int(*h)
		m.Width, m.Height = &wi, &hi
	}
	t, err := dbx.ParseTime(at)
	if err != nil {
		return Meta{}, err
	}
	m.CreatedAt = t
	if by != nil {
		m.CreatedBy = *by
	}
	m.HasThumbnail = hasThumbnail(m.ContentType)
	return m, nil
}

// Get returns a file's metadata; unknown ids are NOT_FOUND.
func (s *Service) Get(ctx context.Context, l *workspace.Lease, id string) (Meta, error) {
	if !ids.Valid(id) {
		return Meta{}, errNotFound()
	}
	var m Meta
	err := l.Read(ctx, func(q dbx.Querier) error {
		var e error
		m, e = scanMeta(q.QueryRow(ctx, `SELECT `+metaCols+` FROM _sys_files WHERE id = ?`, id))
		return e
	})
	if dbx.IsNoRows(err) {
		return Meta{}, errNotFound()
	}
	if err != nil {
		return Meta{}, fault.Classify(err)
	}
	return m, nil
}

// Open returns the file's blob for reading, with its metadata. The caller closes it.
func (s *Service) Open(ctx context.Context, l *workspace.Lease, id string) (*os.File, Meta, error) {
	m, err := s.Get(ctx, l, id)
	if err != nil {
		return nil, Meta{}, err
	}
	bk, err := s.BucketFor(l.ID())
	if err != nil {
		return nil, Meta{}, err
	}
	f, err := bk.Open(m.SHA256)
	if errors.Is(err, os.ErrNotExist) {
		return nil, Meta{}, fault.New(fault.NotFound, "This file has been removed.")
	}
	if err != nil {
		return nil, Meta{}, err
	}
	return f, m, nil
}

// Read returns the whole content of a file no larger than max bytes (Lua files.read).
func (s *Service) Read(ctx context.Context, l *workspace.Lease, id string, max int64) ([]byte, Meta, error) {
	m, err := s.Get(ctx, l, id)
	if err != nil {
		return nil, Meta{}, err
	}
	if m.Size > max {
		return nil, m, fault.Newf(fault.BadRequest, "The file is %s; this script may read at most %s.", humanBytes(m.Size), humanBytes(max))
	}
	f, _, err := s.Open(ctx, l, id)
	if err != nil {
		return nil, m, err
	}
	defer func() { _ = f.Close() }()
	b, err := io.ReadAll(io.LimitReader(f, m.Size+1))
	return b, m, err
}

// Thumbnail returns the cached (or freshly made) JPEG thumbnail for an image.
// Files that are not PNG, JPEG or GIF have none: NOT_FOUND.
func (s *Service) Thumbnail(ctx context.Context, l *workspace.Lease, id string) (*os.File, Meta, error) {
	m, err := s.Get(ctx, l, id)
	if err != nil {
		return nil, Meta{}, err
	}
	if !m.HasThumbnail {
		return nil, m, fault.New(fault.NotFound, "This file has no thumbnail.")
	}
	bk, err := s.BucketFor(l.ID())
	if err != nil {
		return nil, Meta{}, err
	}
	tp, err := bk.ThumbPath(m.SHA256)
	if err != nil {
		return nil, Meta{}, err
	}
	if f, err := os.Open(tp); err == nil { //nolint:gosec // ThumbPath validates the hash
		return f, m, nil
	}
	mu, _ := s.thumbMu.LoadOrStore(m.SHA256, &sync.Mutex{})
	mu.(*sync.Mutex).Lock()
	defer mu.(*sync.Mutex).Unlock()
	if f, err := os.Open(tp); err == nil { //nolint:gosec // as above
		return f, m, nil
	}
	src, err := bk.Open(m.SHA256)
	if errors.Is(err, os.ErrNotExist) {
		return nil, m, fault.New(fault.NotFound, "This file has been removed.")
	}
	if err != nil {
		return nil, m, err
	}
	data, err := MakeThumbnail(ctx, src, m.ContentType)
	_ = src.Close()
	if err != nil {
		if ctx.Err() != nil {
			return nil, m, ctx.Err()
		}
		return nil, m, fault.New(fault.NotFound, "This file has no thumbnail.")
	}
	if err := os.MkdirAll(filepath.Dir(tp), 0o750); err != nil {
		return nil, m, err
	}
	if err := writeAtomic(tp, data); err != nil {
		return nil, m, err
	}
	f, err := os.Open(tp) //nolint:gosec // as above
	return f, m, err
}

func writeAtomic(path string, data []byte) error {
	f, err := os.CreateTemp(filepath.Dir(path), ".t-*")
	if err != nil {
		return err
	}
	name := f.Name()
	_, err = f.Write(data)
	if err == nil {
		err = f.Sync()
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Rename(name, path)
	}
	if err != nil {
		_ = os.Remove(name)
	}
	return err
}

// Usage reports the distinct bytes and the number of file rows in a workspace.
func (s *Service) Usage(ctx context.Context, l *workspace.Lease) (bytes int64, count int64, err error) {
	err = l.Read(ctx, func(q dbx.Querier) error {
		if err := q.QueryRow(ctx, `SELECT COALESCE(SUM(size), 0) FROM (SELECT size FROM _sys_files GROUP BY sha256)`).Scan(&bytes); err != nil {
			return err
		}
		return q.QueryRow(ctx, `SELECT count(*) FROM _sys_files`).Scan(&count)
	})
	return bytes, count, err
}

func humanBytes(n int64) string {
	switch {
	case n >= 1<<30:
		return fmt.Sprintf("%.1f GiB", float64(n)/(1<<30))
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MiB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.1f KiB", float64(n)/(1<<10))
	}
	return fmt.Sprintf("%d bytes", n)
}
