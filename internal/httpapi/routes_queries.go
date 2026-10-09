package httpapi

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"abidata/internal/admin"
	"abidata/internal/fault"
	"abidata/internal/query"
)

// RunRequest supplies parameter values and a row cap.
type RunRequest struct {
	Params map[string]any `json:"params,omitempty"`
	Limit  int            `json:"limit,omitempty" maximum:"50000"`
}

// AdHocRequest runs an unsaved visual model or (builders) raw SQL.
type AdHocRequest struct {
	Model  *query.Model   `json:"model,omitempty"`
	SQL    string         `json:"sql,omitempty"`
	Params map[string]any `json:"params,omitempty"`
	Limit  int            `json:"limit,omitempty" maximum:"50000"`
}

// CompileResult shows the SQL a visual model produces.
type CompileResult struct {
	SQL      string     `json:"sql"`
	Columns  []string   `json:"columns"`
	Editable bool       `json:"editable"`
	TableID  string     `json:"table_id,omitempty"`
	Deps     query.Deps `json:"deps"`
}

func (s *Server) policy(ctx context.Context) query.Policy {
	m, _ := MembershipFrom(ctx)
	return query.Policy{Builder: m.Role.AtLeast(admin.RoleBuilder)}
}

// decodeParams re-decodes params with UseNumber for exact numerics.
func decodeParams(raw []byte) (query.Params, int, *query.Model, string, error) {
	var body struct {
		Params map[string]any `json:"params"`
		Limit  int            `json:"limit"`
		Model  *query.Model   `json:"model"`
		SQL    string         `json:"sql"`
	}
	if len(raw) == 0 {
		return nil, 0, nil, "", nil
	}
	if err := decodeJSON(raw, &body); err != nil {
		return nil, 0, nil, "", err
	}
	return query.Params(body.Params), body.Limit, body.Model, body.SQL, nil
}

func (s *Server) registerQueries(api huma.API) {
	tags := []string{"queries"}
	builder := map[string]any{metaRole: string(admin.RoleBuilder)}
	editor := map[string]any{metaRole: string(admin.RoleEditor)}
	type savedOut struct {
		ETag string `header:"ETag"`
		Body query.Saved
	}

	huma.Register(api, huma.Operation{OperationID: "listQueries", Method: http.MethodGet, Path: "/api/v1/w/{ws}/queries", Tags: tags,
		Summary: "Saved queries", Errors: []int{401, 404}},
		func(ctx context.Context, _ *struct {
			WS string `path:"ws"`
		}) (*struct {
			Body struct {
				Items []query.Saved `json:"items"`
			}
		}, error) {
			l, err := s.lease(ctx)
			if err != nil {
				return nil, err
			}
			defer l.Release()
			qs, err := s.deps.Queries.List(ctx, l)
			if err != nil {
				return nil, err
			}
			out := &struct {
				Body struct {
					Items []query.Saved `json:"items"`
				}
			}{}
			out.Body.Items = append([]query.Saved{}, qs...)
			return out, nil
		})

	huma.Register(api, huma.Operation{OperationID: "getQuery", Method: http.MethodGet, Path: "/api/v1/w/{ws}/queries/{id}", Tags: tags,
		Summary: "A saved query (ETag = version)", Errors: []int{401, 404}},
		func(ctx context.Context, in *struct {
			WS string `path:"ws"`
			ID string `path:"id"`
		}) (*savedOut, error) {
			l, err := s.lease(ctx)
			if err != nil {
				return nil, err
			}
			defer l.Release()
			q, err := s.deps.Queries.Get(ctx, l, in.ID)
			if err != nil {
				return nil, err
			}
			return &savedOut{ETag: etag(q.Version), Body: q}, nil
		})

	huma.Register(api, huma.Operation{OperationID: "createQuery", Method: http.MethodPost, Path: "/api/v1/w/{ws}/queries", Tags: tags, Metadata: builder,
		Summary: "Save a new query", DefaultStatus: http.StatusCreated, Errors: []int{400, 401, 403, 404, 409}},
		func(ctx context.Context, in *struct {
			WS   string `path:"ws"`
			Body query.SavedInput
		}) (*savedOut, error) {
			l, err := s.lease(ctx)
			if err != nil {
				return nil, err
			}
			defer l.Release()
			p, _ := PrincipalFrom(ctx)
			q, err := s.deps.Queries.Put(ctx, l, "", 0, in.Body, p.UserID)
			if err != nil {
				return nil, err
			}
			return &savedOut{ETag: etag(q.Version), Body: q}, nil
		})

	huma.Register(api, huma.Operation{OperationID: "updateQuery", Method: http.MethodPut, Path: "/api/v1/w/{ws}/queries/{id}", Tags: tags, Metadata: builder,
		Summary: "Replace a saved query (If-Match: version)", Errors: []int{400, 401, 403, 404, 409}},
		func(ctx context.Context, in *struct {
			WS      string `path:"ws"`
			ID      string `path:"id"`
			IfMatch string `header:"If-Match" required:"true"`
			Body    query.SavedInput
		}) (*savedOut, error) {
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
			q, err := s.deps.Queries.Put(ctx, l, in.ID, ver, in.Body, p.UserID)
			if err != nil {
				return nil, err
			}
			return &savedOut{ETag: etag(q.Version), Body: q}, nil
		})

	huma.Register(api, huma.Operation{OperationID: "deleteQuery", Method: http.MethodDelete, Path: "/api/v1/w/{ws}/queries/{id}", Tags: tags, Metadata: builder,
		Summary: "Delete a saved query (If-Match: version)", DefaultStatus: http.StatusNoContent, Errors: []int{400, 401, 403, 404, 409}},
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
			return nil, s.deps.Queries.Delete(ctx, l, in.ID, ver)
		})

	huma.Register(api, huma.Operation{OperationID: "compileQuery", Method: http.MethodPost, Path: "/api/v1/w/{ws}/queries/compile", Tags: tags, Metadata: editor,
		Summary: "Compile a visual model to SQL", Errors: []int{400, 401, 403, 404}},
		func(ctx context.Context, in *struct {
			WS   string `path:"ws"`
			Body struct {
				Model query.Model `json:"model"`
			}
			RawBody []byte
		}) (*struct{ Body CompileResult }, error) {
			_, _, m, _, err := decodeParams(in.RawBody)
			if err != nil {
				return nil, err
			}
			if m == nil {
				m = &in.Body.Model
			}
			l, err := s.lease(ctx)
			if err != nil {
				return nil, err
			}
			defer l.Release()
			c, err := s.deps.Queries.CompileModel(ctx, l, *m)
			if err != nil {
				return nil, err
			}
			return &struct{ Body CompileResult }{Body: CompileResult{SQL: c.SQL, Columns: c.Columns, Editable: c.Editable, TableID: c.TableID, Deps: c.Deps}}, nil
		})

	huma.Register(api, huma.Operation{OperationID: "runQuery", Method: http.MethodPost, Path: "/api/v1/w/{ws}/queries/{id}/run", Tags: tags,
		Summary: "Run a saved query with parameters", Errors: []int{400, 401, 403, 404, 503}},
		func(ctx context.Context, in *struct {
			WS      string `path:"ws"`
			ID      string `path:"id"`
			Body    RunRequest
			RawBody []byte
		}) (*struct{ Body query.Result }, error) {
			params, limit, _, _, err := decodeParams(in.RawBody)
			if err != nil {
				return nil, err
			}
			l, err := s.lease(ctx)
			if err != nil {
				return nil, err
			}
			defer l.Release()
			r, err := s.deps.Queries.RunSaved(ctx, l, s.policy(ctx), in.ID, params, limit)
			if err != nil {
				return nil, err
			}
			return &struct{ Body query.Result }{Body: r}, nil
		})

	huma.Register(api, huma.Operation{OperationID: "runAdHocQuery", Method: http.MethodPost, Path: "/api/v1/w/{ws}/queries/run", Tags: tags, Metadata: editor,
		Summary: "Run an unsaved visual model (editors can run but not save queries)", Errors: []int{400, 401, 403, 404, 503}},
		func(ctx context.Context, in *struct {
			WS      string `path:"ws"`
			Body    AdHocRequest
			RawBody []byte
		}) (*struct{ Body query.Result }, error) {
			params, limit, m, _, err := decodeParams(in.RawBody)
			if err != nil {
				return nil, err
			}
			if m == nil {
				return nil, errBadf("Send a visual model to run.")
			}
			l, err := s.lease(ctx)
			if err != nil {
				return nil, err
			}
			defer l.Release()
			c, err := s.deps.Queries.CompileModel(ctx, l, *m)
			if err != nil {
				return nil, err
			}
			r, err := query.Run(ctx, l, s.policy(ctx), c.SQL, params, limit)
			if err != nil {
				return nil, err
			}
			return &struct{ Body query.Result }{Body: r}, nil
		})

	huma.Register(api, huma.Operation{OperationID: "runSQL", Method: http.MethodPost, Path: "/api/v1/w/{ws}/sql/run", Tags: tags, Metadata: builder,
		Summary: "Raw read-only SQL console (builders)", Errors: []int{400, 401, 403, 404, 503}},
		func(ctx context.Context, in *struct {
			WS      string `path:"ws"`
			Body    AdHocRequest
			RawBody []byte
		}) (*struct{ Body query.Result }, error) {
			params, limit, _, sql, err := decodeParams(in.RawBody)
			if err != nil {
				return nil, err
			}
			l, err := s.lease(ctx)
			if err != nil {
				return nil, err
			}
			defer l.Release()
			r, err := query.Run(ctx, l, s.policy(ctx), sql, params, limit)
			if err != nil {
				return nil, err
			}
			return &struct{ Body query.Result }{Body: r}, nil
		})

	huma.Register(api, huma.Operation{OperationID: "usages", Method: http.MethodGet, Path: "/api/v1/w/{ws}/usages", Tags: tags,
		Summary: "Objects that use a table, column, query, script, form or report", Errors: []int{400, 401, 404}},
		func(ctx context.Context, in *struct {
			WS   string `path:"ws"`
			Kind string `query:"kind" enum:"table,column,query,script,form,report" required:"true"`
			ID   string `query:"id" required:"true"`
		}) (*struct {
			Body struct {
				Items []query.Usage `json:"items"`
			}
		}, error) {
			l, err := s.lease(ctx)
			if err != nil {
				return nil, err
			}
			defer l.Release()
			us, err := query.Usages(ctx, l, in.Kind, in.ID)
			if err != nil {
				return nil, err
			}
			out := &struct {
				Body struct {
					Items []query.Usage `json:"items"`
				}
			}{}
			out.Body.Items = us
			return out, nil
		})
}

func errBadf(msg string) error { return fault.New(fault.BadRequest, msg) }
