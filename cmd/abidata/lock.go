package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// errLocked means another process (normally the server) holds the data dir.
var errLocked = errors.New("the data directory is in use by a running server; stop it or use the admin API")

// serverLock is an exclusive lock on system/server.lock.
type serverLock struct{ f *os.File }

func acquireLock(dataDir string) (*serverLock, error) {
	dir := filepath.Join(dataDir, "system")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(filepath.Join(dir, "server.lock"), os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return nil, err
	}
	if err := lockFile(f); err != nil {
		_ = f.Close()
		return nil, err
	}
	_ = f.Truncate(0)
	_, _ = fmt.Fprintf(f, "%d\n", os.Getpid())
	return &serverLock{f: f}, nil
}

func (l *serverLock) Release() {
	if l == nil || l.f == nil {
		return
	}
	unlockFile(l.f)
	_ = l.f.Close()
	l.f = nil
}
