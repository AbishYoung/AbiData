package main

import (
	"bytes"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

func TestLockExclusive(t *testing.T) {
	dir := t.TempDir()
	l, err := acquireLock(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := acquireLock(dir); !errors.Is(err, errLocked) {
		t.Fatalf("second lock: %v", err)
	}
	l.Release()
	l2, err := acquireLock(dir)
	if err != nil {
		t.Fatal(err)
	}
	l2.Release()
}

func freePort(t *testing.T) int {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}

// TestServeSecondInstanceAndShutdown builds the binary, starts serve, verifies a
// second serve on the same data dir fails on the lock, then sends SIGTERM and
// expects a clean exit.
func TestServeSecondInstanceAndShutdown(t *testing.T) {
	if testing.Short() {
		t.Skip("builds the binary")
	}
	bin := filepath.Join(t.TempDir(), "abidata")
	if out, err := exec.Command("go", "build", "-o", bin, ".").CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}
	data := t.TempDir()
	port := freePort(t)
	env := append(os.Environ(),
		"ABIDATA_DATA_DIR="+data, "ABIDATA_BACKUP_DIR="+filepath.Join(data, "backups"),
		fmt.Sprintf("ABIDATA_LISTEN_ADDR=127.0.0.1:%d", port), fmt.Sprintf("ABIDATA_PUBLIC_URL=http://127.0.0.1:%d", port),
		"ABIDATA_BOOTSTRAP_ADMIN_EMAIL=admin@example.com")

	var out1 syncBuffer
	first := exec.Command(bin, "serve")
	first.Env, first.Stdout, first.Stderr = env, &out1, &out1
	if err := first.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = first.Process.Kill() }()
	url := fmt.Sprintf("http://127.0.0.1:%d/healthz", port)
	deadline := time.Now().Add(20 * time.Second)
	for {
		resp, err := http.Get(url)
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == 200 {
				break
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("server did not come up:\n%s", out1.String())
		}
		time.Sleep(50 * time.Millisecond)
	}
	if !strings.Contains(out1.String(), "setup_token") {
		t.Errorf("bootstrap token not logged:\n%s", out1.String())
	}

	hc := exec.Command(bin, "healthcheck")
	hc.Env = env
	if out, err := hc.CombinedOutput(); err != nil {
		t.Fatalf("healthcheck: %v %s", err, out)
	}

	second := exec.Command(bin, "serve")
	second.Env = env
	out2, err := second.CombinedOutput()
	if err == nil || !strings.Contains(string(out2), "in use by a running server") {
		t.Fatalf("second serve: err=%v out=%s", err, out2)
	}
	cli := exec.Command(bin, "user", "create-admin", "--email", "x@example.com")
	cli.Env, cli.Stdin = env, strings.NewReader("long-enough-pw\n")
	if out, err := cli.CombinedOutput(); err == nil || !strings.Contains(string(out), "in use") {
		t.Fatalf("CLI ran against a live server: %v %s", err, out)
	}

	if err := first.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- first.Wait() }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("serve exit: %v\n%s", err, out1.String())
		}
	case <-time.After(30 * time.Second):
		t.Fatal("serve did not stop on SIGTERM")
	}
	if !strings.Contains(out1.String(), `"msg":"stopped"`) {
		t.Errorf("no clean stop logged:\n%s", out1.String())
	}

	// After shutdown the CLI works.
	cli = exec.Command(bin, "user", "create-admin", "--email", "x@example.com")
	cli.Env, cli.Stdin = env, strings.NewReader("long-enough-pw\n")
	if out, err := cli.CombinedOutput(); err != nil {
		t.Fatalf("create-admin: %v %s", err, out)
	}
}

type syncBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}
