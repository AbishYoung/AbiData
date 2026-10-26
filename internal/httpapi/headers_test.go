package httpapi

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"abidata/internal/config"
)

// TestSecurityHeadersHardening: the app's CSP pins <base>, form targets and
// plugins, and an https install asks browsers to stay on https (HSTS).
func TestSecurityHeadersHardening(t *testing.T) {
	ok := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })
	headers := func(publicURL string) http.Header {
		s := &Server{deps: Deps{Config: config.Config{PublicURL: publicURL}}}
		rec := httptest.NewRecorder()
		s.securityHeaders(ok).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
		return rec.Header()
	}
	h := headers("https://abidata.example.com")
	csp := h.Get("Content-Security-Policy")
	for _, d := range []string{"base-uri 'none'", "form-action 'self'", "object-src 'none'", "frame-ancestors 'none'", "script-src 'self'"} {
		if !strings.Contains(csp, d) {
			t.Errorf("CSP lacks %q: %s", d, csp)
		}
	}
	if got := h.Get("Strict-Transport-Security"); got != "max-age=31536000" {
		t.Errorf("HSTS on https: %q", got)
	}
	// Plain http (a LAN or dev install) and local https trials must not send HSTS.
	for _, u := range []string{"http://localhost:8080", "http://abidata.example.com", "https://localhost", "https://app.localhost:8443", "https://192.168.1.20"} {
		if got := headers(u).Get("Strict-Transport-Security"); got != "" {
			t.Errorf("HSTS for %s: %q", u, got)
		}
	}
	// The tile origin is still added to img-src only.
	if p := buildCSP("https://tiles.example.org"); !strings.Contains(p, "img-src 'self' data: https://tiles.example.org;") || !strings.Contains(p, "base-uri 'none'") {
		t.Errorf("CSP with tiles: %s", p)
	}
}
