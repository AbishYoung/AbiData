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

const userUsage = "usage: abidata user create-admin --email E [--name N] | abidata user reset-password --email E"

func userCmd(args []string) error {
	if len(args) == 0 || (args[0] != "create-admin" && args[0] != "reset-password") {
		return errors.New(userUsage)
	}
	reset := args[0] == "reset-password"
	fs := flag.NewFlagSet(args[0], flag.ContinueOnError)
	email := fs.String("email", "", "user email (required)")
	name := fs.String("name", "", "display name (create-admin)")
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

	pw := os.Getenv("ABIDATA_PASSWORD")
	if pw == "" {
		fmt.Fprint(os.Stderr, "Password (read from stdin): ")
		pw, err = bufio.NewReader(os.Stdin).ReadString('\n')
		if err != nil && !errors.Is(err, io.EOF) {
			return err
		}
		pw = strings.TrimRight(pw, "\r\n")
		fmt.Fprintln(os.Stderr)
	}

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
	svc := auth.NewService(adm, keys, auth.DefaultArgon2, nil, nil)
	if reset {
		em, err := auth.NormalizeEmail(*email)
		if err != nil {
			return err
		}
		target, err := adm.GetUserByEmail(ctx, em)
		if errors.Is(err, admin.ErrNotFound) {
			return fmt.Errorf("no user has the email %s", em)
		} else if err != nil {
			return err
		}
		u, err := svc.ResetPassword(ctx, target.ID, pw)
		if err != nil {
			return err
		}
		_ = adm.Audit(ctx, admin.AuditEntry{At: time.Now(), Action: "user.password_reset_cli", ObjectKind: "user", ObjectID: u.ID,
			Details: map[string]any{"target_email": u.Email}})
		fmt.Printf("reset the password of %s (%s); all of their sessions were ended\n", u.Email, u.ID)
		return nil
	}
	u, err := svc.CreateUser(ctx, *email, *name, pw, true)
	if err != nil {
		return err
	}
	_ = adm.Audit(ctx, admin.AuditEntry{At: time.Now(), Action: "user.create_admin_cli", ObjectKind: "user", ObjectID: u.ID})
	fmt.Printf("created platform admin %s (%s)\n", u.Email, u.ID)
	return nil
}
