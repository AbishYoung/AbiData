package blogs

import (
	"bytes"
	_ "embed"
	"html/template"
)

// Stylesheet is the one stylesheet of the public pages and the preview.
//
//go:embed assets/blog.css
var Stylesheet string

// The preview document goes into an <iframe sandbox srcdoc> in the editor.
// Its own CSP allows no script at all and images only from the app's
// origin, so even a sanitiser bypass cannot run in the authenticated app.
var previewDoc = template.Must(template.New("preview").Parse(`<!doctype html>
<html lang="en"><head><meta charset="utf-8">
<meta http-equiv="Content-Security-Policy" content="default-src 'none'; img-src 'self'; style-src 'unsafe-inline'; base-uri 'none'; form-action 'none'">
<meta name="viewport" content="width=device-width, initial-scale=1">
<style>{{.CSS}}</style></head>
<body><main><article><h1>{{.Title}}</h1>{{.Body}}</article></main></body></html>`))

// Preview renders a post body through the public pipeline into a complete
// document for the editor's sandboxed preview. Nothing is stored.
func Preview(title, body string, images ImageURL) (string, error) {
	h, err := RenderMarkdown(body, images)
	if err != nil {
		return "", err
	}
	var buf bytes.Buffer
	err = previewDoc.Execute(&buf, map[string]any{
		"CSS":   template.CSS(Stylesheet), //nolint:gosec // the embedded stylesheet, not user input
		"Title": title,
		"Body":  template.HTML(h), //nolint:gosec // sanitised by RenderMarkdown
	})
	return buf.String(), err
}
