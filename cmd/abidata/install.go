package main

import (
	"context"
	"errors"
	"path/filepath"

	"github.com/jackc/pgx/v5/pgxpool"

	"abidata/internal/admin"
	"abidata/internal/catalog"
	"abidata/internal/config"
	"abidata/internal/dbx"
	"abidata/internal/ids"
	"abidata/internal/pgdb"
	"abidata/internal/workspace"
)

// offlineInstall is a stopped install opened by an admin command (user,
// demo) on the backend its configuration names, as serve opens it but
// without the server's background work.
type offlineInstall struct {
	Admin *admin.Store
	pool  *pgxpool.Pool
	pc    *pgxpool.Config
	wm    *workspace.Manager
}

// openOffline opens the install's admin store. The caller holds the data
// directory's lock and has run checkBackend.
func openOffline(ctx context.Context, cfg config.Config) (*offlineInstall, error) {
	in := &offlineInstall{}
	var err error
	if cfg.DB != config.DBPostgres {
		in.Admin, err = admin.Open(ctx, filepath.Join(cfg.DataDir, "system", "admin.db"), cfg.Synchronous)
		return in, err
	}
	if in.pc, err = pgxpool.ParseConfig(cfg.PostgresURL); err != nil {
		return nil, errors.New("ABIDATA_POSTGRES_URL is not a valid PostgreSQL connection string")
	}
	if in.pool, err = pgxpool.NewWithConfig(ctx, in.pc); err != nil {
		return nil, err
	}
	if in.Admin, err = admin.OpenPostgres(ctx, in.pc, pgdb.Names{Prefix: cfg.PostgresPrefix}); err != nil {
		in.pool.Close()
		return nil, err
	}
	return in, nil
}

// Workspaces opens the install's workspace manager.
func (in *offlineInstall) Workspaces(ctx context.Context, cfg config.Config) (*workspace.Manager, error) {
	wc := workspace.Config{DataDir: cfg.DataDir, MaxOpen: 4, Synchronous: cfg.Synchronous, Migrate: catalog.Migrate,
		MaxPageCount: func(ctx context.Context, id ids.WorkspaceID) (int64, error) {
			w, err := in.Admin.GetWorkspace(ctx, id)
			if err != nil {
				return 0, err
			}
			return w.Quota.MaxPageCount, nil
		}}
	if in.pool != nil {
		dialect, err := pgdb.Dialect(ctx, pgdb.Pool(in.pool))
		if err != nil {
			return nil, err
		}
		pool, database := in.pool, in.pc.ConnConfig.Database
		wc.Postgres = &workspace.PostgresConfig{Pool: pool, Database: database, Prefix: cfg.PostgresPrefix, Dialect: dialect,
			Migrate: func(ctx context.Context, id ids.WorkspaceID, d dbx.Execer, n pgdb.Names) error {
				return catalog.MigratePostgres(ctx, id, d, n)
			},
			Adopt: func(ctx context.Context, id ids.WorkspaceID, src string) error {
				return catalog.AdoptPostgres(ctx, pool, database, cfg.PostgresPrefix, dialect, id, src)
			}}
	}
	wm, err := workspace.NewManager(wc)
	if err != nil {
		return nil, err
	}
	in.wm = wm
	return wm, nil
}

// Close closes what openOffline and Workspaces opened.
func (in *offlineInstall) Close() {
	if in.wm != nil {
		_ = in.wm.Shutdown(context.Background())
	}
	if in.Admin != nil {
		_ = in.Admin.Close()
	}
	if in.pool != nil {
		in.pool.Close()
	}
}
