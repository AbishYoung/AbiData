package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

// errLocked means another process (normally the server) holds the data dir.
var errLocked = errors.New("the data directory is in use by a running server; stop it or use the admin API")

// serverLock is an exclusive flock on system/server.lock.
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
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		_ = f.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) {
			return nil, errLocked
		}
		return nil, fmt.Errorf("lock data dir: %w", err)
	}
	_ = f.Truncate(0)
	_, _ = fmt.Fprintf(f, "%d\n", os.Getpid())
	return &serverLock{f: f}, nil
}

func (l *serverLock) Release() {
	if l == nil || l.f == nil {
		return
	}
	_ = syscall.Flock(int(l.f.Fd()), syscall.LOCK_UN)
	_ = l.f.Close()
	l.f = nil
}
