package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"abidata/internal/admin"
	"abidata/internal/auth"
	"abidata/internal/config"
)

func healthcheck() error {
	cfg, err := config.FromEnv()
	if err != nil {
		return err
	}
	_, port, err := net.SplitHostPort(cfg.ListenAddr)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://127.0.0.1:"+port+"/healthz", nil)
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	_, _ = io.Copy(io.Discard, resp.Body)
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("healthz returned %d", resp.StatusCode)
	}
	return nil
}

func userCmd(args []string) error {
	if len(args) == 0 || args[0] != "create-admin" {
		return errors.New("usage: abidata user create-admin --email E [--name N]")
	}
	fs := flag.NewFlagSet("create-admin", flag.ContinueOnError)
	email := fs.String("email", "", "admin email (required)")
	name := fs.String("name", "", "display name")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	if *email == "" {
		return errors.New("--email is required")
	}
	cfg, err := config.FromEnv()
	if err != nil {
		return err
	}
	lock, err := acquireLock(cfg.DataDir)
	if err != nil {
		return err
	}
	defer lock.Release()

	fmt.Fprint(os.Stderr, "Password (read from stdin): ")
	pw, err := bufio.NewReader(os.Stdin).ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return err
	}
	pw = strings.TrimRight(pw, "\r\n")
	fmt.Fprintln(os.Stderr)

	ctx := context.Background()
	keys, err := auth.LoadKeys(filepath.Join(cfg.DataDir, "system", "keys"), cfg.SessionKey, cfg.ManifestKey)
	if err != nil {
		return err
	}
	adm, err := admin.Open(ctx, filepath.Join(cfg.DataDir, "system", "admin.db"), cfg.Synchronous)
	if err != nil {
		return err
	}
	defer func() { _ = adm.Close() }()
	u, err := auth.NewService(adm, keys, auth.DefaultArgon2, nil, nil).CreateUser(ctx, *email, *name, pw, true)
	if err != nil {
		return err
	}
	_ = adm.Audit(ctx, admin.AuditEntry{At: time.Now(), Action: "user.create_admin_cli", ObjectKind: "user", ObjectID: u.ID})
	fmt.Printf("created platform admin %s (%s)\n", u.Email, u.ID)
	return nil
}
