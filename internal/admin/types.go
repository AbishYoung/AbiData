package admin

import (
	"context"
	"errors"
	"time"

	"abidata/internal/ids"
)

// ErrNotFound is returned when a looked-up row does not exist.
var ErrNotFound = errors.New("admin: not found")

// ErrConflict is returned when a unique constraint (e.g. email, workspace name) is violated.
var ErrConflict = errors.New("admin: conflict")

// Role is a workspace role. Order: owner > builder > editor > viewer.
type Role string

const (
	RoleOwner   Role = "owner"
	RoleBuilder Role = "builder"
	RoleEditor  Role = "editor"
	RoleViewer  Role = "viewer"
)

// Rank orders roles; an unknown role ranks 0 (below viewer).
func (r Role) Rank() int {
	switch r {
	case RoleOwner:
		return 4
	case RoleBuilder:
		return 3
	case RoleEditor:
		return 2
	case RoleViewer:
		return 1
	}
	return 0
}

// AtLeast reports whether r grants at least min.
func (r Role) AtLeast(min Role) bool { return r.Rank() >= min.Rank() && r.Rank() > 0 }

// Valid reports whether r is one of the four roles.
func (r Role) Valid() bool { return r.Rank() > 0 }

// WorkspaceStatus is the lifecycle state of a workspace.
type WorkspaceStatus string

const (
	StatusActive      WorkspaceStatus = "active"
	StatusSuspended   WorkspaceStatus = "suspended"
	StatusRestoring   WorkspaceStatus = "restoring"
	StatusNeedsRepair WorkspaceStatus = "needs_repair"
	StatusDeleted     WorkspaceStatus = "deleted"
)

// User is a platform account.
type User struct {
	ID              string
	Email           string
	DisplayName     string
	PasswordHash    string
	IsPlatformAdmin bool
	Disabled        bool
	CreatedAt       time.Time
	LastLoginAt     *time.Time
}

// UserPatch updates selected user fields; nil fields are unchanged.
type UserPatch struct {
	DisplayName     *string
	IsPlatformAdmin *bool
	Disabled        *bool
}

// Session is a server-side login session. IDHash is hex SHA-256 of the cookie value.
type Session struct {
	IDHash     string
	UserID     string
	CSRFToken  string
	CreatedAt  time.Time
	LastSeenAt time.Time
	ExpiresAt  time.Time // absolute expiry
	IP         string
	UserAgent  string
}

// Quota holds per-workspace limits. Zero means "use the platform default".
type Quota struct {
	MaxPageCount       int64 `json:"max_page_count,omitempty"`
	MaxConcurrentJobs  int   `json:"max_concurrent_jobs,omitempty"`
	EgressPerMinute    int   `json:"egress_per_minute,omitempty"`
	ArtifactBytes      int64 `json:"artifact_bytes,omitempty"`
	UIEventBudgetMS    int   `json:"ui_event_budget_ms,omitempty"`
	ScriptConcurrency  int   `json:"script_concurrency,omitempty"`
}

// Workspace is the admin-side record of a workspace.
type Workspace struct {
	ID        ids.WorkspaceID
	Name      string
	Status    WorkspaceStatus
	Quota     Quota
	CreatedAt time.Time
	CreatedBy string
}

// WorkspacePatch updates selected workspace fields; nil fields are unchanged.
type WorkspacePatch struct {
	Name   *string
	Status *WorkspaceStatus
	Quota  *Quota
}

// Membership grants a user a role in a workspace.
type Membership struct {
	WorkspaceID ids.WorkspaceID
	UserID      string
	Role        Role
}

// Member is a membership joined with the user's identity.
type Member struct {
	UserID      string
	Email       string
	DisplayName string
	Role        Role
}

// WorkspaceAccess is a workspace visible to a user, with that user's role.
type WorkspaceAccess struct {
	Workspace Workspace
	Role      Role
}

// LoginAttempts is a fixed-window failure counter.
type LoginAttempts struct {
	Key         string
	Count       int
	WindowStart time.Time
}

// AuditEntry is a platform audit record.
type AuditEntry struct {
	At         time.Time
	UserID     string // empty for system actions
	Action     string
	ObjectKind string
	ObjectID   string
	Details    map[string]any
}

// Repository is the admin data API. *Store implements it.
type Repository interface {
	// Users
	CreateUser(ctx context.Context, u User) error // ErrConflict on duplicate email
	GetUser(ctx context.Context, id string) (User, error)
	GetUserByEmail(ctx context.Context, email string) (User, error) // case-insensitive
	ListUsers(ctx context.Context) ([]User, error)                    // ordered by email
	CountUsers(ctx context.Context) (int, error)
	UpdateUser(ctx context.Context, id string, p UserPatch) error
	SetPasswordHash(ctx context.Context, id, hash string) error
	TouchLogin(ctx context.Context, id string, at time.Time) error

	// Sessions
	CreateSession(ctx context.Context, s Session) error
	GetSession(ctx context.Context, idHash string) (Session, error)
	TouchSession(ctx context.Context, idHash string, lastSeen time.Time) error
	DeleteSession(ctx context.Context, idHash string) error
	DeleteUserSessions(ctx context.Context, userID string) error
	PurgeSessions(ctx context.Context, now time.Time, idle time.Duration) (int64, error) // expired or idle

	// Login rate limiting (fixed window)
	GetLoginAttempts(ctx context.Context, key string) (LoginAttempts, error) // zero value + nil if absent
	// RecordLoginFailure increments the counter for key, starting a new window at now
	// if the existing one began before now-window. Returns the updated record.
	RecordLoginFailure(ctx context.Context, key string, now time.Time, window time.Duration) (LoginAttempts, error)
	ClearLoginAttempts(ctx context.Context, key string) error

	// Workspaces
	CreateWorkspace(ctx context.Context, w Workspace, owner string) error // also inserts owner membership; ErrConflict on duplicate name
	GetWorkspace(ctx context.Context, id ids.WorkspaceID) (Workspace, error)
	ListWorkspaces(ctx context.Context) ([]Workspace, error)                          // excludes deleted; ordered by name
	ListWorkspacesForUser(ctx context.Context, userID string) ([]WorkspaceAccess, error) // excludes deleted; ordered by name
	UpdateWorkspace(ctx context.Context, id ids.WorkspaceID, p WorkspacePatch) error

	// Memberships
	GetRole(ctx context.Context, ws ids.WorkspaceID, userID string) (Role, error) // ErrNotFound if not a member
	SetMembership(ctx context.Context, m Membership) error                      // upsert
	DeleteMembership(ctx context.Context, ws ids.WorkspaceID, userID string) error
	ListMembers(ctx context.Context, ws ids.WorkspaceID) ([]Member, error) // ordered by email
	CountOwners(ctx context.Context, ws ids.WorkspaceID) (int, error)

	// Settings (values are JSON text)
	GetSetting(ctx context.Context, key string) (string, error) // ErrNotFound if absent
	PutSetting(ctx context.Context, key, value string) error
	ListSettings(ctx context.Context) (map[string]string, error)

	// Audit
	Audit(ctx context.Context, e AuditEntry) error
	ListAudit(ctx context.Context, limit int) ([]AuditEntry, error) // newest first
}
