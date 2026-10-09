package auth

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// Keys are the server secrets.
type Keys struct {
	Session  []byte // HMAC key for session-ID hashing
	Manifest []byte // HMAC key for backup manifests
}

// LoadKeys returns the configured keys, generating any missing one into
// dir (system/keys, mode 0700) as a 0600 file on first boot. Supplied keys
// (from env or _FILE) always win over files.
func LoadKeys(dir string, session, manifest []byte) (Keys, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return Keys{}, err
	}
	var k Keys
	var err error
	if k.Session, err = loadOrCreate(filepath.Join(dir, "session.key"), session); err != nil {
		return Keys{}, err
	}
	if k.Manifest, err = loadOrCreate(filepath.Join(dir, "manifest.key"), manifest); err != nil {
		return Keys{}, err
	}
	return k, nil
}

func loadOrCreate(path string, supplied []byte) ([]byte, error) {
	if len(supplied) > 0 {
		return supplied, nil
	}
	if fi, err := os.Stat(path); err == nil && fi.Mode().Perm()&0o077 != 0 {
		return nil, fmt.Errorf("auth: key file %s is accessible by other users (mode %v); chmod 600 it", path, fi.Mode().Perm())
	}
	b, err := os.ReadFile(path)
	if err == nil {
		key, derr := base64.RawURLEncoding.DecodeString(strings.TrimSpace(string(b)))
		if derr != nil || len(key) < 32 {
			return nil, fmt.Errorf("auth: key file %s is malformed", path)
		}
		return key, nil
	}
	if !errors.Is(err, fs.ErrNotExist) {
		return nil, err
	}
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return nil, err
	}
	if _, err := f.WriteString(base64.RawURLEncoding.EncodeToString(key) + "\n"); err != nil {
		_ = f.Close()
		return nil, err
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		return nil, err
	}
	return key, f.Close()
}
