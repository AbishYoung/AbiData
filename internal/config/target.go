package config

import (
	"errors"
	"os"

	"github.com/jackc/pgx/v5/pgxpool"
)

// TargetPostgres is where abidata migrate-backend --to postgres puts an
// install (ADR 0012 §9). It is read from its own variables, because the
// source install's configuration says sqlite and refuses a PostgreSQL URL.
type TargetPostgres struct {
	URL    string // ABIDATA_TARGET_POSTGRES_URL (+ _FILE); secret
	Prefix string // ABIDATA_TARGET_POSTGRES_SCHEMA_PREFIX, default "abd"
}

// TargetPostgresFromEnv loads TargetPostgres from the process environment.
func TargetPostgresFromEnv() (TargetPostgres, error) {
	return LoadTargetPostgres(os.Getenv, os.ReadFile)
}

// LoadTargetPostgres builds a TargetPostgres as Load builds a Config. Errors
// never include the URL.
func LoadTargetPostgres(getenv func(string) string, readFile func(string) ([]byte, error)) (TargetPostgres, error) {
	l := loader{getenv: getenv, readFile: readFile}
	var t TargetPostgres
	var err error
	if t.URL, _, err = l.lookup("ABIDATA_TARGET_POSTGRES_URL"); err != nil {
		return TargetPostgres{}, err
	}
	if t.URL == "" {
		return TargetPostgres{}, errors.New("config: set ABIDATA_TARGET_POSTGRES_URL (or ABIDATA_TARGET_POSTGRES_URL_FILE) to the PostgreSQL database to move the install to")
	}
	if _, err := pgxpool.ParseConfig(t.URL); err != nil {
		return TargetPostgres{}, errors.New("config: ABIDATA_TARGET_POSTGRES_URL is not a valid PostgreSQL connection string")
	}
	if t.Prefix, err = l.nonEmpty("ABIDATA_TARGET_POSTGRES_SCHEMA_PREFIX", "abd"); err != nil {
		return TargetPostgres{}, err
	}
	if !prefixPattern.MatchString(t.Prefix) {
		return TargetPostgres{}, errors.New("config: ABIDATA_TARGET_POSTGRES_SCHEMA_PREFIX must be a lower-case letter followed by at most 15 lower-case letters, digits or underscores")
	}
	return t, nil
}
