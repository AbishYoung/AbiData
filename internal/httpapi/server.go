// Package httpapi is the HTTP surface: chi router, huma-generated OpenAPI 3.1
// operations, the middleware chain (request ID → security headers → access log
// → session → CSRF/Origin → workspace membership → role guard), health checks,
// and the embedded SPA.
package httpapi

import (
	"context"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humachi"
	"github.com/go-chi/chi/v5"

	"abidata/internal/admin"
	"abidata/internal/artifacts"
	"abidata/internal/auth"
	"abidata/internal/backup"
	"abidata/internal/catalog"
	"abidata/internal/config"
	"abidata/internal/docsrv"
	"abidata/internal/egress"
	"abidata/internal/events"
	"abidata/internal/forms"
	"abidata/internal/ingest"
	"abidata/internal/jobs"
	"abidata/internal/obs"
	"abidata/internal/ops"
	"abidata/internal/query"
	"abidata/internal/records"
	"abidata/internal/reports"
	"abidata/internal/scripts"
	"abidata/internal/workspace"
)

// Deps are the services the API serves.
type Deps struct {
	Config     config.Config
	Log        *slog.Logger
	Metrics    *obs.Metrics
	Admin      *admin.Store
	Auth       *auth.Service
	Workspaces *workspace.Manager
	Schemas    *catalog.Cache
	Catalog    *catalog.Engine
	Records    *records.Service
	Queries    *query.Service
	Forms      *forms.Service
	Scripts    *scripts.Service
	Reports    *reports.Service
	Events     *events.Hub
	Docs       *docsrv.Site     // rendered documentation (nil: docs routes answer NOT_FOUND)
	Logs       *events.Bus      // server log records for the admin live log stream (nil: 404)
	Jobs       *jobs.Service    // nil: job routes answer NOT_FOUND
	Ops        *ops.Service     // long-running operations (imports, exports)
	Artifacts  *artifacts.Store // job/export output files
	Egress     *egress.Gateway
	Ingest     *ingest.Service // imports and exports; nil: ingest routes answer NOT_FOUND
	Backups    *backup.Service // backups and restores; nil: backup routes answer NOT_FOUND
	Version    string
	SPA        fs.FS // contents of web/dist; nil serves nothing
}

// Server is the HTTP handler.
type Server struct {
	deps     Deps
	log      *slog.Logger
	router   chi.Router
	api      huma.API
	draining atomic.Bool
	started  time.Time
	// keyRecheck overrides keyStreamRecheck (tests).
	keyRecheck time.Duration
}

// New builds the server and registers every route.
func New(deps Deps) *Server {
	log := deps.Log
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	installHumaErrors(log)
	s := &Server{deps: deps, log: obs.Subsystem(log, "http"), started: time.Now()}
	r := chi.NewMux()
	r.Use(s.requestID, securityHeaders, s.accessLog)

	cfg := huma.DefaultConfig("AbiData API", deps.Version)
	cfg.OpenAPIPath = "/api/v1/openapi"
	cfg.DocsPath = ""
	cfg.SchemasPath = ""
	cfg.CreateHooks = nil // no $schema links in bodies
	cfg.Info.Description = "AbiData REST API. Errors use the {\"error\":{...}} envelope."
	cfg.Components.SecuritySchemes = map[string]*huma.SecurityScheme{
		"apiKey":  {Type: "http", Scheme: "bearer", Description: "A workspace API key (abd_...). Valid only on /api/v1/w/{ws}/... routes of its own workspace, and never on key management; see the API access guide."},
		"session": {Type: "apiKey", In: "cookie", Name: "abidata_session", Description: "The browser session cookie (`__Host-abidata_session` over https). Unsafe methods also need X-CSRF-Token and a matching Origin."},
	}
	cfg.Security = []map[string][]string{{"apiKey": {}}, {"session": {}}}
	api := humachi.New(r, cfg)
	api.UseMiddleware(s.authenticate)
	s.router, s.api = r, api

	r.Get("/healthz", s.healthz)
	r.Get("/readyz", s.readyz)
	r.Get("/metrics", s.metricsHandler)
	r.Get("/api/v1/w/{ws}/events", s.eventsHandler)
	r.Get("/api/v1/admin/logs/stream", s.logsHandler)
	r.Get("/api/v1/docs/highlight.css", s.docsCSS)

	s.registerAuth(api)
	s.registerAdmin(api)
	s.registerWorkspace(api)
	s.registerData(api)
	s.registerQueries(api)
	s.registerForms(api)
	s.registerReports(api)
	s.registerJobs(api)
	s.registerEgress(api)
	s.registerIngest(api)
	s.registerBackups(api)
	s.registerAPIKeys(api)
	s.registerDocs(api)

	r.NotFound(s.notFound)
	return s
}

// API exposes the huma API (for OpenAPI generation).
func (s *Server) API() huma.API { return s.api }

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) { s.router.ServeHTTP(w, r) }

// SetDraining makes /readyz report 503 during shutdown.
func (s *Server) SetDraining() { s.draining.Store(true) }

func (s *Server) healthz(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write([]byte("ok\n"))
}

func (s *Server) readyz(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	fail := func(reason string) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte(reason + "\n"))
	}
	if s.draining.Load() {
		fail("draining")
		return
	}
	if err := s.deps.Admin.Ping(r.Context()); err != nil {
		fail("admin db unavailable")
		return
	}
	f, err := os.CreateTemp(filepath.Join(s.deps.Config.DataDir, "system"), ".readyz-*")
	if err != nil {
		fail("data dir not writable")
		return
	}
	_ = f.Close()
	_ = os.Remove(f.Name())
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = w.Write([]byte("ready\n"))
}

// metricsHandler serves Prometheus metrics to platform admins (session auth).
func (s *Server) metricsHandler(w http.ResponseWriter, r *http.Request) {
	if rejectBearer(r) {
		writeError(w, r, s.log, errUnauth)
		return
	}
	c, err := r.Cookie(s.cookieName())
	if err != nil {
		writeError(w, r, s.log, errUnauth)
		return
	}
	p, err := s.deps.Auth.Authenticate(r.Context(), c.Value)
	if err != nil {
		writeError(w, r, s.log, err)
		return
	}
	if !p.IsPlatformAdmin {
		writeError(w, r, s.log, errForbidden)
		return
	}
	if s.deps.Metrics == nil {
		http.NotFound(w, r)
		return
	}
	s.deps.Metrics.Handler().ServeHTTP(w, r)
}

// notFound serves the SPA for non-API paths (index.html fallback); API paths 404
// with the error envelope.
func (s *Server) notFound(w http.ResponseWriter, r *http.Request) {
	if strings.HasPrefix(r.URL.Path, "/api/") || s.deps.SPA == nil || (r.Method != http.MethodGet && r.Method != http.MethodHead) {
		writeError(w, r, s.log, errNotFound)
		return
	}
	name := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
	if name != "" && name != "index.html" {
		if st, err := fs.Stat(s.deps.SPA, name); err == nil && !st.IsDir() {
			if strings.HasPrefix(name, "assets/") {
				w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
			} else {
				w.Header().Set("Cache-Control", "no-cache")
			}
			http.ServeFileFS(w, r, s.deps.SPA, name) //nolint:gosec // name is path.Clean-ed and fs.FS rejects ".." elements
			return
		}
		if strings.HasPrefix(name, "assets/") {
			http.NotFound(w, r)
			return
		}
	}
	body, err := fs.ReadFile(s.deps.SPA, "index.html")
	if err != nil {
		http.Error(w, "The web app has not been built. Run `make web-build`.", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	_, _ = w.Write(body)
}

func (s *Server) cookieName() string {
	if s.deps.Config.SecureCookies() {
		return "__Host-abidata_session"
	}
	return "abidata_session"
}

func (s *Server) sessionCookie(value string, maxAge int) *http.Cookie {
	//nolint:gosec // Secure follows the public URL scheme: plain-http dev servers cannot set Secure cookies.
	return &http.Cookie{Name: s.cookieName(), Value: value, Path: "/", HttpOnly: true,
		Secure: s.deps.Config.SecureCookies(), SameSite: http.SameSiteLaxMode, MaxAge: maxAge}
}

// detached gives work that must finish (e.g. audit) a context that survives client aborts.
func detached(ctx context.Context) context.Context { return context.WithoutCancel(ctx) }

func chiParam(r *http.Request, name string) string { return chi.URLParam(r, name) }
