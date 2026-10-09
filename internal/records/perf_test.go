package records

import (
	"context"
	"math/rand/v2"
	"slices"
	"testing"
	"time"

	"abidata/internal/catalog"
	"abidata/internal/ids"
	"abidata/internal/sqlitex"
	"abidata/internal/workspace"
)

var raceEnabled = false

// TestPerfMillionRowPageFetch is the Gate 1 perf check: on a 1M-row table, a
// 200-row page sorted by an indexed column has p95 latency < 50 ms.
func TestPerfMillionRowPageFetch(t *testing.T) {
	if testing.Short() || raceEnabled {
		t.Skip("perf test (not meaningful under -short or -race)")
	}
	ctx := context.Background()
	m, err := workspace.NewManager(workspace.Config{DataDir: t.TempDir(), Migrate: catalog.Migrate})
	if err != nil {
		t.Fatal(err)
	}
	id := ids.NewWorkspaceID()
	if err := m.Create(ctx, id); err != nil {
		t.Fatal(err)
	}
	l, err := m.Acquire(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { l.Release(); _ = m.Shutdown(ctx) }()
	e := &catalog.Engine{}
	if _, err := e.Run(ctx, l, []catalog.Op{{Kind: catalog.OpCreateTable, Table: &catalog.TableSpec{DisplayName: "Big", Columns: []catalog.ColumnSpec{
		{DisplayName: "Name", Type: catalog.ShortText}, {DisplayName: "Amount", Type: catalog.Currency}, {DisplayName: "Note", Type: catalog.LongText}}}}},
		catalog.Apply, catalog.ApplyOptions{}); err != nil {
		t.Fatal(err)
	}
	if err := l.Write(ctx, workspace.Background, func(tx *sqlitex.Tx) error {
		_, err := tx.Exec(ctx, `WITH RECURSIVE n(i) AS (SELECT 1 UNION ALL SELECT i+1 FROM n WHERE i < 1000000)
			INSERT INTO Big(Name, Amount, Note) SELECT printf('name-%07d', (i * 7919) % 1000000), i * 10000, 'note ' || i FROM n`)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	cache := catalog.NewCache()
	sch, _ := cache.Get(ctx, l)
	big, _ := sch.TableByPhysical("Big")
	name, _ := big.ColumnByPhysical("Name")
	if _, err := e.Run(ctx, l, []catalog.Op{{Kind: catalog.OpCreateIndex, Index: &catalog.IndexSpec{TableID: big.ID, ColumnIDs: []string{name.ID}}}},
		catalog.Apply, catalog.ApplyOptions{}); err != nil {
		t.Fatal(err)
	}
	svc := &Service{Schemas: cache}
	rng := rand.New(rand.NewPCG(1, 2))
	var lat []time.Duration
	for range 200 {
		off := rng.IntN(1000000 - 200)
		start := time.Now()
		rows, _, err := svc.List(ctx, l, big.ID, ListQuery{Sort: []Sort{{Col: name.ID}}, Offset: off, Limit: 200})
		if err != nil || len(rows) != 200 {
			t.Fatalf("%v %d", err, len(rows))
		}
		lat = append(lat, time.Since(start))
	}
	slices.Sort(lat)
	p50, p95 := lat[len(lat)/2], lat[len(lat)*95/100]
	t.Logf("page fetch over 1M rows: p50=%v p95=%v max=%v", p50, p95, lat[len(lat)-1])
	if p95 > 50*time.Millisecond {
		t.Fatalf("p95 %v exceeds 50 ms", p95)
	}
}
