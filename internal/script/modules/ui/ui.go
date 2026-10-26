// Package ui registers the form-event modules: event, record, ui, notify, and
// the workspace module available in every context.
package ui

import (
	"net/url"
	"regexp"
	"sort"
	"strings"

	lua "github.com/yuin/gopher-lua"

	"abidata/internal/catalog"
	"abidata/internal/script"
	"abidata/internal/script/host"
	recmod "abidata/internal/script/modules/records"
)

var uiOnly = []script.Context{script.UIEvent}

func init() {
	script.Register(script.ModuleDef{Doc: eventDoc, Build: buildEvent})
	script.Register(script.ModuleDef{Doc: recordDoc, Build: buildRecord})
	script.Register(script.ModuleDef{Doc: uiDoc, Build: buildUI})
	script.Register(script.ModuleDef{Doc: notifyDoc, Build: buildNotify})
	script.Register(script.ModuleDef{Doc: workspaceDoc, Build: buildWorkspace})
}

func session(r *script.Run, L *lua.LState) *host.UISession {
	d, ok := r.Host.(*host.Data)
	if !ok || d == nil || d.UI == nil {
		L.RaiseError("this function is only available in form events")
	}
	return d.UI
}

func sessionOrNil(r *script.Run) *host.UISession {
	if d, ok := r.Host.(*host.Data); ok && d != nil {
		return d.UI
	}
	return nil
}

// ---- event -------------------------------------------------------------------

var eventDoc = script.ModuleDoc{
	Name: "event", Contexts: uiOnly, Global: true,
	Doc: "The form event being handled.",
	Values: []script.ValueDoc{
		{Name: "name", Type: "string", Doc: "Event name: load, current, before_insert, before_update, after_update, before_delete, after_delete, close, click, change, confirm."},
		{Name: "control", Type: "string|nil", Doc: "Name of the control that raised a control event."},
		{Name: "old_value", Type: "any", Doc: "Previous value for change/before_update control events."},
		{Name: "new_value", Type: "any", Doc: "New value for change/before_update control events."},
		{Name: "args", Type: "table", Doc: "Arguments the form was opened with (open_args)."},
		{Name: "answer", Type: "boolean|nil", Doc: "The user's answer when re-invoked after ui.confirm."},
	},
}

func buildEvent(r *script.Run) lua.LValue {
	t := r.L.NewTable()
	s := sessionOrNil(r)
	if s == nil {
		return t
	}
	t.RawSetString("name", lua.LString(s.Event))
	if s.Control != "" {
		t.RawSetString("control", lua.LString(s.Control))
	}
	t.RawSetString("old_value", valueOrNull(r, s.OldValue))
	t.RawSetString("new_value", valueOrNull(r, s.NewValue))
	args := map[string]any{}
	for k, v := range s.Args {
		args[k] = v
	}
	t.RawSetString("args", script.ToLua(r, args))
	if s.Answer != nil {
		t.RawSetString("answer", lua.LBool(*s.Answer))
	}
	return t
}

func valueOrNull(r *script.Run, v any) lua.LValue {
	if v == nil {
		return r.Sentinel(script.NullName)
	}
	return script.ToLua(r, v)
}

// ---- record ------------------------------------------------------------------

var recordDoc = script.ModuleDoc{
	Name: "record", Contexts: uiOnly, Global: true,
	Doc: "The form's current record buffer. Read and assign fields by name (record.Qty = 5); assignments are sent back to the form and saved with the record.",
	Funcs: []script.FuncDoc{
		{Name: "get", Signature: "record.get(field) → value", Doc: "Reads a field (db.NULL when empty).", Example: `local q = record.get("Qty")`},
		{Name: "set", Signature: "record.set(field, value)", Doc: "Assigns a field.", Example: `record.set("Status", "Approved")`},
		{Name: "changed_fields", Signature: "record.changed_fields() → fields", Doc: "Returns a list of fields assigned during this event.", Example: `local fields = record.changed_fields()`},
		{Name: "reset", Signature: "record.reset(field?)", Doc: "Discards changes to a field or the entire record buffer.", Example: `record.reset("Status")`},
		{Name: "to_table", Signature: "record.to_table() → table", Doc: "Returns a snapshot of all current record values as a plain Lua table.", Example: `local snapshot = record.to_table()`},
	},
	Values: []script.ValueDoc{
		{Name: "id", Type: "number|nil", Doc: "Row id (nil for a new record)."},
		{Name: "version", Type: "number|nil", Doc: "Row _version the form loaded."},
		{Name: "is_new", Type: "boolean", Doc: "True for a record not yet saved."},
		{Name: "is_dirty", Type: "boolean", Doc: "True when the user changed fields."},
	},
}

func buildRecord(r *script.Run) lua.LValue {
	L := r.L
	proxy := L.NewTable()
	mt := L.NewTable()
	mt.RawSetString("__metatable", lua.LString("locked"))
	read := func(L *lua.LState, name string) lua.LValue {
		s := session(r, L)
		switch name {
		case "id":
			if s.IsNew || s.RecordID == 0 {
				return lua.LNil
			}
			return lua.LNumber(s.RecordID)
		case "version":
			if s.IsNew {
				return lua.LNil
			}
			return lua.LNumber(s.RecordVersion)
		case "is_new":
			return lua.LBool(s.IsNew)
		case "is_dirty":
			return lua.LBool(len(s.Dirty) > 0 || len(s.Changed) > 0)
		}
		if s.Table == nil {
			L.RaiseError("this form has no record source")
		}
		col, ok := s.Table.ColumnByPhysical(name)
		if !ok {
			L.RaiseError("the record has no field %q", name)
		}
		return recmod.ValueToLua(r, *col, s.Record[col.PhysicalName])
	}
	write := func(L *lua.LState, name string, v lua.LValue) {
		s := session(r, L)
		switch name {
		case "id", "version", "is_new", "is_dirty", "_version":
			L.RaiseError("record.%s cannot be assigned", name)
		}
		if s.Table == nil {
			L.RaiseError("this form has no record source")
		}
		col, ok := s.Table.ColumnByPhysical(name)
		if !ok {
			L.RaiseError("the record has no field %q", name)
		}
		g := script.FromLua(r, v)
		if _, isNull := g.(script.Null); isNull {
			g = nil
		}
		s.SetField(col.PhysicalName, g)
	}
	getFn := L.NewFunction(func(L *lua.LState) int { L.Push(read(L, L.CheckString(1))); return 1 })
	setFn := L.NewFunction(func(L *lua.LState) int { write(L, L.CheckString(1), L.Get(2)); return 0 })
	changedFieldsFn := L.NewFunction(func(L *lua.LState) int {
		s := session(r, L)
		out := L.NewTable()
		var keys []string
		for k := range s.Changed {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			out.Append(lua.LString(k))
		}
		L.Push(out)
		return 1
	})
	resetFn := L.NewFunction(func(L *lua.LState) int {
		s := session(r, L)
		if L.GetTop() >= 1 && L.Get(1) != lua.LNil {
			fieldName := L.CheckString(1)
			delete(s.Changed, fieldName)
			delete(s.Dirty, fieldName)
		} else {
			s.Changed = map[string]any{}
			s.Dirty = map[string]bool{}
		}
		return 0
	})
	toTableFn := L.NewFunction(func(L *lua.LState) int {
		s := session(r, L)
		out := L.NewTable()
		if s.Table != nil {
			for _, c := range s.Table.Columns {
				if c.Type == catalog.Autonumber {
					continue
				}
				out.RawSetString(c.PhysicalName, recmod.ValueToLua(r, c, s.Record[c.PhysicalName]))
			}
		}
		L.Push(out)
		return 1
	})
	mt.RawSetString("__index", L.NewFunction(func(L *lua.LState) int {
		key := L.CheckString(2)
		switch key {
		case "get":
			L.Push(getFn)
		case "set":
			L.Push(setFn)
		case "changed_fields":
			L.Push(changedFieldsFn)
		case "reset":
			L.Push(resetFn)
		case "to_table":
			L.Push(toTableFn)
		default:
			L.Push(read(L, key))
		}
		return 1
	}))
	mt.RawSetString("__newindex", L.NewFunction(func(L *lua.LState) int {
		write(L, L.CheckString(2), L.Get(3))
		return 0
	}))
	proxy.Metatable = mt
	return proxy
}

// ---- ui ----------------------------------------------------------------------

// settableProps are the control properties handlers may change.
var settableProps = map[string]string{
	"visible": "boolean", "enabled": "boolean", "locked": "boolean", "caption": "string",
	"required": "boolean", "tooltip": "string", "value": "any", "tone": "string",
	"error": "string",
}

var uiDoc = script.ModuleDoc{
	Name: "ui", Contexts: uiOnly,
	Doc: "Change the form: control properties, focus, navigation and confirmations.",
	Funcs: []script.FuncDoc{
		{Name: "control", Signature: "ui.control(name) → props", Doc: "A snapshot of a control's properties (visible, enabled, locked, caption, value…).", Example: `if ui.control("Approve").visible then … end`},
		{Name: "set", Signature: "ui.set(name, props)", Doc: "Changes control properties: visible, enabled, locked, required (booleans), caption, tooltip, error (text), tone (\"default\"|\"warning\"|\"danger\"|\"success\"), value (unbound controls).", Example: `ui.set("Approve", {visible = record.Total > 5000})`},
		{Name: "focus", Signature: "ui.focus(name)", Doc: "Moves focus to a control.", Example: `ui.focus("Qty")`},
		{Name: "goto_record", Signature: `ui.goto_record(target)`, Doc: `Moves to a record: an id, or "next", "prev", "first", "last", "new".`, Example: `ui.goto_record("new")`},
		{Name: "open_form", Signature: "ui.open_form(name, {where?, args?})", Doc: "Opens another form, optionally filtered with a filter-model table and passing args.", Example: `ui.open_form("Customer", {args = {id = record.Customer}})`},
		{Name: "open_report", Signature: "ui.open_report(name, {params?})", Doc: "Opens a report.", Example: `ui.open_report("Invoice", {params = {order_id = record.id}})`},
		{Name: "open_url", Signature: "ui.open_url(url, target?)", Doc: "Opens a URL in the browser: an http, https or mailto link, or a path on this server (/...). target is a window name such as _blank (default) or _self.", Example: `ui.open_url("https://example.com/docs", "_blank")`},
		{Name: "close", Signature: "ui.close()", Doc: "Closes the form.", Example: `ui.close()`},
		{Name: "requery", Signature: "ui.requery(control?)", Doc: "Reloads the form's records, or one subform/combo box.", Example: `ui.requery("Lines")`},
		{Name: "refresh", Signature: "ui.refresh()", Doc: "Alias for ui.requery(); reloads the form's record source.", Example: `ui.refresh()`},
		{Name: "cancel", Signature: "ui.cancel(message)", Doc: "Cancels the pending action (save, insert, delete) in before_* events and shows message.", Example: `ui.cancel("Orders over 5,000 need an approver.")`},
		{Name: "confirm", Signature: "ui.confirm(message, continuation)", Doc: "Asks the user to confirm. When answered, the form calls the handler function named continuation with event.answer set.", Example: `ui.confirm("Delete all lines?", "on_delete_lines")`},
		{Name: "set_error", Signature: "ui.set_error(control, message)", Doc: "Sets an inline validation error message on a control.", Example: `ui.set_error("Email", "Invalid email address format.")`},
		{Name: "clear_error", Signature: "ui.clear_error(control)", Doc: "Clears any inline validation error on a control.", Example: `ui.clear_error("Email")`},
		{Name: "clear_errors", Signature: "ui.clear_errors()", Doc: "Clears inline validation errors from all controls on the form.", Example: `ui.clear_errors()`},
		{Name: "set_visible", Signature: "ui.set_visible(control, visible)", Doc: "Shows or hides a control.", Example: `ui.set_visible("ShippingAddress", record.NeedsDelivery)`},
		{Name: "set_enabled", Signature: "ui.set_enabled(control, enabled)", Doc: "Enables or disables a control.", Example: `ui.set_enabled("SubmitBtn", record.is_dirty)`},
		{Name: "set_read_only", Signature: "ui.set_read_only(control, read_only)", Doc: "Locks or unlocks a control for editing.", Example: `ui.set_read_only("UnitPrice", not user.is_admin)`},
		{Name: "set_locked", Signature: "ui.set_locked(control, locked)", Doc: "Locks or unlocks a control (alias for set_read_only).", Example: `ui.set_locked("Qty", true)`},
		{Name: "set_required", Signature: "ui.set_required(control, required)", Doc: "Marks a control as required or optional.", Example: `ui.set_required("Reason", record.Status == "Rejected")`},
		{Name: "set_label", Signature: "ui.set_label(control, text)", Doc: "Sets the caption or label text of a control.", Example: `ui.set_label("SaveBtn", "Update Record")`},
		{Name: "set_caption", Signature: "ui.set_caption(control, text)", Doc: "Sets the caption text of a control (alias for set_label).", Example: `ui.set_caption("Title", "Edit Mode")`},
		{Name: "set_value", Signature: "ui.set_value(control, value)", Doc: "Sets the value of an unbound control.", Example: `ui.set_value("SearchBox", "")`},
	},
}

func buildUI(r *script.Run) lua.LValue {
	L := r.L
	t := L.NewTable()
	fn := func(name string, f lua.LGFunction) { t.RawSetString(name, L.NewFunction(f)) }
	controlName := func(L *lua.LState, s *host.UISession) string {
		name := L.CheckString(1)
		if _, ok := s.Controls[name]; !ok {
			L.RaiseError("the form has no control named %q", name)
		}
		return name
	}
	fn("control", func(L *lua.LState) int {
		s := session(r, L)
		L.Push(script.ToLua(r, s.Controls[controlName(L, s)]))
		return 1
	})
	fn("set", func(L *lua.LState) int {
		s := session(r, L)
		name := controlName(L, s)
		props := map[string]any{}
		L.CheckTable(2).ForEach(func(k, v lua.LValue) {
			key := k.String()
			want, ok := settableProps[key]
			if !ok {
				L.RaiseError("property %q cannot be set (allowed: visible, enabled, locked, required, caption, tooltip, tone, value)", key)
			}
			g := script.FromLua(r, v)
			switch want {
			case "boolean":
				b, ok := g.(bool)
				if !ok {
					L.RaiseError("property %q must be true or false", key)
				}
				props[key] = b
			case "string":
				sv, ok := g.(string)
				if !ok {
					L.RaiseError("property %q must be text", key)
				}
				if key == "tone" && sv != "default" && sv != "warning" && sv != "danger" && sv != "success" {
					L.RaiseError("tone must be default, warning, danger or success")
				}
				if len(sv) > 1000 {
					sv = sv[:1000]
				}
				props[key] = sv
			default:
				if _, isNull := g.(script.Null); isNull {
					g = nil
				}
				props[key] = g
			}
		})
		s.Patch(name, props)
		return 0
	})
	fn("focus", func(L *lua.LState) int { s := session(r, L); s.Focus = controlName(L, s); return 0 })
	fn("goto_record", func(L *lua.LState) int {
		s := session(r, L)
		switch v := L.Get(1).(type) {
		case lua.LNumber:
			s.Navigate = map[string]any{"record": int64(v)}
		case lua.LString:
			switch string(v) {
			case "next", "prev", "first", "last", "new":
				s.Navigate = map[string]any{"record": string(v)}
			default:
				L.ArgError(1, `expected an id or "next", "prev", "first", "last", "new"`)
			}
		default:
			L.ArgError(1, "expected an id or a direction")
		}
		return 0
	})
	open := func(kind, paramKey string) lua.LGFunction {
		return func(L *lua.LState) int {
			s := session(r, L)
			nav := map[string]any{kind: L.CheckString(1)}
			if opts, ok := L.Get(2).(*lua.LTable); ok {
				for _, k := range []string{"where", "args", paramKey} {
					if v := opts.RawGetString(k); v != lua.LNil {
						nav[k] = script.FromLua(r, v)
					}
				}
			}
			s.Navigate = nav
			return 0
		}
	}
	fn("open_form", open("form", "args"))
	fn("open_report", open("report", "params"))
	fn("close", func(L *lua.LState) int { session(r, L).Close = true; return 0 })
	fn("requery", func(L *lua.LState) int {
		s := session(r, L)
		target := "*"
		if L.GetTop() >= 1 {
			target = controlName(L, s)
		}
		s.Requery = append(s.Requery, target)
		return 0
	})
	fn("cancel", func(L *lua.LState) int {
		s := session(r, L)
		msg := L.OptString(1, "The action was cancelled.")
		s.Cancel = &msg
		return 0
	})
	fn("confirm", func(L *lua.LState) int {
		s := session(r, L)
		msg := L.CheckString(1)
		cont := L.CheckString(2)
		if !validIdent(cont) {
			L.ArgError(2, "continuation must be the name of a handler function")
		}
		s.Confirm = &host.Confirm{Message: msg, Continuation: cont}
		return 0
	})
	fn("open_url", func(L *lua.LState) int {
		s := session(r, L)
		urlStr := L.CheckString(1)
		target := L.OptString(2, "_blank")
		if !safeOpenURL(urlStr) {
			L.ArgError(1, "url must be an http, https or mailto link, or a path on this server starting with /")
		}
		if !windowNameRe.MatchString(target) {
			L.ArgError(2, "target must be a window name such as _blank or _self")
		}
		s.Navigate = map[string]any{"url": urlStr, "target": target}
		return 0
	})
	fn("refresh", func(L *lua.LState) int {
		s := session(r, L)
		s.Requery = append(s.Requery, "*")
		return 0
	})
	fn("set_error", func(L *lua.LState) int {
		s := session(r, L)
		name := controlName(L, s)
		s.Patch(name, map[string]any{"error": L.CheckString(2)})
		return 0
	})
	fn("clear_error", func(L *lua.LState) int {
		s := session(r, L)
		name := controlName(L, s)
		s.Patch(name, map[string]any{"error": ""})
		return 0
	})
	fn("clear_errors", func(L *lua.LState) int {
		s := session(r, L)
		for name := range s.Controls {
			s.Patch(name, map[string]any{"error": ""})
		}
		return 0
	})
	fn("set_visible", func(L *lua.LState) int {
		s := session(r, L)
		s.Patch(controlName(L, s), map[string]any{"visible": L.CheckBool(2)})
		return 0
	})
	fn("set_enabled", func(L *lua.LState) int {
		s := session(r, L)
		s.Patch(controlName(L, s), map[string]any{"enabled": L.CheckBool(2)})
		return 0
	})
	setLocked := func(L *lua.LState) int {
		s := session(r, L)
		s.Patch(controlName(L, s), map[string]any{"locked": L.CheckBool(2)})
		return 0
	}
	fn("set_read_only", setLocked)
	fn("set_locked", setLocked)
	fn("set_required", func(L *lua.LState) int {
		s := session(r, L)
		s.Patch(controlName(L, s), map[string]any{"required": L.CheckBool(2)})
		return 0
	})
	setCaption := func(L *lua.LState) int {
		s := session(r, L)
		s.Patch(controlName(L, s), map[string]any{"caption": L.CheckString(2)})
		return 0
	}
	fn("set_caption", setCaption)
	fn("set_label", setCaption)
	fn("set_value", func(L *lua.LState) int {
		s := session(r, L)
		v := script.FromLua(r, L.Get(2))
		if _, null := v.(script.Null); null {
			v = nil
		}
		s.Patch(controlName(L, s), map[string]any{"value": v})
		return 0
	})
	return t
}

var identRe = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]{0,99}$`)

func validIdent(s string) bool { return identRe.MatchString(s) }

// windowNameRe is a window name ui.open_url may target.
var windowNameRe = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)

// safeOpenURL reports whether ui.open_url may open u in the user's browser:
// an absolute http, https or mailto URL, or a path on this server. Script
// URLs (javascript:, data:, vbscript:) and protocol-relative links are
// refused, as are control characters and surrounding spaces, which browsers
// strip before reading the scheme.
func safeOpenURL(u string) bool {
	if u == "" || len(u) > 8192 || strings.TrimSpace(u) != u {
		return false
	}
	for i := 0; i < len(u); i++ {
		if u[i] < 0x20 || u[i] == 0x7f {
			return false
		}
	}
	if strings.HasPrefix(u, "/") {
		return !strings.HasPrefix(u, "//") && !strings.HasPrefix(u, `/\`)
	}
	p, err := url.Parse(u)
	if err != nil {
		return false
	}
	switch strings.ToLower(p.Scheme) {
	case "http", "https":
		return p.Host != ""
	case "mailto":
		return p.Opaque != ""
	}
	return false
}

// ---- notify ------------------------------------------------------------------

var notifyDoc = script.ModuleDoc{
	Name: "notify", Contexts: uiOnly,
	Doc: "Show messages to the user.",
	Funcs: []script.FuncDoc{
		{Name: "toast", Signature: "notify.toast(text, {level?})", Doc: `A brief message; level is "info" (default), "success", "warning" or "error".`, Example: `notify.toast("Order saved", {level = "success"})`},
		{Name: "alert", Signature: "notify.alert(text, {title?})", Doc: "A message the user must dismiss.", Example: `notify.alert("Stock is low", {title = "Heads up"})`},
	},
}

func buildNotify(r *script.Run) lua.LValue {
	L := r.L
	t := L.NewTable()
	opt := func(L *lua.LState, key string) string {
		if o, ok := L.Get(2).(*lua.LTable); ok {
			if v, ok := o.RawGetString(key).(lua.LString); ok {
				return string(v)
			}
		}
		return ""
	}
	clip := func(s string) string {
		if len(s) > 2000 {
			return s[:2000] + "…"
		}
		return s
	}
	t.RawSetString("toast", L.NewFunction(func(L *lua.LState) int {
		s := session(r, L)
		level := opt(L, "level")
		switch level {
		case "":
			level = "info"
		case "info", "success", "warning", "error":
		default:
			L.ArgError(2, "level must be info, success, warning or error")
		}
		s.Notify(host.Notification{Kind: "toast", Level: level, Text: clip(L.CheckString(1))})
		return 0
	}))
	t.RawSetString("alert", L.NewFunction(func(L *lua.LState) int {
		s := session(r, L)
		s.Notify(host.Notification{Kind: "alert", Level: "info", Text: clip(L.CheckString(1)), Title: clip(opt(L, "title"))})
		return 0
	}))
	return t
}

// ---- workspace ---------------------------------------------------------------

var workspaceDoc = script.ModuleDoc{
	Name: "workspace", Contexts: []script.Context{script.UIEvent, script.Report, script.Job}, Global: true,
	Doc: "Information about the workspace and the user the script runs as.",
	Values: []script.ValueDoc{
		{Name: "timezone", Type: "string", Doc: "The workspace's IANA time zone."},
		{Name: "user", Type: "table", Doc: "{id, email, display_name, role} of the user (jobs run as the job principal)."},
	},
}

func buildWorkspace(r *script.Run) lua.LValue {
	t := r.L.NewTable()
	t.RawSetString("timezone", lua.LString(r.TZ.String()))
	u := r.L.NewTable()
	u.RawSetString("id", lua.LString(r.Principal.UserID))
	u.RawSetString("email", lua.LString(r.Principal.Email))
	u.RawSetString("display_name", lua.LString(r.Principal.DisplayName))
	u.RawSetString("role", lua.LString(strings.ToLower(r.Principal.Role)))
	t.RawSetString("user", u)
	return t
}
