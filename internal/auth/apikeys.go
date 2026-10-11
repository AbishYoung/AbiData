package auth

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"errors"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"abidata/internal/admin"
	"abidata/internal/fault"
	"abidata/internal/ids"
)

// API keys (ADR 0008). A token reads abd_<keyID>_<secret>: the key ID is a
// ULID that locates the row, the secret is 43 base62 characters (about 256
// bits from crypto/rand). Only HMAC-SHA256(session key, domain tag + secret)
// is stored, so a leaked admin DB yields no usable key.
const (
	APIKeyPrefix = "abd_"
	// ActorPrefix starts the synthetic user id an API key acts as: audit
	// entries, row history and created_by columns record "apikey:<keyID>".
	ActorPrefix = "apikey:"

	apiKeySecretLen = 43
	apiKeyTokenLen  = len(APIKeyPrefix) + 26 + 1 + apiKeySecretLen
	apiKeyDomain    = "abidata-api-key-v1\x00" //nolint:gosec // domain-separation tag, not a credential
	maxAPIKeyName   = 100

	// A failed bearer attempt counts against the client address: more than
	// KeyFailMax failures per KeyFailWindow get RATE_LIMITED without any lookup.
	KeyFailWindow = time.Minute
	KeyFailMax    = 30

	// DefaultKeyRatePerMinute is each key's request budget (token bucket with a
	// burst of one sixth of a minute's budget).
	DefaultKeyRatePerMinute = 600
	keyTouchInterval        = time.Minute
)

const base62 = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"

// ActorID is the synthetic user id of an API key.
func ActorID(keyID string) string { return ActorPrefix + keyID }

// IsAPIKeyActor reports whether a recorded user id belongs to an API key.
func IsAPIKeyActor(userID string) bool { return strings.HasPrefix(userID, ActorPrefix) }

// APIKeyDisplayName is how a key is named in history and audit views.
func APIKeyDisplayName(name string) string { return `API key "` + name + `"` }

func newAPIKeySecret() string {
	out := make([]byte, 0, apiKeySecretLen)
	buf := make([]byte, 64)
	for len(out) < apiKeySecretLen {
		if _, err := rand.Read(buf); err != nil {
			panic(err) // crypto/rand never fails on supported platforms
		}
		for _, b := range buf {
			// 248 = 4*62: reject the tail so every character is uniform.
			if b < 248 && len(out) < apiKeySecretLen {
				out = append(out, base62[b%62])
			}
		}
	}
	return string(out)
}

// ParseAPIKeyToken splits a token into its key ID and secret. It accepts only
// the exact shape that NewAPIKey produces.
func ParseAPIKeyToken(tok string) (keyID, secret string, ok bool) {
	if len(tok) != apiKeyTokenLen || !strings.HasPrefix(tok, APIKeyPrefix) {
		return "", "", false
	}
	rest := tok[len(APIKeyPrefix):]
	keyID, secret = rest[:26], rest[27:]
	if rest[26] != '_' || !ids.Valid(keyID) {
		return "", "", false
	}
	for i := 0; i < len(secret); i++ {
		if strings.IndexByte(base62, secret[i]) < 0 {
			return "", "", false
		}
	}
	return keyID, secret, true
}

// HashAPIKeySecret is the stored form of a key secret.
func (s *Service) HashAPIKeySecret(secret string) []byte {
	m := hmac.New(sha256.New, s.keys.Session)
	m.Write([]byte(apiKeyDomain))
	m.Write([]byte(secret))
	return m.Sum(nil)
}

// ValidateAPIKeyName trims and checks a key's label.
func ValidateAPIKeyName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" || utf8.RuneCountInString(name) > maxAPIKeyName {
		return "", fault.Newf(fault.BadRequest, "Enter a name of 1 to %d characters.", maxAPIKeyName)
	}
	for _, r := range name {
		if unicode.IsControl(r) || r == '"' || r == '\\' {
			return "", fault.New(fault.BadRequest, `The name cannot contain control characters, quotes or backslashes.`)
		}
	}
	return name, nil
}

// CreateAPIKey stores a new key for ws and returns it with the full token. The
// token is not recoverable afterwards. role must be viewer, editor or builder.
func (s *Service) CreateAPIKey(ctx context.Context, ws ids.WorkspaceID, name string, role admin.Role, createdBy string, expiresIn time.Duration) (admin.APIKey, string, error) {
	name, err := ValidateAPIKeyName(name)
	if err != nil {
		return admin.APIKey{}, "", err
	}
	if role != admin.RoleViewer && role != admin.RoleEditor && role != admin.RoleBuilder {
		return admin.APIKey{}, "", fault.New(fault.BadRequest, "An API key can have the viewer, editor or builder role.")
	}
	secret := newAPIKeySecret()
	now := s.now()
	k := admin.APIKey{ID: ids.New(), WorkspaceID: ws, Name: name, Role: role, SecretHash: s.HashAPIKeySecret(secret),
		CreatedBy: createdBy, CreatedAt: now}
	if expiresIn > 0 {
		e := now.Add(expiresIn)
		k.ExpiresAt = &e
	}
	if err := s.repo.CreateAPIKey(ctx, k); err != nil {
		if errors.Is(err, admin.ErrTooManyKeys) {
			return admin.APIKey{}, "", fault.Newf(fault.BadRequest, "This workspace already has %d active API keys. Revoke one first.", admin.MaxActiveAPIKeys)
		}
		return admin.APIKey{}, "", err
	}
	return k, APIKeyPrefix + k.ID + "_" + secret, nil
}

type keyBucket struct {
	tokens float64
	last   time.Time
}

// allowKey spends one token from the key's bucket.
func (s *Service) allowKey(id string, now time.Time) (bool, time.Duration) {
	rate := s.KeyRatePerMinute
	if rate <= 0 {
		rate = DefaultKeyRatePerMinute
	}
	capacity := float64(max(1, rate/6))
	perSec := float64(rate) / 60
	s.kmu.Lock()
	defer s.kmu.Unlock()
	if len(s.kbuckets) > 1024 {
		for k, b := range s.kbuckets {
			if now.Sub(b.last) > 10*time.Minute {
				delete(s.kbuckets, k)
			}
		}
	}
	b, ok := s.kbuckets[id]
	if !ok {
		b = &keyBucket{tokens: capacity, last: now}
		s.kbuckets[id] = b
	}
	if el := now.Sub(b.last).Seconds(); el > 0 {
		b.tokens = min(capacity, b.tokens+el*perSec)
		b.last = now
	}
	if b.tokens < 1 {
		return false, time.Duration((1 - b.tokens) / perSec * float64(time.Second))
	}
	b.tokens--
	return true, 0
}

// claimTouch reports whether last_used_at should be written now (at most once
// per keyTouchInterval per key, across requests and restarts).
func (s *Service) claimTouch(k admin.APIKey, now time.Time) bool {
	s.kmu.Lock()
	defer s.kmu.Unlock()
	if t, ok := s.ktouched[k.ID]; ok && now.Sub(t) < keyTouchInterval {
		return false
	}
	if k.LastUsedAt != nil && now.Sub(*k.LastUsedAt) < keyTouchInterval {
		s.ktouched[k.ID] = *k.LastUsedAt
		return false
	}
	if len(s.ktouched) > 1024 {
		for id, t := range s.ktouched {
			if now.Sub(t) > keyTouchInterval {
				delete(s.ktouched, id)
			}
		}
	}
	s.ktouched[k.ID] = now
	return true
}

// AuthenticateAPIKey resolves an Authorization header value ("Bearer abd_…").
// Every failure other than throttling returns the same UNAUTHENTICATED error,
// whichever part of the token was wrong; failures count against ip.
func (s *Service) AuthenticateAPIKey(ctx context.Context, authorization, ip string) (Principal, error) {
	now := s.now()
	failKey := "apikey-ip:" + limitKeyIP(ip)
	if a, err := s.repo.GetLoginAttempts(ctx, failKey); err != nil {
		return Principal{}, err
	} else if a.Count >= KeyFailMax && now.Before(a.WindowStart.Add(KeyFailWindow)) {
		return Principal{}, &fault.Error{Code: fault.RateLimited, RetryAfter: a.WindowStart.Add(KeyFailWindow).Sub(now),
			Message: "Too many failed attempts to use an API key. Wait before trying again."}
	}
	fail := func() (Principal, error) {
		if _, err := s.repo.RecordLoginFailure(ctx, failKey, now, KeyFailWindow); err != nil {
			s.logWarn("recording failed API key attempt", err)
		}
		return Principal{}, fault.E(fault.Unauthenticated)
	}

	scheme, tok, found := strings.Cut(authorization, " ")
	if !found || !strings.EqualFold(scheme, "Bearer") {
		return fail()
	}
	keyID, secret, ok := ParseAPIKeyToken(tok)
	if !ok {
		return fail()
	}
	k, err := s.repo.GetAPIKey(ctx, keyID)
	missing := errors.Is(err, admin.ErrNotFound)
	if err != nil && !missing {
		return Principal{}, err
	}
	// Hash and compare even when the key does not exist so lookups of real and
	// made-up key IDs take the same time.
	want := k.SecretHash
	if missing || len(want) != sha256.Size {
		want = make([]byte, sha256.Size)
		missing = true
	}
	match := subtle.ConstantTimeCompare(s.HashAPIKeySecret(secret), want) == 1
	if missing || !match || !k.Active(now) {
		return fail()
	}
	if ok, wait := s.allowKey(k.ID, now); !ok {
		return Principal{}, &fault.Error{Code: fault.RateLimited, RetryAfter: wait,
			Message: "This API key is sending requests too quickly. Slow down and retry."}
	}
	if s.claimTouch(k, now) {
		if err := s.repo.TouchAPIKey(context.WithoutCancel(ctx), k.ID, now); err != nil {
			s.logWarn("recording API key use", err)
		}
	}
	return Principal{UserID: ActorID(k.ID), DisplayName: APIKeyDisplayName(k.Name), APIKeyID: k.ID, APIKeyName: k.Name,
		KeyWorkspace: k.WorkspaceID, KeyRole: k.Role}, nil
}

func (s *Service) logWarn(msg string, err error) {
	if s.log != nil {
		s.log.Warn(msg, "err", err)
	}
}
