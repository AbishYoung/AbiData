package auth

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"abidata/internal/admin"
	"abidata/internal/admin/admintest"
	"abidata/internal/fault"
)

var fastParams = Argon2Params{Memory: 1024, Time: 1, Threads: 1, KeyLen: 32, SaltLen: 16}

type clock struct{ t time.Time }

func (c *clock) now() time.Time { return c.t }

func newSvc(t *testing.T) (*Service, *admin.Store, *clock) {
	t.Helper()
	st := admintest.Open(t)
	c := &clock{t: time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)}
	return NewService(st, Keys{Session: make([]byte, 32)}, fastParams, c.now, nil), st, c
}

func code(err error) fault.Code {
	if err == nil {
		return ""
	}
	return fault.Classify(err).Code
}

func TestPasswordHashing(t *testing.T) {
	h, err := HashPassword("correct horse battery", fastParams)
	if err != nil {
		t.Fatal(err)
	}
	if ok, _ := VerifyPassword("correct horse battery", h); !ok {
		t.Fatal("verify failed")
	}
	if ok, _ := VerifyPassword("wrong", h); ok {
		t.Fatal("wrong password verified")
	}
	if !NeedsRehash(h, DefaultArgon2) || NeedsRehash(h, fastParams) {
		t.Fatal("NeedsRehash wrong")
	}
	if _, err := VerifyPassword("x", "$argon2i$v=19$m=1,t=1,p=1$AA$AA"); !errors.Is(err, ErrBadHash) {
		t.Fatal("accepted non-argon2id hash")
	}
}

func TestLoginSessionLifecycle(t *testing.T) {
	ctx := context.Background()
	s, _, c := newSvc(t)
	if _, err := s.CreateUser(ctx, "Ada@Example.com", "Ada", "long-enough-pw", false); err != nil {
		t.Fatal(err)
	}
	res, err := s.Login(ctx, "ada@example.com", "long-enough-pw", "10.0.0.1", "ua")
	if err != nil {
		t.Fatal(err)
	}
	p, err := s.Authenticate(ctx, res.Token)
	if err != nil || p.Email != "ada@example.com" || p.CSRFToken == "" {
		t.Fatalf("%+v %v", p, err)
	}
	// A second login rotates: a different token, both valid until logout.
	res2, _ := s.Login(ctx, "ada@example.com", "long-enough-pw", "10.0.0.1", "ua")
	if res2.Token == res.Token {
		t.Fatal("session token reused")
	}
	// Idle expiry.
	c.t = c.t.Add(IdleTimeout + time.Second)
	if _, err := s.Authenticate(ctx, res.Token); code(err) != fault.Unauthenticated {
		t.Fatalf("idle session accepted: %v", err)
	}
	// Absolute expiry even when active.
	res3, _ := s.Login(ctx, "ada@example.com", "long-enough-pw", "10.0.0.1", "ua")
	for i := 0; i < 15; i++ {
		c.t = c.t.Add(11 * time.Hour)
		_, err := s.Authenticate(ctx, res3.Token)
		if c.t.Sub(res3.ExpiresAt.Add(-AbsoluteTimeout)) < AbsoluteTimeout && err != nil {
			t.Fatalf("active session rejected early at step %d: %v", i, err)
		}
		if c.t.Sub(res3.ExpiresAt.Add(-AbsoluteTimeout)) >= AbsoluteTimeout {
			if code(err) != fault.Unauthenticated {
				t.Fatal("session outlived absolute timeout")
			}
			break
		}
	}
	// Logout.
	res4, _ := s.Login(ctx, "ada@example.com", "long-enough-pw", "10.0.0.1", "ua")
	p4, _ := s.Authenticate(ctx, res4.Token)
	_ = s.Logout(ctx, p4)
	if _, err := s.Authenticate(ctx, res4.Token); code(err) != fault.Unauthenticated {
		t.Fatal("logged-out session accepted")
	}
	if _, err := s.Authenticate(ctx, "garbage"); code(err) != fault.Unauthenticated {
		t.Fatal("garbage token accepted")
	}
}

func TestLoginLockoutAfterFiveAttempts(t *testing.T) {
	ctx := context.Background()
	s, _, c := newSvc(t)
	_, _ = s.CreateUser(ctx, "bob@example.com", "Bob", "long-enough-pw", false)
	for i := 0; i < 5; i++ {
		if _, err := s.Login(ctx, "bob@example.com", "wrong-password", "1.2.3.4", ""); code(err) != fault.Unauthenticated {
			t.Fatalf("attempt %d: %v", i, err)
		}
	}
	_, err := s.Login(ctx, "bob@example.com", "long-enough-pw", "1.2.3.4", "")
	if fe := fault.Classify(err); fe.Code != fault.RateLimited || fe.RetryAfter <= 0 {
		t.Fatalf("want RATE_LIMITED, got %v", err)
	}
	// Another IP is not locked out for this email.
	if _, err := s.Login(ctx, "bob@example.com", "long-enough-pw", "5.6.7.8", ""); err != nil {
		t.Fatal(err)
	}
	// Unknown emails count too and give the same error as wrong passwords.
	if _, err := s.Login(ctx, "nobody@example.com", "x", "9.9.9.9", ""); code(err) != fault.Unauthenticated {
		t.Fatal(err)
	}
	// After the window the correct password works again.
	c.t = c.t.Add(LoginWindow + time.Second)
	if _, err := s.Login(ctx, "bob@example.com", "long-enough-pw", "1.2.3.4", ""); err != nil {
		t.Fatal(err)
	}
}

func TestDisabledUserAndPasswordChange(t *testing.T) {
	ctx := context.Background()
	s, st, _ := newSvc(t)
	u, _ := s.CreateUser(ctx, "eve@example.com", "Eve", "long-enough-pw", false)
	a, _ := s.Login(ctx, "eve@example.com", "long-enough-pw", "1.1.1.1", "")
	b, _ := s.Login(ctx, "eve@example.com", "long-enough-pw", "1.1.1.2", "")
	pa, _ := s.Authenticate(ctx, a.Token)
	if err := s.ChangePassword(ctx, pa, "long-enough-pw", "another-long-pw"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Authenticate(ctx, a.Token); err != nil {
		t.Fatal("own session ended by password change")
	}
	if _, err := s.Authenticate(ctx, b.Token); code(err) != fault.Unauthenticated {
		t.Fatal("other session survived password change")
	}
	disabled := true
	_ = st.UpdateUser(ctx, u.ID, admin.UserPatch{Disabled: &disabled})
	if _, err := s.Authenticate(ctx, a.Token); code(err) != fault.Unauthenticated {
		t.Fatal("disabled user authenticated")
	}
	if _, err := s.Login(ctx, "eve@example.com", "another-long-pw", "1.1.1.1", ""); code(err) != fault.Unauthenticated {
		t.Fatal("disabled user logged in")
	}
}

func TestBootstrapOneTime(t *testing.T) {
	ctx := context.Background()
	s, _, _ := newSvc(t)
	tok, err := s.PrepareBootstrap(ctx, "root@example.com")
	if err != nil || tok == "" {
		t.Fatal(tok, err)
	}
	if _, err := s.CompleteBootstrap(ctx, "wrong", "Root", "long-enough-pw"); code(err) != fault.Forbidden {
		t.Fatal("wrong token accepted")
	}
	u, err := s.CompleteBootstrap(ctx, tok, "Root", "long-enough-pw")
	if err != nil || !u.IsPlatformAdmin {
		t.Fatal(u, err)
	}
	if _, err := s.CompleteBootstrap(ctx, tok, "Root", "long-enough-pw"); code(err) != fault.Forbidden {
		t.Fatal("token reused")
	}
	if tok2, _ := s.PrepareBootstrap(ctx, "root@example.com"); tok2 != "" {
		t.Fatal("bootstrap offered with existing users")
	}
}

func TestKeysGeneratedOnce(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "keys")
	k1, err := LoadKeys(dir, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	k2, _ := LoadKeys(dir, nil, nil)
	if string(k1.Session) != string(k2.Session) || len(k1.Manifest) != 32 {
		t.Fatal("keys not stable")
	}
	fi, _ := os.Stat(filepath.Join(dir, "session.key"))
	if fi.Mode().Perm() != 0o600 {
		t.Fatalf("key mode %v", fi.Mode().Perm())
	}
	sup := []byte("0123456789abcdef0123456789abcdef")
	k3, _ := LoadKeys(dir, sup, nil)
	if string(k3.Session) != string(sup) {
		t.Fatal("supplied key ignored")
	}
}

// Regression (Phase 0 review #3): parallel attempts cannot exceed the limit.
func TestLoginRateLimitUnderConcurrency(t *testing.T) {
	ctx := context.Background()
	s, _, _ := newSvc(t)
	_, _ = s.CreateUser(ctx, "par@example.com", "P", "long-enough-pw", false)
	var wg sync.WaitGroup
	var unauth, limited atomic.Int64
	for range 40 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := s.Login(ctx, "par@example.com", "wrong-password", "7.7.7.7", "")
			switch code(err) {
			case fault.Unauthenticated:
				unauth.Add(1)
			case fault.RateLimited:
				limited.Add(1)
			default:
				t.Errorf("unexpected %v", err)
			}
		}()
	}
	wg.Wait()
	if unauth.Load() > LoginMaxPerKey {
		t.Fatalf("%d password checks ran; limit is %d", unauth.Load(), LoginMaxPerKey)
	}
}

// Regression (#7): an account cannot be brute-forced by rotating source addresses.
func TestLoginPerAccountLimitAndIPv6Prefix(t *testing.T) {
	ctx := context.Background()
	s, _, _ := newSvc(t)
	_, _ = s.CreateUser(ctx, "acct@example.com", "A", "long-enough-pw", false)
	for i := range LoginMaxPerEmail {
		ip := fmt.Sprintf("10.0.%d.%d", i/250, i%250)
		_, _ = s.Login(ctx, "acct@example.com", "wrong-password", ip, "")
	}
	if _, err := s.Login(ctx, "acct@example.com", "wrong-password", "192.0.2.99", ""); code(err) != fault.RateLimited {
		t.Fatalf("per-account limit not enforced: %v", err)
	}
	// Addresses in one IPv6 /64 share a key.
	if limitKeyIP("2001:db8:1:2::1") != limitKeyIP("2001:db8:1:2:ffff::9") || limitKeyIP("2001:db8:1:3::1") == limitKeyIP("2001:db8:1:2::1") {
		t.Fatal("IPv6 /64 keying wrong")
	}
	if limitKeyIP("::ffff:1.2.3.4") != limitKeyIP("1.2.3.4") {
		t.Fatal("v4-mapped address not normalised")
	}
}

// Regression (#8): current-password guesses on ChangePassword are limited.
func TestChangePasswordAttemptsLimited(t *testing.T) {
	ctx := context.Background()
	s, _, _ := newSvc(t)
	_, _ = s.CreateUser(ctx, "cp@example.com", "C", "long-enough-pw", false)
	res, _ := s.Login(ctx, "cp@example.com", "long-enough-pw", "1.1.1.1", "")
	p, _ := s.Authenticate(ctx, res.Token)
	for range LoginMaxPerKey {
		_ = s.ChangePassword(ctx, p, "wrong-password", "another-long-pw")
	}
	if err := s.ChangePassword(ctx, p, "long-enough-pw", "another-long-pw"); code(err) != fault.RateLimited {
		t.Fatalf("want RATE_LIMITED, got %v", err)
	}
}
