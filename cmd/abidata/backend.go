package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"abidata/internal/config"
	"abidata/internal/pgdb"
)

// checkBackend runs the startup checks of ADR 0012 §1 before anything is
// created: the data directory must not hold an install of the other
// backend, and a PostgreSQL server must be one the app can run on. It
// connects but changes nothing.
func checkBackend(ctx context.Context, cfg config.Config) error {
	if _, err := os.Stat(filepath.Join(cfg.DataDir, "system", migrateMarker)); err == nil {
		return fmt.Errorf("%s is the target of an unfinished abidata migrate-backend; run that command again to finish the move, or delete the directory", cfg.DataDir)
	}
	if cfg.DB != config.DBPostgres {
		return nil
	}
	if _, err := os.Stat(filepath.Join(cfg.DataDir, "system", "admin.db")); err == nil {
		return fmt.Errorf("ABIDATA_DB=postgres, but %s holds a SQLite install; move it with abidata migrate-backend --to postgres, or set ABIDATA_DB=sqlite", cfg.DataDir)
	}
	pc, err := pgxpool.ParseConfig(cfg.PostgresURL)
	if err != nil {
		return errors.New("ABIDATA_POSTGRES_URL is not a valid PostgreSQL connection string")
	}
	pc.MaxConns = 1
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	pool, err := pgxpool.NewWithConfig(ctx, pc)
	if err != nil {
		return fmt.Errorf("connecting to PostgreSQL: %w", err)
	}
	defer pool.Close()
	if err := pgdb.CheckServer(ctx, pgdb.Pool(pool)); err != nil {
		return err
	}
	return nil
}
