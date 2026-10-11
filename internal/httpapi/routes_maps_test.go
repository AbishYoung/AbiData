package httpapi

import (
	"context"
	"net/http"
	"strings"
	"testing"
)

func TestTileOriginValidation(t *testing.T) {
	ok := []struct{ in, origin string }{
		{"https://tile.openstreetmap.org/{z}/{x}/{y}.png", "https://tile.openstreetmap.org"},
		{"https://{s}.tile.example.org/{z}/{x}/{y}{r}.png", "https://*.tile.example.org"},
		{"https://tiles.example.org:8443/v1/{z}/{x}/{y}.png?key=abc", "https://tiles.example.org:8443"},
		{"HTTPS://Tiles.Example.org/{z}/{x}/{y}.png", "https://tiles.example.org"},
		{"http://localhost:8080/{z}/{x}/{y}.png", "http://localhost:8080"},
		{"http://127.0.0.1/{z}/{x}/{y}.png", "http://127.0.0.1"},
		{"http://[::1]:9000/{z}/{x}/{y}.png", "http://[::1]:9000"},
		{"https://10.0.0.5/{z}/{x}/{y}.png", "https://10.0.0.5"},
	}
	for _, c := range ok {
		got, err := tileOrigin(c.in)
		if err != nil || got != c.origin {
			t.Errorf("%s: got %q, %v; want %q", c.in, got, err, c.origin)
		}
	}
	bad := []string{
		"http://tiles.example.org/{z}/{x}/{y}.png",              // http off localhost
		"ftp://tiles.example.org/{z}/{x}/{y}.png",               // scheme
		"https://tiles.example.org/{z}/{x}.png",                 // no {y}
		"https://tiles.example.org/{z}/{x}/{y}/{q}.png",         // unknown placeholder
		"https://tiles.example.org/{z}/{x}/{y}.png; x",          // space and semicolon
		"https://tiles.example.org/{z}/{x}/{y}.png'",            // quote
		"https://a.b/{z}/{x}/{y},https://evil.org",              // comma
		"https://tiles.{s}.example.org/{z}/{x}/{y}.png",         // {s} not first label
		"https://{s}.org/{z}/{x}/{y}.png",                       // wildcard over a TLD
		"https://user:pw@tiles.example.org/{z}/{x}/{y}",         // credentials
		"https://tiles.example.org/{z}/{x}/{y}#frag",            // fragment
		"//tiles.example.org/{z}/{x}/{y}.png",                   // no scheme
		"tiles.example.org/{z}/{x}/{y}.png",                     // no scheme
		"https://ti_les.example.org/{z}/{x}/{y}.png",            // bad host
		"https://*.example.org/{z}/{x}/{y}.png",                 // literal wildcard
		"https://tiles.example.org/{z}/{x}/{y}.png{",            // unbalanced
		"https://" + strings.Repeat("a", 2050) + "/{z}/{x}/{y}", // too long
	}
	for _, in := range bad {
		if got, err := tileOrigin(in); err == nil {
			t.Errorf("%s: accepted as %q", in, got)
		}
	}
	if _, _, err := validateMaps(MapsSettings{TileURL: "https://t.example.org/{z}/{x}/{y}.png", Attribution: "line\nbreak"}); err == nil {
		t.Error("multi-line attribution accepted")
	}
	if m, origin, err := validateMaps(MapsSettings{TileURL: "  ", Attribution: "x"}); err != nil || m != (MapsSettings{}) || origin != "" {
		t.Errorf("empty URL should clear: %+v %q %v", m, origin, err)
	}
}

func TestBuildCSP(t *testing.T) {
	if buildCSP("") != baseCSP {
		t.Fatal("base CSP changed")
	}
	got := buildCSP("https://*.tile.example.org")
	if !strings.Contains(got, "img-src 'self' data: https://*.tile.example.org;") {
		t.Fatalf("img-src lacks origin: %s", got)
	}
	if strings.Count(got, "https://*.tile.example.org") != 1 || !strings.Contains(got, "connect-src 'self';") {
		t.Fatalf("origin leaked outside img-src: %s", got)
	}
}

func TestMapsSettingChangesCSP(t *testing.T) {
	e := newEnv(t)
	e.user("root@example.com", true)
	e.user("ed@example.com", false)
	adm := e.login("root@example.com")
	ed := e.login("ed@example.com")

	cspOf := func() string {
		resp, err := http.Get(e.srv.URL + "/")
		if err != nil {
			t.Fatal(err)
		}
		_ = resp.Body.Close()
		return resp.Header.Get("Content-Security-Policy")
	}
	if got := cspOf(); got != baseCSP {
		t.Fatalf("default CSP: %s", got)
	}
	if st, body, _ := ed.do("PUT", "/api/v1/admin/settings/maps", map[string]string{"tile_url": "https://t.example.org/{z}/{x}/{y}.png"}); st != 403 {
		t.Fatalf("non-admin put: %d %v", st, body)
	}
	if st, body, _ := adm.do("PUT", "/api/v1/admin/settings/maps", map[string]string{"tile_url": "http://t.example.org/{z}/{x}/{y}.png"}); st != 400 || errCode(body) != "BAD_REQUEST" {
		t.Fatalf("http accepted: %d %v", st, body)
	}
	if st, body, _ := adm.do("PUT", "/api/v1/admin/settings/maps", map[string]string{"tile_url": "https://t.example.org/{z}/{x}.png"}); st != 400 {
		t.Fatalf("missing {y} accepted: %d %v", st, body)
	}
	if got := cspOf(); got != baseCSP {
		t.Fatalf("rejected setting changed the CSP: %s", got)
	}

	st, body, _ := adm.do("PUT", "/api/v1/admin/settings/maps", map[string]string{"tile_url": " https://{s}.tiles.example.org/{z}/{x}/{y}.png ", "attribution": "© Example"})
	if st != 200 || body["tile_url"] != "https://{s}.tiles.example.org/{z}/{x}/{y}.png" {
		t.Fatalf("put: %d %v", st, body)
	}
	if got := cspOf(); !strings.Contains(got, "img-src 'self' data: https://*.tiles.example.org;") {
		t.Fatalf("CSP lacks tile origin: %s", got)
	}
	// Signed-in users see the setting on /auth/me; the admin endpoint reads it back.
	st, body, _ = ed.do("GET", "/api/v1/auth/me", nil)
	maps, _ := body["maps"].(map[string]any)
	if st != 200 || maps["tile_url"] != "https://{s}.tiles.example.org/{z}/{x}/{y}.png" || maps["attribution"] != "© Example" {
		t.Fatalf("me maps: %d %v", st, body)
	}
	if st, body, _ := adm.do("GET", "/api/v1/admin/settings/maps", nil); st != 200 || body["attribution"] != "© Example" {
		t.Fatalf("get: %d %v", st, body)
	}
	// A fresh server reads the stored setting (the cache is per process).
	e.s.cspValue.Store(nil)
	if got := cspOf(); !strings.Contains(got, "https://*.tiles.example.org") {
		t.Fatalf("CSP not reloaded from the setting: %s", got)
	}

	// Clearing the URL turns tiles off and restores the base policy.
	if st, body, _ := adm.do("PUT", "/api/v1/admin/settings/maps", map[string]string{"tile_url": "", "attribution": "ignored"}); st != 200 || body["tile_url"] != "" || body["attribution"] != "" {
		t.Fatalf("clear: %d %v", st, body)
	}
	if got := cspOf(); got != baseCSP {
		t.Fatalf("CSP after clear: %s", got)
	}
	_, body, _ = ed.do("GET", "/api/v1/auth/me", nil)
	if maps, _ := body["maps"].(map[string]any); maps["tile_url"] != "" {
		t.Fatalf("me after clear: %v", body)
	}

	// A hand-edited stored value that fails validation never widens the CSP.
	if err := e.ad.PutSetting(context.Background(), MapsSettingKey, `{"tile_url":"https://evil.org/{z}/{x}/{y}; script-src *"}`); err != nil {
		t.Fatal(err)
	}
	e.s.cspValue.Store(nil)
	if got := cspOf(); got != baseCSP {
		t.Fatalf("invalid stored setting widened the CSP: %s", got)
	}
}
