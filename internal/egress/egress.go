// Package egress is the only way workspace code reaches the network (brief
// <egress>, amended by ADR 0008). A request must pass the platform
// administrator's global allowlist (scheme, host pattern, port) on every hop,
// and every
// connection is checked against PublicRoutable on the actual address being
// dialed, so DNS rebinding and redirects to internal addresses fail closed.
package egress

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"abidata/internal/fault"
	"abidata/internal/ids"
)

// Limits on a single request (the Lua fetch caps).
const (
	DefaultTimeout  = 10 * time.Second
	MaxTimeout      = 30 * time.Second
	DefaultMaxBytes = 2 << 20
	MaxBytes        = 10 << 20
	MaxRequestBody  = 10 << 20
	maxHeaders      = 64
	maxHeaderBytes  = 16 << 10
	maxRedirects    = 5
	// DefaultPerMinute is the per-workspace request rate when the quota does not set one.
	DefaultPerMinute = 60
)

// ErrBlockedAddress is returned when a connection would reach a non-public address.
var ErrBlockedAddress = errors.New("egress: address is not publicly routable")

// errDenied marks a request the allowlist rejects (wrapped with the reason).
var errDenied = errors.New("egress: denied by policy")

// Entry is one row of the platform allowlist.
type Entry struct {
	Pattern string `json:"pattern" maxLength:"255" doc:"Exact host (api.example.com) or *.domain for any subdomain."`
	Ports   string `json:"ports,omitempty" maxLength:"100" doc:"Comma-separated ports (default 443; list 80 to allow plain http when the platform permits it)."`
	Note    string `json:"note,omitempty" maxLength:"500"`
}

// Global is the platform administrator's policy (stored in _adm_settings
// "egress_policy"). It is the single source of truth: an empty allowlist
// permits no outbound request at all.
type Global struct {
	// Allowlist holds the hosts and ports every workspace's jobs may reach.
	Allowlist []Entry `json:"allowlist" maxItems:"200"`
	// AllowHTTP permits plain http (the port must still be listed).
	AllowHTTP bool `json:"allow_http,omitempty"`
	// Disabled turns all egress off.
	Disabled bool `json:"disabled,omitempty"`
}

// DefaultGlobal is the fail-closed policy of a fresh install: nothing is reachable.
var DefaultGlobal = Global{Allowlist: []Entry{}}

// ParseGlobal decodes a stored policy. Policies written before ADR 0008 have a
// "hosts" list (where "*" meant "each workspace decides") and no allowlist; they
// are converted by keeping the non-wildcard hosts as allowlist entries (port 443,
// plus 80 when plain http was enabled) and dropping "*". migrated reports that
// the stored value had the old shape and should be rewritten; dropped reports
// that a "*" was discarded, which the administrator should review.
func ParseGlobal(raw string) (g Global, migrated, dropped bool, err error) {
	var keys map[string]json.RawMessage
	if err = json.Unmarshal([]byte(raw), &keys); err != nil {
		return Global{}, false, false, err
	}
	var hosts []string
	if h, ok := keys["hosts"]; ok {
		if err = json.Unmarshal(h, &hosts); err != nil {
			return Global{}, false, false, err
		}
		migrated = true
		delete(keys, "hosts")
	}
	b, _ := json.Marshal(keys)
	if err = json.Unmarshal(b, &g); err != nil {
		return Global{}, false, false, err
	}
	if g.Allowlist == nil {
		g.Allowlist = []Entry{}
	}
	if _, has := keys["allowlist"]; has || !migrated {
		return g, migrated, false, nil
	}
	ports := "443"
	if g.AllowHTTP {
		ports = "443,80"
	}
	for _, h := range hosts {
		h = normHost(strings.TrimSpace(h))
		switch {
		case h == "*":
			dropped = true
		case ValidPattern(h):
			g.Allowlist = append(g.Allowlist, Entry{Pattern: h, Ports: ports, Note: "Migrated from the previous platform policy"})
		}
	}
	return g, migrated, dropped, nil
}

// Config wires a Gateway to its policy sources.
type Config struct {
	// Global returns the platform policy (default: DefaultGlobal, which denies everything).
	Global func(ctx context.Context) (Global, error)
	// PerMinute returns the workspace's request rate (0 → DefaultPerMinute).
	PerMinute func(ws ids.WorkspaceID) int
	// Routable decides which connect addresses are allowed (default PublicRoutable).
	// Tests inject a policy that treats a loopback test server as public.
	Routable func(netip.Addr) bool
	// Lookup resolves host names (default net.DefaultResolver.LookupNetIP).
	Lookup func(ctx context.Context, host string) ([]netip.Addr, error)
	Log    *slog.Logger
	Now    func() time.Time
}

// Gateway performs policy-checked HTTP requests for workspaces.
type Gateway struct {
	cfg Config
	tr  *http.Transport

	mu      sync.Mutex
	buckets map[ids.WorkspaceID]*bucket
}

// New returns a Gateway. One shared transport is used for all workspaces; the
// allowlist is enforced per request by the round tripper.
func New(cfg Config) *Gateway {
	if cfg.Global == nil {
		cfg.Global = func(context.Context) (Global, error) { return DefaultGlobal, nil }
	}
	if cfg.Routable == nil {
		cfg.Routable = PublicRoutable
	}
	if cfg.Lookup == nil {
		cfg.Lookup = func(ctx context.Context, host string) ([]netip.Addr, error) {
			return net.DefaultResolver.LookupNetIP(ctx, "ip", host)
		}
	}
	if cfg.Log == nil {
		cfg.Log = slog.New(slog.DiscardHandler)
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	g := &Gateway{cfg: cfg, buckets: map[ids.WorkspaceID]*bucket{}}
	g.tr = &http.Transport{
		Proxy:                  nil,
		DialContext:            g.dial,
		ForceAttemptHTTP2:      true,
		TLSHandshakeTimeout:    5 * time.Second,
		ResponseHeaderTimeout:  15 * time.Second,
		MaxResponseHeaderBytes: 64 << 10,
		MaxIdleConns:           64,
		IdleConnTimeout:        60 * time.Second,
	}
	return g
}

// Close releases idle connections.
func (g *Gateway) Close() { g.tr.CloseIdleConnections() }

// dial resolves the host itself and connects only to routable addresses; the
// dialer's Control hook re-checks the address actually being connected.
func (g *Gateway) dial(ctx context.Context, network, address string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return nil, err
	}
	var addrs []netip.Addr
	if a, err := netip.ParseAddr(host); err == nil {
		addrs = []netip.Addr{a}
	} else {
		addrs, err = g.cfg.Lookup(ctx, host)
		if err != nil {
			return nil, err
		}
	}
	d := &net.Dialer{Timeout: 5 * time.Second, ControlContext: func(_ context.Context, _, address string, _ syscall.RawConn) error {
		ap, err := netip.ParseAddrPort(address)
		if err != nil {
			return err
		}
		if !g.cfg.Routable(ap.Addr().Unmap()) {
			return ErrBlockedAddress
		}
		return nil
	}}
	var firstErr error
	for _, a := range addrs {
		a = a.Unmap()
		if !g.cfg.Routable(a) {
			if firstErr == nil {
				firstErr = ErrBlockedAddress
			}
			continue
		}
		c, err := d.DialContext(ctx, network, net.JoinHostPort(a.String(), port))
		if err == nil {
			return c, nil
		}
		if firstErr == nil || errors.Is(firstErr, ErrBlockedAddress) {
			firstErr = err
		}
	}
	if firstErr == nil {
		firstErr = fmt.Errorf("egress: no addresses for %s", host)
	}
	return nil, firstErr
}

// policyRT checks every request (including redirect hops) against the policy.
type policyRT struct {
	g      *Gateway
	global Global
}

func (p *policyRT) RoundTrip(req *http.Request) (*http.Response, error) {
	if err := p.g.check(req.URL, p.global); err != nil {
		return nil, err
	}
	return p.g.tr.RoundTrip(req)
}

// check enforces scheme, host pattern and port against the platform allowlist.
func (g *Gateway) check(u *url.URL, global Global) error {
	if global.Disabled {
		return fmt.Errorf("%w: Egress is disabled by the administrator", errDenied)
	}
	switch u.Scheme {
	case "https":
	case "http":
		if !global.AllowHTTP {
			return fmt.Errorf("%w: Only https is allowed", errDenied)
		}
	default:
		return fmt.Errorf("%w: Scheme %q is not allowed", errDenied, u.Scheme)
	}
	if u.User != nil {
		return fmt.Errorf("%w: Credentials in the URL are not allowed", errDenied)
	}
	host := normHost(u.Hostname())
	if host == "" {
		return fmt.Errorf("%w: The URL has no host", errDenied)
	}
	port := u.Port()
	if port == "" {
		port = "443"
		if u.Scheme == "http" {
			port = "80"
		}
	}
	for _, e := range global.Allowlist {
		if matchPattern(e.Pattern, host) && portListed(e.Ports, port) {
			return nil
		}
	}
	return fmt.Errorf("%w: The platform allowlist does not include %s:%s; ask a platform administrator to add it", errDenied, host, port)
}

func normHost(h string) string { return strings.TrimSuffix(strings.ToLower(h), ".") }

// matchPattern: exact host, or "*.domain" for any subdomain (not the apex).
func matchPattern(pattern, host string) bool {
	p := normHost(strings.TrimSpace(pattern))
	if p == "" {
		return false
	}
	if rest, ok := strings.CutPrefix(p, "*."); ok {
		return rest != "" && strings.HasSuffix(host, "."+rest)
	}
	// An IPv6 pattern may be written with brackets.
	return strings.Trim(p, "[]") == host
}

func portListed(ports, port string) bool {
	if strings.TrimSpace(ports) == "" {
		ports = "443"
	}
	for _, p := range strings.Split(ports, ",") {
		if strings.TrimSpace(p) == port {
			return true
		}
	}
	return false
}

// ValidPattern reports whether an allowlist pattern is well formed.
func ValidPattern(p string) bool {
	p = normHost(strings.TrimSpace(p))
	rest, wild := strings.CutPrefix(p, "*.")
	if wild {
		p = rest
	}
	if p == "" || len(p) > 253 || strings.ContainsAny(p, "*/:@ ") && !strings.HasPrefix(p, "[") {
		return false
	}
	if a, err := netip.ParseAddr(strings.Trim(p, "[]")); err == nil {
		return !wild && a.IsValid()
	}
	for _, label := range strings.Split(p, ".") {
		if label == "" || len(label) > 63 {
			return false
		}
		for _, c := range label {
			if c != '-' && (c < 'a' || c > 'z') && (c < '0' || c > '9') {
				return false
			}
		}
	}
	return true
}

// ValidPorts reports whether a ports list is well formed (1–65535, comma-separated).
func ValidPorts(s string) bool {
	if strings.TrimSpace(s) == "" {
		return true
	}
	for _, p := range strings.Split(s, ",") {
		n, err := strconv.Atoi(strings.TrimSpace(p))
		if err != nil || n < 1 || n > 65535 {
			return false
		}
	}
	return true
}

// Request is one outbound call.
type Request struct {
	Method   string
	URL      string
	Headers  map[string]string
	Body     []byte
	Timeout  time.Duration // 0 → DefaultTimeout; capped at MaxTimeout
	MaxBytes int64         // 0 → DefaultMaxBytes; capped at MaxBytes
}

// Response is a fully read response.
type Response struct {
	Status  int
	Headers map[string]string
	Body    []byte
}

// Do performs req for workspace ws.
func (g *Gateway) Do(ctx context.Context, ws ids.WorkspaceID, req Request) (*Response, error) {
	start := g.cfg.Now()
	u, err := url.Parse(req.URL)
	if err != nil || !u.IsAbs() {
		return nil, fault.New(fault.BadRequest, "fetch needs an absolute https URL.")
	}
	host := u.Hostname()
	resp, n, err := g.do(ctx, ws, u, req)
	status := 0
	if resp != nil {
		status = resp.Status
	}
	g.cfg.Log.LogAttrs(ctx, slog.LevelInfo, "egress", slog.String("subsystem", "egress"), slog.String("workspace_id", string(ws)),
		slog.String("host", host), slog.Int("status", status), slog.Int64("bytes", n),
		slog.Int64("ms", g.cfg.Now().Sub(start).Milliseconds()), slog.Bool("ok", err == nil))
	return resp, err
}

func (g *Gateway) do(ctx context.Context, ws ids.WorkspaceID, u *url.URL, req Request) (*Response, int64, error) {
	if !g.allow(ws) {
		return nil, 0, fault.New(fault.RateLimited, "Too many outbound requests from this workspace; try again in a minute.")
	}
	global, err := g.cfg.Global(ctx)
	if err != nil {
		return nil, 0, err
	}
	// Check before building anything so denied requests never touch the network.
	if err := g.check(u, global); err != nil {
		return nil, 0, classify(err)
	}

	method := strings.ToUpper(req.Method)
	if method == "" {
		method = http.MethodGet
	}
	switch method {
	case http.MethodGet, http.MethodHead, http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete, http.MethodOptions:
	default:
		return nil, 0, fault.Newf(fault.BadRequest, "Method %s is not allowed.", method)
	}
	if len(req.Body) > MaxRequestBody {
		return nil, 0, fault.New(fault.BadRequest, "The request body is larger than 10 MiB.")
	}
	timeout := req.Timeout
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	timeout = min(timeout, MaxTimeout)
	limit := req.MaxBytes
	if limit <= 0 {
		limit = DefaultMaxBytes
	}
	limit = min(limit, MaxBytes)

	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	var body io.Reader
	if req.Body != nil {
		body = strings.NewReader(string(req.Body))
	}
	hr, err := http.NewRequestWithContext(ctx, method, u.String(), body)
	if err != nil {
		return nil, 0, fault.Wrap(fault.BadRequest, err)
	}
	if len(req.Headers) > maxHeaders {
		return nil, 0, fault.New(fault.BadRequest, "Too many request headers.")
	}
	size := 0
	for k, v := range req.Headers {
		size += len(k) + len(v)
		ck := http.CanonicalHeaderKey(k)
		switch ck {
		case "Host", "Connection", "Proxy-Authorization", "Proxy-Connection", "Te", "Trailer", "Transfer-Encoding", "Upgrade", "Content-Length":
			return nil, 0, fault.Newf(fault.BadRequest, "Header %s cannot be set.", ck)
		}
		hr.Header.Set(k, v)
	}
	if size > maxHeaderBytes {
		return nil, 0, fault.New(fault.BadRequest, "Request headers are larger than 16 KiB.")
	}
	if hr.Header.Get("User-Agent") == "" {
		hr.Header.Set("User-Agent", "AbiData")
	}

	client := &http.Client{
		Transport: &policyRT{g: g, global: global},
		CheckRedirect: func(next *http.Request, via []*http.Request) error {
			if len(via) >= maxRedirects {
				return fault.New(fault.EgressFailed, "Too many redirects.")
			}
			// Each hop is a request: it costs a rate-limit token.
			if !g.allow(ws) {
				return fault.New(fault.RateLimited, "Too many outbound requests from this workspace; try again in a minute.")
			}
			// Script-set headers (API keys, tokens) never follow a redirect to
			// another host; net/http only strips Authorization and Cookie.
			if !strings.EqualFold(next.URL.Host, via[0].URL.Host) {
				ua := next.Header.Get("User-Agent")
				next.Header = http.Header{} //nolint:gosec // G119 false positive: this clears headers on a cross-host hop
				next.Header.Set("User-Agent", ua)
			}
			return nil
		},
	}
	resp, err := client.Do(hr)
	if err != nil {
		return nil, 0, classify(err)
	}
	defer func() { _ = resp.Body.Close() }()
	// resp.Body is the decoded stream when the transport negotiated compression,
	// so the cap also bounds decompressed bytes.
	b, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	n := int64(len(b))
	if err != nil {
		return &Response{Status: resp.StatusCode}, n, classify(err)
	}
	if n > limit {
		return &Response{Status: resp.StatusCode}, n, fault.Newf(fault.EgressResponseTooLarge, "The response is larger than %d bytes.", limit)
	}
	h := make(map[string]string, len(resp.Header))
	for k, v := range resp.Header {
		h[strings.ToLower(k)] = strings.Join(v, ", ")
	}
	return &Response{Status: resp.StatusCode, Headers: h, Body: b}, n, nil
}

func classify(err error) error {
	var fe *fault.Error
	switch {
	case errors.Is(err, errDenied):
		_, msg, _ := strings.Cut(err.Error(), errDenied.Error()+": ")
		return fault.New(fault.EgressDenied, msg+".")
	case errors.Is(err, ErrBlockedAddress):
		return fault.New(fault.EgressDenied, "The host resolves to an address that is not publicly routable.")
	case errors.As(err, &fe):
		return fe
	case errors.Is(err, context.DeadlineExceeded):
		return fault.New(fault.EgressFailed, "The request timed out.")
	default:
		return fault.Wrap(fault.EgressFailed, err)
	}
}

// bucket is a token bucket refilled at perMinute/60 tokens per second.
type bucket struct {
	tokens float64
	last   time.Time
}

func (g *Gateway) allow(ws ids.WorkspaceID) bool {
	per := 0
	if g.cfg.PerMinute != nil {
		per = g.cfg.PerMinute(ws)
	}
	if per <= 0 {
		per = DefaultPerMinute
	}
	now := g.cfg.Now()
	g.mu.Lock()
	defer g.mu.Unlock()
	b, ok := g.buckets[ws]
	if !ok {
		b = &bucket{tokens: float64(per), last: now}
		g.buckets[ws] = b
	}
	b.tokens = min(float64(per), b.tokens+now.Sub(b.last).Seconds()*float64(per)/60)
	b.last = now
	if b.tokens < 1 {
		return false
	}
	b.tokens--
	return true
}

var denied4, denied6 = mustPrefixes(
	"0.0.0.0/8", "10.0.0.0/8", "100.64.0.0/10", "127.0.0.0/8", "169.254.0.0/16", "172.16.0.0/12",
	"192.0.0.0/24", "192.0.2.0/24", "192.88.99.0/24", "192.168.0.0/16", "198.18.0.0/15",
	"198.51.100.0/24", "203.0.113.0/24", "224.0.0.0/4", "240.0.0.0/4", "255.255.255.255/32",
), mustPrefixes("2001::/23", "2001:db8::/32", "2002::/16", "64:ff9b::/96")

var global6 = netip.MustParsePrefix("2000::/3")

func mustPrefixes(s ...string) []netip.Prefix {
	out := make([]netip.Prefix, len(s))
	for i, p := range s {
		out[i] = netip.MustParsePrefix(p)
	}
	return out
}

// PublicRoutable reports whether a is a public unicast address (brief <egress>).
// IPv4-mapped IPv6 addresses are judged as IPv4.
func PublicRoutable(a netip.Addr) bool {
	a = a.Unmap()
	if !a.IsValid() || a.Zone() != "" {
		return false
	}
	if a.Is4() {
		for _, p := range denied4 {
			if p.Contains(a) {
				return false
			}
		}
		return true
	}
	if !global6.Contains(a) {
		return false
	}
	for _, p := range denied6 {
		if p.Contains(a) {
			return false
		}
	}
	return true
}
