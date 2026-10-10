package httpapi

import (
	"context"
	"io"
	"mime"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"abidata/internal/admin"
	"abidata/internal/artifacts"
	"abidata/internal/fault"
	"abidata/internal/ids"
	"abidata/internal/jobs"
	"abidata/internal/ops"
	"abidata/internal/workspace"
)

// RunDetail is a run with its artifacts.
type RunDetail struct {
	jobs.Run
	Log       string           `json:"log"`
	Artifacts []artifacts.Info `json:"artifacts"`
}

func (s *Server) registerJobs(api huma.API) {
	tags := []string{"jobs"}
	builder := map[string]any{metaRole: string(admin.RoleBuilder)}
	type jobOut struct {
		ETag string `header:"ETag"`
		Body jobs.Job
	}
	type itemsJobs struct {
		Body struct {
			Items []jobs.Job `json:"items"`
		}
	}
	need := func() error {
		if s.deps.Jobs == nil {
			return fault.New(fault.NotFound, "Jobs are not enabled on this server.")
		}
		return nil
	}

	huma.Register(api, huma.Operation{OperationID: "listJobs", Method: http.MethodGet, Path: "/api/v1/w/{ws}/jobs", Tags: tags, Metadata: builder,
		Summary: "Jobs", Errors: []int{401, 403, 404}},
		func(ctx context.Context, _ *struct {
			WS string `path:"ws"`
		}) (*itemsJobs, error) {
			if err := need(); err != nil {
				return nil, err
			}
			l, err := s.lease(ctx)
			if err != nil {
				return nil, err
			}
			defer l.Release()
			js, err := s.deps.Jobs.List(ctx, l)
			if err != nil {
				return nil, err
			}
			out := &itemsJobs{}
			out.Body.Items = js
			return out, nil
		})
	huma.Register(api, huma.Operation{OperationID: "getJob", Method: http.MethodGet, Path: "/api/v1/w/{ws}/jobs/{id}", Tags: tags, Metadata: builder,
		Summary: "A job (ETag = version)", Errors: []int{401, 403, 404}},
		func(ctx context.Context, in *struct {
			WS string `path:"ws"`
			ID string `path:"id"`
		}) (*jobOut, error) {
			if err := need(); err != nil {
				return nil, err
			}
			l, err := s.lease(ctx)
			if err != nil {
				return nil, err
			}
			defer l.Release()
			j, err := s.deps.Jobs.Get(ctx, l, in.ID)
			if err != nil {
				return nil, err
			}
			return &jobOut{ETag: etag(j.Version), Body: j}, nil
		})
	huma.Register(api, huma.Operation{OperationID: "createJob", Method: http.MethodPost, Path: "/api/v1/w/{ws}/jobs", Tags: tags, Metadata: builder,
		Summary: "Create a job", DefaultStatus: http.StatusCreated, Errors: []int{400, 401, 403, 404, 409, 422}},
		func(ctx context.Context, in *struct {
			WS   string `path:"ws"`
			Body jobs.JobInput
		}) (*jobOut, error) {
			if err := need(); err != nil {
				return nil, err
			}
			l, err := s.lease(ctx)
			if err != nil {
				return nil, err
			}
			defer l.Release()
			p, _ := PrincipalFrom(ctx)
			j, err := s.deps.Jobs.Put(ctx, l, "", 0, in.Body, p.UserID)
			if err != nil {
				return nil, err
			}
			return &jobOut{ETag: etag(j.Version), Body: j}, nil
		})
	huma.Register(api, huma.Operation{OperationID: "updateJob", Method: http.MethodPut, Path: "/api/v1/w/{ws}/jobs/{id}", Tags: tags, Metadata: builder,
		Summary: "Replace a job (If-Match: version)", Errors: []int{400, 401, 403, 404, 409, 422}},
		func(ctx context.Context, in *struct {
			WS      string `path:"ws"`
			ID      string `path:"id"`
			IfMatch string `header:"If-Match" required:"true"`
			Body    jobs.JobInput
		}) (*jobOut, error) {
			if err := need(); err != nil {
				return nil, err
			}
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
			j, err := s.deps.Jobs.Put(ctx, l, in.ID, ver, in.Body, p.UserID)
			if err != nil {
				return nil, err
			}
			return &jobOut{ETag: etag(j.Version), Body: j}, nil
		})
	huma.Register(api, huma.Operation{OperationID: "deleteJob", Method: http.MethodDelete, Path: "/api/v1/w/{ws}/jobs/{id}", Tags: tags, Metadata: builder,
		Summary: "Delete a job and its run history (If-Match: version)", DefaultStatus: http.StatusNoContent, Errors: []int{400, 401, 403, 404, 409}},
		func(ctx context.Context, in *struct {
			WS      string `path:"ws"`
			ID      string `path:"id"`
			IfMatch string `header:"If-Match" required:"true"`
		}) (*struct{}, error) {
			if err := need(); err != nil {
				return nil, err
			}
			ver, err := parseIfMatch(in.IfMatch)
			if err != nil {
				return nil, err
			}
			l, err := s.lease(ctx)
			if err != nil {
				return nil, err
			}
			defer l.Release()
			return nil, s.deps.Jobs.Delete(ctx, l, in.ID, ver)
		})
	huma.Register(api, huma.Operation{OperationID: "runJob", Method: http.MethodPost, Path: "/api/v1/w/{ws}/jobs/{id}/run", Tags: tags, Metadata: builder,
		Summary: "Run a job now", DefaultStatus: http.StatusAccepted, Errors: []int{400, 401, 403, 404, 409}},
		func(ctx context.Context, in *struct {
			WS   string `path:"ws"`
			ID   string `path:"id"`
			Body *struct {
				Args map[string]any `json:"args,omitempty" doc:"Merged over the job's args for this run."`
			}
		}) (*struct{ Body jobs.Run }, error) {
			if err := need(); err != nil {
				return nil, err
			}
			m, _ := MembershipFrom(ctx)
			p, _ := PrincipalFrom(ctx)
			var args map[string]any
			if in.Body != nil {
				args = in.Body.Args
			}
			run, err := s.deps.Jobs.Scheduler.RunNow(ctx, m.Workspace.ID, in.ID, jobs.TriggerManual, p.UserID, args)
			if err != nil {
				return nil, err
			}
			return &struct{ Body jobs.Run }{Body: run}, nil
		})
	huma.Register(api, huma.Operation{OperationID: "listJobRuns", Method: http.MethodGet, Path: "/api/v1/w/{ws}/jobs/{id}/runs", Tags: tags, Metadata: builder,
		Summary: "A job's runs, newest first", Errors: []int{401, 403, 404}},
		func(ctx context.Context, in *struct {
			WS     string    `path:"ws"`
			ID     string    `path:"id"`
			Limit  int       `query:"limit" minimum:"0" maximum:"200"`
			Before time.Time `query:"before" doc:"Cursor: queued_at of the last run of the previous page."`
		}) (*struct {
			Body struct {
				Items      []jobs.Run `json:"items"`
				NextCursor string     `json:"next_cursor,omitempty"`
			}
		}, error) {
			if err := need(); err != nil {
				return nil, err
			}
			l, err := s.lease(ctx)
			if err != nil {
				return nil, err
			}
			defer l.Release()
			if _, err := s.deps.Jobs.Get(ctx, l, in.ID); err != nil {
				return nil, err
			}
			limit := in.Limit
			if limit == 0 {
				limit = 50
			}
			runs, err := s.deps.Jobs.Runs(ctx, l, in.ID, limit, in.Before)
			if err != nil {
				return nil, err
			}
			out := &struct {
				Body struct {
					Items      []jobs.Run `json:"items"`
					NextCursor string     `json:"next_cursor,omitempty"`
				}
			}{}
			out.Body.Items = runs
			if len(runs) == limit {
				out.Body.NextCursor = runs[len(runs)-1].QueuedAt.Format(time.RFC3339Nano)
			}
			return out, nil
		})
	huma.Register(api, huma.Operation{OperationID: "getJobRun", Method: http.MethodGet, Path: "/api/v1/w/{ws}/runs/{run}", Tags: tags, Metadata: builder,
		Summary: "A run with its log and artifacts", Errors: []int{401, 403, 404}},
		func(ctx context.Context, in *struct {
			WS  string `path:"ws"`
			Run string `path:"run"`
		}) (*struct{ Body RunDetail }, error) {
			if err := need(); err != nil {
				return nil, err
			}
			l, err := s.lease(ctx)
			if err != nil {
				return nil, err
			}
			defer l.Release()
			r, err := s.deps.Jobs.GetRun(ctx, l, in.Run)
			if err != nil {
				return nil, err
			}
			d := RunDetail{Run: r, Log: r.Log, Artifacts: []artifacts.Info{}}
			if s.deps.Artifacts != nil {
				if list, err := s.deps.Artifacts.List(l.ID(), r.ID); err == nil {
					d.Artifacts = list
				}
			}
			return &struct{ Body RunDetail }{Body: d}, nil
		})
	huma.Register(api, huma.Operation{OperationID: "cancelJobRun", Method: http.MethodPost, Path: "/api/v1/w/{ws}/runs/{run}/cancel", Tags: tags, Metadata: builder,
		Summary: "Cancel a running or queued run", DefaultStatus: http.StatusAccepted, Errors: []int{401, 403, 404, 409}},
		func(ctx context.Context, in *struct {
			WS  string `path:"ws"`
			Run string `path:"run"`
		}) (*struct{}, error) {
			if err := need(); err != nil {
				return nil, err
			}
			l, err := s.lease(ctx)
			if err != nil {
				return nil, err
			}
			defer l.Release()
			r, err := s.deps.Jobs.GetRun(ctx, l, in.Run)
			if err != nil {
				return nil, err
			}
			if r.FinishedAt != nil {
				return nil, fault.New(fault.BadRequest, "The run has already finished.")
			}
			if s.deps.Jobs.Scheduler.Cancel(l.ID(), r.ID) {
				return nil, nil
			}
			if ok, err := s.deps.Jobs.Scheduler.CancelQueued(ctx, l.ID(), r.JobID, r.ID); err != nil || ok {
				return nil, err
			}
			return nil, fault.New(fault.BadRequest, "The run is not running on this server.")
		})
	huma.Register(api, huma.Operation{OperationID: "getRunArtifact", Method: http.MethodGet, Path: "/api/v1/w/{ws}/runs/{run}/artifacts/{name}", Tags: tags, Metadata: builder,
		Summary: "Download a run artifact", Errors: []int{401, 403, 404}},
		func(ctx context.Context, in *struct {
			WS   string `path:"ws"`
			Run  string `path:"run"`
			Name string `path:"name"`
		}) (*huma.StreamResponse, error) {
			m, _ := MembershipFrom(ctx)
			return s.artifactStream(ctx, m.Workspace.ID, in.Run, in.Name)
		})

	// ---- operations ----------------------------------------------------------
	otags := []string{"operations"}
	huma.Register(api, huma.Operation{OperationID: "getOperation", Method: http.MethodGet, Path: "/api/v1/w/{ws}/operations/{id}", Tags: otags,
		Summary: "An operation's status and result (its creator or a builder)", Errors: []int{401, 404}},
		func(ctx context.Context, in *struct {
			WS string `path:"ws"`
			ID string `path:"id"`
		}) (*struct{ Body OperationDetail }, error) {
			op, l, err := s.ownOperation(ctx, in.ID)
			if err != nil {
				return nil, err
			}
			defer l.Release()
			d := OperationDetail{Op: op, Artifacts: []artifacts.Info{}}
			if s.deps.Artifacts != nil {
				if list, err := s.deps.Artifacts.List(l.ID(), op.ID); err == nil {
					d.Artifacts = list
				}
			}
			return &struct{ Body OperationDetail }{Body: d}, nil
		})
	huma.Register(api, huma.Operation{OperationID: "cancelOperation", Method: http.MethodPost, Path: "/api/v1/w/{ws}/operations/{id}/cancel", Tags: otags,
		Summary: "Cancel a running operation (its creator or a builder)", DefaultStatus: http.StatusAccepted, Errors: []int{401, 404, 409}},
		func(ctx context.Context, in *struct {
			WS string `path:"ws"`
			ID string `path:"id"`
		}) (*struct{}, error) {
			op, l, err := s.ownOperation(ctx, in.ID)
			if err != nil {
				return nil, err
			}
			defer l.Release()
			if op.FinishedAt != nil || !s.deps.Ops.Cancel(l.ID(), op.ID) {
				return nil, fault.New(fault.BadRequest, "The operation is not running.")
			}
			return nil, nil
		})
	huma.Register(api, huma.Operation{OperationID: "getOperationArtifact", Method: http.MethodGet, Path: "/api/v1/w/{ws}/operations/{id}/artifacts/{name}", Tags: otags,
		Summary: "Download an operation's artifact (e.g. an export)", Errors: []int{401, 404}},
		func(ctx context.Context, in *struct {
			WS   string `path:"ws"`
			ID   string `path:"id"`
			Name string `path:"name"`
		}) (*huma.StreamResponse, error) {
			op, l, err := s.ownOperation(ctx, in.ID)
			if err != nil {
				return nil, err
			}
			l.Release()
			m, _ := MembershipFrom(ctx)
			return s.artifactStream(ctx, m.Workspace.ID, op.ID, in.Name)
		})
}

// OperationDetail is an operation with its artifacts.
type OperationDetail struct {
	ops.Op
	Artifacts []artifacts.Info `json:"artifacts"`
}

// ownOperation loads an operation the caller may see: its creator, or a builder.
// Others get NOT_FOUND (no existence leak). The caller releases the lease.
func (s *Server) ownOperation(ctx context.Context, id string) (ops.Op, *workspace.Lease, error) {
	if s.deps.Ops == nil {
		return ops.Op{}, nil, fault.New(fault.NotFound, "Operation not found.")
	}
	l, err := s.lease(ctx)
	if err != nil {
		return ops.Op{}, nil, err
	}
	op, err := s.deps.Ops.Get(ctx, l, id)
	if err != nil {
		l.Release()
		return ops.Op{}, nil, err
	}
	m, _ := MembershipFrom(ctx)
	p, _ := PrincipalFrom(ctx)
	if op.CreatedBy != p.UserID && !m.Role.AtLeast(admin.RoleBuilder) {
		l.Release()
		return ops.Op{}, nil, fault.New(fault.NotFound, "Operation not found.")
	}
	return op, l, nil
}

// artifactStream serves an artifact as a download. It is always an attachment
// with a sandbox CSP, so HTML or SVG a script saved can never run in our origin.
func (s *Server) artifactStream(_ context.Context, ws ids.WorkspaceID, owner, name string) (*huma.StreamResponse, error) {
	if s.deps.Artifacts == nil {
		return nil, fault.New(fault.NotFound, "Artifact not found.")
	}
	f, info, err := s.deps.Artifacts.Open(ws, owner, name)
	if err != nil {
		return nil, err
	}
	return &huma.StreamResponse{Body: func(hctx huma.Context) {
		defer func() { _ = f.Close() }()
		ct := info.MIME
		if _, _, err := mime.ParseMediaType(ct); err != nil {
			ct = "application/octet-stream"
		}
		hctx.SetHeader("Content-Type", ct)
		hctx.SetHeader("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": info.Name}))
		hctx.SetHeader("Content-Security-Policy", "sandbox; default-src 'none'")
		hctx.SetHeader("Cache-Control", "private, no-store")
		_, _ = io.Copy(hctx.BodyWriter(), f)
	}}, nil
}
