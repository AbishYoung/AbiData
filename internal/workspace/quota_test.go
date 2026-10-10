package workspace

import (
	"context"
	"strings"
	"testing"

	"abidata/internal/fault"
	"abidata/internal/ids"
	"abidata/internal/sqlitex"
)

// Gate 6: exceeding max_page_count → QUOTA_EXCEEDED, nothing partial is
// written, and the workspace stays usable (reads, smaller writes, deletes).
func TestQuotaExceededStaysUsable(t *testing.T) {
	ctx := context.Background()
	m, err := NewManager(Config{DataDir: t.TempDir(), Migrate: testMigrate,
		MaxPageCount: func(context.Context, ids.WorkspaceID) (int64, error) { return 256, nil }}) // 1 MiB at 4 KiB pages
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = m.Shutdown(context.Background()) })
	id := create(t, m)
	l, err := m.Acquire(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	defer l.Release()
	write := func(sql string, args ...any) error {
		return l.Write(ctx, Interactive, func(tx *sqlitex.Tx) error {
			_, err := tx.Exec(ctx, sql, args...)
			return err
		})
	}
	if err := write(`CREATE TABLE blobs (id INTEGER PRIMARY KEY, b TEXT)`); err != nil {
		t.Fatal(err)
	}
	if err := write(`INSERT INTO blobs (b) VALUES (?)`, strings.Repeat("a", 100<<10)); err != nil {
		t.Fatal(err)
	}
	// One transaction that would grow the file past the quota.
	err = l.Write(ctx, Interactive, func(tx *sqlitex.Tx) error {
		for range 20 {
			if _, err := tx.Exec(ctx, `INSERT INTO blobs (b) VALUES (?)`, strings.Repeat("b", 100<<10)); err != nil {
				return err
			}
		}
		return nil
	})
	if fault.Classify(err).Code != fault.QuotaExceeded {
		t.Fatalf("expected QUOTA_EXCEEDED, got %T %v", err, err)
	}
	count := func() int64 {
		var n int64
		if err := l.Read(ctx, func(q sqlitex.Querier) error {
			return q.QueryRow(ctx, `SELECT count(*) FROM blobs`).Scan(&n)
		}); err != nil {
			t.Fatal(err)
		}
		return n
	}
	if n := count(); n != 1 {
		t.Fatalf("partial transaction visible: %d rows", n)
	}
	if err := write(`INSERT INTO blobs (b) VALUES ('small')`); err != nil {
		t.Fatalf("small write after quota error: %v", err)
	}
	if err := write(`DELETE FROM blobs`); err != nil {
		t.Fatal(err)
	}
	if n := count(); n != 0 {
		t.Fatalf("delete: %d rows", n)
	}
}
