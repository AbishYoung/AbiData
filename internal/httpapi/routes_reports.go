package httpapi

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"abidata/internal/admin"
	"abidata/internal/reports"
)

func (s *Server) registerReports(api huma.API) {
	tags := []string{"reports"}
	builder := map[string]any{metaRole: string(admin.RoleBuilder)}
	type repOut struct {
		ETag string `header:"ETag"`
		Body reports.Report
	}
	huma.Register(api, huma.Operation{OperationID: "listReports", Method: http.MethodGet, Path: "/api/v1/w/{ws}/reports", Tags: tags, Summary: "Reports", Errors: []int{401, 404}},
		func(ctx context.Context, _ *struct {
			WS string `path:"ws"`
		}) (*struct {
			Body struct {
				Items []reports.Report `json:"items"`
			}
		}, error) {
			l, err := s.lease(ctx)
			if err != nil {
				return nil, err
			}
			defer l.Release()
			rs, err := s.deps.Reports.List(ctx, l)
			if err != nil {
				return nil, err
			}
			out := &struct {
				Body struct {
					Items []reports.Report `json:"items"`
				}
			}{}
			out.Body.Items = rs
			return out, nil
		})
	huma.Register(api, huma.Operation{OperationID: "getReport", Method: http.MethodGet, Path: "/api/v1/w/{ws}/reports/{id}", Tags: tags, Summary: "A report (ETag = version)", Errors: []int{401, 404}},
		func(ctx context.Context, in *struct {
			WS string `path:"ws"`
			ID string `path:"id"`
		}) (*repOut, error) {
			l, err := s.lease(ctx)
			if err != nil {
				return nil, err
			}
			defer l.Release()
			r, err := s.deps.Reports.Get(ctx, l, in.ID)
			if err != nil {
				return nil, err
			}
			if m, _ := MembershipFrom(ctx); !m.Role.AtLeast(admin.RoleBuilder) {
				r.ModuleSource = ""
			}
			return &repOut{ETag: etag(r.Version), Body: r}, nil
		})
	huma.Register(api, huma.Operation{OperationID: "createReport", Method: http.MethodPost, Path: "/api/v1/w/{ws}/reports", Tags: tags, Metadata: builder,
		Summary: "Create a report", DefaultStatus: http.StatusCreated, Errors: []int{400, 401, 403, 404, 409, 422}},
		func(ctx context.Context, in *struct {
			WS   string `path:"ws"`
			Body reports.ReportInput
		}) (*repOut, error) {
			l, err := s.lease(ctx)
			if err != nil {
				return nil, err
			}
			defer l.Release()
			p, _ := PrincipalFrom(ctx)
			r, err := s.deps.Reports.Put(ctx, l, "", 0, in.Body, p.UserID)
			if err != nil {
				return nil, err
			}
			return &repOut{ETag: etag(r.Version), Body: r}, nil
		})
	huma.Register(api, huma.Operation{OperationID: "updateReport", Method: http.MethodPut, Path: "/api/v1/w/{ws}/reports/{id}", Tags: tags, Metadata: builder,
		Summary: "Replace a report (If-Match: version)", Errors: []int{400, 401, 403, 404, 409, 422}},
		func(ctx context.Context, in *struct {
			WS      string `path:"ws"`
			ID      string `path:"id"`
			IfMatch string `header:"If-Match" required:"true"`
			Body    reports.ReportInput
		}) (*repOut, error) {
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
			r, err := s.deps.Reports.Put(ctx, l, in.ID, ver, in.Body, p.UserID)
			if err != nil {
				return nil, err
			}
			return &repOut{ETag: etag(r.Version), Body: r}, nil
		})
	huma.Register(api, huma.Operation{OperationID: "deleteReport", Method: http.MethodDelete, Path: "/api/v1/w/{ws}/reports/{id}", Tags: tags, Metadata: builder,
		Summary: "Delete a report (If-Match: version)", DefaultStatus: http.StatusNoContent, Errors: []int{400, 401, 403, 404, 409}},
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
			return nil, s.deps.Reports.Delete(ctx, l, in.ID, ver)
		})
	huma.Register(api, huma.Operation{OperationID: "runReport", Method: http.MethodPost, Path: "/api/v1/w/{ws}/reports/{id}/run", Tags: tags,
		Summary:     "Run a report: NDJSON stream of section instances {band, group_level, template, values, row}",
		Description: "The first two lines are the page header/footer templates. Errors before streaming starts use the error envelope; errors after are a final line {\"error\":{…}}.",
		Errors:      []int{400, 401, 403, 404, 422, 503}},
		func(ctx context.Context, in *struct {
			WS      string `path:"ws"`
			ID      string `path:"id"`
			Body    RunRequest
			RawBody []byte
		}) (*huma.StreamResponse, error) {
			params, _, _, _, err := decodeParams(in.RawBody)
			if err != nil {
				return nil, err
			}
			l, err := s.lease(ctx)
			if err != nil {
				return nil, err
			}
			rep, err := s.deps.Reports.Get(ctx, l, in.ID)
			if err != nil {
				l.Release()
				return nil, err
			}
			sch, err := s.deps.Schemas.Get(ctx, l)
			if err != nil {
				l.Release()
				return nil, err
			}
			p, _ := PrincipalFrom(ctx)
			m, _ := MembershipFrom(ctx)
			tz := workspaceTZ(ctx, l)
			return &huma.StreamResponse{Body: func(hctx huma.Context) {
				defer l.Release()
				hctx.SetHeader("Content-Type", "application/x-ndjson")
				hctx.SetHeader("Cache-Control", "no-store")
				hctx.SetHeader("X-Accel-Buffering", "no")
				w := hctx.BodyWriter()
				enc := json.NewEncoder(w)
				flusher, _ := w.(http.Flusher)
				n := 0
				bctx, stop := l.Bind(hctx.Context())
				defer stop()
				err := s.deps.Reports.Run(bctx, reports.RunInput{Lease: l, Schema: sch, Report: rep, Params: params,
					Caller: reports.Caller{UserID: p.UserID, Builder: m.Role.AtLeast(admin.RoleBuilder), TZ: tz},
					Emit: func(inst reports.Instance) error {
						if err := enc.Encode(inst); err != nil {
							return err
						}
						n++
						if n%200 == 0 && flusher != nil {
							flusher.Flush()
						}
						return nil
					}})
				if err != nil {
					ae := toAPIError(hctx.Context(), s.log, err)
					if n == 0 {
						hctx.SetStatus(ae.status)
					}
					_ = enc.Encode(ae)
				}
				if flusher != nil {
					flusher.Flush()
				}
			}}, nil
		})
}
