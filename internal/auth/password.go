// Package auth implements password hashing, server-side sessions, login rate
// limiting, key management and the first-admin bootstrap (D15).
package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"

	"golang.org/x/crypto/argon2"
)

// Argon2Params are the argon2id cost parameters.
type Argon2Params struct {
	Memory  uint32 // KiB
	Time    uint32
	Threads uint8
	KeyLen  uint32
	SaltLen int
}

// DefaultArgon2 is the brief's m=64MiB, t=3, p=2.
var DefaultArgon2 = Argon2Params{Memory: 64 * 1024, Time: 3, Threads: 2, KeyLen: 32, SaltLen: 16}

// ErrBadHash is returned for malformed stored hashes.
var ErrBadHash = errors.New("auth: malformed password hash")

// MinPasswordLen is the minimum accepted password length.
const MinPasswordLen = 10

// HashPassword returns a PHC-format argon2id hash.
func HashPassword(password string, p Argon2Params) (string, error) {
	salt := make([]byte, p.SaltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	key := argon2.IDKey([]byte(password), salt, p.Time, p.Memory, p.Threads, p.KeyLen)
	enc := base64.RawStdEncoding
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s", argon2.Version, p.Memory, p.Time, p.Threads,
		enc.EncodeToString(salt), enc.EncodeToString(key)), nil
}

func parseHash(hash string) (Argon2Params, []byte, []byte, error) {
	parts := strings.Split(hash, "$")
	if len(parts) != 6 || parts[1] != "argon2id" {
		return Argon2Params{}, nil, nil, ErrBadHash
	}
	var version int
	if _, err := fmt.Sscanf(parts[2], "v=%d", &version); err != nil || version != argon2.Version {
		return Argon2Params{}, nil, nil, ErrBadHash
	}
	var p Argon2Params
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &p.Memory, &p.Time, &p.Threads); err != nil {
		return Argon2Params{}, nil, nil, ErrBadHash
	}
	enc := base64.RawStdEncoding
	salt, err := enc.DecodeString(parts[4])
	if err != nil {
		return Argon2Params{}, nil, nil, ErrBadHash
	}
	key, err := enc.DecodeString(parts[5])
	if err != nil || len(key) == 0 {
		return Argon2Params{}, nil, nil, ErrBadHash
	}
	p.KeyLen = uint32(len(key))
	p.SaltLen = len(salt)
	return p, salt, key, nil
}

// VerifyPassword reports whether password matches hash, in constant time.
func VerifyPassword(password, hash string) (bool, error) {
	p, salt, key, err := parseHash(hash)
	if err != nil {
		return false, err
	}
	got := argon2.IDKey([]byte(password), salt, p.Time, p.Memory, p.Threads, p.KeyLen)
	return subtle.ConstantTimeCompare(got, key) == 1, nil
}

// NeedsRehash reports whether hash was made with parameters other than want.
func NeedsRehash(hash string, want Argon2Params) bool {
	p, _, _, err := parseHash(hash)
	if err != nil {
		return true
	}
	return p.Memory != want.Memory || p.Time != want.Time || p.Threads != want.Threads || p.KeyLen != want.KeyLen
}
