package egress

import (
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"abidata/internal/fault"
	"abidata/internal/ids"
)

const ws = ids.WorkspaceID("01J0000000000000000000000W")

func TestPublicRoutable(t *testing.T) {
	deny := []string{
		"127.0.0.1", "::1", "169.254.169.254", "100.100.100.200", "::ffff:127.0.0.1", "0.0.0.0",
		"fd00:ec2::254", "64:ff9b::7f00:1", "10.1.2.3", "172.16.0.1", "192.168.1.1", "198.18.0.1",
		"192.0.2.1", "203.0.113.9", "224.0.0.1", "255.255.255.255", "240.0.0.1", "fe80::1", "2001:db8::1",
		"2002:7f00:1::", "2001::1", "ff02::1", "::", "::ffff:10.0.0.1",
	}
	for _, s := range deny {
		if PublicRoutable(netip.MustParseAddr(s)) {
			t.Errorf("%s is routable", s)
		}
	}
	for _, s := range []string{"8.8.8.8", "1.1.1.1", "2606:4700:4700::1111", "::ffff:8.8.8.8", "100.128.0.1"} {
		if !PublicRoutable(netip.MustParseAddr(s)) {
			t.Errorf("%s is not routable", s)
		}
	}
}

func TestPatterns(t *testing.T) {
	cases := []struct {
		p, h string
		ok   bool
	}{
		{"api.example.com", "api.example.com", true},
		{"API.Example.com.", "api.example.com", true},
		{"api.example.com", "x.api.example.com", false},
		{"*.example.com", "a.example.com", true},
		{"*.example.com", "a.b.example.com", true},
		{"*.example.com", "example.com", false},
		{"*.example.com", "badexample.com", false},
		{"[2606:4700::1]", "2606:4700::1", true},
	}
	for _, c := range cases {
		if got := matchPattern(c.p, c.h); got != c.ok {
			t.Errorf("match(%q, %q) = %v", c.p, c.h, got)
		}
	}
	for _, p := range []string{"api.example.com", "*.example.com", "1.2.3.4", "[2606:4700::1]"} {
		if !ValidPattern(p) {
			t.Errorf("%q should be valid", p)
		}
	}
	for _, p := range []string{"", "*", "*.", "a..b", "exa mple.com", "http://x.com", "x.com/path", "*.1.2.3.4", "a_b.com", "*.*.com"} {
		if ValidPattern(p) {
			t.Errorf("%q should be invalid", p)
		}
	}
	if !ValidPorts("443, 8443") || ValidPorts("0") || ValidPorts("443,x") || ValidPorts("70000") {
		t.Error("ports validation")
	}
}

// env is a gateway whose policy treats 127.0.0.1 as "public" so a local test
// server can stand in for an allowed internet host.
type env struct {
	g      *Gateway
	allow  []Entry
	global Global
	lookup func(host string) []netip.Addr
	mu     sync.Mutex
}

func newEnv(t *testing.T) *env {
	t.Helper()
	e := &env{global: Global{Hosts: []string{"*"}, AllowHTTP: true}}
	e.g = New(Config{
		Global: func(context.Context) (Global, error) { e.mu.Lock(); defer e.mu.Unlock(); return e.global, nil },
		Allowlist: func(context.Context, ids.WorkspaceID) ([]Entry, error) {
			e.mu.Lock()
			defer e.mu.Unlock()
			return e.allow, nil
		},
		PerMinute: func(ids.WorkspaceID) int { return 10000 },
		Routable: func(a netip.Addr) bool {
			return a == netip.MustParseAddr("127.0.0.1") || PublicRoutable(a)
		},
		Lookup: func(_ context.Context, host string) ([]netip.Addr, error) {
			if e.lookup != nil {
				if a := e.lookup(host); a != nil {
					return a, nil
				}
			}
			return nil, &net.DNSError{Err: "no such host", Name: host, IsNotFound: true}
		},
	})
	t.Cleanup(e.g.Close)
	return e
}

func (e *env) permit(host, ports string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.allow = append(e.allow, Entry{Pattern: host, Ports: ports})
}

// server returns a test server, its port, and a hit counter.
func server(t *testing.T, h http.HandlerFunc) (string, *atomic.Int64) {
	t.Helper()
	var hits atomic.Int64
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		h(w, r)
	}))
	t.Cleanup(s.Close)
	u, _ := url.Parse(s.URL)
	return u.Port(), &hits
}

func code(err error) fault.Code {
	if err == nil {
		return ""
	}
	return fault.Classify(err).Code
}

func TestAllowedPublicHost(t *testing.T) {
	e := newEnv(t)
	port, hits := server(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Test", "yes")
		w.Header().Set("X-Method", r.Method)
		_, _ = w.Write([]byte(`{"ok":true}`))
	})
	e.permit("api.test", port)
	e.lookup = func(h string) []netip.Addr {
		if h == "api.test" {
			return []netip.Addr{netip.MustParseAddr("127.0.0.1")}
		}
		return nil
	}
	resp, err := e.g.Do(context.Background(), ws, Request{Method: "post", URL: "http://api.test:" + port + "/x", Body: []byte("hi")})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Status != 200 || string(resp.Body) != `{"ok":true}` || resp.Headers["x-method"] != "POST" || resp.Headers["x-test"] != "yes" || hits.Load() != 1 {
		t.Fatalf("%+v", resp)
	}
}

func TestSSRFDenied(t *testing.T) {
	e := newEnv(t)
	port, hits := server(t, func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("secret")) })
	// The policy treats 127.0.0.1 as public; every other internal address must be refused
	// at connect time even when the workspace allowlist names the host.
	e.lookup = func(h string) []netip.Addr {
		switch h {
		case "2130706433":
			return []netip.Addr{netip.MustParseAddr("127.0.0.2")}
		case "metadata.test":
			return []netip.Addr{netip.MustParseAddr("169.254.169.254")}
		case "mixed.test":
			return []netip.Addr{netip.MustParseAddr("10.0.0.1"), netip.MustParseAddr("fd00::1")}
		}
		return nil
	}
	urls := []string{
		"http://[::1]:" + port + "/",
		"http://169.254.169.254:" + port + "/latest/meta-data",
		"http://100.100.100.200:" + port + "/",
		"http://[::ffff:127.0.0.2]:" + port + "/",
		"http://2130706433:" + port + "/",
		"http://0.0.0.0:" + port + "/",
		"http://[fd00:ec2::254]:" + port + "/",
		"http://[64:ff9b::7f00:1]:" + port + "/",
		"http://metadata.test:" + port + "/",
		"http://mixed.test:" + port + "/",
	}
	for _, raw := range urls {
		u, _ := url.Parse(raw)
		e.permit(u.Hostname(), port)
		_, err := e.g.Do(context.Background(), ws, Request{URL: raw, Timeout: 2 * time.Second})
		if code(err) != fault.EgressDenied {
			t.Errorf("%s: %v", raw, err)
		}
	}
	if hits.Load() != 0 {
		t.Fatalf("server reached %d times", hits.Load())
	}
}

func TestPolicyDenials(t *testing.T) {
	e := newEnv(t)
	port, hits := server(t, func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("x")) })
	e.lookup = func(string) []netip.Addr { return []netip.Addr{netip.MustParseAddr("127.0.0.1")} }
	e.permit("ok.test", port)
	e.permit("*.wild.test", "")
	cases := map[string]Global{
		"http://other.test:" + port + "/":         e.global,                                                // not in allowlist
		"http://ok.test:1/":                       e.global,                                                // port not listed
		"http://ok.test:" + port + "/":            {Hosts: []string{"*"}},                                  // http not enabled
		"https://a.wild.test:" + port + "/":       e.global,                                                // wildcard listed with 443 only
		"http://u:p@ok.test:" + port + "/":        e.global,                                                // credentials in URL
		"ftp://ok.test/":                          e.global,                                                // scheme
		"file:///etc/passwd":                      e.global,                                                // scheme
		"http://ok.test:" + port + "/?global=off": {Hosts: []string{"*.example.com"}, AllowHTTP: true},     // global ∩ workspace
		"http://ok.test:" + port + "/?disabled":   {Hosts: []string{"*"}, AllowHTTP: true, Disabled: true}, // egress off
	}
	for raw, g := range cases {
		e.mu.Lock()
		e.global = g
		e.mu.Unlock()
		_, err := e.g.Do(context.Background(), ws, Request{URL: raw})
		if code(err) != fault.EgressDenied {
			t.Errorf("%s: %v", raw, err)
		}
	}
	if hits.Load() != 0 {
		t.Fatalf("server reached %d times", hits.Load())
	}
	if _, err := e.g.Do(context.Background(), ws, Request{URL: "/relative"}); code(err) != fault.BadRequest {
		t.Errorf("relative URL: %v", err)
	}
}

func TestRedirects(t *testing.T) {
	e := newEnv(t)
	innerPort, innerHits := server(t, func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("internal")) })
	port, _ := server(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/to-loopback":
			http.Redirect(w, r, "http://127.0.0.1:"+innerPort+"/", http.StatusFound)
		case "/to-internal-name":
			http.Redirect(w, r, "http://internal.test:"+innerPort+"/", http.StatusFound)
		case "/loop":
			http.Redirect(w, r, "/loop", http.StatusFound)
		default:
			_, _ = w.Write([]byte("fine"))
		}
	})
	e.lookup = func(h string) []netip.Addr {
		switch h {
		case "pub.test":
			return []netip.Addr{netip.MustParseAddr("127.0.0.1")}
		case "internal.test":
			return []netip.Addr{netip.MustParseAddr("10.0.0.7")}
		}
		return nil
	}
	e.permit("pub.test", port)
	// The redirect target 127.0.0.1 is not in the allowlist.
	if _, err := e.g.Do(context.Background(), ws, Request{URL: "http://pub.test:" + port + "/to-loopback"}); code(err) != fault.EgressDenied {
		t.Fatalf("redirect to loopback: %v", err)
	}
	// Listed, but it resolves to a private address.
	e.permit("internal.test", innerPort)
	if _, err := e.g.Do(context.Background(), ws, Request{URL: "http://pub.test:" + port + "/to-internal-name"}); code(err) != fault.EgressDenied {
		t.Fatalf("redirect to internal name: %v", err)
	}
	if innerHits.Load() != 0 {
		t.Fatal("internal server reached")
	}
	if _, err := e.g.Do(context.Background(), ws, Request{URL: "http://pub.test:" + port + "/loop"}); code(err) != fault.EgressFailed {
		t.Fatalf("redirect loop: %v", err)
	}
}

func TestDNSRebinding(t *testing.T) {
	e := newEnv(t)
	port, hits := server(t, func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("x")) })
	var calls atomic.Int64
	e.lookup = func(h string) []netip.Addr {
		if h != "rebind.test" {
			return nil
		}
		if calls.Add(1) == 1 {
			return []netip.Addr{netip.MustParseAddr("127.0.0.1")} // "public"
		}
		return []netip.Addr{netip.MustParseAddr("192.168.0.10")}
	}
	e.permit("rebind.test", port)
	if _, err := e.g.Do(context.Background(), ws, Request{URL: "http://rebind.test:" + port + "/"}); err != nil {
		t.Fatal(err)
	}
	e.g.Close() // force a new dial (and resolution) for the next request
	if _, err := e.g.Do(context.Background(), ws, Request{URL: "http://rebind.test:" + port + "/"}); code(err) != fault.EgressDenied {
		t.Fatalf("rebinding: %v", err)
	}
	if hits.Load() != 1 {
		t.Fatalf("hits %d", hits.Load())
	}
}

func TestResponseCaps(t *testing.T) {
	e := newEnv(t)
	big := bytes.Repeat([]byte("a"), 3<<20)
	var gz bytes.Buffer
	zw := gzip.NewWriter(&gz)
	_, _ = zw.Write(make([]byte, 50<<20))
	_ = zw.Close()
	port, _ := server(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/big":
			_, _ = w.Write(big)
		case "/bomb":
			w.Header().Set("Content-Encoding", "gzip")
			_, _ = w.Write(gz.Bytes())
		case "/slow":
			time.Sleep(2 * time.Second)
		default:
			_, _ = w.Write([]byte("small"))
		}
	})
	e.lookup = func(string) []netip.Addr { return []netip.Addr{netip.MustParseAddr("127.0.0.1")} }
	e.permit("cap.test", port)
	base := "http://cap.test:" + port
	if _, err := e.g.Do(context.Background(), ws, Request{URL: base + "/big"}); code(err) != fault.EgressResponseTooLarge {
		t.Fatalf("default cap: %v", err)
	}
	if r, err := e.g.Do(context.Background(), ws, Request{URL: base + "/big", MaxBytes: 4 << 20}); err != nil || len(r.Body) != 3<<20 {
		t.Fatalf("raised cap: %v", err)
	}
	if _, err := e.g.Do(context.Background(), ws, Request{URL: base + "/bomb", MaxBytes: 1 << 20}); code(err) != fault.EgressResponseTooLarge {
		t.Fatalf("gzip bomb (%d compressed bytes): %v", gz.Len(), err)
	}
	if _, err := e.g.Do(context.Background(), ws, Request{URL: base + "/x", MaxBytes: 1 << 30}); err != nil {
		t.Fatal(err)
	}
	if _, err := e.g.Do(context.Background(), ws, Request{URL: base + "/slow", Timeout: 200 * time.Millisecond}); code(err) != fault.EgressFailed {
		t.Fatalf("timeout: %v", err)
	}
	for _, h := range []string{"Host", "transfer-encoding", "Connection"} {
		if _, err := e.g.Do(context.Background(), ws, Request{URL: base + "/x", Headers: map[string]string{h: "x"}}); code(err) != fault.BadRequest {
			t.Errorf("header %s: %v", h, err)
		}
	}
	if _, err := e.g.Do(context.Background(), ws, Request{URL: base + "/x", Method: "CONNECT"}); code(err) != fault.BadRequest {
		t.Errorf("CONNECT: %v", err)
	}
}

func TestRateLimit(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	g := New(Config{
		PerMinute: func(ids.WorkspaceID) int { return 2 },
		Allowlist: func(context.Context, ids.WorkspaceID) ([]Entry, error) { return nil, nil },
		Now:       func() time.Time { return now },
	})
	do := func() error { _, err := g.Do(context.Background(), ws, Request{URL: "https://x.test/"}); return err }
	// Denied by the allowlist, but the requests still count.
	for range 2 {
		if code(do()) != fault.EgressDenied {
			t.Fatal("the first two should reach the policy check")
		}
	}
	if code(do()) != fault.RateLimited {
		t.Fatal("third should be rate limited")
	}
	if code(func() error {
		_, err := g.Do(context.Background(), ids.WorkspaceID("01J0000000000000000000000X"), Request{URL: "https://x.test/"})
		return err
	}()) != fault.EgressDenied {
		t.Fatal("other workspace has its own bucket")
	}
	now = now.Add(30 * time.Second)
	if code(do()) != fault.EgressDenied {
		t.Fatal("bucket should refill")
	}
}

func TestDeniedMessage(t *testing.T) {
	e := newEnv(t)
	_, err := e.g.Do(context.Background(), ws, Request{URL: "https://nope.test/"})
	var fe *fault.Error
	if !errors.As(err, &fe) || !strings.Contains(fe.Message, "The workspace allowlist does not include nope.test:443") {
		t.Fatalf("%#v", err)
	}
}
