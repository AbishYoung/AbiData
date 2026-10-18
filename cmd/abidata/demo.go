package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"path/filepath"
	"time"

	"abidata/internal/admin"
	"abidata/internal/catalog"
	"abidata/internal/config"
	"abidata/internal/demo"
	"abidata/internal/files"
	"abidata/internal/ids"
	"abidata/internal/workspace"
)

// createDemoAfterSetup builds both sample workspaces for the first
// administrator. It never fails setup: problems (including panics) are logged
// and swallowed.
func createDemoAfterSetup(ctx context.Context, b *demo.Builder, u admin.User, log *slog.Logger) {
	for _, kind := range demo.Kinds {
		createDemo(ctx, b, kind, u, log)
	}
}

func createDemo(ctx context.Context, b *demo.Builder, kind demo.Kind, u admin.User, log *slog.Logger) {
	defer func() {
		if r := recover(); r != nil {
			log.Error("demo workspace panicked", "demo", kind, "panic", r)
		}
	}()
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	start := time.Now()
	res, err := b.Create(ctx, kind, u.ID)
	switch {
	case err != nil:
		log.Error("could not create a demo workspace; setup is unaffected", "name", kind.Name(), "err", err)
	case res.Created:
		log.Info("created demo workspace", "workspace_id", res.WorkspaceID, "name", kind.Name(), "owner", u.Email, "took", time.Since(start).Round(time.Millisecond))
	}
}

// demoCmd implements `abidata demo --owner EMAIL [--kind traders|invasives|all]`:
// create the sample workspaces on an existing install. Like the other admin
// commands it needs the data directory to itself, so stop the server first.
func demoCmd(args []string) error {
	fs := flag.NewFlagSet("demo", flag.ContinueOnError)
	owner := fs.String("owner", "", "email of the user who will own the workspace (required)")
	which := fs.String("kind", "all", "which sample to create: traders (Northwind Traders), invasives (Northwind Invasives) or all")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *owner == "" {
		return errors.New("usage: abidata demo --owner EMAIL [--kind traders|invasives|all]")
	}
	kinds := demo.Kinds
	if *which != "all" {
		k, err := demo.ParseKind(*which)
		if err != nil {
			return err
		}
		kinds = []demo.Kind{k}
	}
	cfg, err := config.FromEnv()
	if err != nil {
		return err
	}
	if err := checkBackend(context.Background(), cfg); err != nil {
		return err
	}
	lock, err := acquireLock(cfg.DataDir)
	if err != nil {
		return err
	}
	defer lock.Release()

	ctx := context.Background()
	adm, err := admin.Open(ctx, filepath.Join(cfg.DataDir, "system", "admin.db"), cfg.Synchronous)
	if err != nil {
		return err
	}
	defer func() { _ = adm.Close() }()
	u, err := adm.GetUserByEmail(ctx, *owner)
	if errors.Is(err, admin.ErrNotFound) {
		return fmt.Errorf("no user has the email %q", *owner)
	} else if err != nil {
		return err
	}
	wm, err := workspace.NewManager(workspace.Config{DataDir: cfg.DataDir, MaxOpen: 4, Synchronous: cfg.Synchronous, Migrate: catalog.Migrate,
		MaxPageCount: func(ctx context.Context, id ids.WorkspaceID) (int64, error) {
			w, err := adm.GetWorkspace(ctx, id)
			if err != nil {
				return 0, err
			}
			return w.Quota.MaxPageCount, nil
		}})
	if err != nil {
		return err
	}
	defer func() { _ = wm.Shutdown(ctx) }()

	b := demo.NewBuilder(adm, wm, nil, &files.Service{DataDir: cfg.DataDir}, slog.Default())
	for _, kind := range kinds {
		res, err := b.Create(ctx, kind, u.ID)
		if err != nil {
			return fmt.Errorf("%s: %w", kind.Name(), err)
		}
		if !res.Created {
			fmt.Printf("workspace %q already exists (%s); nothing to do\n", kind.Name(), res.WorkspaceID)
			continue
		}
		fmt.Printf("created workspace %q (%s) owned by %s\n", kind.Name(), res.WorkspaceID, u.Email)
	}
	return nil
}
