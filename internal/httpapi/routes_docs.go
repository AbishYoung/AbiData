package httpapi

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"abidata/internal/docsrv"
)

// DocsIndex lists every docs page with its search text.
type DocsIndex struct {
	Pages []docsrv.Summary `json:"pages"`
}

func (s *Server) registerDocs(api huma.API) {
	tags := []string{"docs"}
	// Docs are static product documentation: readable without signing in.
	public := map[string]any{metaAccess: "public"}
	huma.Register(api, huma.Operation{OperationID: "docsIndex", Method: http.MethodGet, Path: "/api/v1/docs", Tags: tags, Metadata: public,
		Summary: "Docs pages in reading order, with plain text for client-side search", Errors: []int{404}},
		func(context.Context, *struct{}) (*struct{ Body DocsIndex }, error) {
			if s.deps.Docs == nil {
				return nil, errNotFound
			}
			return &struct{ Body DocsIndex }{Body: DocsIndex{Pages: s.deps.Docs.Index()}}, nil
		})
	huma.Register(api, huma.Operation{OperationID: "docsPage", Method: http.MethodGet, Path: "/api/v1/docs/page", Tags: tags, Metadata: public,
		Summary: "One rendered docs page", Errors: []int{404}},
		func(_ context.Context, in *struct {
			Slug string `query:"slug" maxLength:"200"`
		}) (*struct{ Body docsrv.Page }, error) {
			if s.deps.Docs == nil {
				return nil, errNotFound
			}
			p, ok := s.deps.Docs.Pages[in.Slug]
			if !ok {
				return nil, errNotFound
			}
			return &struct{ Body docsrv.Page }{Body: *p}, nil
		})
}

func (s *Server) docsCSS(w http.ResponseWriter, _ *http.Request) {
	css, err := docsrv.HighlightCSS()
	if err != nil {
		http.Error(w, "unavailable", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/css; charset=utf-8")
	w.Header().Set("Cache-Control", "public, max-age=3600")
	_, _ = w.Write([]byte(css))
}
