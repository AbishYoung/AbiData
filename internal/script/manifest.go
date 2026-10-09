package script

import (
	"fmt"
	"sort"
	"sync"
	"time"

	lua "github.com/yuin/gopher-lua"
)

// Context is the kind of run a script executes in.
type Context string

const (
	UIEvent Context = "ui_event"
	Report  Context = "report"
	Job     Context = "job"
)

// AllContexts lists every context.
var AllContexts = []Context{UIEvent, Report, Job}

// Budget bounds one run (brief <lua_runtime> budgets table).
type Budget struct {
	Wall      time.Duration // total wall time
	TxBlock   time.Duration // max duration of one db.transaction block (0 = not allowed)
	MaxString int           // bytes
	Network   bool          // fetch allowed
	Writes    bool          // db writes allowed (role permitting)
}

// DefaultBudgets are the brief's defaults; workspaces may tighten or widen
// within admin maxima.
var DefaultBudgets = map[Context]Budget{
	UIEvent: {Wall: time.Second, TxBlock: 250 * time.Millisecond, MaxString: 1 << 20, Writes: true},
	Report:  {Wall: 30 * time.Second, MaxString: 1 << 20},
	Job:     {Wall: 300 * time.Second, TxBlock: 2 * time.Second, MaxString: 16 << 20, Network: true, Writes: true},
}

// ParamDoc documents one parameter.
type ParamDoc struct {
	Name string `json:"name"`
	Type string `json:"type"`
	Doc  string `json:"doc"`
	Opt  bool   `json:"optional,omitempty"`
}

// FuncDoc documents one host function. It is the single source of truth for
// docs, editor completions and the runtime registration check (D19).
type FuncDoc struct {
	Name      string     `json:"name"`
	Signature string     `json:"signature"`
	Params    []ParamDoc `json:"params,omitempty"`
	Returns   string     `json:"returns,omitempty"`
	Errors    []string   `json:"errors,omitempty"`
	Doc       string     `json:"doc"`
	Example   string     `json:"example,omitempty"`
	Contexts  []Context  `json:"contexts,omitempty"` // nil = the module's contexts
}

// ValueDoc documents a non-function member (e.g. db.NULL, record.id).
type ValueDoc struct {
	Name string `json:"name"`
	Type string `json:"type"`
	Doc  string `json:"doc"`
}

// ModuleDoc documents a module.
type ModuleDoc struct {
	Name     string     `json:"name"`
	Doc      string     `json:"doc"`
	Contexts []Context  `json:"contexts"`
	Funcs    []FuncDoc  `json:"functions"`
	Values   []ValueDoc `json:"values,omitempty"`
	// Global marks a module whose value is a function or object rather than a
	// table of functions (e.g. fetch, record). Parity is then checked by name only.
	Global bool `json:"global,omitempty"`
}

// ModuleDef registers a host module.
type ModuleDef struct {
	Doc ModuleDoc
	// Build returns the module value for one run. It must create fresh tables
	// per run (nothing a script mutates may persist into another run).
	Build func(r *Run) lua.LValue
}

var (
	regMu   sync.RWMutex
	modules = map[string]ModuleDef{}
)

// Register adds a module; it panics on duplicates (programming error).
func Register(def ModuleDef) {
	regMu.Lock()
	defer regMu.Unlock()
	if _, dup := modules[def.Doc.Name]; dup {
		panic("script: duplicate module " + def.Doc.Name)
	}
	if len(def.Doc.Contexts) == 0 {
		panic("script: module " + def.Doc.Name + " has no contexts")
	}
	modules[def.Doc.Name] = def
}

// Manifest returns every module's documentation, sorted by name.
func Manifest() []ModuleDoc {
	regMu.RLock()
	defer regMu.RUnlock()
	out := make([]ModuleDoc, 0, len(modules))
	for _, m := range modules {
		out = append(out, m.Doc)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func (d ModuleDoc) available(k Context) bool {
	for _, c := range d.Contexts {
		if c == k {
			return true
		}
	}
	return false
}

func (f FuncDoc) available(m ModuleDoc, k Context) bool {
	if len(f.Contexts) == 0 {
		return m.available(k)
	}
	for _, c := range f.Contexts {
		if c == k {
			return true
		}
	}
	return false
}

// hostGlobals builds the module values for run r (only modules and functions
// available in r.Kind). Functions not available in this context are removed.
func hostGlobals(r *Run) map[string]lua.LValue {
	regMu.RLock()
	defs := make([]ModuleDef, 0, len(modules))
	for _, d := range modules {
		defs = append(defs, d)
	}
	regMu.RUnlock()
	out := map[string]lua.LValue{}
	for _, d := range defs {
		if !d.Doc.available(r.Kind) {
			continue
		}
		v := d.Build(r)
		if t, ok := v.(*lua.LTable); ok && !d.Doc.Global {
			for _, f := range d.Doc.Funcs {
				if !f.available(d.Doc, r.Kind) {
					t.RawSetString(f.Name, lua.LNil)
				}
			}
		}
		out[d.Doc.Name] = v
	}
	return out
}

// CheckParity verifies that every registered module's built value exposes
// exactly the documented functions (the manifest ↔ registry test).
func CheckParity(r *Run) []string {
	var problems []string
	regMu.RLock()
	defer regMu.RUnlock()
	for name, d := range modules {
		if d.Doc.Global {
			continue
		}
		v := d.Build(r)
		t, ok := v.(*lua.LTable)
		if !ok {
			problems = append(problems, fmt.Sprintf("%s: Build did not return a table", name))
			continue
		}
		documented := map[string]bool{}
		for _, f := range d.Doc.Funcs {
			documented[f.Name] = true
		}
		for _, val := range d.Doc.Values {
			documented[val.Name] = true
		}
		seen := map[string]bool{}
		t.ForEach(func(k, val lua.LValue) {
			key := k.String()
			seen[key] = true
			if !documented[key] {
				problems = append(problems, fmt.Sprintf("%s.%s is registered but not in the manifest", name, key))
			}
		})
		for k := range documented {
			if !seen[k] {
				problems = append(problems, fmt.Sprintf("%s.%s is in the manifest but not registered", name, k))
			}
		}
	}
	sort.Strings(problems)
	return problems
}
