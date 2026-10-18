package all_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"abidata/internal/admin"
	"abidata/internal/artifacts"
	"abidata/internal/catalog"
	"abidata/internal/dbx"
	"abidata/internal/demo"
	"abidata/internal/forms"
	"abidata/internal/ids"
	"abidata/internal/jobs"
	"abidata/internal/query"
	"abidata/internal/records"
	"abidata/internal/reports"
	"abidata/internal/script"
	"abidata/internal/script/host"
	_ "abidata/internal/script/modules/all"
	"abidata/internal/scripts"
	"abidata/internal/workspace"
)

// The Lua examples in the user guide must keep working. Every fenced ```lua
// block must compile, every module.function it names must exist in the Lua API
// manifest, and the blocks that need no form, network or staging run against
// the Northwind sample workspace.

var (
	guideLuaBlock = regexp.MustCompile("(?s)```lua\n(.*?)```")
	guideModRef   = regexp.MustCompile(`\b([A-Za-z_][A-Za-z0-9_]*)\.([A-Za-z_][A-Za-z0-9_]*)\b`)
	guideEntryFn  = regexp.MustCompile(`(?m)^function ([A-Za-z_][A-Za-z0-9_]*)\(\)`)
)

// guideNeedsHost lists calls that exist only in form events, reports, or with
// the network or job staging. Such blocks are compiled and name-checked only.
var guideNeedsHost = []string{"ui.", "notify.", "event.", "fetch(", "staging.", "artifacts.", "require(", "jobs."}

// guideRecord stands in for the form's record, which exists only in form events.
// It supports the field access and record.get/record.set forms the guide uses.
const guideRecord = `
local __row = {id = 1, Customer = 1, Quantity = 3, Status = "Open", Notes = ""}
record = setmetatable({}, {
  __index = function(_, k)
    if k == "get" then return function(f) return __row[f] end end
    if k == "set" then return function(f, v) __row[f] = v end end
    return __row[k]
  end,
  __newindex = function(_, k, v) __row[k] = v end,
})
`

func TestGuideLuaExamples(t *testing.T) {
	apiFuncs := map[string]map[string]bool{}
	for _, m := range script.Manifest() {
		names := map[string]bool{}
		for _, f := range m.Funcs {
			names[f.Name] = true
		}
		for _, v := range m.Values {
			names[v.Name] = true
		}
		apiFuncs[m.Name] = names
	}
	n := newNorthwind(t)

	dir := filepath.Join("..", "..", "..", "..", "docs", "content", "guide")
	blocks := 0
	for _, page := range []string{"scripts.md", "forms.md", "jobs.md", "reports.md"} {
		src, err := os.ReadFile(filepath.Join(dir, page))
		if err != nil {
			t.Fatal(err)
		}
		for i, m := range guideLuaBlock.FindAllStringSubmatch(string(src), -1) {
			blocks++
			code := m[1]
			name := fmt.Sprintf("%s#%d", page, i+1)

			// Every module.name the block uses must exist in the Lua API.
			// Names the block defines itself (local M, function M.f) are not API.
			for _, ref := range guideModRef.FindAllStringSubmatch(code, -1) {
				mod, member := ref[1], ref[2]
				if mod == "record" { // record fields are the form's fields, not API
					continue
				}
				members, isAPI := apiFuncs[mod]
				if !isAPI || strings.Contains(code, "local "+mod+" ") || strings.Contains(code, "function "+mod+".") {
					continue
				}
				if !members[member] {
					t.Errorf("%s: %s.%s is not in the Lua API", name, mod, member)
				}
			}

			// A block is a chunk, or a bare calculated-control expression (the
			// server checks those as "return (expr)").
			if _, err := script.Compile(code, name); err != nil {
				if _, exprErr := script.Compile("return ("+strings.TrimSpace(code)+")", name); exprErr != nil {
					t.Errorf("%s does not compile: %v", name, err)
				}
				continue
			}
			if guideBlockNeedsHost(code) {
				continue
			}

			// Run the block's top-level code, then each no-argument handler it defines.
			entries := append([]string{""}, guideEntryNames(code)...)
			for _, entry := range entries {
				res, err := n.run(t, guideRecord+code, name, entry)
				if err != nil {
					t.Errorf("%s (entry %q): %v", name, entry, err)
				}
				// The order-total example must return the order's real total. db.query
				// gives whole ten-thousandths, so the total is the SQL sum over 10000.
				if entry == "" && strings.Contains(code, "FROM Order_lines WHERE Sales_order") {
					raw := n.count(t, "SELECT COALESCE(SUM(Quantity*Unit_price),0) FROM Order_lines WHERE Sales_order = 1")
					want := strconv.FormatFloat(float64(raw)/10000, 'f', -1, 64)
					if len(res.Values) != 1 || fmt.Sprint(res.Values[0]) != want {
						t.Errorf("%s returned %#v, want the order total %s", name, res.Values, want)
					}
				}
			}
		}
	}
	if blocks == 0 {
		t.Fatalf("no lua blocks found under %s", dir)
	}
}

func guideBlockNeedsHost(code string) bool {
	for _, marker := range guideNeedsHost {
		if strings.Contains(code, marker) {
			return true
		}
	}
	return false
}

func guideEntryNames(code string) []string {
	var out []string
	for _, m := range guideEntryFn.FindAllStringSubmatch(code, -1) {
		out = append(out, m[1])
	}
	return out
}

// northwindRig is the Northwind sample workspace, built the way the server builds it.
type northwindRig struct {
	ctx context.Context
	ws  ids.WorkspaceID
	l   *workspace.Lease
	hd  *host.Data
	rn  *script.Runner
}

func newNorthwind(t *testing.T) *northwindRig {
	t.Helper()
	ctx := context.Background()
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "system"), 0o700); err != nil {
		t.Fatal(err)
	}
	adm, err := admin.Open(ctx, filepath.Join(dir, "system", "admin.db"), "")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = adm.Close() })
	owner := ids.New()
	if err := adm.CreateUser(ctx, admin.User{ID: owner, Email: "owner@example.com", DisplayName: "Owner", IsPlatformAdmin: true, CreatedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	wm, err := workspace.NewManager(workspace.Config{DataDir: dir, Migrate: catalog.Migrate})
	if err != nil {
		t.Fatal(err)
	}
	schemas := catalog.NewCache()
	engine := &catalog.Engine{Validators: []catalog.Validator{query.SchemaValidator, forms.SchemaValidator, reports.SchemaValidator},
		AfterCommit: func(_ context.Context, l *workspace.Lease, s *catalog.Schema) { schemas.Put(l.ID(), s) }}
	rec := &records.Service{Schemas: schemas}
	qs := &query.Service{Schemas: schemas}
	ss := &scripts.Service{}
	rn := script.NewRunner(4, 2)
	arts := &artifacts.Store{Dir: filepath.Join(dir, "artifacts")}
	store := &jobs.SQLStore{Admin: adm, Workspaces: wm}
	exec := &jobs.LuaExecutor{Workspaces: wm, Scripts: ss, Runner: rn, Schemas: schemas, Records: rec, Artifacts: arts}
	sched := jobs.New(jobs.Config{Store: store, Executor: exec, Workers: 2})
	t.Cleanup(func() {
		sched.Wait()
		_ = wm.Shutdown(ctx)
	})
	b := &demo.Builder{
		Admin: adm, Workspaces: wm, Schemas: schemas, Catalog: engine, Records: rec, Queries: qs, Scripts: ss,
		Forms:   &forms.Service{Schemas: schemas, Records: rec, Queries: qs, Scripts: ss, Runner: rn},
		Reports: &reports.Service{Schemas: schemas, Records: rec, Queries: qs, Scripts: ss, Runner: rn},
		Jobs:    &jobs.Service{Scripts: ss, Scheduler: sched},
	}
	res, err := b.Create(ctx, owner)
	if err != nil {
		t.Fatal(err)
	}
	l, err := wm.Acquire(ctx, res.WorkspaceID)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { l.Release() })
	return &northwindRig{ctx: ctx, ws: res.WorkspaceID, l: l, rn: rn,
		hd: &host.Data{Lease: l, Schemas: schemas, Records: rec, CanWrite: true, UserID: owner}}
}

// run compiles code, runs it as a job against Northwind, and calls entry if set.
func (n *northwindRig) run(t *testing.T, code, name, entry string) (script.ExecResult, error) {
	t.Helper()
	p, err := script.Compile(code, name)
	if err != nil {
		return script.ExecResult{}, err
	}
	return n.rn.Exec(n.ctx, script.ExecRequest{Kind: script.Job, Workspace: n.ws, Proto: p, Entry: entry, Host: n.hd})
}

func (n *northwindRig) count(t *testing.T, sql string) int {
	t.Helper()
	var c int
	if err := n.l.Read(n.ctx, func(q dbx.Querier) error { return q.QueryRow(n.ctx, sql).Scan(&c) }); err != nil {
		t.Fatalf("%s: %v", sql, err)
	}
	return c
}
