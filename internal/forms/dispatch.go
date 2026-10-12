package forms

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	lua "github.com/yuin/gopher-lua"

	"abidata/internal/admin"
	"abidata/internal/catalog"
	"abidata/internal/fault"
	"abidata/internal/records"
	"abidata/internal/script"
	"abidata/internal/script/host"
	recmod "abidata/internal/script/modules/records"
	"abidata/internal/workspace"
)

// Caller is the user raising a form event.
type Caller struct {
	UserID      string
	Email       string
	DisplayName string
	Role        admin.Role
	TZ          *time.Location
}

// EventRequest is the client's event (brief <forms> event protocol). Record
// values use the rows API representation keyed by column ID.
type EventRequest struct {
	Event         string                    `json:"event"`
	Control       string                    `json:"control,omitempty"`
	Record        map[string]any            `json:"record,omitempty"`
	RecordID      int64                     `json:"record_id,omitempty"`
	RecordVersion int64                     `json:"record_version,omitempty"`
	IsNew         bool                      `json:"is_new,omitempty"`
	DirtyFields   []string                  `json:"dirty_fields,omitempty"`
	OldValue      any                       `json:"old_value,omitempty"`
	NewValue      any                       `json:"new_value,omitempty"`
	OpenArgs      map[string]any            `json:"open_args,omitempty"`
	Controls      map[string]map[string]any `json:"controls,omitempty" doc:"Current control property overrides (by control name)."`
	Continuation  string                    `json:"continuation,omitempty"`
	Answer        *bool                     `json:"answer,omitempty"`
}

// EventResponse is the server's patch (D10).
type EventResponse struct {
	Patches       []host.Patch        `json:"patches"`
	Record        map[string]any      `json:"record"` // changed fields by column ID
	Cancel        *CancelInfo         `json:"cancel,omitempty"`
	Notifications []host.Notification `json:"notifications"`
	Navigate      map[string]any      `json:"navigate,omitempty"`
	Confirm       *host.Confirm       `json:"confirm,omitempty"`
	Calc          map[string]any      `json:"calc"`
	CalcErrors    map[string]string   `json:"calc_errors,omitempty"`
	Focus         string              `json:"focus,omitempty"`
	Requery       []string            `json:"requery,omitempty"`
	Close         bool                `json:"close,omitempty"`
	Saved         *records.Row        `json:"saved,omitempty"`
	Logs          []script.LogEntry   `json:"logs,omitempty"`
}

// CancelInfo reports a cancelled action.
type CancelInfo struct {
	Message string `json:"message"`
}

type prepared struct {
	form    Form
	sch     *catalog.Schema
	table   *catalog.Table
	session *host.UISession
}

func (s *Service) prepare(ctx context.Context, l *workspace.Lease, formID string, req EventRequest) (*prepared, error) {
	f, err := s.Get(ctx, l, formID)
	if err != nil {
		return nil, err
	}
	sch, err := s.Schemas.Get(ctx, l)
	if err != nil {
		return nil, err
	}
	src, err := s.source(ctx, l, sch, f.SourceKind, f.SourceID)
	if err != nil {
		return nil, err
	}
	p := &prepared{form: f, sch: sch, table: src.Table}
	sess := &host.UISession{Table: src.Table, Event: req.Event, Control: req.Control, OldValue: req.OldValue, NewValue: req.NewValue,
		Args: req.OpenArgs, RecordID: req.RecordID, RecordVersion: req.RecordVersion, IsNew: req.IsNew, Dirty: map[string]bool{},
		Continuation: req.Continuation, Answer: req.Answer, Record: map[string]any{}, Controls: map[string]map[string]any{}}
	if src.Table != nil {
		for colID, v := range req.Record {
			c, ok := src.Table.Column(colID)
			if !ok {
				return nil, fault.Newf(fault.BadRequest, "Unknown field %s in the record buffer.", colID)
			}
			sess.Record[c.PhysicalName] = normalize(v)
		}
		for _, colID := range req.DirtyFields {
			if c, ok := src.Table.Column(colID); ok {
				sess.Dirty[c.PhysicalName] = true
			}
		}
	}
	for _, c := range f.Layout.AllControls() {
		props := map[string]any{"visible": c.Visible == nil || *c.Visible, "enabled": c.Enabled == nil || *c.Enabled,
			"locked": c.Locked, "caption": c.Caption, "type": c.Type}
		for k, v := range req.Controls[c.Name] {
			switch k {
			case "visible", "enabled", "locked", "caption", "required", "tooltip", "tone", "value":
				props[k] = v
			}
		}
		sess.Controls[c.Name] = props
	}
	p.session = sess
	return p, nil
}

// normalize turns json.Number into int64/float64 for Lua conversion.
func normalize(v any) any {
	if n, ok := v.(json.Number); ok {
		if i, err := n.Int64(); err == nil {
			return i
		}
		f, _ := n.Float64()
		return f
	}
	return v
}

func (s *Service) handlerFor(l *workspace.Lease, c Caller, p *prepared, req EventRequest) (string, error) {
	f := p.form
	switch {
	case req.Event == "confirm":
		return openContinuation(l, c, p.form.ID, req.Continuation, time.Now())
	case req.Control != "":
		for _, c := range f.Layout.AllControls() {
			if c.Name == req.Control {
				if !ControlEvents[req.Event] {
					return "", fault.Newf(fault.BadRequest, "Unknown control event %q.", req.Event)
				}
				return c.Events[req.Event], nil
			}
		}
		return "", fault.Newf(fault.BadRequest, "The form has no control named %q.", req.Control)
	default:
		if !FormEvents[req.Event] {
			return "", fault.Newf(fault.BadRequest, "Unknown form event %q.", req.Event)
		}
		return f.Layout.Events[req.Event], nil
	}
}

func (s *Service) hostData(l *workspace.Lease, c Caller, p *prepared) *host.Data {
	d := &host.Data{Lease: l, Schemas: s.Schemas, Records: s.Records, Builder: c.Role.AtLeast(admin.RoleBuilder),
		CanWrite: c.Role.AtLeast(admin.RoleEditor), UserID: c.UserID, UI: p.session, Files: s.Files}
	// Enqueueing a job is a write: editors and above only.
	if s.Enqueue != nil && c.Role.AtLeast(admin.RoleEditor) {
		d.Enqueue = func(ctx context.Context, name string, args map[string]any) (string, error) {
			return s.Enqueue(ctx, l, name, c.UserID, args, true)
		}
	}
	return d
}

// runHandler executes handler (if any) and folds the session into resp.
func (s *Service) runHandler(ctx context.Context, l *workspace.Lease, c Caller, p *prepared, handler string, resp *EventResponse) error {
	if handler == "" {
		return nil
	}
	mod, err := s.Scripts.Get(ctx, l, p.form.ModuleID)
	if err != nil {
		return err
	}
	proto, err := s.Scripts.Proto(l.ID(), mod)
	if err != nil {
		return err
	}
	budget := script.UIEventBudget(0)
	if s.UIEventBudgetMS != nil {
		budget = script.UIEventBudget(s.UIEventBudgetMS(l.ID()))
	}
	res, err := s.Runner.Exec(ctx, script.ExecRequest{Kind: script.UIEvent, Workspace: l.ID(), Budget: budget,
		Principal: script.Principal{UserID: c.UserID, Email: c.Email, DisplayName: c.DisplayName, Role: string(c.Role)},
		TZ:        c.TZ, Proto: proto, ChunkName: "form module", Entry: handler, Host: s.hostData(l, c, p), Loader: s.Scripts.Loader(ctx, l)})
	if c.Role.AtLeast(admin.RoleBuilder) {
		resp.Logs = append(resp.Logs, res.Logs...)
	}
	if err != nil {
		return scriptFault(err, c)
	}
	return nil
}

// scriptFault maps script errors to the catalog codes; tracebacks only for builders.
func scriptFault(err error, c Caller) error {
	var se *script.Error
	if !errors.As(err, &se) {
		return err
	}
	code := map[script.ErrorKind]fault.Code{script.KindRuntime: fault.ScriptError, script.KindSyntax: fault.ScriptError,
		script.KindTimeout: fault.ScriptTimeout, script.KindLimit: fault.ScriptLimitExceeded}[se.Kind]
	fe := &fault.Error{Code: code, Message: "Script: " + se.Message, Cause: err}
	if c.Role.AtLeast(admin.RoleBuilder) && se.Traceback != "" {
		fe.Details = map[string]any{"traceback": se.Traceback}
	}
	return fe
}

func (p *prepared) changedByID() map[string]any {
	out := map[string]any{}
	if p.table == nil {
		return out
	}
	for phys, v := range p.session.Changed {
		if c, ok := p.table.ColumnByPhysical(phys); ok {
			out[c.ID] = apiValue(v)
		}
	}
	return out
}

func apiValue(v any) any {
	switch x := v.(type) {
	case script.Null:
		return nil
	case int64:
		if x > 1<<53 || x < -(1<<53) {
			return strconv.FormatInt(x, 10)
		}
	}
	return v
}

func (p *prepared) fold(resp *EventResponse) {
	s := p.session
	resp.Patches = append(resp.Patches, s.Patches...)
	resp.Notifications = append(resp.Notifications, s.Notifications...)
	for k, v := range p.changedByID() {
		resp.Record[k] = v
	}
	if s.Cancel != nil {
		resp.Cancel = &CancelInfo{Message: *s.Cancel}
	}
	if s.Navigate != nil {
		resp.Navigate = s.Navigate
	}
	if s.Confirm != nil {
		resp.Confirm = s.Confirm
	}
	if s.Focus != "" {
		resp.Focus = s.Focus
	}
	resp.Requery = append(resp.Requery, s.Requery...)
	resp.Close = resp.Close || s.Close
}

func newResponse() *EventResponse {
	return &EventResponse{Patches: []host.Patch{}, Record: map[string]any{}, Notifications: []host.Notification{}, Calc: map[string]any{}}
}

// Event dispatches a non-save event (load, current, click, change, confirm …).
func (s *Service) Event(ctx context.Context, l *workspace.Lease, c Caller, formID string, req EventRequest) (*EventResponse, error) {
	switch req.Event {
	case "before_insert", "before_update", "before_delete", "after_update", "after_delete":
		if req.Control == "" {
			return nil, fault.New(fault.BadRequest, "Record lifecycle events run through save and delete.")
		}
	}
	p, err := s.prepare(ctx, l, formID, req)
	if err != nil {
		return nil, err
	}
	handler, err := s.handlerFor(l, c, p, req)
	if err != nil {
		return nil, err
	}
	if req.Event == "confirm" {
		p.session.Continuation = handler // scripts see the name, not the token
	}
	resp := newResponse()
	if err := s.runHandler(ctx, l, c, p, handler, resp); err != nil {
		return nil, err
	}
	s.fold(l, c, p, resp)
	switch req.Event {
	case "load", "current", "after_update", "change", "confirm", "click":
		s.computeCalc(ctx, l, c, p, resp)
	}
	return resp, nil
}

// SaveRequest saves the record buffer through the form's lifecycle.
type SaveRequest struct {
	RecordID       int64          `json:"record_id,omitempty"`
	RecordVersion  int64          `json:"record_version,omitempty"`
	IsNew          bool           `json:"is_new"`
	Fields         map[string]any `json:"fields" doc:"Changed fields by column ID (rows API representation)."`
	OpenArgs       map[string]any `json:"open_args,omitempty"`
	IdempotencyKey string         `json:"-"`
}

// Save runs before_insert/before_update (outside the writer), the OCC write,
// then after_update (D10). A cancel from the before-handler stops the save.
func (s *Service) Save(ctx context.Context, l *workspace.Lease, c Caller, formID string, req SaveRequest) (*EventResponse, error) {
	if !c.Role.AtLeast(admin.RoleEditor) {
		return nil, fault.E(fault.Forbidden)
	}
	ev := "before_update"
	if req.IsNew {
		ev = "before_insert"
	}
	dirty := make([]string, 0, len(req.Fields))
	for k := range req.Fields {
		dirty = append(dirty, k)
	}
	p, err := s.prepare(ctx, l, formID, EventRequest{Event: ev, Record: req.Fields, RecordID: req.RecordID, RecordVersion: req.RecordVersion,
		IsNew: req.IsNew, DirtyFields: dirty, OpenArgs: req.OpenArgs})
	if err != nil {
		return nil, err
	}
	f := p.form
	if p.table == nil {
		return nil, fault.New(fault.BadRequest, "This form has no editable record source.")
	}
	if (req.IsNew && !f.AllowAdditions) || (!req.IsNew && !f.AllowEdits) {
		return nil, fault.New(fault.Forbidden, "This form does not allow that change.")
	}
	resp := newResponse()
	if err := s.runHandler(ctx, l, c, p, f.Layout.Events[ev], resp); err != nil {
		return nil, err
	}
	s.fold(l, c, p, resp)
	if resp.Cancel != nil {
		return resp, nil
	}
	fields := map[string]any{}
	for k, v := range req.Fields {
		fields[k] = v
	}
	for k, v := range p.changedByID() {
		fields[k] = toNumber(v)
	}
	var idem *records.Idempotency
	if req.IdempotencyKey != "" {
		idem = &records.Idempotency{Key: req.IdempotencyKey, UserID: c.UserID}
	}
	var row records.Row
	if req.IsNew {
		row, err = s.Records.Insert(ctx, l, p.table.ID, fields, records.Actor{UserID: c.UserID}, idem)
	} else {
		row, err = s.Records.Update(ctx, l, p.table.ID, req.RecordID, req.RecordVersion, fields, records.Actor{UserID: c.UserID}, idem)
	}
	if err != nil {
		return nil, err
	}
	resp.Saved = &row
	// after_update sees the saved row.
	p.session = &host.UISession{Table: p.table, Event: "after_update", Args: req.OpenArgs, RecordID: row.ID, RecordVersion: row.Version,
		Record: physicalRecord(p.table, row), Dirty: map[string]bool{}, Controls: p.session.Controls}
	if err := s.runHandler(ctx, l, c, p, f.Layout.Events["after_update"], resp); err != nil {
		resp.Notifications = append(resp.Notifications, host.Notification{Kind: "alert", Level: "error",
			Text: "The record was saved, but the after-update script failed: " + fault.Classify(err).DisplayMessage()})
	} else {
		s.fold(l, c, p, resp)
	}
	s.computeCalc(ctx, l, c, p, resp)
	return resp, nil
}

func toNumber(v any) any {
	switch x := v.(type) {
	case int64:
		return json.Number(strconv.FormatInt(x, 10))
	case float64:
		return json.Number(strconv.FormatFloat(x, 'f', -1, 64))
	}
	return v
}

func physicalRecord(t *catalog.Table, row records.Row) map[string]any {
	out := map[string]any{}
	for _, c := range t.Columns {
		if c.Type != catalog.Autonumber {
			out[c.PhysicalName] = normalize(row.Fields[c.ID])
		}
	}
	return out
}

// DeleteRecord runs before_delete (may cancel), deletes with OCC, then after_delete.
func (s *Service) DeleteRecord(ctx context.Context, l *workspace.Lease, c Caller, formID string, recordID, version int64) (*EventResponse, error) {
	if !c.Role.AtLeast(admin.RoleEditor) {
		return nil, fault.E(fault.Forbidden)
	}
	p, err := s.prepare(ctx, l, formID, EventRequest{Event: "before_delete", RecordID: recordID, RecordVersion: version})
	if err != nil {
		return nil, err
	}
	if p.table == nil || !p.form.AllowDeletions {
		return nil, fault.New(fault.Forbidden, "This form does not allow deleting records.")
	}
	cur, err := s.Records.Get(ctx, l, p.table.ID, recordID)
	if err != nil {
		return nil, err
	}
	p.session.Record = physicalRecord(p.table, cur)
	resp := newResponse()
	if err := s.runHandler(ctx, l, c, p, p.form.Layout.Events["before_delete"], resp); err != nil {
		return nil, err
	}
	s.fold(l, c, p, resp)
	if resp.Cancel != nil {
		return resp, nil
	}
	if err := s.Records.Delete(ctx, l, p.table.ID, recordID, version, records.Actor{UserID: c.UserID}, nil); err != nil {
		return nil, err
	}
	p.session.Event = "after_delete"
	if err := s.runHandler(ctx, l, c, p, p.form.Layout.Events["after_delete"], resp); err == nil {
		s.fold(l, c, p, resp)
	}
	return resp, nil
}

// computeCalc evaluates calculated controls with the record's fields as Lua
// globals (server-authoritative; read-only report context, short budget).
func (s *Service) computeCalc(ctx context.Context, l *workspace.Lease, c Caller, p *prepared, resp *EventResponse) {
	var calcs []*Control
	for _, ctl := range p.form.Layout.AllControls() {
		if ctl.Calc != nil {
			calcs = append(calcs, ctl)
		}
	}
	if len(calcs) == 0 {
		return
	}
	var b strings.Builder
	b.WriteString("local __r = {}\n")
	for _, ctl := range calcs {
		fmt.Fprintf(&b, "__r[%q] = {pcall(function() return (%s) end)}\n", ctl.ID, ctl.Calc.LuaExpr)
	}
	b.WriteString("return __r\n")
	proto, err := script.CompileCached(b.String(), "calc")
	if err != nil {
		resp.CalcErrors = map[string]string{"*": fault.Classify(err).DisplayMessage()}
		return
	}
	record := p.session.Record
	table := p.table
	res, err := s.Runner.Exec(ctx, script.ExecRequest{Kind: script.Report, Workspace: l.ID(), TZ: c.TZ,
		Principal: script.Principal{UserID: c.UserID, Role: string(c.Role)}, Proto: proto,
		Budget: script.Budget{Wall: 250 * time.Millisecond, MaxString: 1 << 20},
		Host:   &host.Data{Lease: l, Schemas: s.Schemas, Records: s.Records, UserID: c.UserID, Files: s.Files},
		Globals: func(r *script.Run) map[string]lua.LValue {
			g := map[string]lua.LValue{}
			if table == nil {
				return g
			}
			for _, col := range table.Columns {
				if col.Type == catalog.Autonumber {
					g["id"] = script.ToLua(r, p.session.RecordID)
					continue
				}
				g[col.PhysicalName] = recmod.ValueToLua(r, col, apiValue(record[col.PhysicalName]))
			}
			return g
		}})
	if err != nil {
		resp.CalcErrors = map[string]string{"*": fault.Classify(scriptFault(err, c)).DisplayMessage()}
		return
	}
	out, _ := res.Values[0].(map[string]any)
	for _, ctl := range calcs {
		pair, _ := out[ctl.ID].([]any)
		if len(pair) == 0 {
			continue
		}
		if ok, _ := pair[0].(bool); ok {
			var v any
			if len(pair) > 1 {
				v = apiValue(pair[1])
			}
			resp.Calc[ctl.ID] = v
			continue
		}
		if resp.CalcErrors == nil {
			resp.CalcErrors = map[string]string{}
		}
		msg := "error"
		if len(pair) > 1 {
			msg = fmt.Sprint(pair[1])
		}
		resp.CalcErrors[ctl.ID] = msg
		resp.Calc[ctl.ID] = nil
	}
}
