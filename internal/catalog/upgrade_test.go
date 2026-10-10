package catalog_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"abidata/internal/catalog"
	"abidata/internal/fault"
	"abidata/internal/ids"
	"abidata/internal/records"
	"abidata/internal/sqlitex"
	"abidata/internal/workspace"
)

// Gate 7: a workspace created by the Phase 1 server (testdata/phase1-workspace.db:
// every field type, a unique index, a lookup relationship, history tracking,
// 25 customers and 10 orders, made through the Phase 1 HTTP API) opens on the
// current server: migrations apply, the catalog reconciles without drift, and
// reads, writes, OCC and relationship enforcement all work.
func TestUpgradeFromPhase1Workspace(t *testing.T) {
	ctx := context.Background()
	const wsID = ids.WorkspaceID("01M4KH9PDH55C94NX1TEGX99WD")
	dir := t.TempDir()
	src, err := os.ReadFile(filepath.Join("testdata", "phase1-workspace.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "workspaces"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "workspaces", string(wsID)+".db"), src, 0o600); err != nil { //nolint:gosec // test temp dir, constant id
		t.Fatal(err)
	}
	migrate := func(ctx context.Context, id ids.WorkspaceID, c *sqlitex.Conn) error {
		if err := catalog.Migrate(ctx, id, c); err != nil {
			return err
		}
		s, err := catalog.Load(ctx, c)
		if err != nil {
			return err
		}
		drift, err := catalog.Reconcile(ctx, c, s)
		if err != nil {
			return err
		}
		if len(drift) > 0 {
			return fmt.Errorf("catalog drift after upgrade: %v", drift)
		}
		return nil
	}
	wm, err := workspace.NewManager(workspace.Config{DataDir: dir, Migrate: migrate})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = wm.Shutdown(context.Background()) })
	l, err := wm.Acquire(ctx, wsID)
	if err != nil {
		t.Fatalf("open upgraded workspace: %v", err)
	}
	defer l.Release()

	var userVersion int64
	var integrity string
	if err := l.ReadConn(ctx, func(c *sqlitex.Conn) error {
		if err := c.QueryRow(ctx, `PRAGMA user_version`).Scan(&userVersion); err != nil {
			return err
		}
		return c.QueryRow(ctx, `PRAGMA integrity_check`).Scan(&integrity)
	}); err != nil {
		t.Fatal(err)
	}
	if userVersion != int64(catalog.Version()) || integrity != "ok" {
		t.Fatalf("user_version %d (want %d), integrity %s", userVersion, catalog.Version(), integrity)
	}

	cache := catalog.NewCache()
	sch, err := cache.Get(ctx, l)
	if err != nil {
		t.Fatal(err)
	}
	var cust, orders *catalog.Table
	for i := range sch.Tables {
		switch sch.Tables[i].DisplayName {
		case "Customers":
			cust = &sch.Tables[i]
		case "Orders":
			orders = &sch.Tables[i]
		}
	}
	if cust == nil || orders == nil {
		t.Fatalf("tables missing after upgrade: %+v", sch.Tables)
	}
	colID := func(t0 *catalog.Table, name string) string {
		for _, c := range t0.Columns {
			if c.DisplayName == name {
				return c.ID
			}
		}
		t.Fatalf("column %s missing", name)
		return ""
	}
	rs := &records.Service{Schemas: cache}
	rows, _, err := rs.List(ctx, l, cust.ID, records.ListQuery{Limit: 100})
	if err != nil || len(rows) != 25 {
		t.Fatalf("customers: %d %v", len(rows), err)
	}
	ords, _, err := rs.List(ctx, l, orders.ID, records.ListQuery{Limit: 100})
	if err != nil || len(ords) != 10 {
		t.Fatalf("orders: %d %v", len(ords), err)
	}
	first, err := rs.Get(ctx, l, cust.ID, rows[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if first.Fields[colID(cust, "Credit")] != "1.2500" || first.Fields[colID(cust, "Name")] != "Customer 1 ✓" ||
		first.Fields[colID(cust, "Joined")] != "2026-01-01" || first.Fields[colID(cust, "Active")] != false {
		t.Fatalf("values changed by the upgrade: %#v", first.Fields)
	}

	// Writes, OCC, unique index and relationship enforcement still hold.
	upd, err := rs.Update(ctx, l, cust.ID, first.ID, first.Version, map[string]any{colID(cust, "Visits"): json.Number("99")}, records.Actor{UserID: "u"}, nil)
	if err != nil || upd.Version <= first.Version {
		t.Fatalf("update: %v", err)
	}
	if _, err := rs.Update(ctx, l, cust.ID, first.ID, first.Version, map[string]any{colID(cust, "Visits"): json.Number("1")}, records.Actor{UserID: "u"}, nil); fault.Classify(err).Code != fault.ConcurrentModification {
		t.Fatalf("stale update: %v", err)
	}
	if _, err := rs.Insert(ctx, l, cust.ID, map[string]any{colID(cust, "Name"): "Dup", colID(cust, "Email"): "c2@example.com"}, records.Actor{UserID: "u"}, nil); fault.Classify(err).Code != fault.DuplicateValue {
		t.Fatalf("unique index: %v", err)
	}
	referenced, err := rs.Get(ctx, l, cust.ID, rows[1].ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := rs.Delete(ctx, l, cust.ID, referenced.ID, referenced.Version, records.Actor{UserID: "u"}, nil); fault.Classify(err).Code != fault.LinkedRecords {
		t.Fatalf("delete of a referenced customer: %v", err)
	}
	if _, err := rs.Insert(ctx, l, orders.ID, map[string]any{colID(orders, "Customer"): json.Number(fmt.Sprint(first.ID)), colID(orders, "Total"): "5.00"}, records.Actor{UserID: "u"}, nil); err != nil {
		t.Fatalf("insert order: %v", err)
	}
}
