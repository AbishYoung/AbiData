package auth

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"abidata/internal/admin"
	"abidata/internal/fault"
	"abidata/internal/ids"
)

func newKeyWorkspace(t *testing.T) (*Service, *admin.Store, *clock, ids.WorkspaceID) {
	t.Helper()
	s, st, c := newSvc(t)
	ctx := context.Background()
	u, err := s.CreateUser(ctx, "o@example.com", "Owner", "long-enough-pw", false)
	if err != nil {
		t.Fatal(err)
	}
	ws := ids.NewWorkspaceID()
	if err := st.CreateWorkspace(ctx, admin.Workspace{ID: ws, Name: "W", Status: admin.StatusActive, CreatedAt: c.now(), CreatedBy: u.ID}, u.ID); err != nil {
		t.Fatal(err)
	}
	return s, st, c, ws
}

func TestAPIKeyTokenFormat(t *testing.T) {
	s, _, _, ws := newKeyWorkspace(t)
	k, tok, err := s.CreateAPIKey(context.Background(), ws, "bot", admin.RoleEditor, "u1", 0)
	if err != nil {
		t.Fatal(err)
	}
	id, secret, ok := ParseAPIKeyToken(tok)
	if !ok || id != k.ID || len(secret) != 43 || tok != "abd_"+id+"_"+secret {
		t.Fatalf("round trip: %q %q %q %v", tok, id, secret, ok)
	}
	// Only the keyed hash of the secret is stored.
	if bytes.Contains(k.SecretHash, []byte(secret)) || !bytes.Equal(k.SecretHash, s.HashAPIKeySecret(secret)) || len(k.SecretHash) != 32 {
		t.Fatal("stored hash is not HMAC(secret)")
	}
	stored, _ := s.repo.GetAPIKey(context.Background(), k.ID)
	if bytes.Contains(stored.SecretHash, []byte(secret)) {
		t.Fatal("secret stored in the clear")
	}
	// The hash is keyed: another server key yields another hash.
	other := NewService(s.repo, Keys{Session: bytes.Repeat([]byte{1}, 32)}, fastParams, nil, nil)
	if bytes.Equal(other.HashAPIKeySecret(secret), k.SecretHash) {
		t.Fatal("hash does not depend on the server key")
	}
	// Secrets are unique and use only base62.
	seen := map[string]bool{}
	for range 200 {
		sec := newAPIKeySecret()
		if seen[sec] || len(sec) != 43 || strings.Trim(sec, base62) != "" {
			t.Fatalf("bad secret %q", sec)
		}
		seen[sec] = true
	}
	for _, bad := range []string{"", "abd_", "abd_" + id, "abd_" + id + "_", "abd_" + id + "_" + secret + "x", "abd_" + id + "_" + secret[1:],
		"abc_" + id + "_" + secret, "abd_" + strings.ToLower(id) + "_" + secret, "abd_" + id + "-" + secret,
		"abd_" + id + "_" + secret[:42] + "-", "abd_" + id + "_" + secret[:42] + "_", " " + tok} {
		if _, _, ok := ParseAPIKeyToken(bad); ok {
			t.Errorf("accepted %q", bad)
		}
	}
}

func TestAPIKeyCreateValidation(t *testing.T) {
	s, _, _, ws := newKeyWorkspace(t)
	ctx := context.Background()
	if _, _, err := s.CreateAPIKey(ctx, ws, "x", admin.RoleOwner, "u", 0); code(err) != fault.BadRequest {
		t.Fatalf("owner key: %v", err)
	}
	if _, _, err := s.CreateAPIKey(ctx, ws, "x", admin.Role("root"), "u", 0); code(err) != fault.BadRequest {
		t.Fatalf("bogus role: %v", err)
	}
	for _, name := range []string{"", "   ", strings.Repeat("n", 101), "a\nb", `a"b`} {
		if _, _, err := s.CreateAPIKey(ctx, ws, name, admin.RoleViewer, "u", 0); code(err) != fault.BadRequest {
			t.Errorf("name %q accepted: %v", name, err)
		}
	}
	// The database refuses an owner key even if the application did not.
	err := s.repo.CreateAPIKey(ctx, admin.APIKey{ID: ids.New(), WorkspaceID: ws, Name: "n", Role: admin.RoleOwner, SecretHash: make([]byte, 32), CreatedBy: "u", CreatedAt: time.Now()})
	if err == nil {
		t.Fatal("owner role stored")
	}
}

func TestAPIKeyAuthenticateExpiryRevocationAndAttribution(t *testing.T) {
	s, st, c, ws := newKeyWorkspace(t)
	ctx := context.Background()
	k, tok, err := s.CreateAPIKey(ctx, ws, "Nightly sync", admin.RoleBuilder, "u1", 24*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	p, err := s.AuthenticateAPIKey(ctx, "Bearer "+tok, "10.0.0.1")
	if err != nil {
		t.Fatal(err)
	}
	if p.UserID != "apikey:"+k.ID || p.DisplayName != `API key "Nightly sync"` || !p.IsAPIKey() || p.KeyWorkspace != ws ||
		p.KeyRole != admin.RoleBuilder || p.IsPlatformAdmin || p.CSRFToken != "" || p.SessionHash != "" {
		t.Fatalf("principal %+v", p)
	}
	// Expiry.
	c.t = c.t.Add(24*time.Hour + time.Second)
	if _, err := s.AuthenticateAPIKey(ctx, "Bearer "+tok, "10.0.0.1"); code(err) != fault.Unauthenticated {
		t.Fatalf("expired: %v", err)
	}
	// Revocation.
	c.t = c.t.Add(-24 * time.Hour)
	k2, tok2, _ := s.CreateAPIKey(ctx, ws, "b", admin.RoleViewer, "u1", 0)
	if _, err := s.AuthenticateAPIKey(ctx, "Bearer "+tok2, "10.0.0.1"); err != nil {
		t.Fatal(err)
	}
	if err := st.RevokeAPIKey(ctx, k2.ID, c.now()); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AuthenticateAPIKey(ctx, "Bearer "+tok2, "10.0.0.1"); code(err) != fault.Unauthenticated {
		t.Fatalf("revoked: %v", err)
	}
	// A key whose workspace is deleted from the admin DB disappears with it.
	if _, err := st.GetAPIKey(ctx, k.ID); err != nil {
		t.Fatal(err)
	}
}

func TestAPIKeyLastUsedThrottle(t *testing.T) {
	s, st, c, ws := newKeyWorkspace(t)
	ctx := context.Background()
	k, tok, _ := s.CreateAPIKey(ctx, ws, "n", admin.RoleViewer, "u", 0)
	used := func() time.Time {
		got, _ := st.GetAPIKey(ctx, k.ID)
		if got.LastUsedAt == nil {
			return time.Time{}
		}
		return *got.LastUsedAt
	}
	if !used().IsZero() {
		t.Fatal("used before first request")
	}
	call := func() {
		if _, err := s.AuthenticateAPIKey(ctx, "Bearer "+tok, "10.0.0.1"); err != nil {
			t.Fatal(err)
		}
	}
	call()
	t0 := used()
	if t0.IsZero() {
		t.Fatal("first use not recorded")
	}
	for range 50 {
		c.t = c.t.Add(time.Second)
		if c.t.Sub(t0) >= time.Minute {
			break
		}
		call()
		if !used().Equal(t0) {
			t.Fatalf("last_used_at rewritten after %v", c.t.Sub(t0))
		}
	}
	c.t = t0.Add(61 * time.Second)
	call()
	if !used().After(t0) {
		t.Fatal("last_used_at not refreshed after a minute")
	}
}

func TestAPIKeyPerKeyRateLimit(t *testing.T) {
	s, _, c, ws := newKeyWorkspace(t)
	ctx := context.Background()
	s.KeyRatePerMinute = 60 // burst 10, one per second
	_, tok, _ := s.CreateAPIKey(ctx, ws, "n", admin.RoleViewer, "u", 0)
	_, tok2, _ := s.CreateAPIKey(ctx, ws, "m", admin.RoleViewer, "u", 0)
	n := 0
	for ; n < 100; n++ {
		if _, err := s.AuthenticateAPIKey(ctx, "Bearer "+tok, "10.0.0.1"); err != nil {
			fe := fault.Classify(err)
			if fe.Code != fault.RateLimited || fe.RetryAfter <= 0 || fe.RetryAfter > 2*time.Second {
				t.Fatalf("limit error %v", err)
			}
			break
		}
	}
	if n != 10 {
		t.Fatalf("burst allowed %d requests, want 10", n)
	}
	if _, err := s.AuthenticateAPIKey(ctx, "Bearer "+tok2, "10.0.0.1"); err != nil {
		t.Fatalf("second key shares the first's budget: %v", err)
	}
	c.t = c.t.Add(3 * time.Second)
	for i := range 3 {
		if _, err := s.AuthenticateAPIKey(ctx, "Bearer "+tok, "10.0.0.1"); err != nil {
			t.Fatalf("refill %d: %v", i, err)
		}
	}
	if _, err := s.AuthenticateAPIKey(ctx, "Bearer "+tok, "10.0.0.1"); code(err) != fault.RateLimited {
		t.Fatalf("refill overshoot: %v", err)
	}
}

func TestAPIKeyFailedAttemptsThrottleByAddress(t *testing.T) {
	s, _, c, ws := newKeyWorkspace(t)
	ctx := context.Background()
	_, tok, _ := s.CreateAPIKey(ctx, ws, "n", admin.RoleViewer, "u", 0)
	bad := "Bearer abd_" + ids.New() + "_" + strings.Repeat("z", 43)
	for i := range KeyFailMax {
		if _, err := s.AuthenticateAPIKey(ctx, bad, "203.0.113.9"); code(err) != fault.Unauthenticated {
			t.Fatalf("attempt %d: %v", i, err)
		}
	}
	// Past the limit even a valid key from that address waits...
	_, err := s.AuthenticateAPIKey(ctx, "Bearer "+tok, "203.0.113.9")
	if fe := fault.Classify(err); fe.Code != fault.RateLimited || fe.RetryAfter <= 0 {
		t.Fatalf("not throttled: %v", err)
	}
	// ...other addresses are unaffected, and the window passes.
	if _, err := s.AuthenticateAPIKey(ctx, "Bearer "+tok, "203.0.113.10"); err != nil {
		t.Fatalf("other address: %v", err)
	}
	c.t = c.t.Add(KeyFailWindow + time.Second)
	if _, err := s.AuthenticateAPIKey(ctx, "Bearer "+tok, "203.0.113.9"); err != nil {
		t.Fatalf("after window: %v", err)
	}
}
