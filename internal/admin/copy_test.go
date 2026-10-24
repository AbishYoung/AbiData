package admin

import (
	"context"
	"fmt"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"testing"
	"time"

	"abidata/internal/dbx"
)

func openSQLiteStore(t *testing.T) *Store {
	t.Helper()
	s, err := Open(context.Background(), filepath.Join(t.TempDir(), "admin.db"), "NORMAL")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

// TestCopyTablesComplete: every admin table is either copied by
// migrate-backend or left behind on purpose.
func TestCopyTablesComplete(t *testing.T) {
	ctx := context.Background()
	s := openSQLiteStore(t)
	known := slices.Clone(notCopied)
	for _, c := range copyTables {
		known = append(known, c.name)
	}
	err := s.Read(ctx, func(q dbx.Querier) error {
		rows, err := q.Query(ctx, `SELECT name FROM sqlite_schema WHERE type = 'table' AND name LIKE '\_adm\_%' ESCAPE '\'`)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var name string
			if err := rows.Scan(&name); err != nil {
				return err
			}
			if !slices.Contains(known, name) {
				t.Errorf("admin table %s is neither in copyTables nor in notCopied", name)
			}
		}
		return rows.Err()
	})
	if err != nil {
		t.Fatal(err)
	}
}

// dumpStore renders every copied table's rows, sorted, for comparison.
func dumpStore(t *testing.T, s *Store) map[string][]string {
	t.Helper()
	ctx := context.Background()
	out := map[string][]string{}
	for _, c := range copyTables {
		err := s.Read(ctx, func(q dbx.Querier) error {
			rows, err := q.Query(ctx, `SELECT * FROM `+c.name)
			if err != nil {
				return err
			}
			defer rows.Close()
			cols := rows.Columns()
			for rows.Next() {
				var b strings.Builder
				for i, v := range rows.Values() {
					if c.name == "_adm_settings" && cols[i] == "key" && v == settingBackend {
						b.Reset()
						break
					}
					fmt.Fprintf(&b, "%s=%v;", cols[i], plainValue(v))
				}
				if b.Len() > 0 {
					out[c.name] = append(out[c.name], b.String())
				}
			}
			return rows.Err()
		})
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		sort.Strings(out[c.name])
	}
	return out
}

// TestCopyTo copies a populated SQLite store to the backend under test and
// back, and compares every row.
func TestCopyTo(t *testing.T) {
	ctx := context.Background()
	src := openSQLiteStore(t)
	mkUser(t, src, "u1", "ann@example.com")
	mkUser(t, src, "u2", "bob@example.com")
	w := mkWS(t, src, "Sales", "u1")
	if err := src.Audit(ctx, AuditEntry{At: t0, UserID: "u1", Action: "user.login", Details: map[string]any{"n": 1}}); err != nil {
		t.Fatal(err)
	}
	if err := src.PutSetting(ctx, "egress", `{"enabled":true}`); err != nil {
		t.Fatal(err)
	}
	thr, err := src.CreateMessageThread(ctx, CreateThreadParams{Kind: KindDirectThread, SenderID: "u1", RecipientIDs: []string{"u2"},
		Subject: "Hello", Body: "first", Now: t0})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := src.ReplyMessageThread(ctx, ReplyThreadParams{ThreadID: thr.ID, SenderID: "u2", ParentID: thr.Messages[0].ID,
		Body: "reply", Now: t0.Add(time.Minute)}); err != nil {
		t.Fatal(err)
	}
	if err := src.CreateSession(ctx, Session{IDHash: "h1", UserID: "u1", CSRFToken: "c", CreatedAt: t0, LastSeenAt: t0,
		ExpiresAt: t0.Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}

	mid := newStore(t)
	res, err := src.CopyTo(ctx, mid, CopyOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Workspaces) != 1 || res.Workspaces[0].ID != w.ID || res.Workspaces[0].Status != StatusActive {
		t.Fatalf("workspaces copied: %+v", res.Workspaces)
	}
	if res.Rows["_adm_users"] != 2 || res.Rows["_adm_messages"] != 2 {
		t.Fatalf("rows copied: %v", res.Rows)
	}
	back := openSQLiteStore(t)
	if _, err := mid.CopyTo(ctx, back, CopyOptions{}); err != nil {
		t.Fatal(err)
	}
	want := dumpStore(t, src)
	for name, s := range map[string]*Store{"target": mid, "round trip": back} {
		got := dumpStore(t, s)
		for _, c := range copyTables {
			if !slices.Equal(got[c.name], want[c.name]) {
				t.Errorf("%s: %s differs:\n got %v\nwant %v", name, c.name, got[c.name], want[c.name])
			}
		}
	}
	// Sessions stay behind; the backend stays the target's own.
	if _, err := mid.GetSession(ctx, "h1"); err == nil {
		t.Error("a session was copied")
	}
	// New audit entries number on after the copied ones.
	if err := mid.Audit(ctx, AuditEntry{At: t0.Add(time.Hour), Action: "after"}); err != nil {
		t.Fatalf("audit after copy: %v", err)
	}
	// A store that already has users is refused.
	if _, err := src.CopyTo(ctx, mid, CopyOptions{}); err == nil {
		t.Fatal("copy into a store with users succeeded")
	}
	// Rewrite replaces values on the way.
	other := openSQLiteStore(t)
	if _, err := src.CopyTo(ctx, other, CopyOptions{Rewrite: func(table, column string, v any) any {
		if table == "_adm_workspaces" && column == "name" {
			return "Renamed"
		}
		return v
	}}); err != nil {
		t.Fatal(err)
	}
	if got, err := other.GetWorkspace(ctx, w.ID); err != nil || got.Name != "Renamed" {
		t.Fatalf("rewritten workspace: %+v %v", got, err)
	}
}
