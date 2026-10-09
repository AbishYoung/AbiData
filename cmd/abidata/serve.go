package main

import (
	"context"
	"errors"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"net/http/pprof"
	"os"
	"os/signal"
	"path/filepath"
	"runtime/debug"
	"syscall"
	"time"

	"abidata/internal/admin"
	"abidata/internal/auth"
	"abidata/internal/catalog"
	"abidata/internal/config"
	"abidata/internal/events"
	"abidata/internal/httpapi"
	"abidata/internal/ids"
	"abidata/internal/obs"
	"abidata/internal/records"
	"abidata/internal/sqlitex"
	"abidata/internal/workspace"
	"abidata/web"
)

func serve() error {
	cfg, err := config.FromEnv()
	if err != nil {
		return err
	}
	metrics := obs.NewMetrics()
	log := obs.NewLogger(os.Stdout, cfg.LogLevel)
	slog.SetDefault(log)
	if cfg.MaxMemoryMB > 0 {
		debug.SetMemoryLimit(int64(cfg.MaxMemoryMB) << 20)
	}
	for _, d := range []string{filepath.Join(cfg.DataDir, "system"), filepath.Join(cfg.DataDir, "workspaces"),
		filepath.Join(cfg.DataDir, "staging"), filepath.Join(cfg.DataDir, "artifacts"), cfg.BackupDir} {
		if err := os.MkdirAll(d, 0o750); err != nil {
			return err
		}
	}
	lock, err := acquireLock(cfg.DataDir)
	if err != nil {
		return err
	}
	defer lock.Release()

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	keys, err := auth.LoadKeys(filepath.Join(cfg.DataDir, "system", "keys"), cfg.SessionKey, cfg.ManifestKey)
	if err != nil {
		return err
	}
	adm, err := admin.Open(ctx, filepath.Join(cfg.DataDir, "system", "admin.db"), cfg.Synchronous)
	if err != nil {
		return err
	}
	defer func() { _ = adm.Close() }()
	authSvc := auth.NewService(adm, keys, auth.DefaultArgon2, nil, obs.Subsystem(log, "auth"))
	if tok, err := authSvc.PrepareBootstrap(ctx, cfg.BootstrapAdminEmail); err != nil {
		return err
	} else if tok != "" {
		log.Warn("no users exist: complete setup in the web app with this one-time token",
			"subsystem", "auth", "email", cfg.BootstrapAdminEmail, "setup_token", tok)
	}

	hub := events.NewHub(events.DefaultRing)
	schemas := catalog.NewCache()
	wsLog := obs.Subsystem(log, "workspace")
	// Migrate, then reconcile catalog vs physical schema (D5); drift marks the
	// workspace needs_repair and is logged, but does not block opening.
	migrate := func(ctx context.Context, id ids.WorkspaceID, c *sqlitex.Conn) error {
		if err := catalog.Migrate(ctx, id, c); err != nil {
			return err
		}
		s, err := catalog.Load(ctx, c)
		if err != nil {
			return err
		}
		drift, err := catalog.Reconcile(ctx, c, s)
		if err != nil {
			return err
		}
		if len(drift) > 0 {
			wsLog.Error("catalog drift detected", "workspace_id", id, "drift", drift)
			st := admin.StatusNeedsRepair
			_ = adm.UpdateWorkspace(ctx, id, admin.WorkspacePatch{Status: &st})
		}
		return nil
	}
	wm, err := workspace.NewManager(workspace.Config{
		DataDir: cfg.DataDir, MaxOpen: cfg.MaxOpenWorkspaces, Synchronous: cfg.Synchronous,
		Migrate: migrate, Observe: metricsObserver{metrics},
		MaxPageCount: func(ctx context.Context, id ids.WorkspaceID) (int64, error) {
			w, err := adm.GetWorkspace(ctx, id)
			if err != nil {
				return 0, err
			}
			return w.Quota.MaxPageCount, nil
		},
	})
	if err != nil {
		return err
	}

	spa, err := fs.Sub(web.Dist, "dist")
	if err != nil {
		return err
	}
	engine := &catalog.Engine{AfterCommit: func(_ context.Context, l *workspace.Lease, s *catalog.Schema) {
		schemas.Put(l.ID(), s)
		hub.Publish(string(l.ID()), events.SchemaChanged, map[string]any{"schema_version": s.Version})
	}}
	api := httpapi.New(httpapi.Deps{Config: cfg, Log: log, Metrics: metrics, Admin: adm, Auth: authSvc,
		Workspaces: wm, Schemas: schemas, Catalog: engine, Events: hub,
		Records: &records.Service{Schemas: schemas, Events: hub}, Version: version, SPA: spa})
	srv := &http.Server{Addr: cfg.ListenAddr, Handler: api, ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout: 120 * time.Second, MaxHeaderBytes: 64 << 10,
		BaseContext: func(net.Listener) context.Context { return context.Background() }}

	if cfg.DebugAddr != "" {
		go serveDebug(cfg.DebugAddr, log)
	}
	janitorCtx, stopJanitor := context.WithCancel(context.Background())
	go janitor(janitorCtx, log, wm, adm)

	errc := make(chan error, 1)
	go func() {
		log.Info("listening", "addr", cfg.ListenAddr, "version", version, "public_url", cfg.PublicURL)
		errc <- srv.ListenAndServe()
	}()
	select {
	case err := <-errc:
		stopJanitor()
		_ = wm.Shutdown(context.Background())
		return err
	case <-ctx.Done():
	}

	// Graceful shutdown (brief <deployment>).
	log.Info("shutting down")
	api.SetDraining()
	stopJanitor()
	shutCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutCtx); err != nil {
		log.Warn("http drain incomplete", "err", err)
	}
	wsCtx, cancel2 := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel2()
	if err := wm.Shutdown(wsCtx); err != nil {
		log.Warn("workspace shutdown incomplete", "err", err)
	}
	if err := <-errc; err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	log.Info("stopped")
	return nil
}

func janitor(ctx context.Context, log *slog.Logger, wm *workspace.Manager, adm *admin.Store) {
	t := time.NewTicker(5 * time.Minute)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		for _, id := range wm.OpenIDs() {
			l, err := wm.Acquire(ctx, id)
			if err != nil {
				continue
			}
			if err := records.PurgeIdempotency(ctx, l, time.Now()); err != nil {
				log.Warn("purge idempotency keys", "subsystem", "janitor", "workspace_id", id, "err", err)
			}
			l.Release()
		}
		wm.CloseIdle(ctx, 10*time.Minute)
		if n, err := adm.PurgeSessions(ctx, time.Now(), auth.IdleTimeout); err != nil {
			log.Warn("purge sessions", "subsystem", "janitor", "err", err)
		} else if n > 0 {
			log.Info("purged sessions", "subsystem", "janitor", "count", n)
		}
	}
}

func serveDebug(addr string, log *slog.Logger) {
	mux := http.NewServeMux()
	mux.HandleFunc("/debug/pprof/", pprof.Index)
	mux.HandleFunc("/debug/pprof/cmdline", pprof.Cmdline)
	mux.HandleFunc("/debug/pprof/profile", pprof.Profile)
	mux.HandleFunc("/debug/pprof/symbol", pprof.Symbol)
	mux.HandleFunc("/debug/pprof/trace", pprof.Trace)
	srv := &http.Server{Addr: addr, Handler: mux, ReadHeaderTimeout: 10 * time.Second}
	log.Info("debug server listening", "addr", addr)
	if err := srv.ListenAndServe(); err != nil {
		log.Error("debug server", "err", err)
	}
}

type metricsObserver struct{ m *obs.Metrics }

func (o metricsObserver) GateWait(p workspace.Priority, d time.Duration) {
	o.m.WriterGateWait.WithLabelValues(p.String()).Observe(d.Seconds())
}
func (o metricsObserver) GateHold(d time.Duration)   { o.m.WriterGateHold.Observe(d.Seconds()) }
func (o metricsObserver) ReaderWait(d time.Duration) { o.m.ReaderPoolWait.Observe(d.Seconds()) }
func (o metricsObserver) OpenWorkspaces(n int)       { o.m.OpenWorkspaces.Set(float64(n)) }
