package config

import (
	"encoding/base64"
	"errors"
	"log/slog"
	"net/netip"
	"os"
	"reflect"
	"runtime"
	"strings"
	"testing"
)

func loadWith(env map[string]string, files map[string]string) (Config, error) {
	return Load(
		func(k string) string { return env[k] },
		func(p string) ([]byte, error) {
			if s, ok := files[p]; ok {
				return []byte(s), nil
			}
			return nil, &os.PathError{Op: "open", Path: p, Err: os.ErrNotExist}
		},
	)
}

func TestDefaults(t *testing.T) {
	c, err := loadWith(nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	want := Config{
		ListenAddr:        ":8080",
		PublicURL:         "http://localhost:8080",
		DataDir:           "/var/lib/abidata",
		BackupDir:         "/var/lib/abidata-backups",
		JobWorkers:        runtime.GOMAXPROCS(0),
		MaxOpenWorkspaces: 128,
		Synchronous:       "NORMAL",
		LogLevel:          slog.LevelInfo,
	}
	if !reflect.DeepEqual(c, want) {
		t.Fatalf("got %+v want %+v", c, want)
	}
	if c.SecureCookies() {
		t.Fatal("http default must not be secure")
	}
}

func TestValidValues(t *testing.T) {
	key := base64.StdEncoding.EncodeToString(make([]byte, 32))
	c, err := loadWith(map[string]string{
		"ABIDATA_LISTEN_ADDR":           "127.0.0.1:9000",
		"ABIDATA_PUBLIC_URL":            "https://example.com:8443/",
		"ABIDATA_DATA_DIR":              "/data//x/../y/",
		"ABIDATA_BACKUP_DIR":            "rel/dir/",
		"ABIDATA_MAX_MEMORY_MB":         "512",
		"ABIDATA_JOB_WORKERS":           "3",
		"ABIDATA_MAX_OPEN_WORKSPACES":   "7",
		"ABIDATA_SYNCHRONOUS":           "Full",
		"ABIDATA_SESSION_KEY":           key,
		"ABIDATA_BOOTSTRAP_ADMIN_EMAIL": "a@b.c",
		"ABIDATA_DEBUG_ADDR":            "localhost:6060",
		"ABIDATA_LOG_LEVEL":             "WARN",
		"ABIDATA_TRUSTED_PROXIES":       " 10.0.0.0/8 , 192.168.1.1,::1 ",
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if c.ListenAddr != "127.0.0.1:9000" || c.PublicURL != "https://example.com:8443" ||
		c.DataDir != "/data/y" || c.BackupDir != "rel/dir" || c.MaxMemoryMB != 512 ||
		c.JobWorkers != 3 || c.MaxOpenWorkspaces != 7 || c.Synchronous != "FULL" ||
		len(c.SessionKey) != 32 || c.ManifestKey != nil || c.BootstrapAdminEmail != "a@b.c" ||
		c.DebugAddr != "localhost:6060" || c.LogLevel != slog.LevelWarn {
		t.Fatalf("unexpected config %+v", c)
	}
	if !c.SecureCookies() {
		t.Fatal("https must be secure")
	}
	wantP := []netip.Prefix{
		netip.MustParsePrefix("10.0.0.0/8"),
		netip.MustParsePrefix("192.168.1.1/32"),
		netip.MustParsePrefix("::1/128"),
	}
	if !reflect.DeepEqual(c.TrustedProxies, wantP) {
		t.Fatalf("proxies %v", c.TrustedProxies)
	}
}

func TestErrors(t *testing.T) {
	short := base64.StdEncoding.EncodeToString(make([]byte, 31))
	cases := []struct {
		name, key, val string
	}{
		{"url bad parse", "ABIDATA_PUBLIC_URL", "http://[::1"},
		{"url scheme", "ABIDATA_PUBLIC_URL", "ftp://example.com"},
		{"url no scheme", "ABIDATA_PUBLIC_URL", "example.com"},
		{"url no host", "ABIDATA_PUBLIC_URL", "http://"},
		{"url path", "ABIDATA_PUBLIC_URL", "http://example.com/app"},
		{"url query", "ABIDATA_PUBLIC_URL", "http://example.com/?a=b"},
		{"url empty query", "ABIDATA_PUBLIC_URL", "http://example.com?"},
		{"url fragment", "ABIDATA_PUBLIC_URL", "http://example.com/#x"},
		{"url userinfo", "ABIDATA_PUBLIC_URL", "http://u:p@example.com"},
		{"mem nan", "ABIDATA_MAX_MEMORY_MB", "abc"},
		{"mem negative", "ABIDATA_MAX_MEMORY_MB", "-1"},
		{"workers zero", "ABIDATA_JOB_WORKERS", "0"},
		{"workers nan", "ABIDATA_JOB_WORKERS", "x"},
		{"workspaces zero", "ABIDATA_MAX_OPEN_WORKSPACES", "0"},
		{"sync bad", "ABIDATA_SYNCHRONOUS", "off"},
		{"session not base64", "ABIDATA_SESSION_KEY", "!!!notbase64!!!"},
		{"session short", "ABIDATA_SESSION_KEY", short},
		{"manifest short", "ABIDATA_MANIFEST_KEY", short},
		{"loglevel bad", "ABIDATA_LOG_LEVEL", "trace"},
		{"proxy bad cidr", "ABIDATA_TRUSTED_PROXIES", "10.0.0.0/33"},
		{"proxy bad ip", "ABIDATA_TRUSTED_PROXIES", "10.0.0.0/8,nope"},
		{"proxy only commas", "ABIDATA_TRUSTED_PROXIES", " , "},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := loadWith(map[string]string{tc.key: tc.val}, nil)
			if err == nil {
				t.Fatal("expected error")
			}
			if !strings.Contains(err.Error(), tc.key) {
				t.Fatalf("error %q does not name %s", err, tc.key)
			}
			if strings.HasSuffix(tc.key, "_KEY") && strings.Contains(err.Error(), tc.val) {
				t.Fatalf("error leaks secret: %q", err)
			}
		})
	}
}

func TestEmptyValuesRejected(t *testing.T) {
	for _, name := range []string{"ABIDATA_LISTEN_ADDR", "ABIDATA_DATA_DIR", "ABIDATA_BACKUP_DIR"} {
		t.Run(name, func(t *testing.T) {
			// A whitespace-only file yields an empty value.
			_, err := loadWith(map[string]string{name + "_FILE": "/f"}, map[string]string{"/f": " \n"})
			if err == nil || !strings.Contains(err.Error(), name) {
				t.Fatalf("got %v", err)
			}
		})
	}
}

func TestKeyEncodings(t *testing.T) {
	raw := make([]byte, 33)
	for i := range raw {
		raw[i] = byte(0xfb + i) // produces + and / in std, - and _ in url
	}
	for name, enc := range map[string]*base64.Encoding{
		"std": base64.StdEncoding, "rawstd": base64.RawStdEncoding,
		"url": base64.URLEncoding, "rawurl": base64.RawURLEncoding,
	} {
		t.Run(name, func(t *testing.T) {
			c, err := loadWith(map[string]string{"ABIDATA_MANIFEST_KEY": enc.EncodeToString(raw)}, nil)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(c.ManifestKey, raw) {
				t.Fatal("decoded key mismatch")
			}
		})
	}
}

func TestFileVariants(t *testing.T) {
	key := base64.StdEncoding.EncodeToString(make([]byte, 40))
	c, err := loadWith(map[string]string{
		"ABIDATA_SESSION_KEY_FILE":           "/run/secrets/session",
		"ABIDATA_BOOTSTRAP_ADMIN_EMAIL_FILE": "/run/secrets/email",
		"ABIDATA_JOB_WORKERS_FILE":           "/run/secrets/workers",
	}, map[string]string{
		"/run/secrets/session": key + "\n",
		"/run/secrets/email":   "  admin@example.com \r\n",
		"/run/secrets/workers": "5\n",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(c.SessionKey) != 40 || c.BootstrapAdminEmail != "admin@example.com" || c.JobWorkers != 5 {
		t.Fatalf("unexpected %+v", c)
	}
}

func TestFileReadError(t *testing.T) {
	_, err := loadWith(map[string]string{"ABIDATA_SESSION_KEY_FILE": "/missing"}, nil)
	if err == nil || !strings.Contains(err.Error(), "ABIDATA_SESSION_KEY_FILE") || !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("got %v", err)
	}
}

func TestBothSet(t *testing.T) {
	for _, name := range []string{
		"ABIDATA_LISTEN_ADDR", "ABIDATA_PUBLIC_URL", "ABIDATA_DATA_DIR", "ABIDATA_BACKUP_DIR",
		"ABIDATA_MAX_MEMORY_MB", "ABIDATA_JOB_WORKERS", "ABIDATA_MAX_OPEN_WORKSPACES",
		"ABIDATA_SYNCHRONOUS", "ABIDATA_SESSION_KEY", "ABIDATA_MANIFEST_KEY",
		"ABIDATA_BOOTSTRAP_ADMIN_EMAIL", "ABIDATA_DEBUG_ADDR", "ABIDATA_LOG_LEVEL", "ABIDATA_TRUSTED_PROXIES",
	} {
		t.Run(name, func(t *testing.T) {
			_, err := loadWith(
				map[string]string{name: "secret-value", name + "_FILE": "/f"},
				map[string]string{"/f": "x"},
			)
			if err == nil || !strings.Contains(err.Error(), name) {
				t.Fatalf("got %v", err)
			}
			if strings.Contains(err.Error(), "secret-value") {
				t.Fatal("error leaks value")
			}
		})
	}
}

func TestFromEnv(t *testing.T) {
	t.Setenv("ABIDATA_LISTEN_ADDR", ":9999")
	c, err := FromEnv()
	if err != nil {
		t.Fatal(err)
	}
	if c.ListenAddr != ":9999" {
		t.Fatalf("got %q", c.ListenAddr)
	}
}
