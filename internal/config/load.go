package config

import (
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"net/netip"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
)

const minKeyBytes = 32

// FromEnv loads the configuration from the process environment and the
// filesystem.
func FromEnv() (Config, error) {
	return Load(os.Getenv, os.ReadFile)
}

// Load builds a Config using getenv to read variables and readFile to resolve
// NAME_FILE variants. Error messages name the offending variable but never
// include secret values.
func Load(getenv func(string) string, readFile func(string) ([]byte, error)) (Config, error) {
	l := loader{getenv: getenv, readFile: readFile}
	var c Config
	var err error

	if c.ListenAddr, err = l.nonEmpty("ABIDATA_LISTEN_ADDR", ":8080"); err != nil {
		return Config{}, err
	}
	if c.PublicURL, err = l.publicURL("ABIDATA_PUBLIC_URL", "http://localhost:8080"); err != nil {
		return Config{}, err
	}
	if c.DataDir, err = l.dir("ABIDATA_DATA_DIR", "/var/lib/abidata"); err != nil {
		return Config{}, err
	}
	if c.BackupDir, err = l.dir("ABIDATA_BACKUP_DIR", "/var/lib/abidata-backups"); err != nil {
		return Config{}, err
	}
	if c.MaxMemoryMB, err = l.integer("ABIDATA_MAX_MEMORY_MB", 0, 0); err != nil {
		return Config{}, err
	}
	if c.JobWorkers, err = l.integer("ABIDATA_JOB_WORKERS", runtime.GOMAXPROCS(0), 1); err != nil {
		return Config{}, err
	}
	if c.MaxOpenWorkspaces, err = l.integer("ABIDATA_MAX_OPEN_WORKSPACES", 128, 1); err != nil {
		return Config{}, err
	}
	maxFile, err := l.integer("ABIDATA_MAX_FILE_BYTES", 25<<20, 1)
	if err != nil {
		return Config{}, err
	}
	if maxFile > 1<<30 {
		return Config{}, errors.New("config: ABIDATA_MAX_FILE_BYTES must be at most 1073741824 (1 GiB)")
	}
	c.MaxFileBytes = int64(maxFile)
	if c.Synchronous, err = l.synchronous("ABIDATA_SYNCHRONOUS"); err != nil {
		return Config{}, err
	}
	if c.SessionKey, err = l.key("ABIDATA_SESSION_KEY"); err != nil {
		return Config{}, err
	}
	if c.ManifestKey, err = l.key("ABIDATA_MANIFEST_KEY"); err != nil {
		return Config{}, err
	}
	if c.BootstrapAdminEmail, _, err = l.lookup("ABIDATA_BOOTSTRAP_ADMIN_EMAIL"); err != nil {
		return Config{}, err
	}
	if c.DemoWorkspace, err = l.boolean("ABIDATA_DEMO_WORKSPACE", true); err != nil {
		return Config{}, err
	}
	if c.DebugAddr, _, err = l.lookup("ABIDATA_DEBUG_ADDR"); err != nil {
		return Config{}, err
	}
	if c.MetricsAddr, _, err = l.lookup("ABIDATA_METRICS_ADDR"); err != nil {
		return Config{}, err
	}
	if c.LogLevel, err = l.logLevel("ABIDATA_LOG_LEVEL"); err != nil {
		return Config{}, err
	}
	if c.TrustedProxies, err = l.proxies("ABIDATA_TRUSTED_PROXIES"); err != nil {
		return Config{}, err
	}
	if c.AllowedOrigins, err = l.origins("ABIDATA_ALLOWED_ORIGINS"); err != nil {
		return Config{}, err
	}
	return c, nil
}

type loader struct {
	getenv   func(string) string
	readFile func(string) ([]byte, error)
}

// lookup returns the value of name or of name_FILE. ok is false when neither is set.
func (l loader) lookup(name string) (val string, ok bool, err error) {
	direct := l.getenv(name)
	file := l.getenv(name + "_FILE")
	switch {
	case direct != "" && file != "":
		return "", false, fmt.Errorf("config: %s and %s_FILE are both set; set only one", name, name)
	case direct != "":
		return direct, true, nil
	case file != "":
		b, rerr := l.readFile(file)
		if rerr != nil {
			// The underlying error describes the path, never file contents.
			return "", false, fmt.Errorf("config: reading %s_FILE: %w", name, rerr)
		}
		return strings.TrimSpace(string(b)), true, nil
	}
	return "", false, nil
}

func (l loader) nonEmpty(name, def string) (string, error) {
	v, ok, err := l.lookup(name)
	if err != nil {
		return "", err
	}
	if !ok {
		return def, nil
	}
	if v == "" {
		return "", fmt.Errorf("config: %s must not be empty", name)
	}
	return v, nil
}

func (l loader) dir(name, def string) (string, error) {
	v, err := l.nonEmpty(name, def)
	if err != nil {
		return "", err
	}
	return filepath.Clean(v), nil
}

func (l loader) integer(name string, def, min int) (int, error) {
	v, ok, err := l.lookup(name)
	if err != nil {
		return 0, err
	}
	if !ok {
		return def, nil
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return 0, fmt.Errorf("config: %s must be an integer", name)
	}
	if n < min {
		return 0, fmt.Errorf("config: %s must be at least %d", name, min)
	}
	return n, nil
}

// boolean accepts true/false, 1/0, yes/no and on/off (any case).
func (l loader) boolean(name string, def bool) (bool, error) {
	v, ok, err := l.lookup(name)
	if err != nil {
		return false, err
	}
	if !ok || v == "" {
		return def, nil
	}
	switch strings.ToLower(v) {
	case "true", "1", "yes", "on":
		return true, nil
	case "false", "0", "no", "off":
		return false, nil
	}
	return false, fmt.Errorf("config: %s must be true or false", name)
}

func (l loader) publicURL(name, def string) (string, error) {
	v, ok, err := l.lookup(name)
	if err != nil {
		return "", err
	}
	if !ok {
		v = def
	}
	u, err := url.Parse(v)
	if err != nil {
		return "", fmt.Errorf("config: %s is not a valid URL", name)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return "", fmt.Errorf("config: %s must use scheme http or https", name)
	}
	if u.Hostname() == "" {
		return "", fmt.Errorf("config: %s must include a host", name)
	}
	if u.User != nil {
		return "", fmt.Errorf("config: %s must not include userinfo", name)
	}
	if u.Path != "" && u.Path != "/" {
		return "", fmt.Errorf("config: %s must not include a path", name)
	}
	if u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || strings.Contains(v, "#") {
		return "", fmt.Errorf("config: %s must not include a query or fragment", name)
	}
	return u.Scheme + "://" + u.Host, nil
}

func (l loader) synchronous(name string) (string, error) {
	v, ok, err := l.lookup(name)
	if err != nil {
		return "", err
	}
	if !ok {
		return "NORMAL", nil
	}
	switch strings.ToLower(v) {
	case "normal":
		return "NORMAL", nil
	case "full":
		return "FULL", nil
	}
	return "", fmt.Errorf("config: %s must be \"normal\" or \"full\"", name)
}

func (l loader) key(name string) ([]byte, error) {
	v, ok, err := l.lookup(name)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, nil
	}
	var b []byte
	for _, enc := range []*base64.Encoding{base64.StdEncoding, base64.RawStdEncoding, base64.URLEncoding, base64.RawURLEncoding} {
		if b, err = enc.DecodeString(v); err == nil {
			break
		}
	}
	if err != nil {
		return nil, fmt.Errorf("config: %s is not valid base64", name)
	}
	if len(b) < minKeyBytes {
		return nil, fmt.Errorf("config: %s must decode to at least %d bytes", name, minKeyBytes)
	}
	return b, nil
}

func (l loader) logLevel(name string) (slog.Level, error) {
	v, ok, err := l.lookup(name)
	if err != nil {
		return 0, err
	}
	if !ok {
		return slog.LevelInfo, nil
	}
	switch strings.ToLower(v) {
	case "debug":
		return slog.LevelDebug, nil
	case "info":
		return slog.LevelInfo, nil
	case "warn":
		return slog.LevelWarn, nil
	case "error":
		return slog.LevelError, nil
	}
	return 0, fmt.Errorf("config: %s must be one of debug, info, warn, error", name)
}

func (l loader) proxies(name string) ([]netip.Prefix, error) {
	v, ok, err := l.lookup(name)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, nil
	}
	var out []netip.Prefix
	for _, part := range strings.Split(v, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if strings.Contains(part, "/") {
			p, perr := netip.ParsePrefix(part)
			if perr != nil {
				return nil, fmt.Errorf("config: %s has invalid CIDR %q", name, part)
			}
			out = append(out, p)
			continue
		}
		a, aerr := netip.ParseAddr(part)
		if aerr != nil {
			return nil, fmt.Errorf("config: %s has invalid IP %q", name, part)
		}
		out = append(out, netip.PrefixFrom(a, a.BitLen()))
	}
	if len(out) == 0 {
		return nil, errors.New("config: " + name + " contains no entries")
	}
	return out, nil
}

func (l loader) origins(name string) ([]string, error) {
	v, ok, err := l.lookup(name)
	if err != nil || !ok {
		return nil, err
	}
	var out []string
	for _, part := range strings.Split(v, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		u, perr := url.Parse(part)
		if perr != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" || u.User != nil ||
			(u.Path != "" && u.Path != "/") || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || strings.Contains(part, "#") || strings.Contains(part, "*") {
			return nil, fmt.Errorf("config: %s has invalid origin %q (use scheme://host[:port])", name, part)
		}
		out = append(out, u.Scheme+"://"+u.Host)
	}
	if len(out) == 0 {
		return nil, errors.New("config: " + name + " contains no entries")
	}
	return out, nil
}
