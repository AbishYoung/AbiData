// Package ids generates and validates server-side identifiers and builds
// filesystem paths from them without allowing traversal.
package ids

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/oklog/ulid/v2"
)

// WorkspaceID is a server-generated ULID naming one workspace and its file.
type WorkspaceID string

var ulidRe = regexp.MustCompile(`^[0-9A-HJKMNP-TV-Z]{26}$`)

// ErrInvalidID is returned for identifiers that are not canonical ULIDs.
var ErrInvalidID = errors.New("invalid identifier")

// New returns a new ULID string (monotonic within a millisecond is not required).
func New() string { return ulid.Make().String() }

// NewWorkspaceID returns a fresh workspace ID.
func NewWorkspaceID() WorkspaceID { return WorkspaceID(New()) }

// Valid reports whether s is a canonical upper-case ULID.
func Valid(s string) bool { return ulidRe.MatchString(s) }

// ParseWorkspaceID validates s.
func ParseWorkspaceID(s string) (WorkspaceID, error) {
	if !Valid(s) {
		return "", ErrInvalidID
	}
	return WorkspaceID(s), nil
}

func (id WorkspaceID) String() string { return string(id) }

// Token returns n random bytes encoded as unpadded base64url.
func Token(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic(err) // crypto/rand never fails on supported platforms
	}
	return base64.RawURLEncoding.EncodeToString(b)
}

// SafeJoin joins elems onto base and verifies the cleaned result stays inside base.
func SafeJoin(base string, elems ...string) (string, error) {
	absBase, err := filepath.Abs(base)
	if err != nil {
		return "", err
	}
	for _, e := range elems {
		if e == "" || strings.ContainsRune(e, 0) {
			return "", fmt.Errorf("%w: empty or NUL path element", ErrInvalidID)
		}
	}
	p := filepath.Join(append([]string{absBase}, elems...)...)
	rel, err := filepath.Rel(absBase, p)
	if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return "", fmt.Errorf("%w: path escapes base", ErrInvalidID)
	}
	return p, nil
}
