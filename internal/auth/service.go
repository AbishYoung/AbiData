package auth

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"log/slog"
	"net/mail"
	"net/netip"
	"runtime"
	"strings"
	"sync"
	"time"

	"abidata/internal/admin"
	"abidata/internal/fault"
	"abidata/internal/ids"
)

// Session lifetime and login throttling (brief <auth_and_tenancy>).
const (
	IdleTimeout      = 12 * time.Hour
	AbsoluteTimeout  = 7 * 24 * time.Hour
	LoginWindow      = 15 * time.Minute
	LoginMaxPerKey   = 5   // per email + client address (brief)
	LoginMaxPerEmail = 20  // per account, across addresses (IPv6 rotation)
	LoginMaxPerIP    = 300 // per address, slows password spraying across accounts
	touchInterval    = time.Minute
	hashWait         = 2 * time.Second
)

// limitKeyIP normalises a client address for rate limiting: IPv4 (including
// v4-mapped) as-is, IPv6 by its /64 so one host cannot rotate addresses.
func limitKeyIP(ip string) string {
	a, err := netip.ParseAddr(ip)
	if err != nil {
		return ip
	}
	a = a.Unmap()
	if a.Is6() {
		p, _ := a.Prefix(64)
		return p.String()
	}
	return a.String()
}

// Principal is the authenticated caller.
type Principal struct {
	UserID          string
	Email           string
	DisplayName     string
	IsPlatformAdmin bool
	SessionHash     string
	CSRFToken       string

	// Set only for API-key callers (UserID is then "apikey:<APIKeyID>").
	APIKeyID     string
	APIKeyName   string
	KeyWorkspace ids.WorkspaceID
	KeyRole      admin.Role
}

// IsAPIKey reports whether the caller authenticated with an API key.
func (p Principal) IsAPIKey() bool { return p.APIKeyID != "" }

// Service authenticates users and manages sessions.
type Service struct {
	repo   admin.Repository
	keys   Keys
	params Argon2Params
	now    func() time.Time
	log    *slog.Logger

	dummy func() string
	// hashSlots bounds concurrent argon2 work (64 MiB each) so unauthenticated
	// login floods cannot exhaust memory.
	hashSlots chan struct{}

	// KeyRatePerMinute is the per-key request budget (0: DefaultKeyRatePerMinute).
	KeyRatePerMinute int
	kmu              sync.Mutex
	kbuckets         map[string]*keyBucket
	ktouched         map[string]time.Time

	setupMu    sync.Mutex
	setupToken string // one-time bootstrap token; empty once used
	setupEmail string
}

// NewService builds a Service. now may be nil (time.Now).
func NewService(repo admin.Repository, keys Keys, params Argon2Params, now func() time.Time, log *slog.Logger) *Service {
	if now == nil {
		now = time.Now
	}
	return &Service{repo: repo, keys: keys, params: params, now: now, log: log,
		kbuckets: map[string]*keyBucket{}, ktouched: map[string]time.Time{},
		dummy:     sync.OnceValue(func() string { h, _ := HashPassword("dummy-password", params); return h }),
		hashSlots: make(chan struct{}, max(2, min(8, runtime.GOMAXPROCS(0)/2)))}
}

// HashToken maps a raw session token to the stored id_hash (HMAC-SHA256 with the
// session key, hex) so a leaked admin DB does not yield usable cookies.
func (s *Service) HashToken(raw string) string {
	m := hmac.New(sha256.New, s.keys.Session)
	m.Write([]byte(raw))
	return hex.EncodeToString(m.Sum(nil))
}

// LoginResult is returned by Login.
type LoginResult struct {
	Token     string // raw cookie value
	Principal Principal
	ExpiresAt time.Time
}

// NormalizeEmail validates and lower-cases an email address.
func NormalizeEmail(email string) (string, error) {
	email = strings.TrimSpace(email)
	a, err := mail.ParseAddress(email)
	if err != nil || a.Address != email || len(email) > 254 {
		return "", fault.New(fault.BadRequest, "Enter a valid email address.")
	}
	return strings.ToLower(email), nil
}

// Login verifies credentials, enforces rate limits, and creates a session.
// Failures never reveal whether the email exists.
func (s *Service) Login(ctx context.Context, email, password, ip, userAgent string) (LoginResult, error) {
	now := s.now()
	email = strings.ToLower(strings.TrimSpace(email))
	ipKey := limitKeyIP(ip)
	keyPair := "pair:" + email + "|" + ipKey
	keyEmail := "email:" + email
	// Count the attempt before verifying, atomically per key, so concurrent
	// requests cannot slip past the limit.
	if err := s.countAttempts(ctx, now, map[string]int{keyPair: LoginMaxPerKey, keyEmail: LoginMaxPerEmail, "ip:" + ipKey: LoginMaxPerIP}); err != nil {
		return LoginResult{}, err
	}

	invalid := fault.New(fault.Unauthenticated, "The email or password is incorrect.")
	u, err := s.repo.GetUserByEmail(ctx, email)
	if errors.Is(err, admin.ErrNotFound) {
		_, _ = s.verify(ctx, password, s.dummy())
		return LoginResult{}, invalid
	}
	if err != nil {
		return LoginResult{}, err
	}
	ok, err := s.verify(ctx, password, u.PasswordHash)
	if err != nil {
		var fe *fault.Error
		if errors.As(err, &fe) {
			return LoginResult{}, err
		}
	}
	if !ok || u.Disabled {
		return LoginResult{}, invalid
	}
	_ = s.repo.ClearLoginAttempts(ctx, keyEmail)
	_ = s.repo.ClearLoginAttempts(ctx, keyPair)

	if NeedsRehash(u.PasswordHash, s.params) {
		if h, err := s.hash(ctx, password); err == nil {
			_ = s.repo.SetPasswordHash(ctx, u.ID, h)
		}
	}
	_ = s.repo.TouchLogin(ctx, u.ID, now)

	token := ids.Token(32)
	sess := admin.Session{
		IDHash: s.HashToken(token), UserID: u.ID, CSRFToken: ids.Token(32),
		CreatedAt: now, LastSeenAt: now, ExpiresAt: now.Add(AbsoluteTimeout),
		IP: ip, UserAgent: truncate(userAgent, 256),
	}
	if err := s.repo.CreateSession(ctx, sess); err != nil {
		return LoginResult{}, err
	}
	return LoginResult{Token: token, ExpiresAt: sess.ExpiresAt, Principal: principalOf(u, sess)}, nil
}

// countAttempts increments each key's fixed-window counter and fails with
// RATE_LIMITED if any exceeds its maximum.
func (s *Service) countAttempts(ctx context.Context, now time.Time, limits map[string]int) error {
	for key, limit := range limits {
		a, err := s.repo.RecordLoginFailure(ctx, key, now, LoginWindow)
		if err != nil {
			return err
		}
		if a.Count > limit {
			return &fault.Error{Code: fault.RateLimited, RetryAfter: a.WindowStart.Add(LoginWindow).Sub(now),
				Message: "Too many sign-in attempts. Wait before trying again."}
		}
	}
	return nil
}

func (s *Service) acquireHashSlot(ctx context.Context) (func(), error) {
	t := time.NewTimer(hashWait)
	defer t.Stop()
	select {
	case s.hashSlots <- struct{}{}:
		return func() { <-s.hashSlots }, nil
	case <-t.C:
		return nil, &fault.Error{Code: fault.RateLimited, RetryAfter: time.Second, Message: "The server is busy. Try again in a moment."}
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (s *Service) verify(ctx context.Context, password, hash string) (bool, error) {
	release, err := s.acquireHashSlot(ctx)
	if err != nil {
		return false, err
	}
	defer release()
	return VerifyPassword(password, hash)
}

func (s *Service) hash(ctx context.Context, password string) (string, error) {
	release, err := s.acquireHashSlot(ctx)
	if err != nil {
		return "", err
	}
	defer release()
	return HashPassword(password, s.params)
}

func principalOf(u admin.User, sess admin.Session) Principal {
	return Principal{UserID: u.ID, Email: u.Email, DisplayName: u.DisplayName, IsPlatformAdmin: u.IsPlatformAdmin,
		SessionHash: sess.IDHash, CSRFToken: sess.CSRFToken}
}

// Authenticate resolves a raw session token. Expired (idle or absolute) sessions
// are deleted; disabled users are rejected.
func (s *Service) Authenticate(ctx context.Context, token string) (Principal, error) {
	unauth := fault.E(fault.Unauthenticated)
	if token == "" || len(token) > 128 {
		return Principal{}, unauth
	}
	h := s.HashToken(token)
	sess, err := s.repo.GetSession(ctx, h)
	if errors.Is(err, admin.ErrNotFound) {
		return Principal{}, unauth
	}
	if err != nil {
		return Principal{}, err
	}
	now := s.now()
	if !now.Before(sess.ExpiresAt) || !now.Before(sess.LastSeenAt.Add(IdleTimeout)) {
		_ = s.repo.DeleteSession(ctx, h)
		return Principal{}, unauth
	}
	u, err := s.repo.GetUser(ctx, sess.UserID)
	if err != nil || u.Disabled {
		_ = s.repo.DeleteSession(ctx, h)
		return Principal{}, unauth
	}
	if now.Sub(sess.LastSeenAt) >= touchInterval {
		_ = s.repo.TouchSession(ctx, h, now)
	}
	return principalOf(u, sess), nil
}

// Logout deletes the session.
func (s *Service) Logout(ctx context.Context, p Principal) error {
	return s.repo.DeleteSession(ctx, p.SessionHash)
}

// ChangePassword verifies the current password, stores the new hash, and ends
// every other session of the user.
func (s *Service) ChangePassword(ctx context.Context, p Principal, current, next string) error {
	u, err := s.repo.GetUser(ctx, p.UserID)
	if err != nil {
		return err
	}
	key := "pw:" + u.ID
	if err := s.countAttempts(ctx, s.now(), map[string]int{key: LoginMaxPerKey}); err != nil {
		return err
	}
	ok, err := s.verify(ctx, current, u.PasswordHash)
	if err != nil {
		return err
	}
	if !ok {
		return fault.New(fault.BadRequest, "The current password is incorrect.")
	}
	_ = s.repo.ClearLoginAttempts(ctx, key)
	if err := ValidatePassword(next); err != nil {
		return err
	}
	h, err := s.hash(ctx, next)
	if err != nil {
		return err
	}
	own, err := s.repo.GetSession(ctx, p.SessionHash)
	if err != nil {
		return err
	}
	if err := s.repo.SetPasswordHash(ctx, u.ID, h); err != nil {
		return err
	}
	if err := s.repo.DeleteUserSessions(ctx, u.ID); err != nil {
		return err
	}
	// Keep the caller's own session (same expiry); every other session ends.
	return s.repo.CreateSession(ctx, own)
}

// ValidatePassword enforces the password policy.
func ValidatePassword(pw string) error {
	if len(pw) < MinPasswordLen {
		return fault.Newf(fault.BadRequest, "Passwords must be at least %d characters.", MinPasswordLen)
	}
	if len(pw) > 1024 {
		return fault.New(fault.BadRequest, "Passwords must be at most 1024 characters.")
	}
	return nil
}

// CreateUser creates a user with a hashed password.
func (s *Service) CreateUser(ctx context.Context, email, displayName, password string, platformAdmin bool) (admin.User, error) {
	email, err := NormalizeEmail(email)
	if err != nil {
		return admin.User{}, err
	}
	if err := ValidatePassword(password); err != nil {
		return admin.User{}, err
	}
	if strings.TrimSpace(displayName) == "" {
		displayName = email
	}
	h, err := s.hash(ctx, password)
	if err != nil {
		return admin.User{}, err
	}
	u := admin.User{ID: ids.New(), Email: email, DisplayName: strings.TrimSpace(displayName), PasswordHash: h,
		IsPlatformAdmin: platformAdmin, CreatedAt: s.now()}
	if err := s.repo.CreateUser(ctx, u); err != nil {
		if errors.Is(err, admin.ErrConflict) {
			return admin.User{}, fault.New(fault.DuplicateValue, "A user with this email already exists.")
		}
		return admin.User{}, err
	}
	return u, nil
}

// PrepareBootstrap issues a one-time setup token when no users exist and an
// admin email is configured. It returns the token ("" if not applicable).
func (s *Service) PrepareBootstrap(ctx context.Context, email string) (string, error) {
	if email == "" {
		return "", nil
	}
	email, err := NormalizeEmail(email)
	if err != nil {
		return "", err
	}
	n, err := s.repo.CountUsers(ctx)
	if err != nil || n > 0 {
		return "", err
	}
	s.setupMu.Lock()
	defer s.setupMu.Unlock()
	s.setupToken, s.setupEmail = ids.Token(24), email
	return s.setupToken, nil
}

// CompleteBootstrap consumes the setup token and creates the platform admin.
func (s *Service) CompleteBootstrap(ctx context.Context, token, displayName, password string) (admin.User, error) {
	s.setupMu.Lock()
	defer s.setupMu.Unlock()
	if s.setupToken == "" || subtle.ConstantTimeCompare([]byte(token), []byte(s.setupToken)) != 1 {
		return admin.User{}, fault.New(fault.Forbidden, "The setup token is invalid or has already been used.")
	}
	n, err := s.repo.CountUsers(ctx)
	if err != nil {
		return admin.User{}, err
	}
	if n > 0 {
		s.setupToken = ""
		return admin.User{}, fault.New(fault.Forbidden, "Setup has already been completed.")
	}
	u, err := s.CreateUser(ctx, s.setupEmail, displayName, password, true)
	if err != nil {
		return admin.User{}, err
	}
	s.setupToken = ""
	return u, nil
}

// BootstrapPending reports whether a setup token is outstanding.
func (s *Service) BootstrapPending() bool {
	s.setupMu.Lock()
	defer s.setupMu.Unlock()
	return s.setupToken != ""
}

func truncate(s string, n int) string {
	if len(s) > n {
		return s[:n]
	}
	return s
}
