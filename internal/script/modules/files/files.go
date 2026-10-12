// Package files is the Lua `files` module: metadata and content of the
// workspace's file attachments, and (in jobs) creating files, for example a
// generated CSV to attach to a row.
package files

import (
	"time"

	lua "github.com/yuin/gopher-lua"

	"abidata/internal/fault"
	"abidata/internal/files"
	"abidata/internal/script"
	"abidata/internal/script/host"
)

func init() { script.Register(script.ModuleDef{Doc: doc, Build: build}) }

// Read limits per context. They sit inside the run's string budget (1 MiB in
// form events and reports, 16 MiB in jobs), which also bounds everything one
// run reads in total.
const (
	readLimitUI  = 1 << 20
	readLimitJob = 10 << 20
)

var doc = script.ModuleDoc{
	Name:     "files",
	Doc:      "File attachments. A file is identified by its id, the value stored in an attachments field. Content is read and written as a Lua string; reads count against the run's memory budget.",
	Contexts: []script.Context{script.UIEvent, script.Report, script.Job},
	Funcs: []script.FuncDoc{
		{Name: "info", Signature: "files.info(id) → info|nil",
			Params:  []script.ParamDoc{{Name: "id", Type: "string", Doc: "A file id, as stored in an attachments field."}},
			Returns: "{id, name, size, content_type, sha256, width, height, state, created_at} (width and height only for images; state is \"pending\" or \"attached\"), or nil when no such file exists in this workspace.",
			Doc:     "Describes a file without reading it.",
			Example: `local f = files.info(record.Photos[1])
if f then log.info(f.name, {size = f.size}) end`},
		{Name: "read", Signature: "files.read(id) → content|nil",
			Params:  []script.ParamDoc{{Name: "id", Type: "string", Doc: "A file id."}},
			Returns: "The file's bytes as a string, or nil when no such file exists.",
			Errors:  []string{"BAD_REQUEST: the file is larger than this context allows (1 MiB in form events and reports, 10 MiB in jobs)", "SCRIPT_LIMIT_EXCEEDED: the run has read more than its memory budget"},
			Doc:     "Reads a whole file. Use it for small text or data files; images and documents are meant to be shown to people, not parsed in Lua.",
			Example: `local text = files.read(id)
local rows = csv.parse(text)`},
		{Name: "create", Signature: "files.create(name, content, content_type?) → info",
			Params: []script.ParamDoc{
				{Name: "name", Type: "string", Doc: "The file name people will see; any directory part is dropped."},
				{Name: "content", Type: "string", Doc: "The bytes (at most the server's file size limit, 25 MiB by default)."},
				{Name: "content_type", Type: "string", Opt: true, Doc: "Accepted for readability and ignored: the type is detected from the content."},
			},
			Returns:  "The new file's info table. The file is pending until a row refers to it: put info.id into an attachments field within 24 hours.",
			Errors:   []string{"QUOTA_EXCEEDED: the workspace's file storage is full", "BAD_REQUEST: empty or too large", "RATE_LIMITED: too many uploads at once"},
			Doc:      "Stores a new file.",
			Contexts: []script.Context{script.Job},
			Example: `local f = files.create("totals.csv", csv.encode(rows, {columns = {"id", "total"}}))
records.update("Reports", report_id, {Attachment = {f.id}})`},
	},
}

func build(r *script.Run) lua.LValue {
	L := r.L
	t := L.NewTable()
	data := func() *host.Data {
		d, _ := r.Host.(*host.Data)
		if d == nil || d.Files == nil || d.Lease == nil {
			r.RaiseFault(fault.New(fault.Forbidden, "Files are not available here."))
		}
		return d
	}
	t.RawSetString("info", L.NewFunction(func(L *lua.LState) int {
		d := data()
		m, err := d.Files.Get(r.Ctx, d.Lease, L.CheckString(1))
		if err != nil {
			if fault.Classify(err).Code == fault.NotFound {
				L.Push(lua.LNil)
				return 1
			}
			r.RaiseFault(err)
		}
		L.Push(infoTable(L, m))
		return 1
	}))
	t.RawSetString("read", L.NewFunction(func(L *lua.LState) int {
		d := data()
		limit := int64(readLimitUI)
		if r.Kind == script.Job {
			limit = readLimitJob
		}
		id := L.CheckString(1)
		m, err := d.Files.Get(r.Ctx, d.Lease, id)
		if err != nil {
			if fault.Classify(err).Code == fault.NotFound {
				L.Push(lua.LNil)
				return 1
			}
			r.RaiseFault(err)
			return 0
		}
		// Too big for this context is a mistake in the script (BAD_REQUEST); what
		// the run has already read plus this file exceeding its memory budget is a
		// resource limit.
		used := r.Shared("files.read", func() any { return new(int64) }).(*int64)
		if budget := int64(r.Budget.MaxString); budget > 0 && m.Size <= limit && *used+m.Size > budget {
			r.RaiseLimit("files.read: this run has read as much as its memory budget allows")
			return 0
		}
		b, _, err := d.Files.Read(r.Ctx, d.Lease, id, limit)
		if err != nil {
			if fault.Classify(err).Code == fault.NotFound {
				L.Push(lua.LNil)
				return 1
			}
			r.RaiseFault(err)
			return 0
		}
		*used += int64(len(b))
		L.Push(lua.LString(b))
		return 1
	}))
	t.RawSetString("create", L.NewFunction(func(L *lua.LState) int {
		d := data()
		if r.Kind != script.Job {
			r.RaiseFault(fault.New(fault.Forbidden, "files.create is only available in jobs."))
			return 0
		}
		if _, _, inTx := d.Tx(); inTx {
			r.RaiseFault(fault.New(fault.BadRequest, "files.create cannot run inside db.transaction: create the file first, then use its id in the transaction."))
			return 0
		}
		if !r.Budget.Writes || !d.CanWrite {
			r.RaiseFault(fault.New(fault.Forbidden, "This script may not create files (insufficient role or read-only context)."))
			return 0
		}
		name, content := L.CheckString(1), L.CheckString(2)
		_ = L.OptString(3, "") // the type is detected, never taken from the caller
		m, err := d.Files.Create(r.Ctx, d.Lease, d.UserID, name, []byte(content))
		if err != nil {
			r.RaiseFault(err)
			return 0
		}
		L.Push(infoTable(L, m))
		return 1
	}))
	return t
}

func infoTable(L *lua.LState, m files.Meta) *lua.LTable {
	t := L.CreateTable(0, 9)
	t.RawSetString("id", lua.LString(m.ID))
	t.RawSetString("name", lua.LString(m.Name))
	t.RawSetString("size", lua.LNumber(m.Size))
	t.RawSetString("content_type", lua.LString(m.ContentType))
	t.RawSetString("sha256", lua.LString(m.SHA256))
	t.RawSetString("state", lua.LString(m.State))
	t.RawSetString("created_at", lua.LString(m.CreatedAt.UTC().Format(time.RFC3339)))
	if m.Width != nil && m.Height != nil {
		t.RawSetString("width", lua.LNumber(*m.Width))
		t.RawSetString("height", lua.LNumber(*m.Height))
	}
	return t
}
