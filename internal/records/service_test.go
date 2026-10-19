package records

import (
	"context"
	"encoding/json"
	"sync"
	"sync/atomic"
	"testing"

	"abidata/internal/catalog"
	"abidata/internal/dbx"
	"abidata/internal/fault"
	"abidata/internal/workspace"
	"abidata/internal/workspace/wstest"
)

type capture struct {
	mu  sync.Mutex
	evs []map[string]any
}

func (c *capture) Publish(_, typ string, data any) {
	c.mu.Lock()
	defer c.mu.Unlock()
	b, _ := json.Marshal(data)
	var m map[string]any
	_ = json.Unmarshal(b, &m)
	m["type"] = typ
	c.evs = append(c.evs, m)
}

type env struct {
	t     *testing.T
	ctx   context.Context
	m     *workspace.Manager
	l     *workspace.Lease
	svc   *Service
	feed  *capture
	cust  *catalog.Table
	order *catalog.Table
}

func newEnv(t *testing.T) *env {
	t.Helper()
	ctx := context.Background()
	w := wstest.New(t)
	m := w.Manager
	id := w.Create(t)
	l, err := m.Acquire(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { l.Release(); _ = m.Shutdown(ctx) })
	e := &catalog.Engine{}
	if _, err := e.Run(ctx, l, []catalog.Op{{Kind: catalog.OpCreateTable, Table: &catalog.TableSpec{DisplayName: "Customers", TrackHistory: true,
		Columns: []catalog.ColumnSpec{
			{DisplayName: "Name", Type: catalog.ShortText, Required: true, Options: catalog.TypeOptions{MaxLength: 20}},
			{DisplayName: "Email", Type: catalog.ShortText},
			{DisplayName: "Credit", Type: catalog.Currency, ValidationSQL: "Credit >= 0", ValidationText: "Credit cannot be negative."},
			{DisplayName: "VIP", Type: catalog.YesNo},
		}}}}, catalog.Apply, catalog.ApplyOptions{}); err != nil {
		t.Fatal(err)
	}
	cache := catalog.NewCache()
	sch, err := cache.Get(ctx, l)
	if err != nil {
		t.Fatal(err)
	}
	cust, _ := sch.TableByPhysical("Customers")
	email, _ := cust.ColumnByPhysical("Email")
	if _, err := e.Run(ctx, l, []catalog.Op{
		{Kind: catalog.OpCreateIndex, Index: &catalog.IndexSpec{TableID: cust.ID, Unique: true, ColumnIDs: []string{email.ID}}},
		{Kind: catalog.OpCreateTable, Table: &catalog.TableSpec{DisplayName: "Orders", Columns: []catalog.ColumnSpec{
			{DisplayName: "Customer", Type: catalog.Lookup, Lookup: &catalog.LookupSpec{ParentTableID: cust.ID}}}}},
	}, catalog.Apply, catalog.ApplyOptions{}); err != nil {
		t.Fatal(err)
	}
	sch, _ = cache.Get(ctx, l)
	cust, _ = sch.TableByPhysical("Customers")
	order, _ := sch.TableByPhysical("Orders")
	feed := &capture{}
	return &env{t: t, ctx: ctx, m: m, l: l, svc: &Service{Schemas: cache, Events: feed}, feed: feed, cust: cust, order: order}
}

func (e *env) col(t *catalog.Table, phys string) string {
	c, ok := t.ColumnByPhysical(phys)
	if !ok {
		e.t.Fatalf("no column %s", phys)
	}
	return c.ID
}

func (e *env) insert(fields map[string]any) Row {
	e.t.Helper()
	r, err := e.svc.Insert(e.ctx, e.l, e.cust.ID, fields, Actor{UserID: "u1"}, nil)
	if err != nil {
		e.t.Fatal(err)
	}
	return r
}

func num(s string) json.Number { return json.Number(s) }

func TestOCCConcurrentUpdates(t *testing.T) {
	e := newEnv(t)
	r := e.insert(map[string]any{e.col(e.cust, "Name"): "Ada"})
	var ok, conflict atomic.Int64
	var wg sync.WaitGroup
	for i := range 10 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			l, err := e.m.Acquire(e.ctx, e.l.ID())
			if err != nil {
				t.Error(err)
				return
			}
			defer l.Release()
			_, err = e.svc.Update(e.ctx, l, e.cust.ID, r.ID, r.Version, map[string]any{e.col(e.cust, "Name"): "Ada " + string(rune('A'+i))}, Actor{}, nil)
			switch {
			case err == nil:
				ok.Add(1)
			case fault.Classify(err).Code == fault.ConcurrentModification:
				if cur, _ := fault.Classify(err).Details["current"].(Row); cur.Version != r.Version+1 {
					t.Errorf("details.current = %+v", fault.Classify(err).Details["current"])
				}
				conflict.Add(1)
			default:
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if ok.Load() != 1 || conflict.Load() != 9 {
		t.Fatalf("ok=%d conflict=%d", ok.Load(), conflict.Load())
	}
	// A raw UPDATE without a version bump still increments _version (safety-net trigger).
	if err := e.l.Write(e.ctx, workspace.Interactive, func(tx dbx.Tx) error {
		_, err := tx.Exec(e.ctx, `UPDATE "Customers" SET "Name" = 'raw' WHERE id = ?`, r.ID)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	got, _ := e.svc.Get(e.ctx, e.l, e.cust.ID, r.ID)
	if got.Version != r.Version+2 {
		t.Fatalf("version = %d, want %d", got.Version, r.Version+2)
	}
	// Missing row → NOT_FOUND, not a conflict.
	if _, err := e.svc.Update(e.ctx, e.l, e.cust.ID, 999, 1, map[string]any{}, Actor{}, nil); fault.Classify(err).Code != fault.NotFound {
		t.Fatalf("missing row: %v", err)
	}
}

func TestIdempotency(t *testing.T) {
	e := newEnv(t)
	idem := &Idempotency{Key: "k-1", UserID: "u1"}
	fields := map[string]any{e.col(e.cust, "Name"): "Bo"}
	r1, err := e.svc.Insert(e.ctx, e.l, e.cust.ID, fields, Actor{UserID: "u1"}, idem)
	if err != nil {
		t.Fatal(err)
	}
	r2, err := e.svc.Insert(e.ctx, e.l, e.cust.ID, fields, Actor{UserID: "u1"}, idem)
	if err != nil || r2.ID != r1.ID || r2.Version != r1.Version {
		t.Fatalf("replay: %+v %v", r2, err)
	}
	n, _, _ := e.svc.Count(e.ctx, e.l, e.cust.ID, nil)
	if n != 1 {
		t.Fatalf("rows = %d, want 1", n)
	}
	b1, _ := json.Marshal(r1)
	b2, _ := json.Marshal(r2)
	if string(b1) != string(b2) {
		t.Fatalf("responses differ: %s vs %s", b1, b2)
	}
	if _, err := e.svc.Insert(e.ctx, e.l, e.cust.ID, map[string]any{e.col(e.cust, "Name"): "Other"}, Actor{UserID: "u1"}, idem); fault.Classify(err).Code != fault.IdempotencyConflict {
		t.Fatalf("different body: %v", err)
	}
	if _, err := e.svc.Insert(e.ctx, e.l, e.cust.ID, fields, Actor{UserID: "u2"}, &Idempotency{Key: "k-1", UserID: "u2"}); fault.Classify(err).Code != fault.IdempotencyConflict {
		t.Fatalf("other user reusing key: %v", err)
	}
	if len(e.feed.evs) != 1 {
		t.Fatalf("replay published again: %d events", len(e.feed.evs))
	}
}

func TestConstraintTranslationAndHistory(t *testing.T) {
	e := newEnv(t)
	name, credit, email := e.col(e.cust, "Name"), e.col(e.cust, "Credit"), e.col(e.cust, "Email")
	_, err := e.svc.Insert(e.ctx, e.l, e.cust.ID, map[string]any{name: "X", credit: "-1"}, Actor{}, nil)
	if fe := fault.Classify(err); fe.Code != fault.ValidationFailed || fe.Details["field"] != credit || fe.DisplayMessage() != "Credit cannot be negative. Nothing was saved." {
		t.Fatalf("validation: %v %+v", err, fe)
	}
	_, err = e.svc.Insert(e.ctx, e.l, e.cust.ID, map[string]any{credit: "1"}, Actor{}, nil)
	if fe := fault.Classify(err); fe.Code != fault.RequiredField || fe.Details["field"] != name {
		t.Fatalf("required: %v", err)
	}
	_, err = e.svc.Insert(e.ctx, e.l, e.cust.ID, map[string]any{name: "this name is far too long for twenty"}, Actor{}, nil)
	if fe := fault.Classify(err); fe.Code != fault.BadRequest {
		t.Fatalf("length checked by codec first: %v", err)
	}
	e.insert(map[string]any{name: "A", email: "a@x.test"})
	_, err = e.svc.Insert(e.ctx, e.l, e.cust.ID, map[string]any{name: "B", email: "a@x.test"}, Actor{}, nil)
	if fe := fault.Classify(err); fe.Code != fault.DuplicateValue || len(fe.Details["fields"].([]string)) != 1 {
		t.Fatalf("duplicate: %v %+v", err, fe.Details)
	}
	r := e.insert(map[string]any{name: "C", credit: "12.5", e.col(e.cust, "VIP"): true})
	if r.Fields[credit] != "12.5000" || r.Fields[e.col(e.cust, "VIP")] != true {
		t.Fatalf("encoding: %+v", r.Fields)
	}
	r2, err := e.svc.Update(e.ctx, e.l, e.cust.ID, r.ID, r.Version, map[string]any{credit: num("3")}, Actor{UserID: "u9"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := e.svc.Delete(e.ctx, e.l, e.cust.ID, r2.ID, r2.Version, Actor{UserID: "u9"}, nil); err != nil {
		t.Fatal(err)
	}
	var ops []string
	_ = e.l.Read(e.ctx, func(q dbx.Querier) error {
		rows, err := q.Query(e.ctx, `SELECT op FROM _sys_row_history WHERE row_id = ? ORDER BY id`, r.ID)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var op string
			_ = rows.Scan(&op)
			ops = append(ops, op)
		}
		return nil
	})
	if len(ops) != 3 || ops[0] != "insert" || ops[1] != "update" || ops[2] != "delete" {
		t.Fatalf("history: %v", ops)
	}
}

func TestListFilterSortLookupDisplayAndBatch(t *testing.T) {
	e := newEnv(t)
	name := e.col(e.cust, "Name")
	for _, n := range []string{"Carol", "alice", "Bob"} {
		e.insert(map[string]any{name: n})
	}
	rows, more, err := e.svc.List(e.ctx, e.l, e.cust.ID, ListQuery{Sort: []Sort{{Col: name}}, Limit: 2})
	if err != nil || !more || len(rows) != 2 || rows[0].Fields[name] != "alice" || rows[1].Fields[name] != "Bob" {
		t.Fatalf("%+v %v %v", rows, more, err)
	}
	rows, _, err = e.svc.List(e.ctx, e.l, e.cust.ID, ListQuery{Filter: &Filter{Col: name, Op: OpContains, Value: "O"}, Limit: 10})
	if err != nil || len(rows) != 2 {
		t.Fatalf("filter: %+v %v", rows, err)
	}
	// Lookup display text comes from the parent's display column.
	cust := e.col(e.order, "Customer")
	o, err := e.svc.Insert(e.ctx, e.l, e.order.ID, map[string]any{cust: num("1")}, Actor{}, nil)
	if err != nil || o.Display[cust] != "Carol" {
		t.Fatalf("display: %+v %v", o, err)
	}
	// Batch is all-or-nothing and reports the failing index.
	_, err = e.svc.Batch(e.ctx, e.l, e.cust.ID, []BatchOp{
		{Op: "insert", Fields: map[string]any{name: "Dan"}},
		{Op: "update", ID: 1, Version: 99, Fields: map[string]any{name: "x"}},
	}, Actor{}, nil)
	if fe := fault.Classify(err); fe.Code != fault.ConcurrentModification || fe.Details["op_index"] != 1 {
		t.Fatalf("batch: %v %+v", err, fe.Details)
	}
	if n, _, _ := e.svc.Count(e.ctx, e.l, e.cust.ID, nil); n != 3 {
		t.Fatalf("batch partially applied: %d rows", n)
	}
	res, err := e.svc.Batch(e.ctx, e.l, e.cust.ID, []BatchOp{{Op: "insert", Fields: map[string]any{name: "Dan"}}, {Op: "delete", ID: 2, Version: 1}}, Actor{}, nil)
	if err != nil || len(res) != 2 || res[0].Row == nil {
		t.Fatalf("%+v %v", res, err)
	}
	last := e.feed.evs[len(e.feed.evs)-1]
	if last["type"] != "rows.changed" || len(last["ids"].([]any)) != 2 {
		t.Fatalf("feed: %v", last)
	}
	// Unknown field IDs and the hidden _version column are rejected.
	if _, err := e.svc.Insert(e.ctx, e.l, e.cust.ID, map[string]any{"_version": num("5")}, Actor{}, nil); fault.Classify(err).Code != fault.BadRequest {
		t.Fatalf("_version settable: %v", err)
	}
}
