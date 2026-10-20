package auth

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"abidata/internal/admin"
	"abidata/internal/fault"
)

// Username rules (ADR 0017): 3 to 30 characters of a-z, 0-9 and single
// hyphens, starting with a letter and not ending in a hyphen. Usernames are
// public once a blog is published; login still uses the email.
const (
	MinUsernameLen = 3
	MaxUsernameLen = 30

	// UsernameChangeInterval is how often a username can change.
	UsernameChangeInterval = 30 * 24 * time.Hour
	// UsernameHold is how long an old username redirects and stays reserved.
	UsernameHold = 90 * 24 * time.Hour
)

// reservedUsernames are first path segments the server or SPA uses, or names
// that could pass for the platform. A test checks every router.tsx segment is here.
var reservedUsernames = map[string]bool{
	"a": true, "abidata": true, "admin": true, "api": true, "assets": true, "b": true, "blog": true,
	"docs": true, "healthz": true, "login": true, "logout": true, "messages": true, "metrics": true, "readyz": true,
	"root": true, "setup": true, "static": true, "support": true, "system": true, "w": true, "www": true,
}

// IsReservedUsername reports whether name cannot be taken by any user.
func IsReservedUsername(name string) bool {
	return reservedUsernames[name] || strings.HasPrefix(name, "xn--")
}

// NormalizeUsername lower-cases and validates a username.
func NormalizeUsername(name string) (string, error) {
	name = strings.ToLower(strings.TrimSpace(name))
	if msg := usernameProblem(name); msg != "" {
		return "", fault.New(fault.BadRequest, msg)
	}
	return name, nil
}

func usernameProblem(name string) string {
	if len(name) < MinUsernameLen || len(name) > MaxUsernameLen {
		return fmt.Sprintf("Usernames are %d to %d characters long.", MinUsernameLen, MaxUsernameLen)
	}
	if name[0] < 'a' || name[0] > 'z' {
		return "Usernames start with a letter."
	}
	for i := 0; i < len(name); i++ {
		c := name[i]
		switch {
		case c >= 'a' && c <= 'z', c >= '0' && c <= '9':
		case c == '-':
			if i == len(name)-1 || name[i+1] == '-' {
				return "Usernames cannot end with a hyphen or contain two in a row."
			}
		default:
			return "Usernames use only letters a-z, digits and hyphens."
		}
	}
	if IsReservedUsername(name) {
		return "This username is reserved."
	}
	return ""
}

// DeriveUsername builds a valid username from an email's local part: lower
// case, other characters turned into hyphens, padded to the minimum length,
// and given a numeric suffix while taken reports it in use.
func DeriveUsername(email string, taken func(string) (bool, error)) (string, error) {
	local, _, _ := strings.Cut(strings.ToLower(email), "@")
	var b strings.Builder
	for _, c := range local {
		switch {
		case c >= 'a' && c <= 'z', c >= '0' && c <= '9':
			b.WriteRune(c)
		case b.Len() > 0 && !strings.HasSuffix(b.String(), "-"):
			b.WriteByte('-')
		}
	}
	base := strings.TrimLeft(b.String(), "0123456789-")
	base = strings.TrimRight(base, "-")
	if base == "" {
		base = "user"
	}
	for len(base) < MinUsernameLen {
		base += "0"
	}
	if len(base) > MaxUsernameLen-4 {
		base = strings.TrimRight(base[:MaxUsernameLen-4], "-")
	}
	for i := 1; i < 10000; i++ {
		name := base
		if i > 1 || IsReservedUsername(base) {
			name = base + "-" + strconv.Itoa(i)
		}
		if usernameProblem(name) != "" {
			continue
		}
		t, err := taken(name)
		if err != nil {
			return "", err
		}
		if !t {
			return name, nil
		}
	}
	return "", fmt.Errorf("auth: no free username for %q", base)
}

func (s *Service) usernameTaken(ctx context.Context, name, exceptUser string) (bool, error) {
	owner, err := s.repo.UsernameOwner(ctx, name, s.now())
	if errors.Is(err, admin.ErrNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return owner != exceptUser, nil
}

// chooseUsername validates a requested username, or derives one from the email when it is empty.
func (s *Service) chooseUsername(ctx context.Context, requested, email string) (string, error) {
	if strings.TrimSpace(requested) == "" {
		return DeriveUsername(email, func(n string) (bool, error) { return s.usernameTaken(ctx, n, "") })
	}
	name, err := NormalizeUsername(requested)
	if err != nil {
		return "", err
	}
	if t, err := s.usernameTaken(ctx, name, ""); err != nil {
		return "", err
	} else if t {
		return "", errUsernameTaken
	}
	return name, nil
}

var errUsernameTaken = fault.New(fault.DuplicateValue, "This username is taken.")

// ChangeUsername renames a user. Users may change their own name once per
// UsernameChangeInterval; a platform admin (byAdmin) is not limited. The old
// name redirects and stays reserved for UsernameHold.
func (s *Service) ChangeUsername(ctx context.Context, userID, requested string, byAdmin bool) (admin.User, error) {
	u, err := s.repo.GetUser(ctx, userID)
	if err != nil {
		return admin.User{}, err
	}
	name, err := NormalizeUsername(requested)
	if err != nil {
		return admin.User{}, err
	}
	if name == u.Username {
		return u, nil
	}
	now := s.now()
	if !byAdmin && u.UsernameChangedAt != nil && now.Sub(*u.UsernameChangedAt) < UsernameChangeInterval {
		next := u.UsernameChangedAt.Add(UsernameChangeInterval)
		return admin.User{}, fault.Newf(fault.BadRequest, "You can change your username again on %s.", next.UTC().Format("2 January 2006"))
	}
	if t, err := s.usernameTaken(ctx, name, u.ID); err != nil {
		return admin.User{}, err
	} else if t {
		return admin.User{}, errUsernameTaken
	}
	if err := s.repo.SetUsername(ctx, u.ID, name, now, now.Add(UsernameHold)); err != nil {
		if errors.Is(err, admin.ErrConflict) {
			return admin.User{}, errUsernameTaken
		}
		return admin.User{}, err
	}
	old := u.Username
	u.Username, u.UsernameChangedAt = name, &now
	if s.log != nil {
		s.log.InfoContext(ctx, "username changed", "user_id", u.ID, "from", old, "to", name)
	}
	return u, nil
}

// BackfillUsernames gives every user without a username one derived from
// their email, in creation order so the result is stable. It runs at boot.
func (s *Service) BackfillUsernames(ctx context.Context) (int, error) {
	users, err := s.repo.ListUsersWithoutUsername(ctx)
	if err != nil {
		return 0, err
	}
	n := 0
	for _, u := range users {
		name, err := DeriveUsername(u.Email, func(c string) (bool, error) { return s.usernameTaken(ctx, c, "") })
		if err != nil {
			return n, err
		}
		if err := s.repo.InitUsername(ctx, u.ID, name); err != nil {
			return n, err
		}
		n++
	}
	return n, nil
}
