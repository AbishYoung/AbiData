package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"abidata/internal/admin"
	"abidata/internal/auth"
	"abidata/internal/catalog"
	"abidata/internal/events"
	"abidata/internal/fault"
	"abidata/internal/records"
	"abidata/internal/workspace"
)

// lease acquires the workspace named by the caller's membership.
func (s *Server) lease(ctx context.Context) (*workspace.Lease, error) {
	m, ok := MembershipFrom(ctx)
	if !ok {
		return nil, errNotFound
	}
	l, err := s.deps.Workspaces.Acquire(ctx, m.Workspace.ID)
	if err != nil {
		return nil, fault.Classify(err)
	}
	return l, nil
}

func idempotency(ctx context.Context, key string) *records.Idempotency {
	if key == "" {
		return nil
	}
	p, _ := PrincipalFrom(ctx)
	return &records.Idempotency{Key: key, UserID: p.UserID}
}

func actor(ctx context.Context) records.Actor {
	p, _ := PrincipalFrom(ctx)
	return records.Actor{UserID: p.UserID}
}

// decodeJSON decodes with UseNumber so currency decimals and big integers keep
// full precision (huma's typed Body would go through float64).
func decodeJSON(raw []byte, v any) error {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	if err := dec.Decode(v); err != nil {
		return fault.Newf(fault.BadRequest, "The request body is not valid JSON: %v", err)
	}
	return nil
}

func etag(v int64) string { return `"` + strconv.FormatInt(v, 10) + `"` }

func parseIfMatch(h string) (int64, error) {
	h = strings.TrimSpace(strings.TrimPrefix(h, "W/"))
	v, err := strconv.ParseInt(strings.Trim(h, `"`), 10, 64)
	if err != nil || v <= 0 {
		return 0, fault.New(fault.BadRequest, "If-Match must carry the record version you edited.")
	}
	return v, nil
}

// SchemaChangeRequest is the body of preview/apply.
type SchemaChangeRequest struct {
	Ops         []catalog.Op `json:"ops" maxItems:"200"`
	Force       bool         `json:"force,omitempty" doc:"Builder acknowledged the breaking issues in the impact report."`
	BaseVersion int64        `json:"base_version,omitempty" doc:"schema_version the change was designed against; a mismatch fails with CONCURRENT_MODIFICATION."`
}

// SchemaApplyResult is returned by apply.
type SchemaApplyResult struct {
	Report catalog.ImpactReport `json:"report"`
	Schema *catalog.Schema      `json:"schema"`
}

// RowWrite is the body for insert/update.
type RowWrite struct {
	Fields map[string]any `json:"fields" doc:"Values keyed by column ID, in the API representation of each field type."`
}

// RowPage is a page of rows.
type RowPage struct {
	Items      []records.Row `json:"items"`
	NextOffset *int          `json:"next_offset,omitempty"`
}

func (s *Server) registerData(api huma.API) {
	tags := []string{"data"}
	builder := map[string]any{metaRole: string(admin.RoleBuilder)}
	editor := map[string]any{metaRole: string(admin.RoleEditor)}

	huma.Register(api, huma.Operation{
		OperationID: "getSchema", Method: http.MethodGet, Path: "/api/v1/w/{ws}/schema", Tags: []string{"schema"},
		Summary: "Catalog snapshot (ETag = schema_version)", Errors: []int{401, 404},
	}, func(ctx context.Context, in *struct {
		WS          string `path:"ws"`
		IfNoneMatch string `header:"If-None-Match"`
	}) (*struct {
		ETag string `header:"ETag"`
		Body *catalog.Schema
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
		return &struct {
			ETag string `header:"ETag"`
			Body *catalog.Schema
		}{ETag: etag(sch.Version), Body: sch}, nil
	})

	schemaRun := func(mode catalog.Mode) func(ctx context.Context, in *struct {
		WS   string `path:"ws"`
		Body SchemaChangeRequest
	}) (*struct{ Body SchemaApplyResult }, error) {
		return func(ctx context.Context, in *struct {
			WS   string `path:"ws"`
			Body SchemaChangeRequest
		}) (*struct{ Body SchemaApplyResult }, error) {
			l, err := s.lease(ctx)
			if err != nil {
				return nil, err
			}
			defer l.Release()
			p, _ := PrincipalFrom(ctx)
			rep, err := s.deps.Catalog.Run(ctx, l, in.Body.Ops, mode, catalog.ApplyOptions{Force: in.Body.Force, UserID: p.UserID, BaseVersion: in.Body.BaseVersion})
			if err != nil {
				return nil, err
			}
			out := &struct{ Body SchemaApplyResult }{Body: SchemaApplyResult{Report: rep}}
			if mode == catalog.Apply {
				if out.Body.Schema, err = s.deps.Schemas.Get(ctx, l); err != nil {
					return nil, err
				}
			}
			return out, nil
		}
	}
	huma.Register(api, huma.Operation{
		OperationID: "previewSchema", Method: http.MethodPost, Path: "/api/v1/w/{ws}/schema/preview", Tags: []string{"schema"}, Metadata: builder,
		Summary: "Run schema changes and roll back, returning the impact report", Errors: []int{400, 401, 403, 404, 409},
	}, schemaRun(catalog.Preview))
	huma.Register(api, huma.Operation{
		OperationID: "applySchema", Method: http.MethodPost, Path: "/api/v1/w/{ws}/schema/apply", Tags: []string{"schema"}, Metadata: builder,
		Summary: "Apply schema changes (blocked with SCHEMA_CHANGE_BLOCKED unless force)", Errors: []int{400, 401, 403, 404, 409},
	}, schemaRun(catalog.Apply))

	huma.Register(api, huma.Operation{
		OperationID: "listRows", Method: http.MethodGet, Path: "/api/v1/w/{ws}/tables/{table}/rows", Tags: tags,
		Summary: "A page of rows", Errors: []int{400, 401, 404},
	}, func(ctx context.Context, in *struct {
		WS     string `path:"ws"`
		Table  string `path:"table"`
		Offset int    `query:"offset" minimum:"0"`
		Limit  int    `query:"limit" minimum:"1" maximum:"500" default:"200"`
		Sort   string `query:"sort" doc:"Comma-separated column IDs; prefix '-' for descending."`
		Filter string `query:"filter" doc:"JSON filter model."`
	}) (*struct {
		CacheControl string `header:"Cache-Control"`
		Body         RowPage
	}, error) {
		q, err := parseListQuery(in.Offset, in.Limit, in.Sort, in.Filter)
		if err != nil {
			return nil, err
		}
		l, err := s.lease(ctx)
		if err != nil {
			return nil, err
		}
		defer l.Release()
		rows, more, err := s.deps.Records.List(ctx, l, in.Table, q)
		if err != nil {
			return nil, err
		}
		out := &struct {
			CacheControl string `header:"Cache-Control"`
			Body         RowPage
		}{CacheControl: "no-store", Body: RowPage{Items: rows}}
		if more {
			next := in.Offset + len(rows)
			out.Body.NextOffset = &next
		}
		return out, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "countRows", Method: http.MethodGet, Path: "/api/v1/w/{ws}/tables/{table}/rows/count", Tags: tags,
		Summary: "Row count, capped at 100,000", Errors: []int{400, 401, 404},
	}, func(ctx context.Context, in *struct {
		WS     string `path:"ws"`
		Table  string `path:"table"`
		Filter string `query:"filter"`
	}) (*struct {
		Body struct {
			Count  int64 `json:"count"`
			Capped bool  `json:"capped"`
		}
	}, error) {
		q, err := parseListQuery(0, 1, "", in.Filter)
		if err != nil {
			return nil, err
		}
		l, err := s.lease(ctx)
		if err != nil {
			return nil, err
		}
		defer l.Release()
		n, capped, err := s.deps.Records.Count(ctx, l, in.Table, q.Filter)
		if err != nil {
			return nil, err
		}
		out := &struct {
			Body struct {
				Count  int64 `json:"count"`
				Capped bool  `json:"capped"`
			}
		}{}
		out.Body.Count, out.Body.Capped = n, capped
		return out, nil
	})

	type rowOut struct {
		ETag string `header:"ETag"`
		Body records.Row
	}
	huma.Register(api, huma.Operation{
		OperationID: "getRow", Method: http.MethodGet, Path: "/api/v1/w/{ws}/tables/{table}/rows/{id}", Tags: tags,
		Summary: "One row (ETag = _version)", Errors: []int{401, 404},
	}, func(ctx context.Context, in *struct {
		WS    string `path:"ws"`
		Table string `path:"table"`
		ID    int64  `path:"id"`
	}) (*rowOut, error) {
		l, err := s.lease(ctx)
		if err != nil {
			return nil, err
		}
		defer l.Release()
		r, err := s.deps.Records.Get(ctx, l, in.Table, in.ID)
		if err != nil {
			return nil, err
		}
		return &rowOut{ETag: etag(r.Version), Body: r}, nil
	})

	type historyItem struct {
		records.HistoryEntry
		UserName string `json:"user_name,omitempty" doc:"Display name of a current member; empty for system changes or former members."`
	}
	huma.Register(api, huma.Operation{
		OperationID: "rowHistory", Method: http.MethodGet, Path: "/api/v1/w/{ws}/tables/{table}/rows/{id}/history", Tags: tags,
		Summary:     "A row's recorded changes, newest first (tables with history tracking)",
		Description: "Pages with `before` = the last entry's id. Readers of the table can read its history, including the history of deleted rows.",
		Errors:      []int{401, 404},
	}, func(ctx context.Context, in *struct {
		WS     string `path:"ws"`
		Table  string `path:"table"`
		ID     int64  `path:"id"`
		Before int64  `query:"before" minimum:"0"`
		Limit  int    `query:"limit" minimum:"0" maximum:"100"`
	}) (*struct {
		Body struct {
			Tracked bool          `json:"tracked" doc:"Whether the table currently records history."`
			Items   []historyItem `json:"items"`
			More    bool          `json:"more"`
		}
	}, error) {
		l, err := s.lease(ctx)
		if err != nil {
			return nil, err
		}
		defer l.Release()
		_, t, err := s.deps.Records.Table(ctx, l, in.Table)
		if err != nil {
			return nil, err
		}
		entries, more, err := s.deps.Records.History(ctx, l, in.Table, in.ID, in.Before, in.Limit)
		if err != nil {
			return nil, err
		}
		names := map[string]string{}
		if ms, err := s.deps.Admin.ListMembers(ctx, l.ID()); err == nil {
			for _, m := range ms {
				names[m.UserID] = m.DisplayName
			}
		}
		// Writes made through an API key are attributed to "apikey:<id>".
		for _, e := range entries {
			if auth.IsAPIKeyActor(e.UserID) {
				if ks, err := s.deps.Admin.ListAPIKeys(ctx, l.ID()); err == nil {
					for _, k := range ks {
						names[auth.ActorID(k.ID)] = auth.APIKeyDisplayName(k.Name)
					}
				}
				break
			}
		}
		out := &struct {
			Body struct {
				Tracked bool          `json:"tracked" doc:"Whether the table currently records history."`
				Items   []historyItem `json:"items"`
				More    bool          `json:"more"`
			}
		}{}
		out.Body.Tracked, out.Body.More = t.TrackHistory, more
		out.Body.Items = make([]historyItem, 0, len(entries))
		for _, e := range entries {
			out.Body.Items = append(out.Body.Items, historyItem{HistoryEntry: e, UserName: names[e.UserID]})
		}
		return out, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "insertRow", Method: http.MethodPost, Path: "/api/v1/w/{ws}/tables/{table}/rows", Tags: tags, Metadata: editor,
		Summary: "Insert a row", DefaultStatus: http.StatusCreated, Errors: []int{400, 401, 403, 404, 409, 422, 503},
	}, func(ctx context.Context, in *struct {
		WS             string `path:"ws"`
		Table          string `path:"table"`
		IdempotencyKey string `header:"Idempotency-Key"`
		Body           RowWrite
		RawBody        []byte
	}) (*rowOut, error) {
		var body RowWrite
		if err := decodeJSON(in.RawBody, &body); err != nil {
			return nil, err
		}
		l, err := s.lease(ctx)
		if err != nil {
			return nil, err
		}
		defer l.Release()
		r, err := s.deps.Records.Insert(ctx, l, in.Table, body.Fields, actor(ctx), idempotency(ctx, in.IdempotencyKey))
		if err != nil {
			return nil, err
		}
		return &rowOut{ETag: etag(r.Version), Body: r}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "updateRow", Method: http.MethodPatch, Path: "/api/v1/w/{ws}/tables/{table}/rows/{id}", Tags: tags, Metadata: editor,
		Summary: "Update fields of a row (If-Match: version)", Errors: []int{400, 401, 403, 404, 409, 422, 503},
	}, func(ctx context.Context, in *struct {
		WS             string `path:"ws"`
		Table          string `path:"table"`
		ID             int64  `path:"id"`
		IfMatch        string `header:"If-Match" required:"true"`
		IdempotencyKey string `header:"Idempotency-Key"`
		Body           RowWrite
		RawBody        []byte
	}) (*rowOut, error) {
		ver, err := parseIfMatch(in.IfMatch)
		if err != nil {
			return nil, err
		}
		var body RowWrite
		if err := decodeJSON(in.RawBody, &body); err != nil {
			return nil, err
		}
		l, err := s.lease(ctx)
		if err != nil {
			return nil, err
		}
		defer l.Release()
		r, err := s.deps.Records.Update(ctx, l, in.Table, in.ID, ver, body.Fields, actor(ctx), idempotency(ctx, in.IdempotencyKey))
		if err != nil {
			return nil, err
		}
		return &rowOut{ETag: etag(r.Version), Body: r}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "deleteRow", Method: http.MethodDelete, Path: "/api/v1/w/{ws}/tables/{table}/rows/{id}", Tags: tags, Metadata: editor,
		Summary: "Delete a row (If-Match: version)", DefaultStatus: http.StatusNoContent, Errors: []int{400, 401, 403, 404, 409, 503},
	}, func(ctx context.Context, in *struct {
		WS             string `path:"ws"`
		Table          string `path:"table"`
		ID             int64  `path:"id"`
		IfMatch        string `header:"If-Match" required:"true"`
		IdempotencyKey string `header:"Idempotency-Key"`
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
		return nil, s.deps.Records.Delete(ctx, l, in.Table, in.ID, ver, actor(ctx), idempotency(ctx, in.IdempotencyKey))
	})

	huma.Register(api, huma.Operation{
		OperationID: "batchRows", Method: http.MethodPost, Path: "/api/v1/w/{ws}/tables/{table}/rows/batch", Tags: tags, Metadata: editor,
		Summary: "All-or-nothing batch of inserts/updates/deletes", Errors: []int{400, 401, 403, 404, 409, 422, 503},
	}, func(ctx context.Context, in *struct {
		WS             string `path:"ws"`
		Table          string `path:"table"`
		IdempotencyKey string `header:"Idempotency-Key"`
		Body           struct {
			Ops []records.BatchOp `json:"ops" maxItems:"1000"`
		}
		RawBody []byte
	}) (*struct {
		Body struct {
			Results []records.BatchResult `json:"results"`
		}
	}, error) {
		var body struct {
			Ops []records.BatchOp `json:"ops"`
		}
		if err := decodeJSON(in.RawBody, &body); err != nil {
			return nil, err
		}
		l, err := s.lease(ctx)
		if err != nil {
			return nil, err
		}
		defer l.Release()
		res, err := s.deps.Records.Batch(ctx, l, in.Table, body.Ops, actor(ctx), idempotency(ctx, in.IdempotencyKey))
		if err != nil {
			return nil, err
		}
		out := &struct {
			Body struct {
				Results []records.BatchResult `json:"results"`
			}
		}{}
		out.Body.Results = res
		return out, nil
	})
}

func parseListQuery(offset, limit int, sortParam, filterParam string) (records.ListQuery, error) {
	q := records.ListQuery{Offset: offset, Limit: limit}
	if sortParam != "" {
		for _, part := range strings.Split(sortParam, ",") {
			part = strings.TrimSpace(part)
			if part == "" {
				continue
			}
			desc := strings.HasPrefix(part, "-")
			q.Sort = append(q.Sort, records.Sort{Col: strings.TrimPrefix(part, "-"), Desc: desc})
		}
	}
	if filterParam != "" {
		var f records.Filter
		if err := decodeJSON([]byte(filterParam), &f); err != nil {
			return q, err
		}
		q.Filter = &f
	}
	return q, nil
}

// keyStreamRecheck is how often a bearer-authenticated event stream re-reads
// its key: revoking a key ends its streams within this interval.
const keyStreamRecheck = 30 * time.Second

// eventsHandler serves the per-workspace SSE stream (plain chi route: huma
// does not model streaming). Authorization mirrors the huma middleware: an
// Authorization header means API-key auth only (cookies ignored).
func (s *Server) eventsHandler(w http.ResponseWriter, r *http.Request) {
	ws := r.PathValue("ws")
	if ws == "" {
		ws = chiParam(r, "ws")
	}
	var resolve func(ctx context.Context) (Membership, error)
	recheck := 5 * time.Minute
	if authz := r.Header.Get("Authorization"); isBearer(authz) {
		recheck = keyStreamRecheck
		if s.keyRecheck > 0 {
			recheck = s.keyRecheck
		}
		resolve = func(ctx context.Context) (Membership, error) {
			p, err := s.deps.Auth.AuthenticateAPIKey(ctx, authz, clientIP(r.Context()))
			if err != nil {
				return Membership{}, err
			}
			return s.resolveKeyMembership(ctx, p, ws)
		}
	} else {
		c, err := r.Cookie(s.cookieName())
		if err != nil {
			writeError(w, r, s.log, errUnauth)
			return
		}
		resolve = func(ctx context.Context) (Membership, error) {
			p, err := s.deps.Auth.Authenticate(ctx, c.Value)
			if err != nil {
				return Membership{}, err
			}
			return s.resolveMembership(ctx, p, ws)
		}
	}
	m, err := resolve(r.Context())
	if err != nil {
		writeError(w, r, s.log, err)
		return
	}
	// Job runs are builder-only data everywhere else (their errors carry script
	// text), so job.run events go to builders and owners only.
	var builder atomic.Bool
	builder.Store(m.Role.AtLeast(admin.RoleBuilder))
	events.Stream(w, r, s.deps.Events.Bus(string(m.Workspace.ID)), events.StreamOptions{
		Authorize: func(ctx context.Context) error {
			m2, err := resolve(ctx)
			if err == nil {
				builder.Store(m2.Role.AtLeast(admin.RoleBuilder))
			}
			return err
		},
		Recheck: recheck,
		Filter:  func(ev events.Event) bool { return ev.Type != events.JobRun || builder.Load() },
	})
}
