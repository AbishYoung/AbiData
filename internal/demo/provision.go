package demo

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"abidata/internal/admin"
)

// Provisioned reports what ProvisionAll did.
type Provisioned struct {
	Created []string         // emails of users who received a demo workspace
	Skipped []string         // emails of users who already own one
	Failed  []ProvisionError // users whose demo could not be built
}

// ProvisionError is one user's failure.
type ProvisionError struct {
	Email string
	Err   error
}

// IsDemoName reports whether a workspace name is the sample workspace's: the
// shared "Northwind Traders" or a personal "Northwind Traders (…)" copy.
func IsDemoName(name string) bool {
	return strings.EqualFold(name, WorkspaceName) || (len(name) > len(WorkspaceName)+3 &&
		strings.EqualFold(name[:len(WorkspaceName)+2], WorkspaceName+" (") && strings.HasSuffix(name, ")"))
}

// ProvisionAll gives every active user, administrators included, a personal
// demo workspace that they own, named "Northwind Traders (<display name>)".
// A user who already owns a demo workspace (shared or personal) is skipped,
// so running it again only fills in users added since. One user's failure
// does not stop the others.
func (b *Builder) ProvisionAll(ctx context.Context) (Provisioned, error) {
	var out Provisioned
	users, err := b.Admin.ListUsers(ctx)
	if err != nil {
		return out, err
	}
	for _, u := range users {
		if u.Disabled {
			continue
		}
		access, err := b.Admin.ListWorkspacesForUser(ctx, u.ID)
		if err != nil {
			return out, err
		}
		owns := false
		for _, a := range access {
			if a.Role == admin.RoleOwner && IsDemoName(a.Workspace.Name) {
				owns = true
				break
			}
		}
		if owns {
			out.Skipped = append(out.Skipped, u.Email)
			continue
		}
		if err := b.createPersonal(ctx, u); err != nil {
			out.Failed = append(out.Failed, ProvisionError{Email: u.Email, Err: err})
			continue
		}
		out.Created = append(out.Created, u.Email)
	}
	return out, nil
}

// createPersonal builds a user's copy, falling back to their email and then a
// numbered name when another workspace already has the name.
func (b *Builder) createPersonal(ctx context.Context, u admin.User) error {
	label := strings.TrimSpace(u.DisplayName)
	if label == "" {
		label = u.Email
	}
	candidates := []string{personalName(label), personalName(u.Email)}
	for i := 2; i <= 5; i++ {
		candidates = append(candidates, personalName(fmt.Sprintf("%s %d", label, i)))
	}
	var err error
	for _, name := range candidates {
		if _, err = b.createNamed(ctx, u.ID, name); !errors.Is(err, admin.ErrConflict) {
			return err
		}
	}
	return err
}

// personalName fits "Northwind Traders (label)" into a workspace name's 100 characters.
func personalName(label string) string {
	const maxName = 100
	room := maxName - utf8.RuneCountInString(WorkspaceName) - 3
	if r := []rune(label); len(r) > room {
		label = strings.TrimSpace(string(r[:room-1])) + "…"
	}
	return WorkspaceName + " (" + label + ")"
}
