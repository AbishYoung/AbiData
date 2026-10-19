package backup

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"abidata/internal/admin"
	"abidata/internal/admin/admintest"
	"abidata/internal/catalog"
	"abidata/internal/dbx"
	"abidata/internal/fault"
	"abidata/internal/ids"
	"abidata/internal/sqlitex"
	"abidata/internal/workspace"
	"abidata/internal/workspace/wstest"
)

type env struct {
	t     *testing.T
	ctx   context.Context
	dir   string
	ad    *admin.Store
	wm    *workspace.Manager
	svc   *Service
	ws    ids.WorkspaceID
	owner string
}

func newEnv(t *testing.T) *env {
	t.Helper()
	ctx := context.Background()
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "system"), 0o700); err != nil {
		t.Fatal(err)
	}
	ad, err := admin.Open(ctx, filepath.Join(dir, "system", "admin.db"), "")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ad.Close() })
	e := &env{t: t, ctx: ctx, dir: dir, ad: ad, owner: ids.New()}
	if err := ad.CreateUser(ctx, admin.User{ID: e.owner, Email: "o@example.com", DisplayName: "O", CreatedAt: time.Now().UTC()}); err != nil {
		t.Fatal(err)
	}
	e.start()
	e.ws = e.createWorkspace("Main")
	return e
}

// start (re)creates the workspace manager and service over the same data dir,
// as a process restart would.
func (e *env) start() {
	wm, err := workspace.NewManager(workspace.Config{DataDir: e.dir, Migrate: catalog.Migrate})
	if err != nil {
		e.t.Fatal(err)
	}
	e.wm = wm
	e.t.Cleanup(func() { _ = wm.Shutdown(context.Background()) })
	e.svc = &Service{DataDir: e.dir, BackupDir: filepath.Join(e.dir, "backups"), Admin: e.ad, Workspaces: wm,
		Key: []byte(strings.Repeat("k", 32)), AppVersion: "test"}
}

func (e *env) restart() {
	ctx, cancel := context.WithTimeout(e.ctx, 10*time.Second)
	defer cancel()
	if err := e.wm.Shutdown(ctx); err != nil {
		e.t.Fatal(err)
	}
	e.start()
}

func (e *env) createWorkspace(name string) ids.WorkspaceID {
	ws := ids.NewWorkspaceID()
	if err := e.wm.Create(e.ctx, ws); err != nil {
		e.t.Fatal(err)
	}
	if err := e.ad.CreateWorkspace(e.ctx, admin.Workspace{ID: ws, Name: name, CreatedAt: time.Now().UTC(), CreatedBy: e.owner}, e.owner); err != nil {
		e.t.Fatal(err)
	}
	return ws
}

func (e *env) lease(ws ids.WorkspaceID) *workspace.Lease {
	e.t.Helper()
	l, err := e.wm.Acquire(e.ctx, ws)
	if err != nil {
		e.t.Fatal(err)
	}
	return l
}

func (e *env) exec(ws ids.WorkspaceID, sql string, args ...any) {
	e.t.Helper()
	l := e.lease(ws)
	defer l.Release()
	if err := l.Write(e.ctx, workspace.Interactive, func(tx dbx.Tx) error {
		_, err := tx.Exec(e.ctx, sql, args...)
		return err
	}); err != nil {
		e.t.Fatalf("%s: %v", sql, err)
	}
}

// seed creates a catalog table plus raw tables with data.
func (e *env) seed(ws ids.WorkspaceID) {
	e.t.Helper()
	l := e.lease(ws)
	if _, err := (&catalog.Engine{}).Run(e.ctx, l, []catalog.Op{{Kind: catalog.OpCreateTable, Table: &catalog.TableSpec{DisplayName: "Notes",
		Columns: []catalog.ColumnSpec{{DisplayName: "Body", Type: catalog.ShortText}}}}}, catalog.Apply, catalog.ApplyOptions{}); err != nil {
		e.t.Fatal(err)
	}
	l.Release()
	e.exec(ws, `CREATE TABLE ledger (id INTEGER PRIMARY KEY, amount INTEGER NOT NULL, note TEXT)`)
	e.exec(ws, `CREATE TABLE total (id INTEGER PRIMARY KEY CHECK (id = 1), sum INTEGER NOT NULL)`)
	e.exec(ws, `INSERT INTO total VALUES (1, 0)`)
	for i := range 50 {
		e.exec(ws, `INSERT INTO ledger (amount, note) VALUES (?, ?)`, i, fmt.Sprintf("row %d ✓", i))
	}
	e.exec(ws, `UPDATE total SET sum = (SELECT sum(amount) FROM ledger)`)
}

// fingerprint is the schema SQL plus a hash of each table's sorted rows,
// ignoring the workspace's own identity row.
func (e *env) fingerprint(ws ids.WorkspaceID) map[string]string {
	e.t.Helper()
	l := e.lease(ws)
	defer l.Release()
	out := map[string]string{}
	err := l.ReadConn(e.ctx, func(c *sqlitex.Conn) error {
		rows, err := c.Query(e.ctx, `SELECT type, name, coalesce(sql, '') FROM sqlite_schema WHERE name NOT LIKE 'sqlite\_%' ESCAPE '\' ORDER BY type, name`)
		if err != nil {
			return err
		}
		var tables []string
		for rows.Next() {
			var typ, name, sql string
			if err := rows.Scan(&typ, &name, &sql); err != nil {
				rows.Close()
				return err
			}
			out["schema:"+typ+":"+name] = sql
			if typ == "table" {
				tables = append(tables, name)
			}
		}
		if err := rows.Err(); err != nil {
			return err
		}
		for _, tbl := range tables {
			rs, err := c.Query(e.ctx, `SELECT * FROM `+catalog.Quote(tbl))
			if err != nil {
				return err
			}
			var lines []string
			for rs.Next() {
				line := fmt.Sprintf("%#v", rs.Values())
				if tbl == "_sys_meta" && strings.Contains(line, `"workspace_id"`) {
					continue
				}
				lines = append(lines, line)
			}
			if err := rs.Err(); err != nil {
				return err
			}
			sort.Strings(lines)
			h := sha256.Sum256([]byte(strings.Join(lines, "\n")))
			out["rows:"+tbl] = fmt.Sprintf("%d:%s", len(lines), hex.EncodeToString(h[:8]))
		}
		return nil
	})
	if err != nil {
		e.t.Fatal(err)
	}
	return out
}

func (e *env) integrity(ws ids.WorkspaceID) {
	e.t.Helper()
	l := e.lease(ws)
	defer l.Release()
	if err := l.ReadConn(e.ctx, func(c *sqlitex.Conn) error {
		var res string
		if err := c.QueryRow(e.ctx, `PRAGMA integrity_check`).Scan(&res); err != nil {
			return err
		}
		if res != "ok" {
			return errors.New(res)
		}
		return nil
	}); err != nil {
		e.t.Fatalf("integrity: %v", err)
	}
}

func (e *env) snapshot(ws ids.WorkspaceID, kind string) admin.Backup {
	e.t.Helper()
	l := e.lease(ws)
	defer l.Release()
	b, err := e.svc.Snapshot(e.ctx, l, kind, e.owner)
	if err != nil {
		e.t.Fatal(err)
	}
	return b
}

func diff(a, b map[string]string) string {
	var d []string
	for k, v := range a {
		if b[k] != v {
			d = append(d, fmt.Sprintf("%s: %q vs %q", k, v, b[k]))
		}
	}
	for k := range b {
		if _, ok := a[k]; !ok {
			d = append(d, k+": missing in first")
		}
	}
	sort.Strings(d)
	return strings.Join(d, "\n")
}

// waitAside waits for the background pre_restore archive to finish.
func (e *env) waitAside() {
	e.t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		entries, _ := os.ReadDir(e.svc.asideRoot())
		if len(entries) == 0 {
			return
		}
		if time.Now().After(deadline) {
			e.t.Fatalf("pre-restore files never archived: %v", entries)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// Gate 6: logical parity.
func TestLogicalParity(t *testing.T) {
	e := newEnv(t)
	e.seed(e.ws)
	before := e.fingerprint(e.ws)
	b := e.snapshot(e.ws, admin.BackupManual)
	if _, err := e.svc.Verify(e.ctx, b); err != nil {
		t.Fatal(err)
	}
	id, err := e.svc.RestoreAsNew(e.ctx, b.ID, func(id ids.WorkspaceID) error {
		return e.ad.CreateWorkspace(e.ctx, admin.Workspace{ID: id, Name: "Copy", CreatedAt: time.Now().UTC(), CreatedBy: e.owner}, e.owner)
	})
	if err != nil {
		t.Fatal(err)
	}
	if d := diff(before, e.fingerprint(id)); d != "" {
		t.Fatalf("restore-as-new differs:\n%s", d)
	}
	e.integrity(id)
	// In-place restore after further changes returns to the snapshot.
	e.exec(e.ws, `INSERT INTO ledger (amount) VALUES (1000)`)
	e.exec(e.ws, `DROP TABLE total`)
	if err := e.svc.Restore(e.ctx, e.ws, b.ID, e.owner); err != nil {
		t.Fatal(err)
	}
	if d := diff(before, e.fingerprint(e.ws)); d != "" {
		t.Fatalf("restore differs:\n%s", d)
	}
	e.waitAside()
	all, err := e.ad.ListBackups(e.ctx, e.ws)
	if err != nil {
		t.Fatal(err)
	}
	kinds := map[string]int{}
	for _, x := range all {
		kinds[x.Kind]++
	}
	if kinds[admin.BackupPreRestore] != 1 {
		t.Fatalf("expected a pre_restore backup of the replaced files: %v", kinds)
	}
	w, _ := e.ad.GetWorkspace(e.ctx, e.ws)
	if w.Status != admin.StatusActive {
		t.Fatalf("status %s", w.Status)
	}
	// A backup of another workspace cannot be restored in place.
	other := e.createWorkspace("Other")
	if err := e.svc.Restore(e.ctx, other, b.ID, e.owner); fault.Classify(err).Code != fault.BadRequest {
		t.Fatalf("cross-workspace restore: %v", err)
	}
}

// Gate 6: a backup taken while writes continue is consistent.
func TestBackupUnderLoad(t *testing.T) {
	e := newEnv(t)
	e.seed(e.ws)
	var stop atomic.Bool
	var wg sync.WaitGroup
	var writes atomic.Int64
	for w := range 4 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			l := e.lease(e.ws)
			defer l.Release()
			for i := 0; !stop.Load(); i++ {
				amt := int64(w*1000 + i%97)
				err := l.Write(e.ctx, workspace.Interactive, func(tx dbx.Tx) error {
					if _, err := tx.Exec(e.ctx, `INSERT INTO ledger (amount, note) VALUES (?, ?)`, amt, strings.Repeat("x", 200)); err != nil {
						return err
					}
					_, err := tx.Exec(e.ctx, `UPDATE total SET sum = sum + ?`, amt)
					return err
				})
				if err != nil {
					t.Error(err)
					return
				}
				writes.Add(1)
			}
		}()
	}
	time.Sleep(50 * time.Millisecond)
	var bs []admin.Backup
	for range 3 {
		bs = append(bs, e.snapshot(e.ws, admin.BackupManual))
	}
	stop.Store(true)
	wg.Wait()
	if writes.Load() == 0 {
		t.Fatal("no concurrent writes happened")
	}
	for _, b := range bs {
		id, err := e.svc.RestoreAsNew(e.ctx, b.ID, func(id ids.WorkspaceID) error {
			return e.ad.CreateWorkspace(e.ctx, admin.Workspace{ID: id, Name: "Copy " + b.ID, CreatedAt: time.Now().UTC(), CreatedBy: e.owner}, e.owner)
		})
		if err != nil {
			t.Fatal(err)
		}
		e.integrity(id)
		l := e.lease(id)
		var sum, total int64
		err = l.ReadConn(e.ctx, func(c *sqlitex.Conn) error {
			if err := c.QueryRow(e.ctx, `SELECT sum(amount) FROM ledger`).Scan(&sum); err != nil {
				return err
			}
			return c.QueryRow(e.ctx, `SELECT sum FROM total`).Scan(&total)
		})
		l.Release()
		if err != nil || sum != total {
			t.Fatalf("snapshot %s inconsistent: sum %d total %d err %v", b.ID, sum, total, err)
		}
	}
	t.Logf("%d concurrent writes during 3 snapshots", writes.Load())
}

// Gate 6: tampering is detected and the live database is untouched.
func TestTamper(t *testing.T) {
	for _, what := range []string{"gz", "manifest", "manifest-resigned-other-key", "gz-truncated"} {
		t.Run(what, func(t *testing.T) {
			e := newEnv(t)
			e.seed(e.ws)
			b := e.snapshot(e.ws, admin.BackupManual)
			e.exec(e.ws, `INSERT INTO ledger (amount) VALUES (7)`)
			live := e.fingerprint(e.ws)
			mpath := strings.TrimSuffix(b.Path, ".db.gz") + ".manifest.json"
			switch what {
			case "gz":
				data, _ := os.ReadFile(b.Path)
				data[len(data)/2] ^= 0x01
				if err := os.WriteFile(b.Path, data, 0o600); err != nil { //nolint:gosec // test temp dir
					t.Fatal(err)
				}
			case "gz-truncated":
				data, _ := os.ReadFile(b.Path)
				if err := os.WriteFile(b.Path, data[:len(data)-10], 0o600); err != nil { //nolint:gosec // test temp dir
					t.Fatal(err)
				}
			case "manifest":
				data, _ := os.ReadFile(mpath)
				data = []byte(strings.Replace(string(data), `"kind": "manual"`, `"kind": "scheduled"`, 1))
				if err := os.WriteFile(mpath, data, 0o600); err != nil { //nolint:gosec // test temp dir
					t.Fatal(err)
				}
			case "manifest-resigned-other-key":
				m, err := e.svc.Verify(e.ctx, b)
				if err != nil {
					t.Fatal(err)
				}
				other := &Service{Key: []byte(strings.Repeat("z", 32))}
				signed, err := other.sign(m)
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(mpath, signed, 0o600); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := e.svc.Verify(e.ctx, b); fault.Classify(err).Code != fault.BackupInvalid {
				t.Fatalf("verify: %v", err)
			}
			if err := e.svc.Restore(e.ctx, e.ws, b.ID, e.owner); fault.Classify(err).Code != fault.BackupInvalid {
				t.Fatalf("restore: %v", err)
			}
			if _, err := e.svc.RestoreAsNew(e.ctx, b.ID, func(ids.WorkspaceID) error { return nil }); fault.Classify(err).Code != fault.BackupInvalid {
				t.Fatalf("restore as new: %v", err)
			}
			if d := diff(live, e.fingerprint(e.ws)); d != "" {
				t.Fatalf("live database changed:\n%s", d)
			}
			if entries, _ := os.ReadDir(e.svc.restoreDir()); len(entries) != 0 {
				t.Fatalf("temporary restore files left: %v", entries)
			}
		})
	}
}

// Gate 6: crash at every restore step → after restart and Recover the
// workspace is entirely old or entirely new, never a mix, and a stale -wal
// never applies to the restored file.
func TestCrashMatrix(t *testing.T) {
	if testing.Short() {
		t.Skip("restores once per crash point; runs without -short")
	}
	steps := []string{"prepared", "restoring", "move:.db", "move:.db-wal", "move:.db-shm", "moved_aside", "renamed", "swapped", "finish"}
	for _, step := range steps {
		t.Run(step, func(t *testing.T) {
			e := newEnv(t)
			e.seed(e.ws)
			b := e.snapshot(e.ws, admin.BackupManual)
			backupFP := e.fingerprint(e.ws)
			// Diverge the live database, leaving the changes in its -wal.
			e.exec(e.ws, `INSERT INTO ledger (amount, note) VALUES (424242, 'after backup')`)
			e.exec(e.ws, `UPDATE total SET sum = sum + 424242`)
			liveFP := e.fingerprint(e.ws)
			p, _ := e.wm.PathsFor(e.ws)
			if _, err := os.Stat(p.WAL); err != nil {
				t.Fatalf("expected a live -wal: %v", err)
			}
			hit := false
			e.svc.hook = func(s string) error {
				if s == step {
					hit = true
					return errCrash
				}
				return nil
			}
			if err := e.svc.Restore(e.ctx, e.ws, b.ID, e.owner); !errors.Is(err, errCrash) {
				t.Fatalf("restore: %v", err)
			}
			if !hit {
				t.Fatalf("step %q never reached", step)
			}
			e.restart()
			if err := e.svc.Recover(e.ctx); err != nil {
				t.Fatalf("recover: %v", err)
			}
			e.waitAside()
			e.integrity(e.ws)
			got := e.fingerprint(e.ws)
			isOld, isNew := diff(liveFP, got) == "", diff(backupFP, got) == ""
			if !isOld && !isNew {
				t.Fatalf("mixed state after crash at %s:\nvs live:\n%s\nvs backup:\n%s", step, diff(liveFP, got), diff(backupFP, got))
			}
			t.Logf("crash at %s → %s", step, map[bool]string{true: "old", false: "new"}[isOld])
			if j, _ := e.svc.readJournal(e.ws); j != nil {
				t.Fatalf("journal left: %+v", j)
			}
			if w, _ := e.ad.GetWorkspace(e.ctx, e.ws); w.Status != admin.StatusActive {
				t.Fatalf("status %s", w.Status)
			}
			if entries, _ := os.ReadDir(e.svc.restoreDir()); len(entries) != 0 {
				t.Fatalf("temporary restore files left: %v", entries)
			}
			// The workspace keeps working, and a later restore succeeds.
			e.exec(e.ws, `INSERT INTO ledger (amount) VALUES (1)`)
			e.svc.hook = nil
			if err := e.svc.Restore(e.ctx, e.ws, b.ID, e.owner); err != nil {
				t.Fatal(err)
			}
			if d := diff(backupFP, e.fingerprint(e.ws)); d != "" {
				t.Fatalf("restore after recovery differs:\n%s", d)
			}
			e.waitAside()
		})
	}
}

func TestRetention(t *testing.T) {
	now := time.Date(2026, 10, 10, 12, 0, 0, 0, time.UTC)
	var all []admin.Backup
	add := func(id, kind string, at time.Time) {
		all = append(all, admin.Backup{ID: id, Kind: kind, CreatedAt: at})
	}
	for d := range 40 {
		at := now.Add(-time.Duration(d) * 24 * time.Hour)
		add(fmt.Sprintf("s%02d-a", d), admin.BackupScheduled, at)
		add(fmt.Sprintf("s%02d-b", d), admin.BackupScheduled, at.Add(-time.Hour))
	}
	for i := range 8 {
		add(fmt.Sprintf("rp%d", i), admin.BackupRestorePoint, now.Add(-time.Duration(i)*time.Minute))
	}
	add("m-old", admin.BackupManual, now.Add(-400*24*time.Hour))
	add("pre-new", admin.BackupPreRestore, now.Add(-24*time.Hour))
	add("pre-old", admin.BackupPreRestore, now.Add(-8*24*time.Hour))
	keep := keepSet(all, DefaultRetention, now)
	want := map[string]bool{"m-old": true, "pre-new": true, "rp0": true, "rp4": true}
	for id, k := range want {
		if keep[id] != k {
			t.Errorf("%s kept=%v", id, keep[id])
		}
	}
	for _, id := range []string{"pre-old", "rp5", "s00-b", "s39-a"} {
		if keep[id] {
			t.Errorf("%s should be deleted", id)
		}
	}
	scheduled := 0
	for _, b := range all {
		if b.Kind == admin.BackupScheduled && keep[b.ID] {
			scheduled++
			if strings.HasSuffix(b.ID, "-b") {
				t.Errorf("kept older backup %s of a day", b.ID)
			}
		}
	}
	// 7 daily + up to 4 weekly (some weeks overlap the daily ones).
	if scheduled < 7 || scheduled > 11 {
		t.Errorf("kept %d scheduled backups", scheduled)
	}
}

func TestScheduledTick(t *testing.T) {
	e := newEnv(t)
	e.seed(e.ws)
	e.createWorkspace("Second")
	now := time.Date(2026, 10, 10, 1, 0, 0, 0, time.UTC)
	e.svc.now = func() time.Time { return now }
	count := func() int {
		n := 0
		all, _ := e.ad.ListWorkspaces(e.ctx)
		for _, w := range all {
			bs, _ := e.ad.ListBackups(e.ctx, w.ID)
			for _, b := range bs {
				if b.Kind == admin.BackupScheduled {
					n++
				}
			}
		}
		return n
	}
	e.svc.ScheduledTick(e.ctx)
	if n := count(); n != 0 {
		t.Fatalf("backed up before the policy hour: %d", n)
	}
	now = now.Add(2 * time.Hour)
	e.svc.ScheduledTick(e.ctx)
	e.svc.ScheduledTick(e.ctx)
	if n := count(); n != 2 {
		t.Fatalf("expected one scheduled backup per workspace, got %d", n)
	}
	now = now.Add(24 * time.Hour)
	e.svc.ScheduledTick(e.ctx)
	if n := count(); n != 4 {
		t.Fatalf("next day: %d", n)
	}
	if err := e.ad.PutSetting(e.ctx, PolicySettingKey, `{"enabled":false,"hour_utc":0}`); err != nil {
		t.Fatal(err)
	}
	now = now.Add(24 * time.Hour)
	e.svc.ScheduledTick(e.ctx)
	if n := count(); n != 4 {
		t.Fatalf("disabled policy still backed up: %d", n)
	}
}

func TestPostgresBackupRestore(t *testing.T) {
	if !wstest.Postgres() {
		t.Skip("PostgreSQL not enabled")
	}
	ctx := context.Background()
	e := wstest.New(t)
	ad := admintest.Open(t)
	owner := ids.New()
	if err := ad.CreateUser(ctx, admin.User{ID: owner, Email: "o@example.com", DisplayName: "O", CreatedAt: time.Now().UTC()}); err != nil {
		t.Fatal(err)
	}

	ws := e.Create(t)
	if err := ad.CreateWorkspace(ctx, admin.Workspace{ID: ws, Name: "Main", CreatedAt: time.Now().UTC(), CreatedBy: owner}, owner); err != nil {
		t.Fatal(err)
	}

	dir := t.TempDir()
	svc := &Service{
		DataDir:    dir,
		BackupDir:  filepath.Join(dir, "backups"),
		Admin:      ad,
		Workspaces: e.Manager,
		Key:        []byte(strings.Repeat("k", 32)),
		AppVersion: "test",
	}

	// Seed data
	l, err := e.Acquire(ctx, ws)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		// Ensure lease is released if test exits early
		if l != nil {
			l.Release()
		}
	}()
	if _, err := (&catalog.Engine{}).Run(ctx, l, []catalog.Op{{Kind: catalog.OpCreateTable, Table: &catalog.TableSpec{
		DisplayName: "notes",
		Columns:     []catalog.ColumnSpec{{DisplayName: "body", Type: catalog.ShortText}},
	}}}, catalog.Apply, catalog.ApplyOptions{}); err != nil {
		t.Fatal(err)
	}
	if err := l.Write(ctx, workspace.Interactive, func(tx dbx.Tx) error {
		if _, err := tx.Exec(ctx, `INSERT INTO notes (body) VALUES (?)`, "hello postgres"); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `CREATE TABLE ledger (id INTEGER GENERATED ALWAYS AS IDENTITY PRIMARY KEY, amount INTEGER NOT NULL, note TEXT)`); err != nil {
			return err
		}
		for i := range 20 {
			if _, err := tx.Exec(ctx, `INSERT INTO ledger (amount, note) VALUES (?, ?)`, i*10, fmt.Sprintf("row %d", i)); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	// 1. Snapshot
	b, err := svc.Snapshot(ctx, l, admin.BackupManual, owner)
	if err != nil {
		t.Fatal(err)
	}
	l.Release()
	l = nil

	// 2. Verify
	man, err := svc.Verify(ctx, b)
	if err != nil {
		t.Fatal(err)
	}
	if man.SourceDialect != "postgres" {
		t.Fatalf("expected source_dialect postgres, got %q", man.SourceDialect)
	}

	// 3. RestoreAsNew
	id, err := svc.RestoreAsNew(ctx, b.ID, func(id ids.WorkspaceID) error {
		return ad.CreateWorkspace(ctx, admin.Workspace{ID: id, Name: "Copy", CreatedAt: time.Now().UTC(), CreatedBy: owner}, owner)
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		n := e.Names(id)
		e.PG.DropRoles(n.ReadLogin(), n.WriteLogin(), n.WriteRole(), n.ReadRole())
	})

	// Verify data in copied workspace
	lCopy, err := e.Acquire(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	var noteCount, ledgerCount int64
	if err := lCopy.Read(ctx, func(q dbx.Querier) error {
		if err := q.QueryRow(ctx, `SELECT count(*) FROM notes`).Scan(&noteCount); err != nil {
			return err
		}
		return q.QueryRow(ctx, `SELECT count(*) FROM ledger`).Scan(&ledgerCount)
	}); err != nil {
		t.Fatal(err)
	}
	lCopy.Release()
	if noteCount != 1 || ledgerCount != 20 {
		t.Fatalf("restored data mismatch: notes=%d ledger=%d", noteCount, ledgerCount)
	}

	// 4. In-place Restore: mutate original, restore, verify reverted
	lMain, err := e.Acquire(ctx, ws)
	if err != nil {
		t.Fatal(err)
	}
	if err := lMain.Write(ctx, workspace.Interactive, func(tx dbx.Tx) error {
		if _, err := tx.Exec(ctx, `INSERT INTO notes (body) VALUES (?)`, "extra note"); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `DROP TABLE ledger`)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	lMain.Release()

	if err := svc.Restore(ctx, ws, b.ID, owner); err != nil {
		t.Fatal(err)
	}
	svc.Wait()

	// Verify original workspace restored
	lRestored, err := e.Acquire(ctx, ws)
	if err != nil {
		t.Fatal(err)
	}
	if err := lRestored.Read(ctx, func(q dbx.Querier) error {
		if err := q.QueryRow(ctx, `SELECT count(*) FROM notes`).Scan(&noteCount); err != nil {
			return err
		}
		return q.QueryRow(ctx, `SELECT count(*) FROM ledger`).Scan(&ledgerCount)
	}); err != nil {
		t.Fatal(err)
	}
	lRestored.Release()
	if noteCount != 1 || ledgerCount != 20 {
		t.Fatalf("after restore: notes=%d ledger=%d", noteCount, ledgerCount)
	}

	// Verify pre_restore backup created
	all, err := ad.ListBackups(ctx, ws)
	if err != nil {
		t.Fatal(err)
	}
	preRestoreFound := false
	for _, x := range all {
		if x.Kind == admin.BackupPreRestore {
			preRestoreFound = true
			break
		}
	}
	if !preRestoreFound {
		t.Fatal("expected pre_restore backup after in-place restore")
	}
}

func TestCrossDialectBackupRestore(t *testing.T) {
	if !wstest.Postgres() {
		t.Skip("PostgreSQL not enabled")
	}
	ctx := context.Background()
	ePG := wstest.New(t)
	ad := admintest.Open(t)
	owner := ids.New()
	if err := ad.CreateUser(ctx, admin.User{ID: owner, Email: "o@example.com", DisplayName: "O", CreatedAt: time.Now().UTC()}); err != nil {
		t.Fatal(err)
	}

	// 1. Create a SQLite workspace manager
	sqliteDir := t.TempDir()
	wmSQLite, err := workspace.NewManager(workspace.Config{
		DataDir: sqliteDir,
		Migrate: catalog.Migrate,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = wmSQLite.Shutdown(ctx) })

	wsSQLite := ids.NewWorkspaceID()
	if err := wmSQLite.Create(ctx, wsSQLite); err != nil {
		t.Fatal(err)
	}
	if err := ad.CreateWorkspace(ctx, admin.Workspace{ID: wsSQLite, Name: "SQLite WS", CreatedAt: time.Now().UTC(), CreatedBy: owner}, owner); err != nil {
		t.Fatal(err)
	}

	lLite, err := wmSQLite.Acquire(ctx, wsSQLite)
	if err != nil {
		t.Fatal(err)
	}
	if err := lLite.Write(ctx, workspace.Interactive, func(tx dbx.Tx) error {
		if _, err := tx.Exec(ctx, `CREATE TABLE items (id INTEGER PRIMARY KEY, title TEXT NOT NULL)`); err != nil {
			return err
		}
		for i := range 10 {
			if _, err := tx.Exec(ctx, `INSERT INTO items (title) VALUES (?)`, fmt.Sprintf("item %d", i)); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	dir := t.TempDir()
	svc := &Service{
		DataDir:    dir,
		BackupDir:  filepath.Join(dir, "backups"),
		Admin:      ad,
		Workspaces: wmSQLite,
		Key:        []byte(strings.Repeat("k", 32)),
		AppVersion: "test",
	}

	bLite, err := svc.Snapshot(ctx, lLite, admin.BackupManual, owner)
	if err != nil {
		t.Fatal(err)
	}
	lLite.Release()

	// 2. Restore SQLite backup into PostgreSQL workspace
	svc.Workspaces = ePG.Manager
	idPG, err := svc.RestoreAsNew(ctx, bLite.ID, func(id ids.WorkspaceID) error {
		return ad.CreateWorkspace(ctx, admin.Workspace{ID: id, Name: "PG from SQLite", CreatedAt: time.Now().UTC(), CreatedBy: owner}, owner)
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		n := ePG.Names(idPG)
		ePG.PG.DropRoles(n.ReadLogin(), n.WriteLogin(), n.WriteRole(), n.ReadRole())
	})

	lPG, err := ePG.Acquire(ctx, idPG)
	if err != nil {
		t.Fatal(err)
	}
	var count int64
	if err := lPG.Read(ctx, func(q dbx.Querier) error {
		return q.QueryRow(ctx, `SELECT count(*) FROM items`).Scan(&count)
	}); err != nil {
		t.Fatal(err)
	}
	lPG.Release()
	if count != 10 {
		t.Fatalf("expected 10 items in PostgreSQL, got %d", count)
	}

	// 3. Take backup from PostgreSQL workspace and restore as new into SQLite
	lPG, err = ePG.Acquire(ctx, idPG)
	if err != nil {
		t.Fatal(err)
	}
	bPG, err := svc.Snapshot(ctx, lPG, admin.BackupManual, owner)
	if err != nil {
		t.Fatal(err)
	}
	lPG.Release()

	svc.Workspaces = wmSQLite
	idLite, err := svc.RestoreAsNew(ctx, bPG.ID, func(id ids.WorkspaceID) error {
		return ad.CreateWorkspace(ctx, admin.Workspace{ID: id, Name: "SQLite from PG", CreatedAt: time.Now().UTC(), CreatedBy: owner}, owner)
	})
	if err != nil {
		t.Fatal(err)
	}

	lLite2, err := wmSQLite.Acquire(ctx, idLite)
	if err != nil {
		t.Fatal(err)
	}
	var countLite int64
	if err := lLite2.Read(ctx, func(q dbx.Querier) error {
		return q.QueryRow(ctx, `SELECT count(*) FROM items`).Scan(&countLite)
	}); err != nil {
		t.Fatal(err)
	}
	lLite2.Release()
	if countLite != 10 {
		t.Fatalf("expected 10 items in SQLite restored from PG, got %d", countLite)
	}
}

func TestPostgresCrashMatrix(t *testing.T) {
	if !wstest.Postgres() {
		t.Skip("PostgreSQL not enabled")
	}
	ctx := context.Background()
	steps := []string{"prepared", "restoring", "swapped", "finish"}
	for _, step := range steps {
		t.Run(step, func(t *testing.T) {
			e := wstest.New(t)
			ad := admintest.Open(t)
			owner := ids.New()
			_ = ad.CreateUser(ctx, admin.User{ID: owner, Email: "o@example.com", DisplayName: "O", CreatedAt: time.Now().UTC()})
			ws := e.Create(t)
			_ = ad.CreateWorkspace(ctx, admin.Workspace{ID: ws, Name: "Main", CreatedAt: time.Now().UTC(), CreatedBy: owner}, owner)

			dir := t.TempDir()
			svc := &Service{
				DataDir:    dir,
				BackupDir:  filepath.Join(dir, "backups"),
				Admin:      ad,
				Workspaces: e.Manager,
				Key:        []byte(strings.Repeat("k", 32)),
				AppVersion: "test",
			}

			l, err := e.Acquire(ctx, ws)
			if err != nil {
				t.Fatal(err)
			}
			_ = l.Write(ctx, workspace.Interactive, func(tx dbx.Tx) error {
				_, err := tx.Exec(ctx, `CREATE TABLE items (id INTEGER GENERATED ALWAYS AS IDENTITY PRIMARY KEY, val TEXT)`)
				if err != nil {
					return err
				}
				_, err = tx.Exec(ctx, `INSERT INTO items (val) VALUES ('v1')`)
				return err
			})

			b, err := svc.Snapshot(ctx, l, admin.BackupManual, owner)
			if err != nil {
				t.Fatal(err)
			}

			// Diverge live workspace
			_ = l.Write(ctx, workspace.Interactive, func(tx dbx.Tx) error {
				_, err := tx.Exec(ctx, `INSERT INTO items (val) VALUES ('v2')`)
				return err
			})
			l.Release()

			svc.hook = func(s string) error {
				if s == step {
					return errCrash
				}
				return nil
			}

			err = svc.Restore(ctx, ws, b.ID, owner)
			if !errors.Is(err, errCrash) {
				t.Fatalf("expected simulated crash at %s, got: %v", step, err)
			}

			// Recover
			svc.hook = nil
			if err := svc.Recover(ctx); err != nil {
				t.Fatalf("recover failed: %v", err)
			}

			// Check workspace status
			w, err := ad.GetWorkspace(ctx, ws)
			if err != nil {
				t.Fatal(err)
			}
			if w.Status != admin.StatusActive {
				t.Fatalf("expected status active, got %s", w.Status)
			}

			// Check row count:
			// prepared, restoring -> old state ('v1', 'v2' => 2 rows)
			// swapped, finish -> new state (snapshot 'v1' => 1 row)
			lCheck, err := e.Acquire(ctx, ws)
			if err != nil {
				t.Fatal(err)
			}
			var count int64
			_ = lCheck.Read(ctx, func(q dbx.Querier) error {
				return q.QueryRow(ctx, `SELECT count(*) FROM items`).Scan(&count)
			})
			lCheck.Release()

			switch step {
			case "prepared", "restoring":
				if count != 2 {
					t.Fatalf("crash at %s: expected old state (2 rows), got %d", step, count)
				}
			case "swapped", "finish":
				if count != 1 {
					t.Fatalf("crash at %s: expected new state (1 row), got %d", step, count)
				}
			}
		})
	}
}
