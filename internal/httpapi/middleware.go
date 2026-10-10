package httpapi

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"net/netip"
	"strconv"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/go-chi/chi/v5"

	"abidata/internal/admin"
	"abidata/internal/auth"
	"abidata/internal/fault"
	"abidata/internal/ids"
	"abidata/internal/obs"
)

type ctxKey int

const (
	keyPrincipal ctxKey = iota
	keyClientIP
	keyMembership
)

// Membership is the caller's access to the workspace named in the path.
type Membership struct {
	Workspace admin.Workspace
	Role      admin.Role
}

// PrincipalFrom returns the authenticated caller, if any.
func PrincipalFrom(ctx context.Context) (auth.Principal, bool) {
	p, ok := ctx.Value(keyPrincipal).(auth.Principal)
	return p, ok
}

// MembershipFrom returns the caller's workspace membership for /w/{ws} routes.
func MembershipFrom(ctx context.Context) (Membership, bool) {
	m, ok := ctx.Value(keyMembership).(Membership)
	return m, ok
}

func clientIP(ctx context.Context) string {
	ip, _ := ctx.Value(keyClientIP).(string)
	return ip
}

// Operation metadata keys.
const (
	metaAccess = "access" // "public" | "session" (default) | "platform_admin"
	metaRole   = "role"   // admin.Role minimum for /w/{ws} routes (default viewer)
)

// --- chi (outer) middleware -------------------------------------------------

func (s *Server) requestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := ids.New()
		w.Header().Set("X-Request-ID", id)
		ctx := obs.WithRequestID(r.Context(), id)
		ctx = context.WithValue(ctx, keyClientIP, s.remoteIP(r))
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// remoteIP honours X-Forwarded-For only when the direct peer is a trusted proxy,
// taking the right-most address that is not itself trusted. When every entry
// is trusted (a visitor on the same private network as the proxies), the
// left-most entry is the client: falling back to the peer would put every such
// visitor under the proxy's address (Phase 7 review M2).
func (s *Server) remoteIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	peer, err := netip.ParseAddr(host)
	if err != nil || !s.trusted(peer) {
		return host
	}
	xff := r.Header.Values("X-Forwarded-For")
	parts := strings.Split(strings.Join(xff, ","), ",")
	client := ""
	for i := len(parts) - 1; i >= 0; i-- {
		a, err := netip.ParseAddr(strings.TrimSpace(parts[i]))
		if err != nil {
			break
		}
		if !s.trusted(a) {
			return a.Unmap().String()
		}
		client = a.Unmap().String()
	}
	if client != "" {
		return client
	}
	return host
}

func (s *Server) trusted(a netip.Addr) bool {
	a = a.Unmap()
	for _, p := range s.deps.Config.TrustedProxies {
		if p.Contains(a) {
			return true
		}
	}
	return false
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; worker-src 'self' blob:; connect-src 'self'; img-src 'self' data:; frame-ancestors 'none'")
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "same-origin")
		next.ServeHTTP(w, r)
	})
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(code int) {
	if r.status == 0 {
		r.status = code
	}
	r.ResponseWriter.WriteHeader(code)
}

func (r *statusRecorder) Write(b []byte) (int, error) {
	if r.status == 0 {
		r.status = http.StatusOK
	}
	return r.ResponseWriter.Write(b)
}

// Flush supports SSE through the recorder.
func (r *statusRecorder) Flush() {
	if f, ok := r.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func (r *statusRecorder) Unwrap() http.ResponseWriter { return r.ResponseWriter }

func (s *Server) accessLog(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w}
		defer func() {
			if p := recover(); p != nil {
				s.log.ErrorContext(r.Context(), "panic", "panic", p)
				if rec.status == 0 {
					writeError(rec, r, s.log, errors.New("handler panic"))
				}
			}
			route := "unmatched"
			if rc := chi.RouteContext(r.Context()); rc != nil && rc.RoutePattern() != "" {
				route = rc.RoutePattern()
			}
			d := time.Since(start)
			if s.deps.Metrics != nil {
				s.deps.Metrics.HTTPRequests.WithLabelValues(methodLabel(r.Method), route, strconv.Itoa(rec.status)).Observe(d.Seconds())
			}
			level := slog.LevelInfo
			if rec.status >= 500 {
				level = slog.LevelError
			}
			if strings.HasPrefix(r.URL.Path, "/api/") || rec.status >= 400 {
				s.log.Log(r.Context(), level, "http", "method", r.Method, "route", route, "status", rec.status,
					"ms", d.Milliseconds(), "ip", clientIP(r.Context()))
			}
		}()
		next.ServeHTTP(rec, r)
	})
}

// --- huma (API) middleware -------------------------------------------------

func isUnsafe(method string) bool {
	switch method {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
		return false
	}
	return true
}

func metaString(op *huma.Operation, key string) string {
	if op == nil || op.Metadata == nil {
		return ""
	}
	v, _ := op.Metadata[key].(string)
	return v
}

// authenticate resolves the session cookie, enforces CSRF + Origin on unsafe
// methods, then workspace membership and role for /w/{ws} routes.
func (s *Server) authenticate(hctx huma.Context, next func(huma.Context)) {
	op := hctx.Operation()
	access := metaString(op, metaAccess)
	unsafe := isUnsafe(hctx.Method())

	// Origin check for every unsafe request, authenticated or not (login CSRF).
	if unsafe && hctx.Header("Origin") != s.deps.Config.PublicURL {
		writeHumaError(hctx, s.log, fault.New(fault.Forbidden, "The request origin is not allowed."))
		return
	}

	var principal *auth.Principal
	if c, err := huma.ReadCookie(hctx, s.cookieName()); err == nil && c.Value != "" {
		p, err := s.deps.Auth.Authenticate(hctx.Context(), c.Value)
		if err == nil {
			principal = &p
		} else if fault.Classify(err).Code != fault.Unauthenticated {
			writeHumaError(hctx, s.log, err)
			return
		}
	}
	if access == "public" {
		if principal != nil {
			hctx = huma.WithValue(hctx, keyPrincipal, *principal)
		}
		next(hctx)
		return
	}
	if principal == nil {
		writeHumaError(hctx, s.log, fault.E(fault.Unauthenticated))
		return
	}
	if unsafe && !constantEq(hctx.Header("X-CSRF-Token"), principal.CSRFToken) {
		writeHumaError(hctx, s.log, fault.New(fault.Forbidden, "The request is missing a valid CSRF token. Reload the page and try again."))
		return
	}
	ctx := obs.WithUser(hctx.Context(), principal.UserID)
	hctx = huma.WithContext(hctx, context.WithValue(ctx, keyPrincipal, *principal))

	if access == "platform_admin" && !principal.IsPlatformAdmin {
		writeHumaError(hctx, s.log, fault.E(fault.Forbidden))
		return
	}

	if strings.Contains(op.Path, "{ws}") {
		m, err := s.resolveMembership(hctx.Context(), *principal, hctx.Param("ws"))
		if err != nil {
			writeHumaError(hctx, s.log, err)
			return
		}
		min := admin.Role(metaString(op, metaRole))
		if min == "" {
			min = admin.RoleViewer
		}
		if !m.Role.AtLeast(min) {
			writeHumaError(hctx, s.log, fault.E(fault.Forbidden))
			return
		}
		ctx := obs.WithWorkspace(hctx.Context(), string(m.Workspace.ID))
		hctx = huma.WithContext(hctx, context.WithValue(ctx, keyMembership, m))
	}
	next(hctx)
}

// resolveMembership never reveals whether a workspace exists to non-members:
// invalid IDs, unknown workspaces and non-membership are all NOT_FOUND.
func (s *Server) resolveMembership(ctx context.Context, p auth.Principal, raw string) (Membership, error) {
	id, err := ids.ParseWorkspaceID(raw)
	if err != nil {
		return Membership{}, fault.E(fault.NotFound)
	}
	role, err := s.deps.Admin.GetRole(ctx, id, p.UserID)
	if errors.Is(err, admin.ErrNotFound) {
		return Membership{}, fault.E(fault.NotFound)
	}
	if err != nil {
		return Membership{}, err
	}
	ws, err := s.deps.Admin.GetWorkspace(ctx, id)
	if errors.Is(err, admin.ErrNotFound) {
		return Membership{}, fault.E(fault.NotFound)
	}
	if err != nil {
		return Membership{}, err
	}
	switch ws.Status {
	case admin.StatusDeleted:
		return Membership{}, fault.E(fault.NotFound)
	case admin.StatusSuspended:
		return Membership{}, fault.New(fault.Forbidden, "This workspace is suspended.")
	case admin.StatusRestoring:
		return Membership{}, &fault.Error{Code: fault.WorkspaceMaintenance, RetryAfter: 5 * time.Second}
	}
	return Membership{Workspace: ws, Role: role}, nil
}

func constantEq(a, b string) bool {
	if len(a) != len(b) || a == "" {
		return false
	}
	var v byte
	for i := 0; i < len(a); i++ {
		v |= a[i] ^ b[i]
	}
	return v == 0
}

// methodLabel bounds the metrics method label: any token is a valid method, so
// a raw label lets anyone mint series without limit (Phase 7 review M1).
func methodLabel(m string) string {
	switch m {
	case http.MethodGet, http.MethodHead, http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete, http.MethodOptions:
		return m
	}
	return "other"
}
