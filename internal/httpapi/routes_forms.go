package httpapi

import (
	"context"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"abidata/internal/admin"
	"abidata/internal/forms"
	"abidata/internal/script"
	"abidata/internal/scripts"
	"abidata/internal/sqlitex"
	"abidata/internal/workspace"
)

// caller builds the forms caller from the request context.
func (s *Server) caller(ctx context.Context, l *workspace.Lease) forms.Caller {
	p, _ := PrincipalFrom(ctx)
	m, _ := MembershipFrom(ctx)
	return forms.Caller{UserID: p.UserID, Email: p.Email, DisplayName: p.DisplayName, Role: m.Role, TZ: workspaceTZ(ctx, l)}
}

// workspaceTZ reads _sys_meta.timezone (UTC on any problem).
func workspaceTZ(ctx context.Context, l *workspace.Lease) *time.Location {
	var name string
	_ = l.Read(ctx, func(q sqlitex.Querier) error {
		return q.QueryRow(ctx, `SELECT value FROM _sys_meta WHERE key = 'timezone'`).Scan(&name)
	})
	if loc, err := time.LoadLocation(name); err == nil && name != "" {
		return loc
	}
	return time.UTC
}

// CheckRequest is the body of /scripts/check.
type CheckRequest struct {
	Source string `json:"source" maxLength:"1000000"`
	Kind   string `json:"kind" enum:"module,form_module,report_module,job"`
}

func (s *Server) registerForms(api huma.API) {
	tags := []string{"forms"}
	builder := map[string]any{metaRole: string(admin.RoleBuilder)}
	editor := map[string]any{metaRole: string(admin.RoleEditor)}
	type formOut struct {
		ETag string `header:"ETag"`
		Body forms.Form
	}
	type evOut struct{ Body *forms.EventResponse }

	huma.Register(api, huma.Operation{OperationID: "listForms", Method: http.MethodGet, Path: "/api/v1/w/{ws}/forms", Tags: tags, Summary: "Forms", Errors: []int{401, 404}},
		func(ctx context.Context, _ *struct {
			WS string `path:"ws"`
		}) (*struct {
			Body struct {
				Items []forms.Form `json:"items"`
			}
		}, error) {
			l, err := s.lease(ctx)
			if err != nil {
				return nil, err
			}
			defer l.Release()
			fs, err := s.deps.Forms.List(ctx, l)
			if err != nil {
				return nil, err
			}
			out := &struct {
				Body struct {
					Items []forms.Form `json:"items"`
				}
			}{}
			out.Body.Items = fs
			return out, nil
		})
	huma.Register(api, huma.Operation{OperationID: "getForm", Method: http.MethodGet, Path: "/api/v1/w/{ws}/forms/{id}", Tags: tags, Summary: "A form with its module (ETag = version)", Errors: []int{401, 404}},
		func(ctx context.Context, in *struct {
			WS string `path:"ws"`
			ID string `path:"id"`
		}) (*formOut, error) {
			l, err := s.lease(ctx)
			if err != nil {
				return nil, err
			}
			defer l.Release()
			f, err := s.deps.Forms.Get(ctx, l, in.ID)
			if err != nil {
				return nil, err
			}
			m, _ := MembershipFrom(ctx)
			if !m.Role.AtLeast(admin.RoleBuilder) {
				f.ModuleSource = "" // module source is design detail
			}
			return &formOut{ETag: etag(f.Version), Body: f}, nil
		})
	huma.Register(api, huma.Operation{OperationID: "createForm", Method: http.MethodPost, Path: "/api/v1/w/{ws}/forms", Tags: tags, Metadata: builder,
		Summary: "Create a form", DefaultStatus: http.StatusCreated, Errors: []int{400, 401, 403, 404, 409, 422}},
		func(ctx context.Context, in *struct {
			WS   string `path:"ws"`
			Body forms.FormInput
		}) (*formOut, error) {
			l, err := s.lease(ctx)
			if err != nil {
				return nil, err
			}
			defer l.Release()
			p, _ := PrincipalFrom(ctx)
			f, err := s.deps.Forms.Put(ctx, l, "", 0, in.Body, p.UserID)
			if err != nil {
				return nil, err
			}
			return &formOut{ETag: etag(f.Version), Body: f}, nil
		})
	huma.Register(api, huma.Operation{OperationID: "updateForm", Method: http.MethodPut, Path: "/api/v1/w/{ws}/forms/{id}", Tags: tags, Metadata: builder,
		Summary: "Replace a form (If-Match: version)", Errors: []int{400, 401, 403, 404, 409, 422}},
		func(ctx context.Context, in *struct {
			WS      string `path:"ws"`
			ID      string `path:"id"`
			IfMatch string `header:"If-Match" required:"true"`
			Body    forms.FormInput
		}) (*formOut, error) {
			ver, err := parseIfMatch(in.IfMatch)
			if err != nil {
				return nil, err
			}
			l, err := s.lease(ctx)
			if err != nil {
				return nil, err
			}
			defer l.Release()
			p, _ := PrincipalFrom(ctx)
			f, err := s.deps.Forms.Put(ctx, l, in.ID, ver, in.Body, p.UserID)
			if err != nil {
				return nil, err
			}
			return &formOut{ETag: etag(f.Version), Body: f}, nil
		})
	huma.Register(api, huma.Operation{OperationID: "deleteForm", Method: http.MethodDelete, Path: "/api/v1/w/{ws}/forms/{id}", Tags: tags, Metadata: builder,
		Summary: "Delete a form (If-Match: version)", DefaultStatus: http.StatusNoContent, Errors: []int{400, 401, 403, 404, 409}},
		func(ctx context.Context, in *struct {
			WS      string `path:"ws"`
			ID      string `path:"id"`
			IfMatch string `header:"If-Match" required:"true"`
		}) (*struct{}, error) {
			ver, err := parseIfMatch(in.IfMatch)
			if err != nil {
				return nil, err
			}
			l, err := s.lease(ctx)
			if err != nil {
				return nil, err
			}
			defer l.Release()
			return nil, s.deps.Forms.Delete(ctx, l, in.ID, ver)
		})

	huma.Register(api, huma.Operation{OperationID: "formEvent", Method: http.MethodPost, Path: "/api/v1/w/{ws}/forms/{id}/events", Tags: tags,
		Summary: "Dispatch a form event; returns patches (D10)", Errors: []int{400, 401, 403, 404, 422}},
		func(ctx context.Context, in *struct {
			WS      string `path:"ws"`
			ID      string `path:"id"`
			Body    forms.EventRequest
			RawBody []byte
		}) (*evOut, error) {
			var req forms.EventRequest
			if err := decodeJSON(in.RawBody, &req); err != nil {
				return nil, err
			}
			l, err := s.lease(ctx)
			if err != nil {
				return nil, err
			}
			defer l.Release()
			bctx, stop := l.Bind(ctx)
			defer stop()
			resp, err := s.deps.Forms.Event(bctx, l, s.caller(ctx, l), in.ID, req)
			if err != nil {
				return nil, err
			}
			return &evOut{Body: resp}, nil
		})
	huma.Register(api, huma.Operation{OperationID: "formSave", Method: http.MethodPost, Path: "/api/v1/w/{ws}/forms/{id}/save", Tags: tags, Metadata: editor,
		Summary: "Save the record buffer: before_* handlers, OCC write, after_update", Errors: []int{400, 401, 403, 404, 409, 422, 503}},
		func(ctx context.Context, in *struct {
			WS             string `path:"ws"`
			ID             string `path:"id"`
			IdempotencyKey string `header:"Idempotency-Key"`
			Body           forms.SaveRequest
			RawBody        []byte
		}) (*evOut, error) {
			var req forms.SaveRequest
			if err := decodeJSON(in.RawBody, &req); err != nil {
				return nil, err
			}
			req.IdempotencyKey = in.IdempotencyKey
			l, err := s.lease(ctx)
			if err != nil {
				return nil, err
			}
			defer l.Release()
			bctx, stop := l.Bind(ctx)
			defer stop()
			resp, err := s.deps.Forms.Save(bctx, l, s.caller(ctx, l), in.ID, req)
			if err != nil {
				return nil, err
			}
			return &evOut{Body: resp}, nil
		})
	huma.Register(api, huma.Operation{OperationID: "formDeleteRecord", Method: http.MethodPost, Path: "/api/v1/w/{ws}/forms/{id}/delete", Tags: tags, Metadata: editor,
		Summary: "Delete the current record: before_delete, OCC delete, after_delete", Errors: []int{400, 401, 403, 404, 409, 422}},
		func(ctx context.Context, in *struct {
			WS   string `path:"ws"`
			ID   string `path:"id"`
			Body struct {
				RecordID      int64 `json:"record_id"`
				RecordVersion int64 `json:"record_version"`
			}
		}) (*evOut, error) {
			l, err := s.lease(ctx)
			if err != nil {
				return nil, err
			}
			defer l.Release()
			bctx, stop := l.Bind(ctx)
			defer stop()
			resp, err := s.deps.Forms.DeleteRecord(bctx, l, s.caller(ctx, l), in.ID, in.Body.RecordID, in.Body.RecordVersion)
			if err != nil {
				return nil, err
			}
			return &evOut{Body: resp}, nil
		})

	// ---- scripts ----
	stags := []string{"scripts"}
	type scriptOut struct {
		ETag string `header:"ETag"`
		Body scripts.Script
	}
	huma.Register(api, huma.Operation{OperationID: "listScripts", Method: http.MethodGet, Path: "/api/v1/w/{ws}/scripts", Tags: stags, Metadata: builder, Summary: "Scripts", Errors: []int{401, 403, 404}},
		func(ctx context.Context, in *struct {
			WS   string `path:"ws"`
			Kind string `query:"kind" enum:"module,form_module,report_module,job,"`
		}) (*struct {
			Body struct {
				Items []scripts.Script `json:"items"`
			}
		}, error) {
			l, err := s.lease(ctx)
			if err != nil {
				return nil, err
			}
			defer l.Release()
			ss, err := s.deps.Scripts.List(ctx, l, in.Kind)
			if err != nil {
				return nil, err
			}
			out := &struct {
				Body struct {
					Items []scripts.Script `json:"items"`
				}
			}{}
			out.Body.Items = ss
			return out, nil
		})
	huma.Register(api, huma.Operation{OperationID: "getScript", Method: http.MethodGet, Path: "/api/v1/w/{ws}/scripts/{id}", Tags: stags, Metadata: builder, Summary: "A script (ETag = version)", Errors: []int{401, 403, 404}},
		func(ctx context.Context, in *struct {
			WS string `path:"ws"`
			ID string `path:"id"`
		}) (*scriptOut, error) {
			l, err := s.lease(ctx)
			if err != nil {
				return nil, err
			}
			defer l.Release()
			sc, err := s.deps.Scripts.Get(ctx, l, in.ID)
			if err != nil {
				return nil, err
			}
			return &scriptOut{ETag: etag(sc.Version), Body: sc}, nil
		})
	huma.Register(api, huma.Operation{OperationID: "createScript", Method: http.MethodPost, Path: "/api/v1/w/{ws}/scripts", Tags: stags, Metadata: builder,
		Summary: "Create a shared module or job script", DefaultStatus: http.StatusCreated, Errors: []int{400, 401, 403, 404, 409}},
		func(ctx context.Context, in *struct {
			WS   string `path:"ws"`
			Body scripts.ScriptInput
		}) (*scriptOut, error) {
			if in.Body.Kind == "form_module" || in.Body.Kind == "report_module" {
				return nil, errBadf("Form and report modules are saved with their form or report.")
			}
			l, err := s.lease(ctx)
			if err != nil {
				return nil, err
			}
			defer l.Release()
			p, _ := PrincipalFrom(ctx)
			sc, err := s.deps.Scripts.Put(ctx, l, "", 0, in.Body, p.UserID)
			if err != nil {
				return nil, err
			}
			return &scriptOut{ETag: etag(sc.Version), Body: sc}, nil
		})
	huma.Register(api, huma.Operation{OperationID: "updateScript", Method: http.MethodPut, Path: "/api/v1/w/{ws}/scripts/{id}", Tags: stags, Metadata: builder,
		Summary: "Replace a script (If-Match: version)", Errors: []int{400, 401, 403, 404, 409}},
		func(ctx context.Context, in *struct {
			WS      string `path:"ws"`
			ID      string `path:"id"`
			IfMatch string `header:"If-Match" required:"true"`
			Body    scripts.ScriptInput
		}) (*scriptOut, error) {
			ver, err := parseIfMatch(in.IfMatch)
			if err != nil {
				return nil, err
			}
			l, err := s.lease(ctx)
			if err != nil {
				return nil, err
			}
			defer l.Release()
			p, _ := PrincipalFrom(ctx)
			sc, err := s.deps.Scripts.Put(ctx, l, in.ID, ver, in.Body, p.UserID)
			if err != nil {
				return nil, err
			}
			return &scriptOut{ETag: etag(sc.Version), Body: sc}, nil
		})
	huma.Register(api, huma.Operation{OperationID: "deleteScript", Method: http.MethodDelete, Path: "/api/v1/w/{ws}/scripts/{id}", Tags: stags, Metadata: builder,
		Summary: "Delete a script (If-Match: version)", DefaultStatus: http.StatusNoContent, Errors: []int{400, 401, 403, 404, 409}},
		func(ctx context.Context, in *struct {
			WS      string `path:"ws"`
			ID      string `path:"id"`
			IfMatch string `header:"If-Match" required:"true"`
		}) (*struct{}, error) {
			ver, err := parseIfMatch(in.IfMatch)
			if err != nil {
				return nil, err
			}
			l, err := s.lease(ctx)
			if err != nil {
				return nil, err
			}
			defer l.Release()
			return nil, s.deps.Scripts.Delete(ctx, l, in.ID, ver)
		})
	huma.Register(api, huma.Operation{OperationID: "checkScript", Method: http.MethodPost, Path: "/api/v1/w/{ws}/scripts/check", Tags: stags, Metadata: builder,
		Summary: "Syntax errors and lint warnings as editor markers", Errors: []int{400, 401, 403, 404}},
		func(ctx context.Context, in *struct {
			WS   string `path:"ws"`
			Body CheckRequest
		}) (*struct {
			Body struct {
				Markers []scripts.Marker `json:"markers"`
			}
		}, error) {
			l, err := s.lease(ctx)
			if err != nil {
				return nil, err
			}
			defer l.Release()
			sch, err := s.deps.Schemas.Get(ctx, l)
			if err != nil {
				return nil, err
			}
			out := &struct {
				Body struct {
					Markers []scripts.Marker `json:"markers"`
				}
			}{}
			out.Body.Markers = scripts.Check(in.Body.Source, in.Body.Kind, sch)
			return out, nil
		})

	huma.Register(api, huma.Operation{OperationID: "luaAPI", Method: http.MethodGet, Path: "/api/v1/docs/lua-api.json", Tags: []string{"docs"},
		Summary: "The Lua API manifest (modules, functions, contexts)", Errors: []int{401}},
		func(ctx context.Context, _ *struct{}) (*struct {
			CacheControl string `header:"Cache-Control"`
			Body         []script.ModuleDoc
		}, error) {
			return &struct {
				CacheControl string `header:"Cache-Control"`
				Body         []script.ModuleDoc
			}{CacheControl: "no-cache", Body: script.Manifest()}, nil
		})
}
