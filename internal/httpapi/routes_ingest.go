package httpapi

import (
	"context"
	"errors"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"strconv"
	"strings"

	"github.com/danielgtaylor/huma/v2"

	"abidata/internal/admin"
	"abidata/internal/artifacts"
	"abidata/internal/fault"
	"abidata/internal/ids"
	"abidata/internal/ingest"
)

type uploadResultKey struct{}

// multipartOverhead is the allowance for boundaries and form fields on top of the file.
const multipartOverhead = 1 << 20

func (s *Server) registerIngest(api huma.API) {
	tags := []string{"ingest"}
	builder := map[string]any{metaRole: string(admin.RoleBuilder)}
	need := func() error {
		if s.deps.Ingest == nil {
			return fault.New(fault.NotFound, "Imports are not enabled on this server.")
		}
		return nil
	}
	str := func(desc string) *huma.Schema { return &huma.Schema{Type: "string", Description: desc} }

	// The upload is streamed straight to disk by this middleware, because huma
	// would buffer a typed body. Auth, CSRF and the role check already ran.
	stream := func(hctx huma.Context, next func(huma.Context)) {
		if err := need(); err != nil {
			writeHumaError(hctx, s.log, err)
			return
		}
		if cl := hctx.Header("Content-Length"); cl != "" {
			if n, err := strconv.ParseInt(cl, 10, 64); err == nil && n > ingest.MaxUploadBytes+multipartOverhead {
				writeHumaError(hctx, s.log, fault.Newf(fault.BadRequest, "The upload is larger than the %d MiB limit.", ingest.MaxUploadBytes>>20))
				return
			}
		}
		mt, params, err := mime.ParseMediaType(hctx.Header("Content-Type"))
		if err != nil || mt != "multipart/form-data" || params["boundary"] == "" {
			writeHumaError(hctx, s.log, fault.New(fault.BadRequest, "Send the file as multipart/form-data with a field named file."))
			return
		}
		m, _ := MembershipFrom(hctx.Context())
		p, _ := PrincipalFrom(hctx.Context())
		mr := multipart.NewReader(io.LimitReader(hctx.BodyReader(), ingest.MaxUploadBytes+multipartOverhead+1), params["boundary"])
		var opt ingest.UploadOptions
		for {
			part, err := mr.NextPart()
			if errors.Is(err, io.EOF) {
				writeHumaError(hctx, s.log, fault.New(fault.BadRequest, "The request has no file field."))
				return
			}
			if err != nil {
				writeHumaError(hctx, s.log, fault.New(fault.BadRequest, "The upload could not be read; it may be larger than the limit or cut off."))
				return
			}
			if part.FormName() != "file" {
				b, _ := io.ReadAll(io.LimitReader(part, 256))
				v := strings.TrimSpace(string(b))
				switch part.FormName() {
				case "format":
					opt.Format = v
				case "delimiter":
					if v == "tab" || v == `\t` {
						v = "\t"
					}
					opt.Delimiter = v
				case "has_header":
					opt.NoHeader = v == "false" || v == "0"
				case "sheet":
					opt.Sheet = v
				}
				continue
			}
			opt.Filename = part.FileName()
			meta, err := s.deps.Ingest.Upload(hctx.Context(), m.Workspace.ID, p.UserID, opt, part)
			if err != nil {
				writeHumaError(hctx, s.log, err)
				return
			}
			next(huma.WithValue(hctx, uploadResultKey{}, meta))
			return
		}
	}

	huma.Register(api, huma.Operation{OperationID: "uploadImport", Method: http.MethodPost, Path: "/api/v1/w/{ws}/imports", Tags: tags, Metadata: builder,
		Summary:       "Upload a CSV, XLSX, JSON or NDJSON file (builders). Fields format, delimiter, has_header and sheet must come before file.",
		DefaultStatus: http.StatusCreated, Errors: []int{400, 401, 403, 404},
		RequestBody: &huma.RequestBody{Content: map[string]*huma.MediaType{"multipart/form-data": {Schema: &huma.Schema{
			Type: "object", Required: []string{"file"},
			Properties: map[string]*huma.Schema{
				"format":     str("csv, xlsx, json, ndjson or geojson; default from the file extension"),
				"delimiter":  str("CSV delimiter: comma, semicolon, tab or pipe; default sniffed"),
				"has_header": str("false when the first row is data"),
				"sheet":      str("XLSX sheet name; default the first visible sheet"),
				"file":       {Type: "string", Format: "binary", Description: "At most 100 MiB."},
			}}}}},
		Middlewares: huma.Middlewares{stream}},
		func(ctx context.Context, _ *struct {
			WS string `path:"ws"`
		}) (*struct{ Body ingest.ImportMeta }, error) {
			meta, _ := ctx.Value(uploadResultKey{}).(ingest.ImportMeta)
			return &struct{ Body ingest.ImportMeta }{Body: meta}, nil
		})

	huma.Register(api, huma.Operation{OperationID: "getImport", Method: http.MethodGet, Path: "/api/v1/w/{ws}/imports/{id}", Tags: tags, Metadata: builder,
		Summary: "An import: columns, inferred types, mapping and the first 50 rows", Errors: []int{401, 403, 404}},
		func(ctx context.Context, in *struct {
			WS string `path:"ws"`
			ID string `path:"id"`
		}) (*struct{ Body ingest.ImportView }, error) {
			if err := need(); err != nil {
				return nil, err
			}
			m, _ := MembershipFrom(ctx)
			v, err := s.deps.Ingest.Get(ctx, m.Workspace.ID, in.ID)
			if err != nil {
				return nil, err
			}
			return &struct{ Body ingest.ImportView }{Body: v}, nil
		})

	huma.Register(api, huma.Operation{OperationID: "setImportMapping", Method: http.MethodPut, Path: "/api/v1/w/{ws}/imports/{id}/mapping", Tags: tags, Metadata: builder,
		Summary: "Set the target and column mapping (validated against the catalog)", Errors: []int{400, 401, 403, 404}},
		func(ctx context.Context, in *struct {
			WS   string `path:"ws"`
			ID   string `path:"id"`
			Body ingest.ImportMapping
		}) (*struct{ Body ingest.ImportMeta }, error) {
			if err := need(); err != nil {
				return nil, err
			}
			l, err := s.lease(ctx)
			if err != nil {
				return nil, err
			}
			defer l.Release()
			meta, err := s.deps.Ingest.SetMapping(ctx, l, in.ID, in.Body)
			if err != nil {
				return nil, err
			}
			return &struct{ Body ingest.ImportMeta }{Body: meta}, nil
		})

	huma.Register(api, huma.Operation{OperationID: "validateImport", Method: http.MethodPost, Path: "/api/v1/w/{ws}/imports/{id}/validate", Tags: tags, Metadata: builder,
		Summary: "Convert every row with the current mapping and list the first 100 row errors", Errors: []int{400, 401, 403, 404}},
		func(ctx context.Context, in *struct {
			WS string `path:"ws"`
			ID string `path:"id"`
		}) (*struct{ Body ingest.ValidationResult }, error) {
			if err := need(); err != nil {
				return nil, err
			}
			l, err := s.lease(ctx)
			if err != nil {
				return nil, err
			}
			defer l.Release()
			r, err := s.deps.Ingest.Validate(ctx, l, in.ID)
			if err != nil {
				return nil, err
			}
			return &struct{ Body ingest.ValidationResult }{Body: r}, nil
		})

	huma.Register(api, huma.Operation{OperationID: "commitImport", Method: http.MethodPost, Path: "/api/v1/w/{ws}/imports/{id}/commit", Tags: tags, Metadata: builder,
		Summary: "Commit the import as an operation (kind import); its result is {inserted, updated, skipped, errors, table_id}", DefaultStatus: http.StatusAccepted,
		Errors: []int{400, 401, 403, 404}},
		func(ctx context.Context, in *struct {
			WS string `path:"ws"`
			ID string `path:"id"`
		}) (*struct{ Body OperationDetail }, error) {
			if err := need(); err != nil {
				return nil, err
			}
			l, err := s.lease(ctx)
			if err != nil {
				return nil, err
			}
			defer l.Release()
			p, _ := PrincipalFrom(ctx)
			op, err := s.deps.Ingest.StartCommit(ctx, l, in.ID, p.UserID)
			if err != nil {
				return nil, err
			}
			return &struct{ Body OperationDetail }{Body: OperationDetail{Op: op, Artifacts: []artifacts.Info{}}}, nil
		})

	huma.Register(api, huma.Operation{OperationID: "deleteImport", Method: http.MethodDelete, Path: "/api/v1/w/{ws}/imports/{id}", Tags: tags, Metadata: builder,
		Summary: "Discard an import and its staging files", DefaultStatus: http.StatusNoContent, Errors: []int{400, 401, 403, 404}},
		func(ctx context.Context, in *struct {
			WS string `path:"ws"`
			ID string `path:"id"`
		}) (*struct{}, error) {
			if err := need(); err != nil {
				return nil, err
			}
			return nil, s.deps.Ingest.Delete(ctx, mustWS(ctx), in.ID)
		})

	huma.Register(api, huma.Operation{OperationID: "createExport", Method: http.MethodPost, Path: "/api/v1/w/{ws}/exports", Tags: tags,
		Summary:       "Export a table or saved query as csv, xlsx or json (an operation of kind export; the file is its artifact). Saved queries are capped at 50,000 rows.",
		DefaultStatus: http.StatusAccepted, Errors: []int{400, 401, 403, 404}},
		func(ctx context.Context, in *struct {
			WS   string `path:"ws"`
			Body ingest.ExportRequest
		}) (*struct{ Body OperationDetail }, error) {
			if err := need(); err != nil {
				return nil, err
			}
			if in.Body.Filter != "" || in.Body.Sort != "" {
				// Same parser as the rows endpoint.
				q, err := parseListQuery(0, 1, in.Body.Sort, in.Body.Filter)
				if err != nil {
					return nil, err
				}
				in.Body.View = &q
			}
			l, err := s.lease(ctx)
			if err != nil {
				return nil, err
			}
			defer l.Release()
			p, _ := PrincipalFrom(ctx)
			op, err := s.deps.Ingest.StartExport(ctx, l, p.UserID, s.policy(ctx), in.Body)
			if err != nil {
				return nil, err
			}
			return &struct{ Body OperationDetail }{Body: OperationDetail{Op: op, Artifacts: []artifacts.Info{}}}, nil
		})
}

func mustWS(ctx context.Context) ids.WorkspaceID {
	m, _ := MembershipFrom(ctx)
	return m.Workspace.ID
}
