package httpapi

import (
	"context"
	"errors"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humachi"

	"abidata/internal/admin"
	"abidata/internal/fault"
	"abidata/internal/files"
)

type fileUploadKey struct{}

// maxLookupIDs bounds GET /files?ids=.
const maxLookupIDs = 100

// fileCSP: nothing a stored file contains may run or load anything, and the
// response may not be framed. (sandbox alone already blocks scripts; the rest
// is belt and braces for browsers that treat the document as active content.)
const fileCSP = "sandbox; default-src 'none'; frame-ancestors 'none'"

func (s *Server) registerFiles(api huma.API) {
	tags := []string{"files"}
	editor := map[string]any{metaRole: string(admin.RoleEditor)}
	need := func() error {
		if s.deps.Files == nil {
			return fault.New(fault.NotFound, "File attachments are not enabled on this server.")
		}
		return nil
	}

	// The upload streams to disk through this middleware (huma would buffer a
	// typed body). Authentication, CSRF/Origin for sessions, API-key scope and
	// the editor role check have already run.
	stream := func(hctx huma.Context, next func(huma.Context)) {
		if err := need(); err != nil {
			writeHumaError(hctx, s.log, err)
			return
		}
		limit := s.deps.Files.MaxFile()
		if cl := hctx.Header("Content-Length"); cl != "" {
			if n, err := strconv.ParseInt(cl, 10, 64); err == nil && n > limit+multipartOverhead {
				writeHumaError(hctx, s.log, fault.Newf(fault.BadRequest, "The file is larger than the %d MiB limit.", limit>>20))
				return
			}
		}
		m, _ := MembershipFrom(hctx.Context())
		p, _ := PrincipalFrom(hctx.Context())
		l, err := s.deps.Workspaces.Acquire(hctx.Context(), m.Workspace.ID)
		if err != nil {
			writeHumaError(hctx, s.log, fault.Classify(err))
			return
		}
		defer l.Release()

		var name string
		var body io.Reader
		mt, params, _ := mime.ParseMediaType(hctx.Header("Content-Type"))
		if mt == "multipart/form-data" {
			if params["boundary"] == "" {
				writeHumaError(hctx, s.log, fault.New(fault.BadRequest, "The multipart upload has no boundary."))
				return
			}
			mr := multipart.NewReader(io.LimitReader(hctx.BodyReader(), limit+multipartOverhead+1), params["boundary"])
			for body == nil {
				part, err := mr.NextPart()
				if errors.Is(err, io.EOF) {
					writeHumaError(hctx, s.log, fault.New(fault.BadRequest, "The request has no file field. Send the file in a multipart field named file."))
					return
				}
				if err != nil {
					writeHumaError(hctx, s.log, fault.New(fault.BadRequest, "The upload could not be read; it may be larger than the limit or cut off."))
					return
				}
				if part.FormName() == "file" {
					name, body = part.FileName(), part
				}
			}
		} else {
			name, body = headerFileName(hctx.Header("X-File-Name")), hctx.BodyReader()
		}
		meta, err := s.deps.Files.Upload(hctx.Context(), l, p.UserID, name, body)
		if err != nil {
			writeHumaError(hctx, s.log, err)
			return
		}
		next(huma.WithValue(hctx, fileUploadKey{}, meta))
	}

	huma.Register(api, huma.Operation{OperationID: "uploadFile", Method: http.MethodPost, Path: "/api/v1/w/{ws}/files", Tags: tags, Metadata: editor,
		Summary: "Upload one file (editors and above)",
		Description: "Send the bytes as the raw request body with an X-File-Name header (percent-encode non-ASCII names), or as multipart/form-data with the file in a field named file. " +
			"The file is stored once per content (SHA-256) and returned as `pending`; it becomes `attached` when a row's attachments field refers to its id, and a pending file nothing refers to within 24 hours is deleted. " +
			"The content type is detected from the bytes; a type sent by the client is ignored.",
		DefaultStatus: http.StatusCreated, Errors: []int{400, 401, 403, 404, 429, 507},
		Parameters: []*huma.Param{{Name: "X-File-Name", In: "header", Description: "The file's name for the raw-body form; percent-decoded when valid. Ignored for multipart.", Schema: &huma.Schema{Type: "string"}}},
		RequestBody: &huma.RequestBody{Required: true, Content: map[string]*huma.MediaType{
			"application/octet-stream": {Schema: &huma.Schema{Type: "string", Format: "binary", Description: "The file bytes."}},
			"multipart/form-data": {Schema: &huma.Schema{Type: "object", Required: []string{"file"},
				Properties: map[string]*huma.Schema{"file": {Type: "string", Format: "binary", Description: "The file."}}}},
		}},
		Middlewares: huma.Middlewares{stream}},
		func(ctx context.Context, _ *struct {
			WS string `path:"ws"`
		}) (*struct{ Body files.Meta }, error) {
			meta, _ := ctx.Value(fileUploadKey{}).(files.Meta)
			return &struct{ Body files.Meta }{Body: meta}, nil
		})

	huma.Register(api, huma.Operation{OperationID: "getFile", Method: http.MethodGet, Path: "/api/v1/w/{ws}/files/{id}", Tags: tags,
		Summary: "A file's metadata", Errors: []int{401, 404}},
		func(ctx context.Context, in *struct {
			WS string `path:"ws"`
			ID string `path:"id"`
		}) (*struct{ Body files.Meta }, error) {
			if err := need(); err != nil {
				return nil, err
			}
			l, err := s.lease(ctx)
			if err != nil {
				return nil, err
			}
			defer l.Release()
			meta, err := s.deps.Files.Get(ctx, l, in.ID)
			if err != nil {
				return nil, err
			}
			return &struct{ Body files.Meta }{Body: meta}, nil
		})

	huma.Register(api, huma.Operation{OperationID: "lookupFiles", Method: http.MethodGet, Path: "/api/v1/w/{ws}/files", Tags: tags,
		Summary: "Metadata of several files by id (unknown ids are left out)", Errors: []int{400, 401, 404}},
		func(ctx context.Context, in *struct {
			WS  string `path:"ws"`
			IDs string `query:"ids" doc:"Comma-separated file ids, at most 100."`
		}) (*struct {
			Body struct {
				Items []files.Meta `json:"items"`
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
			var want []string
			for _, id := range strings.Split(in.IDs, ",") {
				if id = strings.TrimSpace(id); id != "" {
					want = append(want, id)
				}
			}
			if len(want) > maxLookupIDs {
				return nil, fault.Newf(fault.BadRequest, "Ask for at most %d files at a time.", maxLookupIDs)
			}
			out := &struct {
				Body struct {
					Items []files.Meta `json:"items"`
				}
			}{}
			out.Body.Items = []files.Meta{}
			for _, id := range want {
				if meta, err := s.deps.Files.Get(ctx, l, id); err == nil {
					out.Body.Items = append(out.Body.Items, meta)
				} else if fault.Classify(err).Code != fault.NotFound {
					return nil, err
				}
			}
			return out, nil
		})

	huma.Register(api, huma.Operation{OperationID: "getFileContent", Method: http.MethodGet, Path: "/api/v1/w/{ws}/files/{id}/content", Tags: tags,
		Summary: "A file's bytes",
		Description: "Safe raster images (PNG, JPEG, GIF, WebP) are served inline; everything else is an attachment. " +
			"Responses carry X-Content-Type-Options: nosniff and a sandboxing Content-Security-Policy, are cacheable for a year (immutable: the content never changes for an id), and support Range requests. " +
			"Add download=true to force a download.",
		Responses: map[string]*huma.Response{"200": {Description: "The file", Content: map[string]*huma.MediaType{"*/*": {Schema: &huma.Schema{Type: "string", Format: "binary"}}}}},
		Errors:    []int{401, 404}},
		func(ctx context.Context, in *struct {
			WS       string `path:"ws"`
			ID       string `path:"id"`
			Download bool   `query:"download" doc:"Send as an attachment even if the file could be shown in place."`
		}) (*huma.StreamResponse, error) {
			if err := need(); err != nil {
				return nil, err
			}
			l, err := s.lease(ctx)
			if err != nil {
				return nil, err
			}
			f, meta, err := s.deps.Files.Open(ctx, l, in.ID)
			l.Release()
			if err != nil {
				return nil, err
			}
			return s.serveFile(f, meta, "\""+meta.SHA256+"\"", files.ServeType(meta.ContentType), meta.Name,
				files.Inline(meta.ContentType) && !in.Download), nil
		})

	huma.Register(api, huma.Operation{OperationID: "getFileThumbnail", Method: http.MethodGet, Path: "/api/v1/w/{ws}/files/{id}/thumbnail", Tags: tags,
		Summary:     "A JPEG thumbnail (at most 320 px) of a PNG, JPEG or GIF file",
		Description: "Made on first request and cached. Files that are not such images answer 404.",
		Responses:   map[string]*huma.Response{"200": {Description: "The thumbnail", Content: map[string]*huma.MediaType{"image/jpeg": {Schema: &huma.Schema{Type: "string", Format: "binary"}}}}},
		Errors:      []int{401, 404}},
		func(ctx context.Context, in *struct {
			WS string `path:"ws"`
			ID string `path:"id"`
		}) (*huma.StreamResponse, error) {
			if err := need(); err != nil {
				return nil, err
			}
			l, err := s.lease(ctx)
			if err != nil {
				return nil, err
			}
			f, meta, err := s.deps.Files.Thumbnail(ctx, l, in.ID)
			l.Release()
			if err != nil {
				return nil, err
			}
			return s.serveFile(f, meta, "\"t-"+meta.SHA256+"\"", "image/jpeg", "", true), nil
		})
}

// serveFile writes an opened blob with the hardened response headers. The file
// is closed when the response is done.
func (s *Server) serveFile(f interface {
	io.ReadSeeker
	io.Closer
}, meta files.Meta, etag, contentType, name string, inline bool) *huma.StreamResponse {
	return &huma.StreamResponse{Body: func(hctx huma.Context) {
		defer func() { _ = f.Close() }()
		r, w := humachi.Unwrap(hctx)
		h := w.Header()
		h.Set("Content-Type", contentType)
		disp := "attachment"
		if inline {
			disp = "inline"
		}
		if name != "" {
			h.Set("Content-Disposition", mime.FormatMediaType(disp, map[string]string{"filename": name}))
		} else {
			h.Set("Content-Disposition", disp)
		}
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Content-Security-Policy", fileCSP)
		h.Set("Cross-Origin-Resource-Policy", "same-origin")
		h.Set("Cache-Control", "private, max-age=31536000, immutable")
		h.Set("ETag", etag)
		// ServeContent handles Range, If-None-Match and If-Modified-Since. An empty
		// name stops it from guessing a type from the extension.
		http.ServeContent(w, r, "", meta.CreatedAt, f)
	}}
}

// headerFileName reads X-File-Name: percent-decoded when that is valid, else
// taken literally (so "100%.pdf" still works).
func headerFileName(v string) string {
	if d, err := url.PathUnescape(v); err == nil {
		return d
	}
	return v
}
