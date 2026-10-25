// Package jobs registers the job-related script modules: `jobs` (enqueue),
// `job` (the running job's args and run id) and `artifacts` (run output files).
package jobs

import (
	lua "github.com/yuin/gopher-lua"

	"abidata/internal/fault"
	"abidata/internal/script"
	"abidata/internal/script/host"
)

func init() {
	script.Register(script.ModuleDef{Doc: jobsDoc, Build: buildJobs})
	script.Register(script.ModuleDef{Doc: jobDoc, Build: buildJob})
	script.Register(script.ModuleDef{Doc: artifactsDoc, Build: buildArtifacts})
	script.Register(script.ModuleDef{Doc: reportsDoc, Build: buildReports})
}

// maxArtifact bounds one artifacts.save call (the run's string budget bounds it too).
const maxArtifact = 16 << 20

var jobsDoc = script.ModuleDoc{
	Name: "jobs", Contexts: []script.Context{script.UIEvent, script.Job},
	Doc: "Starts background jobs.",
	Funcs: []script.FuncDoc{{
		Name: "enqueue", Signature: "jobs.enqueue(name, args?) → run_id",
		Params: []script.ParamDoc{
			{Name: "name", Type: "string", Doc: "Job name."},
			{Name: "args", Type: "table", Opt: true, Doc: "Arguments merged over the job's own args (string keys; JSON-compatible values)."},
		},
		Returns: "The new run's id. The run starts as soon as a worker is free, following the job's overlap policy.",
		Doc:     "Queues a run of a job. From a form, the user must be an editor or above and the job's args must contain allow_enqueue = true.",
		Errors:  []string{"NOT_FOUND: no job with that name", "FORBIDDEN: the job does not allow enqueue from forms", "WORKSPACE_BUSY: the job is already running (overlap policy)"},
		Example: `local run = jobs.enqueue("Send invoices", {customer = record.id})`,
	}},
}

func buildJobs(r *script.Run) lua.LValue {
	L := r.L
	t := L.NewTable()
	t.RawSetString("enqueue", L.NewFunction(func(L *lua.LState) int {
		name := L.CheckString(1)
		var args map[string]any
		if a := L.OptTable(2, nil); a != nil {
			m, ok := script.FromLua(r, a).(map[string]any)
			if !ok {
				L.ArgError(2, "args must be a table with string keys")
			}
			args = m
		}
		d, _ := r.Host.(*host.Data)
		if d == nil || d.Enqueue == nil {
			r.RaiseFault(fault.New(fault.Forbidden, "jobs.enqueue is not available here."))
			return 0
		}
		id, err := d.Enqueue(r.Ctx, name, args)
		if err != nil {
			r.RaiseFault(err)
			return 0
		}
		L.Push(lua.LString(id))
		return 1
	}))
	return t
}

var jobDoc = script.ModuleDoc{
	Name: "job", Contexts: []script.Context{script.Job}, Global: true,
	Doc: "The running job.",
	Values: []script.ValueDoc{
		{Name: "id", Type: "string", Doc: "Job id."},
		{Name: "name", Type: "string", Doc: "Job name."},
		{Name: "run_id", Type: "string", Doc: "This run's id."},
		{Name: "args", Type: "table", Doc: "The job's args, with any enqueue args merged over them."},
	},
}

func buildJob(r *script.Run) lua.LValue {
	t := r.L.NewTable()
	d, _ := r.Host.(*host.Data)
	if d == nil || d.Job == nil {
		t.RawSetString("args", r.L.NewTable())
		return t
	}
	t.RawSetString("id", lua.LString(d.Job.JobID))
	t.RawSetString("name", lua.LString(d.Job.JobName))
	t.RawSetString("run_id", lua.LString(d.Job.RunID))
	args := d.Job.Args
	if args == nil {
		args = map[string]any{}
	}
	t.RawSetString("args", script.ToLua(r, args))
	return t
}

var artifactsDoc = script.ModuleDoc{
	Name: "artifacts", Contexts: []script.Context{script.Job},
	Doc: "Files a job run produces, downloadable from the run's history.",
	Funcs: []script.FuncDoc{{
		Name: "save", Signature: "artifacts.save(name, content, {mime}?) → {name, size}",
		Params: []script.ParamDoc{
			{Name: "name", Type: "string", Doc: "File name: letters, digits, spaces, '.', '_' or '-' (at most 128 characters). Saving the same name again replaces it."},
			{Name: "content", Type: "string", Doc: "File content (at most 16 MiB)."},
			{Name: "options", Type: "table", Opt: true, Doc: "mime: the media type (default from the file extension)."},
		},
		Doc:     "Saves a file for this run, within the workspace's artifact quota.",
		Errors:  []string{"BAD_REQUEST: invalid name", "QUOTA_EXCEEDED: artifact storage full or too many artifacts in this run"},
		Example: `artifacts.save("summary.csv", csv.encode(rows, {columns = {"id", "total"}}))`,
	}},
}

func buildArtifacts(r *script.Run) lua.LValue {
	L := r.L
	t := L.NewTable()
	t.RawSetString("save", L.NewFunction(func(L *lua.LState) int {
		name := L.CheckString(1)
		content := L.CheckString(2)
		mime := ""
		if o := L.OptTable(3, nil); o != nil {
			if m, ok := o.RawGetString("mime").(lua.LString); ok {
				mime = string(m)
			}
		}
		if len(content) > maxArtifact {
			L.ArgError(2, "content is larger than 16 MiB")
		}
		d, _ := r.Host.(*host.Data)
		if d == nil || d.Job == nil || d.Job.SaveArtifact == nil {
			r.RaiseFault(fault.New(fault.Forbidden, "artifacts.save is only available in jobs."))
			return 0
		}
		n, err := d.Job.SaveArtifact(name, mime, []byte(content))
		if err != nil {
			r.RaiseFault(err)
			return 0
		}
		res := L.NewTable()
		res.RawSetString("name", lua.LString(name))
		res.RawSetString("size", lua.LNumber(n))
		L.Push(res)
		return 1
	}))
	return t
}

var reportsDoc = script.ModuleDoc{
	Name: "reports", Contexts: []script.Context{script.Job},
	Doc: "Runs reports from a job and saves the result as a run artifact.",
	Funcs: []script.FuncDoc{{
		Name: "generate", Signature: "reports.generate(report, params?, options?) → {name, size, rows, format}",
		Params: []script.ParamDoc{
			{Name: "report", Type: "string", Doc: "The report's id or name."},
			{Name: "params", Type: "table", Opt: true, Doc: "Values for the parameters of the report's saved-query source."},
			{Name: "options", Type: "table", Opt: true, Doc: "format: \"csv\" (default), \"json\" or \"html\". name: the artifact file name (default: the report's name with the format's extension)."},
		},
		Returns: "The saved artifact's name and size, the number of detail rows and the format.",
		Doc: "Runs the report as the job (with an editor's data rights) and saves a rendering as an artifact of this run, within the artifact quota. " +
			"csv holds the detail rows with one leading column per grouping level; json mirrors the groups as a tree with their header and footer values; html is a self-contained table. " +
			"Maps and images are not part of generated files. Saving the same name again replaces the file.",
		Errors:  []string{"NOT_FOUND: no such report", "BAD_REQUEST: unknown format, invalid name or a missing report parameter", "QUOTA_EXCEEDED: artifact storage full or too many artifacts in this run"},
		Example: `local out = reports.generate("Monthly sales", {month = "2026-09"}, {format = "csv", name = "sales-2026-09.csv"})`,
	}},
}

func buildReports(r *script.Run) lua.LValue {
	L := r.L
	t := L.NewTable()
	t.RawSetString("generate", L.NewFunction(func(L *lua.LState) int {
		ref := L.CheckString(1)
		var params map[string]any
		if a := L.OptTable(2, nil); a != nil {
			m, ok := script.FromLua(r, a).(map[string]any)
			if !ok {
				L.ArgError(2, "params must be a table with string keys")
			}
			params = m
		}
		format, name := "csv", ""
		if o := L.OptTable(3, nil); o != nil {
			if f, ok := o.RawGetString("format").(lua.LString); ok {
				format = string(f)
			}
			if n, ok := o.RawGetString("name").(lua.LString); ok {
				name = string(n)
			}
		}
		d, _ := r.Host.(*host.Data)
		if d == nil || d.Job == nil || d.Job.GenerateReport == nil {
			r.RaiseFault(fault.New(fault.Forbidden, "reports.generate is only available in jobs."))
			return 0
		}
		out, err := d.Job.GenerateReport(r.Ctx, ref, params, format, name)
		if err != nil {
			r.RaiseFault(err)
			return 0
		}
		res := L.NewTable()
		res.RawSetString("name", lua.LString(out.Name))
		res.RawSetString("size", lua.LNumber(out.Size))
		res.RawSetString("rows", lua.LNumber(out.Rows))
		res.RawSetString("format", lua.LString(out.Format))
		L.Push(res)
		return 1
	}))
	return t
}
