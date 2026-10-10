package httpapi

// Phase 7 security review: repro tests. Each asserts the vulnerable behaviour,
// so it passes today; invert it when the finding is fixed.

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"os"
	"regexp"
	"strings"
	"testing"

	"abidata/internal/obs"
)

func secReview(t *testing.T) {
	t.Helper()
	if os.Getenv("ABIDATA_SECREVIEW") == "" {
		t.Skip("security review repro; set ABIDATA_SECREVIEW=1")
	}
}

// M1: the access log records http_request_duration_seconds{method,...} with
// the raw request method. Any token is a valid HTTP method, chi answers unknown
// ones with 405 after the access-log middleware ran, and no authentication is
// needed, so an anonymous client mints a new histogram series (15 buckets +
// sum + count) per request: unbounded memory growth and a /metrics scrape that
// grows without limit.
func TestPhase7SecReview_MetricsMethodLabelUnbounded(t *testing.T) {
	secReview(t)
	e := newEnv(t)
	m := obs.NewMetrics()
	e.s.deps.Metrics = m
	const n = 200
	for i := range n {
		req, _ := http.NewRequest(fmt.Sprintf("ZZ%05d", i), e.srv.URL+"/api/v1/auth/me", nil)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		_ = resp.Body.Close()
	}
	rec := httptest.NewRecorder()
	m.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	series := regexp.MustCompile(`abidata_http_request_duration_seconds_count\{method="ZZ\d+"`).FindAllString(rec.Body.String(), -1)
	if len(series) < n {
		t.Fatalf("expected one series per attacker-chosen method, got %d", len(series))
	}
}

// M2: with the compose default ABIDATA_TRUSTED_PROXIES (all private ranges),
// remoteIP walks X-Forwarded-For from the right and skips every trusted
// address. Caddy writes the visitor's address as the only XFF entry, so for
// any visitor on a private network (LAN/VPN deployments, Docker Desktop or
// rootless port forwarding) every entry is "trusted" and the function falls
// back to the direct peer: Caddy. All such users share one rate-limit key, so
// one LAN host locks every other LAN user out of sign-in (5 bad guesses lock an
// account for everyone; LoginMaxPerIP locks out everyone).
func TestPhase7SecReview_PrivateClientsCollapseToProxyAddress(t *testing.T) {
	secReview(t)
	e := newEnv(t)
	e.s.deps.Config.TrustedProxies = []netip.Prefix{
		netip.MustParsePrefix("10.0.0.0/8"), netip.MustParsePrefix("172.16.0.0/12"), netip.MustParsePrefix("192.168.0.0/16")}
	const caddy = "172.18.0.5"
	ipOf := func(xff string) string {
		r := httptest.NewRequest(http.MethodGet, "/", nil)
		r.RemoteAddr = caddy + ":41000"
		r.Header.Set("X-Forwarded-For", xff)
		return e.s.remoteIP(r)
	}
	if got := ipOf("203.0.113.7"); got != "203.0.113.7" {
		t.Fatalf("public visitor: %s", got)
	}
	if a, b := ipOf("192.168.1.20"), ipOf("192.168.1.66"); a != caddy || b != caddy {
		t.Fatalf("expected both LAN visitors to collapse to the proxy, got %s and %s", a, b)
	}

	// End to end: an attacker at 192.168.1.66 guesses the victim's password
	// LoginMaxPerKey times; the victim at 192.168.1.20 is then refused with
	// the correct password, although the brief's limit is per email + address.
	e.user("victim@example.com", false)
	login := func(xff, pw string) int {
		body := fmt.Sprintf(`{"email":"victim@example.com","password":%q}`, pw)
		r := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", strings.NewReader(body))
		r.RemoteAddr = caddy + ":41000"
		r.Header.Set("X-Forwarded-For", xff)
		r.Header.Set("Origin", testOrigin)
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		e.s.ServeHTTP(w, r)
		return w.Code
	}
	for range 5 {
		if st := login("192.168.1.66", "wrong-password"); st != http.StatusUnauthorized {
			t.Fatalf("attacker guess: %d", st)
		}
	}
	if st := login("192.168.1.20", "long-enough-pw"); st != http.StatusTooManyRequests {
		t.Fatalf("expected the victim on another LAN address to be rate limited, got %d", st)
	}
}
