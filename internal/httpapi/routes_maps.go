package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"unicode"

	"github.com/danielgtaylor/huma/v2"

	"abidata/internal/admin"
	"abidata/internal/fault"
)

// Maps (ADR 0010): base map tiles are off by default. An administrator may set
// a tile URL template; while it is set, the browser loads tiles from that
// origin and the CSP img-src allows it.

// MapsSettingKey is the _adm_settings key holding the base map tile settings.
const MapsSettingKey = "maps_tiles"

// Limits on the maps settings.
const (
	maxTileURLLen     = 2048
	maxAttributionLen = 300
)

// MapsSettings configures base map tiles. An empty TileURL means no tiles.
type MapsSettings struct {
	TileURL     string `json:"tile_url" maxLength:"2048" doc:"Tile URL template with {z}, {x} and {y} (optionally {s} as the first host label, and {r}); https only, or http for localhost. Empty turns tiles off."`
	Attribution string `json:"attribution" maxLength:"300" doc:"Plain-text attribution shown on maps, for example © OpenStreetMap contributors."`
}

// tilePlaceholders are the placeholders a tile template may use.
var tilePlaceholders = map[string]bool{"{s}": true, "{z}": true, "{x}": true, "{y}": true, "{r}": true}

var placeholderRe = regexp.MustCompile(`\{[^{}]*\}`)

// hostRe matches a DNS host name or IPv4 address (an IPv6 literal is checked separately).
var hostRe = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]*[a-z0-9])?(\.[a-z0-9]([a-z0-9-]*[a-z0-9])?)*$`)

const subdomainMark = "abidata-subdomain"

func isLoopbackHost(h string) bool {
	if h == "localhost" || strings.HasSuffix(h, ".localhost") {
		return true
	}
	ip := net.ParseIP(h)
	return ip != nil && ip.IsLoopback()
}

// tileOrigin validates a tile URL template and returns the CSP source
// expression for its origin (for example https://*.tile.example.org).
func tileOrigin(tmpl string) (string, error) {
	badf := func(format string, args ...any) error {
		return fault.Newf(fault.BadRequest, format, args...).WithDetails(map[string]any{"field": "tile_url"})
	}
	if len(tmpl) > maxTileURLLen {
		return "", badf("The tile URL can be at most %d characters.", maxTileURLLen)
	}
	for _, r := range tmpl {
		if unicode.IsSpace(r) || unicode.IsControl(r) || strings.ContainsRune(`'"<>\;,`+"`", r) {
			return "", badf("The tile URL contains a character that is not allowed (%q).", r)
		}
	}
	for _, p := range []string{"{z}", "{x}", "{y}"} {
		if !strings.Contains(tmpl, p) {
			return "", badf("The tile URL must contain {z}, {x} and {y}, for example https://tiles.example.org/{z}/{x}/{y}.png.")
		}
	}
	for _, p := range placeholderRe.FindAllString(tmpl, -1) {
		if !tilePlaceholders[p] {
			return "", badf("The tile URL uses an unknown placeholder %s; use {s}, {z}, {x}, {y} or {r}.", p)
		}
	}
	probe := strings.NewReplacer("{s}", subdomainMark, "{z}", "0", "{x}", "0", "{y}", "0", "{r}", "").Replace(tmpl)
	if strings.ContainsAny(probe, "{}") {
		return "", badf("The tile URL has an unbalanced brace.")
	}
	u, err := url.Parse(probe)
	if err != nil || u.Host == "" {
		return "", badf("Enter a full tile URL starting with https://.")
	}
	if u.User != nil || u.Fragment != "" || u.Opaque != "" {
		return "", badf("The tile URL cannot contain a user name, password or #fragment.")
	}
	host := strings.ToLower(u.Hostname())
	switch u.Scheme {
	case "https":
	case "http":
		if !isLoopbackHost(host) {
			return "", badf("The tile URL must use https (http is allowed only for localhost).")
		}
	default:
		return "", badf("The tile URL must start with https://.")
	}
	wildcard := false
	if strings.Contains(host, subdomainMark) {
		rest, ok := strings.CutPrefix(host, subdomainMark+".")
		if !ok || strings.Contains(rest, subdomainMark) || !strings.Contains(rest, ".") {
			return "", badf("{s} may only be the first label of the host name, for example https://{s}.tiles.example.org/….")
		}
		host, wildcard = rest, true
	}
	if ip := net.ParseIP(host); ip != nil {
		if wildcard {
			return "", badf("{s} cannot be used with an IP address.")
		}
		if ip.To4() == nil {
			host = "[" + ip.String() + "]"
		}
	} else if !hostRe.MatchString(host) {
		return "", badf("%q is not a valid host name.", host)
	}
	origin := u.Scheme + "://"
	if wildcard {
		origin += "*."
	}
	origin += host
	if p := u.Port(); p != "" {
		origin += ":" + p
	}
	return origin, nil
}

// validateMaps normalises and checks maps settings. An empty tile URL clears
// the attribution too.
func validateMaps(in MapsSettings) (MapsSettings, string, error) {
	out := MapsSettings{TileURL: strings.TrimSpace(in.TileURL), Attribution: strings.TrimSpace(in.Attribution)}
	if out.TileURL == "" {
		return MapsSettings{}, "", nil
	}
	origin, err := tileOrigin(out.TileURL)
	if err != nil {
		return MapsSettings{}, "", err
	}
	if len([]rune(out.Attribution)) > maxAttributionLen {
		return MapsSettings{}, "", fault.Newf(fault.BadRequest, "The attribution can be at most %d characters.", maxAttributionLen).WithDetails(map[string]any{"field": "attribution"})
	}
	for _, r := range out.Attribution {
		if unicode.IsControl(r) {
			return MapsSettings{}, "", fault.New(fault.BadRequest, "The attribution must be one line of plain text.").WithDetails(map[string]any{"field": "attribution"})
		}
	}
	return out, origin, nil
}

// LoadMaps reads the maps settings (no tiles when unset or unreadable as JSON).
func LoadMaps(ctx context.Context, a *admin.Store) (MapsSettings, error) {
	v, err := a.GetSetting(ctx, MapsSettingKey)
	if errors.Is(err, admin.ErrNotFound) {
		return MapsSettings{}, nil
	}
	if err != nil {
		return MapsSettings{}, err
	}
	var m MapsSettings
	if json.Unmarshal([]byte(v), &m) != nil {
		return MapsSettings{}, nil
	}
	// A stored value is re-validated so a hand-edited row cannot widen the CSP.
	m, _, err = validateMaps(m)
	if err != nil {
		return MapsSettings{}, nil
	}
	return m, nil
}

// baseCSP is the policy without any map tile origin.
const baseCSP = "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; worker-src 'self' blob:; manifest-src 'self'; connect-src 'self'; img-src 'self' data:; frame-ancestors 'none'"

// buildCSP returns the Content-Security-Policy, with tileOrigin added to img-src when set.
func buildCSP(tileOrigin string) string {
	if tileOrigin == "" {
		return baseCSP
	}
	return strings.Replace(baseCSP, "img-src 'self' data:", "img-src 'self' data: "+tileOrigin, 1)
}

// csp returns the cached policy, loading the maps setting on first use. A
// failed load serves the base policy and retries on the next request.
func (s *Server) csp(ctx context.Context) string {
	if p := s.cspValue.Load(); p != nil {
		return *p
	}
	if s.deps.Admin == nil {
		return baseCSP
	}
	m, err := LoadMaps(ctx, s.deps.Admin)
	if err != nil {
		return baseCSP
	}
	s.setMapsCSP(m)
	return *s.cspValue.Load()
}

// setMapsCSP recomputes the cached policy from maps settings.
func (s *Server) setMapsCSP(m MapsSettings) {
	origin := ""
	if m.TileURL != "" {
		if o, err := tileOrigin(m.TileURL); err == nil {
			origin = o
		}
	}
	p := buildCSP(origin)
	s.cspValue.Store(&p)
}

func (s *Server) securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Content-Security-Policy", s.csp(r.Context()))
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "same-origin")
		next.ServeHTTP(w, r)
	})
}

func (s *Server) registerMaps(api huma.API) {
	adminOnly := map[string]any{metaAccess: "platform_admin"}
	tags := []string{"admin"}

	huma.Register(api, huma.Operation{OperationID: "adminGetMaps", Method: http.MethodGet, Path: "/api/v1/admin/settings/maps", Tags: tags, Metadata: adminOnly,
		Summary: "The base map tile settings", Errors: []int{401, 403}},
		func(ctx context.Context, _ *struct{}) (*struct{ Body MapsSettings }, error) {
			m, err := LoadMaps(ctx, s.deps.Admin)
			if err != nil {
				return nil, err
			}
			return &struct{ Body MapsSettings }{Body: m}, nil
		})
	huma.Register(api, huma.Operation{OperationID: "adminPutMaps", Method: http.MethodPut, Path: "/api/v1/admin/settings/maps", Tags: tags, Metadata: adminOnly,
		Summary: "Replace the base map tile settings (an empty tile URL turns tiles off)", Errors: []int{400, 401, 403}},
		func(ctx context.Context, in *struct{ Body MapsSettings }) (*struct{ Body MapsSettings }, error) {
			m, origin, err := validateMaps(in.Body)
			if err != nil {
				return nil, err
			}
			b, _ := json.Marshal(m)
			if err := s.deps.Admin.PutSetting(ctx, MapsSettingKey, string(b)); err != nil {
				return nil, err
			}
			s.setMapsCSP(m)
			s.audit(ctx, "maps.tiles", "settings", MapsSettingKey, map[string]any{"enabled": m.TileURL != "", "origin": origin})
			return &struct{ Body MapsSettings }{Body: m}, nil
		})
}
