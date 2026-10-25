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
	"runtime"
	"runtime/debug"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"abidata/docs"
	"abidata/internal/admin"
	"abidata/internal/artifacts"
	"abidata/internal/auth"
	"abidata/internal/backup"
	"abidata/internal/blogs"
	"abidata/internal/catalog"
	"abidata/internal/config"
	"abidata/internal/dbx"
	"abidata/internal/demo"
	"abidata/internal/docsrv"
	"abidata/internal/egress"
	"abidata/internal/events"
	"abidata/internal/files"
	"abidata/internal/forms"
	"abidata/internal/httpapi"
	"abidata/internal/ids"
	"abidata/internal/ingest"
	"abidata/internal/jobs"
	"abidata/internal/obs"
	"abidata/internal/ops"
	"abidata/internal/pgdb"
	"abidata/internal/query"
	"abidata/internal/records"
	"abidata/internal/reports"
	"abidata/internal/script"
	_ "abidata/internal/script/modules/all"
	"abidata/internal/scripts"
	"abidata/internal/sqlitex"
	"abidata/internal/workspace"
	"abidata/web"
)

func serve() error {
	cfg, err := config.FromEnv()
	if err != nil {
		return err
	}
	if err := checkBackend(context.Background(), cfg); err != nil {
		return err
	}
	metrics := obs.NewMetrics()
	sampler := obs.NewSampler(metrics)
	// Records at info and above also feed the admin console's live log stream.
	logBus := events.NewBus(5000)
	log := obs.NewLogger(os.Stdout, cfg.LogLevel, obs.NewBusHandler(logBus, slog.LevelInfo))
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
	// afterOpen runs once a workspace has been opened (set below, once the
	// services it needs exist): reconcile interrupted operations, rebuild the job index.
	var afterOpen func(id ids.WorkspaceID)
	var adm *admin.Store
	var pgCfg *workspace.PostgresConfig
	if cfg.DB == config.DBPostgres {
		pc, err := pgxpool.ParseConfig(cfg.PostgresURL)
		if err != nil {
			return err
		}
		appPool, err := pgxpool.NewWithConfig(ctx, pc)
		if err != nil {
			return err
		}
		defer appPool.Close()

		n := pgdb.Names{Prefix: cfg.PostgresPrefix}
		if err := pgdb.InstallLib(ctx, pgdb.Pool(appPool), n); err != nil {
			return err
		}
		dialect, err := pgdb.Dialect(ctx, pgdb.Pool(appPool))
		if err != nil {
			return err
		}

		adm, err = admin.OpenPostgres(ctx, pc, n)
		if err != nil {
			return err
		}

		pgMigrate := func(ctx context.Context, id ids.WorkspaceID, d dbx.Execer, n pgdb.Names) error {
			if err := catalog.MigratePostgres(ctx, id, d, n); err != nil {
				return err
			}
			if afterOpen != nil {
				go afterOpen(id)
			}
			return nil
		}

		pgCfg = &workspace.PostgresConfig{
			Pool:     appPool,
			Database: pc.ConnConfig.Database,
			Prefix:   cfg.PostgresPrefix,
			Dialect:  dialect,
			Migrate:  pgMigrate,
			Adopt: func(ctx context.Context, id ids.WorkspaceID, src string) error {
				return catalog.AdoptPostgres(ctx, appPool, pc.ConnConfig.Database, cfg.PostgresPrefix, dialect, id, src)
			},
		}
	} else {
		adm, err = admin.Open(ctx, filepath.Join(cfg.DataDir, "system", "admin.db"), cfg.Synchronous)
		if err != nil {
			return err
		}
	}
	defer func() { _ = adm.Close() }()
	if err := httpapi.MigrateEgressPolicy(ctx, adm, obs.Subsystem(log, "egress")); err != nil {
		return err
	}
	authSvc := auth.NewService(adm, keys, auth.DefaultArgon2, nil, obs.Subsystem(log, "auth"))
	// Users created before usernames get one derived from their email (ADR 0017).
	if n, err := authSvc.BackfillUsernames(ctx); err != nil {
		return err
	} else if n > 0 {
		log.Info("assigned usernames to existing users", "subsystem", "auth", "count", n)
	}
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
		if len(drift) == 0 {
			if err := query.RefreshCompiled(ctx, c, s); err != nil {
				// Stale stored SQL is not fatal: the query still runs from its model.
				wsLog.Warn("refreshing compiled queries failed", "workspace_id", id, "err", err)
			}
		}
		if afterOpen != nil {
			go afterOpen(id)
		}
		return nil
	}
	wm, err := workspace.NewManager(workspace.Config{
		DataDir: cfg.DataDir, MaxOpen: cfg.MaxOpenWorkspaces, Synchronous: cfg.Synchronous,
		Migrate: migrate, Observe: metricsObserver{metrics},
		Postgres: pgCfg,
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

	backups := &backup.Service{DataDir: cfg.DataDir, BackupDir: cfg.BackupDir, Admin: adm, Workspaces: wm,
		Key: keys.Manifest, AppVersion: version, Log: obs.Subsystem(log, "backup"),
		Observe: func(ws ids.WorkspaceID, kind string, took time.Duration, gz int64, err error) {
			if err != nil {
				sampler.RecordFailure(obs.Failure{Kind: "backup", WorkspaceID: string(ws), Subject: kind, Status: "failed", Message: err.Error()})
				return
			}
			metrics.BackupDuration.Observe(took.Seconds())
			metrics.BackupBytes.Observe(float64(gz))
		}}

	docContent, err := fs.Sub(docs.Content, "content")
	if err != nil {
		return err
	}
	site, err := docsrv.Build(docContent, docsrv.Generated())
	if err != nil {
		return err
	}

	spa, err := fs.Sub(web.Dist, "dist")
	if err != nil {
		return err
	}
	recSvc := &records.Service{Schemas: schemas, Events: hub}
	// Blog posts follow the same rules from every write path (ADR 0017).
	recSvc.Hook = (&blogs.Rules{Records: recSvc}).Hook
	querySvc := &query.Service{Schemas: schemas, Events: hub}
	scriptSvc := &scripts.Service{Events: hub}
	runner := script.NewRunner(2*runtime.GOMAXPROCS(0), 0)
	runner.Observe = func(kind script.Context, took time.Duration, err error) {
		outcome := "ok"
		if err != nil {
			outcome = "error"
		}
		metrics.ScriptRuns.WithLabelValues(string(kind), outcome).Inc()
		metrics.ScriptDuration.WithLabelValues(string(kind)).Observe(took.Seconds())
	}
	engine := &catalog.Engine{Validators: []catalog.Validator{query.SchemaValidator, forms.SchemaValidator, reports.SchemaValidator}, AfterCommit: func(_ context.Context, l *workspace.Lease, s *catalog.Schema) {
		schemas.Put(l.ID(), s)
		hub.Publish(string(l.ID()), events.SchemaChanged, map[string]any{"schema_version": s.Version})
	}}
	// User blogs (ADR 0017): provisioning, and the guard on their pinned tables.
	blogSvc := &blogs.Provisioner{Admin: adm, Workspaces: wm, Catalog: engine, Schemas: schemas, Log: obs.Subsystem(log, "blogs")}
	engine.Guard = blogSvc.Guard
	// Destructive schema changes and large chunked import replaces take a restore point first.
	restorePoint := func(ctx context.Context, l *workspace.Lease) error {
		_, err := backups.Snapshot(ctx, l, admin.BackupRestorePoint, "system")
		return err
	}
	engine.RestorePoint = restorePoint
	backups.AfterRestore = func(_ context.Context, id ids.WorkspaceID) {
		schemas.Drop(id)
		hub.Publish(string(id), events.Reset, map[string]any{})
	}
	quota := func(id ids.WorkspaceID) admin.Quota {
		w, err := adm.GetWorkspace(context.Background(), id)
		if err != nil {
			return admin.Quota{}
		}
		return w.Quota
	}
	runner.PerWorkspace = func(id ids.WorkspaceID) int { return quota(id).ScriptConcurrency }
	fileSvc := &files.Service{DataDir: cfg.DataDir, MaxFileBytes: cfg.MaxFileBytes,
		Quota:    func(id ids.WorkspaceID) int64 { return quota(id).MaxFileBytes },
		OnUpload: func(n int64) { metrics.FilesUploaded.Inc(); metrics.FileBytes.Add(float64(n)) },
		OnPurge:  func(n int, b int64) { metrics.FilesPurged.Add(float64(n)); metrics.FilePurgeBytes.Add(float64(b)) }}
	backups.Files = fileSvc
	arts := &artifacts.Store{Dir: filepath.Join(cfg.DataDir, "artifacts"), Quota: func(id ids.WorkspaceID) int64 { return quota(id).ArtifactBytes }}
	gateway := egress.New(egress.Config{
		Global:    func(ctx context.Context) (egress.Global, error) { return httpapi.LoadGlobalEgress(ctx, adm) },
		PerMinute: func(id ids.WorkspaceID) int { return quota(id).EgressPerMinute },
		Log:       obs.Subsystem(log, "egress"),
		Observe:   func(_, outcome string) { metrics.EgressRequests.WithLabelValues(outcome).Inc() },
	})
	defer gateway.Close()
	opsSvc := &ops.Service{Workspaces: wm, Events: hub, Log: obs.Subsystem(log, "ops")}
	ingestSvc := &ingest.Service{DataDir: cfg.DataDir, Workspaces: wm, Schemas: schemas, Catalog: engine, Records: recSvc,
		Queries: querySvc, Ops: opsSvc, Artifacts: arts, RestorePoint: restorePoint}
	jobStore := &jobs.SQLStore{Admin: adm, Workspaces: wm}
	jobSvc := &jobs.Service{Scripts: scriptSvc, Events: hub}
	executor := &jobs.LuaExecutor{Workspaces: wm, Scripts: scriptSvc, Runner: runner, Schemas: schemas, Records: recSvc, Files: fileSvc, Egress: gateway, Artifacts: arts, Ingest: ingestSvc,
		Reports: &reports.Service{Schemas: schemas, Records: recSvc, Files: fileSvc, Queries: querySvc, Scripts: scriptSvc, Runner: runner}}
	sched := jobs.New(jobs.Config{Store: jobStore, Executor: executor, Workers: cfg.JobWorkers, Log: obs.Subsystem(log, "jobs"),
		PerWorkspace: func(id ids.WorkspaceID) int { return quota(id).MaxConcurrentJobs },
		Publish: func(id ids.WorkspaceID, r jobs.Run) {
			hub.Publish(string(id), events.JobRun, r)
			switch r.Status {
			case jobs.StatusSucceeded, jobs.StatusFailed, jobs.StatusTimedOut, jobs.StatusCancelled, jobs.StatusInterrupted:
				metrics.JobRuns.WithLabelValues(r.Status).Inc()
			}
			if r.Status == jobs.StatusFailed || r.Status == jobs.StatusTimedOut {
				// Resolve the job name off the scheduler path (it opens the workspace); a deleted job just has no name.
				f := obs.Failure{Kind: "job", WorkspaceID: string(id), Subject: r.JobID, Status: r.Status, Message: r.ErrorMessage, At: time.Now()}
				go func() {
					ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
					defer cancel()
					if j, err := jobStore.Job(ctx, id, f.Subject); err == nil {
						f.SubjectName = j.Name
					}
					sampler.RecordFailure(f)
				}()
			}
		},
		Active: func(ctx context.Context, id ids.WorkspaceID) bool {
			w, err := adm.GetWorkspace(ctx, id)
			return err == nil && w.Status == admin.StatusActive
		}})
	jobSvc.Scheduler = sched
	enqueue := func(ctx context.Context, l *workspace.Lease, name, by string, args map[string]any, fromForm bool) (string, error) {
		return jobSvc.Enqueue(ctx, l, name, by, args, fromForm)
	}
	executor.Enqueue = func(ctx context.Context, id ids.WorkspaceID, name, by string, args map[string]any, fromForm bool) (string, error) {
		l, err := wm.Acquire(ctx, id)
		if err != nil {
			return "", err
		}
		defer l.Release()
		return enqueue(ctx, l, name, by, args, fromForm)
	}
	afterOpen = func(id ids.WorkspaceID) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		l, err := wm.Acquire(ctx, id)
		if err != nil {
			return
		}
		defer l.Release()
		if err := opsSvc.Reconcile(ctx, l); err != nil {
			wsLog.Warn("reconcile operations", "workspace_id", id, "err", err)
		}
		if err := sched.Rebuild(ctx, id); err != nil {
			wsLog.Warn("rebuild job index", "workspace_id", id, "err", err)
		}
	}
	// Complete or undo interrupted restores before any workspace is opened
	// (here, once afterOpen is set, so a recovered workspace's job index is
	// rebuilt when it reopens).
	if err := backups.Recover(ctx); err != nil {
		log.Error("restore recovery", "subsystem", "backup", "err", err)
	}
	if err := sched.Recover(ctx); err != nil {
		log.Error("job recovery", "subsystem", "jobs", "err", err)
	}
	schedCtx, stopSched := context.WithCancel(context.Background())
	schedDone := make(chan struct{})
	go func() { sched.Start(schedCtx); close(schedDone) }()

	demoBuilder := demo.NewBuilder(adm, wm, sched, fileSvc, obs.Subsystem(log, "demo"))
	var onSetup func(context.Context, admin.User)
	if cfg.DemoWorkspace {
		onSetup = func(ctx context.Context, u admin.User) {
			createDemoAfterSetup(ctx, demoBuilder, u, obs.Subsystem(log, "demo"))
		}
	}

	api := httpapi.New(httpapi.Deps{OnSetup: onSetup, Demo: demoBuilder, Blogs: blogSvc, BlogAPI: &blogs.Service{Admin: adm, Records: recSvc, Schemas: schemas, PublicURL: cfg.BlogBaseURL()}, Config: cfg, Log: log, Metrics: metrics, Series: sampler, Admin: adm, Auth: authSvc,
		Workspaces: wm, Schemas: schemas, Catalog: engine, Events: hub,
		Records: recSvc, Queries: querySvc, Scripts: scriptSvc,
		Reports: &reports.Service{Schemas: schemas, Records: recSvc, Files: fileSvc, Queries: querySvc, Scripts: scriptSvc, Runner: runner, Events: hub},
		Forms: &forms.Service{Schemas: schemas, Records: recSvc, Files: fileSvc, Queries: querySvc, Scripts: scriptSvc, Runner: runner, Events: hub,
			UIEventBudgetMS: func(id ids.WorkspaceID) int { return quota(id).UIEventBudgetMS },
			Enqueue:         enqueue},
		Jobs: jobSvc, Ops: opsSvc, Artifacts: arts, Files: fileSvc, Egress: gateway, Ingest: ingestSvc, Backups: backups, Logs: logBus, Docs: site,
		Version: version, SPA: spa})
	srv := &http.Server{Addr: cfg.ListenAddr, Handler: api, ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout: 120 * time.Second, MaxHeaderBytes: 64 << 10,
		BaseContext: func(net.Listener) context.Context { return context.Background() }}

	if cfg.MetricsAddr != "" {
		go serveMetrics(cfg.MetricsAddr, metrics, log)
	}
	if cfg.DebugAddr != "" {
		go serveDebug(cfg.DebugAddr, log)
	}
	janitorCtx, stopJanitor := context.WithCancel(context.Background())
	go sampler.Run(janitorCtx)
	go janitor(janitorCtx, log, wm, adm, arts, ingestSvc, fileSvc)
	go backups.RunSchedule(janitorCtx, 10*time.Minute)
	// Every user has a blog (ADR 0017): create the missing ones, users created
	// before blogs or by the CLI, in the background.
	go func() {
		if n, err := blogSvc.EnsureAll(janitorCtx); err != nil && janitorCtx.Err() == nil {
			log.Error("blog backfill", "subsystem", "blogs", "err", err)
		} else if n > 0 {
			log.Info("created blogs for existing users", "subsystem", "blogs", "count", n)
		}
	}()

	errc := make(chan error, 1)
	go func() {
		log.Info("listening", "addr", cfg.ListenAddr, "version", version, "public_url", cfg.PublicURL)
		errc <- srv.ListenAndServe()
	}()
	select {
	case err := <-errc:
		stopJanitor()
		stopSched()
		<-schedDone
		sched.Shutdown(0)
		hub.Close()
		logBus.Close()
		_ = opsSvc.Shutdown(context.Background())
		_ = wm.Shutdown(context.Background())
		return err
	case <-ctx.Done():
	}

	// Graceful shutdown (brief <deployment>): stop taking work, give running
	// job runs up to 30s (then cancel them, recorded as interrupted), close the
	// SSE streams so they do not hold the drain, drain HTTP for 15s, then close
	// the workspaces.
	log.Info("shutting down")
	api.SetDraining()
	stopJanitor()
	stopSched()
	<-schedDone
	if !sched.Shutdown(30 * time.Second) {
		log.Warn("job runs interrupted at shutdown")
	}
	hub.Close()
	logBus.Close()
	shutCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutCtx); err != nil {
		log.Warn("http drain incomplete", "err", err)
	}
	if err := opsSvc.Shutdown(shutCtx); err != nil {
		log.Warn("operations did not stop in time", "err", err)
	}
	backups.Wait()
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

// artifactRetention is how long job and export artifacts are kept.
const artifactRetention = 30 * 24 * time.Hour

func janitor(ctx context.Context, log *slog.Logger, wm *workspace.Manager, adm *admin.Store, arts *artifacts.Store, ing *ingest.Service, fl *files.Service) {
	t := time.NewTicker(5 * time.Minute)
	defer t.Stop()
	var lastFull time.Time
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
			sweepFiles(ctx, log, fl, l)
			l.Release()
		}
		// Workspaces that nobody has opened still get their expired files purged,
		// once a day (opening each one is not free, so not on every tick).
		if time.Since(lastFull) > 24*time.Hour {
			lastFull = time.Now()
			sweepClosedWorkspaces(ctx, log, wm, adm, fl)
		}
		if n, err := arts.Sweep(artifactRetention, time.Now()); err != nil {
			log.Warn("sweep artifacts", "subsystem", "janitor", "err", err)
		} else if n > 0 {
			log.Info("swept artifacts", "subsystem", "janitor", "count", n)
		}
		if n, err := ing.Sweep(24*time.Hour, time.Now()); err != nil {
			log.Warn("sweep staging", "subsystem", "janitor", "err", err)
		} else if n > 0 {
			log.Info("swept staging files", "subsystem", "janitor", "count", n)
		}
		wm.CloseIdle(ctx, 10*time.Minute)
		if n, err := adm.PurgeSessions(ctx, time.Now(), auth.IdleTimeout); err != nil {
			log.Warn("purge sessions", "subsystem", "janitor", "err", err)
		} else if n > 0 {
			log.Info("purged sessions", "subsystem", "janitor", "count", n)
		}
	}
}

// sweepFiles purges expired file attachments of one open workspace.
func sweepFiles(ctx context.Context, log *slog.Logger, fl *files.Service, l *workspace.Lease) {
	res, err := fl.Sweep(ctx, l, time.Now())
	if err != nil {
		log.Warn("sweep files", "subsystem", "janitor", "workspace_id", l.ID(), "err", err)
		return
	}
	if n := res.PurgedPending + res.PurgedUnreferenced + res.Orphans; n > 0 {
		log.Info("purged files", "subsystem", "janitor", "workspace_id", l.ID(), "pending", res.PurgedPending,
			"unreferenced", res.PurgedUnreferenced, "orphans", res.Orphans, "bytes", res.Bytes)
	}
}

func sweepClosedWorkspaces(ctx context.Context, log *slog.Logger, wm *workspace.Manager, adm *admin.Store, fl *files.Service) {
	list, err := adm.ListWorkspaces(ctx)
	if err != nil {
		log.Warn("list workspaces for file sweep", "subsystem", "janitor", "err", err)
		return
	}
	for _, w := range list {
		if ctx.Err() != nil {
			return
		}
		if w.Status != admin.StatusActive {
			continue
		}
		l, err := wm.Acquire(ctx, w.ID)
		if err != nil {
			continue
		}
		sweepFiles(ctx, log, fl, l)
		l.Release()
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

// serveMetrics serves /metrics without authentication on a separate bind, for
// Prometheus on a private network (brief: "admin or a separate bind").
func serveMetrics(addr string, m *obs.Metrics, log *slog.Logger) {
	mux := http.NewServeMux()
	mux.Handle("/metrics", m.Handler())
	srv := &http.Server{Addr: addr, Handler: mux, ReadHeaderTimeout: 10 * time.Second}
	log.Info("metrics server listening", "addr", addr)
	if err := srv.ListenAndServe(); err != nil {
		log.Error("metrics server", "err", err)
	}
}

type metricsObserver struct{ m *obs.Metrics }

func (o metricsObserver) GateWait(p workspace.Priority, d time.Duration) {
	o.m.WriterGateWait.WithLabelValues(p.String()).Observe(d.Seconds())
}
func (o metricsObserver) GateHold(d time.Duration)   { o.m.WriterGateHold.Observe(d.Seconds()) }
func (o metricsObserver) ReaderWait(d time.Duration) { o.m.ReaderPoolWait.Observe(d.Seconds()) }
func (o metricsObserver) OpenWorkspaces(n int)       { o.m.OpenWorkspaces.Set(float64(n)) }
