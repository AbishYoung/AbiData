// Package config loads server configuration from environment variables. Every
// variable also accepts a NAME_FILE variant whose value is a path to read (for
// Docker/compose secrets); setting both is an error.
package config

import (
	"log/slog"
	"net/netip"
)

// Config is the server configuration.
type Config struct {
	ListenAddr          string         // ABIDATA_LISTEN_ADDR, default ":8080"
	PublicURL           string         // ABIDATA_PUBLIC_URL, default "http://localhost:8080"; scheme+host[:port], no path; used for the Origin check
	DataDir             string         // ABIDATA_DATA_DIR, default "/var/lib/abidata"
	BackupDir           string         // ABIDATA_BACKUP_DIR, default "/var/lib/abidata-backups"
	MaxMemoryMB         int            // ABIDATA_MAX_MEMORY_MB, default 0 (unset: GOMEMLIMIT untouched)
	JobWorkers          int            // ABIDATA_JOB_WORKERS, default runtime.GOMAXPROCS(0)
	MaxOpenWorkspaces   int            // ABIDATA_MAX_OPEN_WORKSPACES, default 128
	Synchronous         string         // ABIDATA_SYNCHRONOUS: "normal" (default) or "full"; stored upper-case
	SessionKey          []byte         // ABIDATA_SESSION_KEY: base64 (std or url, padded or not), ≥32 bytes decoded; nil = generate on first boot
	ManifestKey         []byte         // ABIDATA_MANIFEST_KEY: same format as SessionKey
	BootstrapAdminEmail string         // ABIDATA_BOOTSTRAP_ADMIN_EMAIL
	DemoWorkspace       bool           // ABIDATA_DEMO_WORKSPACE, default true; "false"/"0" disables the Northwind Traders sample workspace created at first-run setup
	DebugAddr           string         // ABIDATA_DEBUG_ADDR, default "" (pprof off)
	MetricsAddr         string         // ABIDATA_METRICS_ADDR, default "" (metrics only at /metrics for admins); unauthenticated, bind to a private interface
	LogLevel            slog.Level     // ABIDATA_LOG_LEVEL: debug|info|warn|error, default info
	TrustedProxies      []netip.Prefix // ABIDATA_TRUSTED_PROXIES: comma-separated CIDRs or bare IPs
}

// SecureCookies reports whether cookies must carry Secure (and may use __Host-).
func (c Config) SecureCookies() bool { return len(c.PublicURL) >= 8 && c.PublicURL[:8] == "https://" }
