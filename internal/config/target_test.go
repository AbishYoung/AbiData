package config

import (
	"errors"
	"strings"
	"testing"
)

func TestLoadTargetPostgres(t *testing.T) {
	secret := "s3cret-value"
	url := "postgres://app:" + secret + "@127.0.0.1:5432/abidata"
	env := func(m map[string]string) func(string) string { return func(k string) string { return m[k] } }
	noFile := func(string) ([]byte, error) { return nil, errors.New("no such file") }

	got, err := LoadTargetPostgres(env(map[string]string{"ABIDATA_TARGET_POSTGRES_URL": url}), noFile)
	if err != nil || got.URL != url || got.Prefix != "abd" {
		t.Fatalf("defaults: %+v %v", got, err)
	}
	got, err = LoadTargetPostgres(env(map[string]string{"ABIDATA_TARGET_POSTGRES_URL_FILE": "/run/secrets/url",
		"ABIDATA_TARGET_POSTGRES_SCHEMA_PREFIX": "moved"}), func(string) ([]byte, error) { return []byte(url + "\n"), nil })
	if err != nil || got.URL != url || got.Prefix != "moved" {
		t.Fatalf("from a file: %+v %v", got, err)
	}
	for name, m := range map[string]map[string]string{
		"missing":    {},
		"bad prefix": {"ABIDATA_TARGET_POSTGRES_URL": url, "ABIDATA_TARGET_POSTGRES_SCHEMA_PREFIX": "Bad-Prefix"},
		"bad url":    {"ABIDATA_TARGET_POSTGRES_URL": "postgres://app:" + secret + "@127.0.0.1:notaport/x"},
		"both":       {"ABIDATA_TARGET_POSTGRES_URL": url, "ABIDATA_TARGET_POSTGRES_URL_FILE": "/x"},
	} {
		_, err := LoadTargetPostgres(env(m), noFile)
		if err == nil {
			t.Errorf("%s: no error", name)
		} else if strings.Contains(err.Error(), secret) {
			t.Errorf("%s: the error shows the password: %v", name, err)
		}
	}
}
