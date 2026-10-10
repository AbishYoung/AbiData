// Package scripts stores Lua scripts (shared modules, form/report modules, job
// scripts) in _sys_scripts with optimistic versioning, and caches compiled code.
package scripts

import (
	"context"
	"regexp"
	"strings"
	"sync"
	"time"

	lua "github.com/yuin/gopher-lua"

	"abidata/internal/events"
	"abidata/internal/fault"
	"abidata/internal/ids"
	"abidata/internal/script"
	"abidata/internal/sqlitex"
	"abidata/internal/workspace"
)

// Script is a stored script.
type Script struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Kind      string    `json:"kind" enum:"module,form_module,report_module,job"`
	OwnerID   string    `json:"owner_id,omitempty"`
	Source    string    `json:"source"`
	Version   int64     `json:"version"`
	UpdatedAt time.Time `json:"updated_at"`
	UpdatedBy string    `json:"updated_by"`
}

// Input creates or replaces a script.
type ScriptInput struct {
	Name    string `json:"name" minLength:"1" maxLength:"200"`
	Kind    string `json:"kind" enum:"module,form_module,report_module,job"`
	OwnerID string `json:"owner_id,omitempty"`
	Source  string `json:"source" maxLength:"1000000"`
}

// MaxSource caps script size.
const MaxSource = 1 << 20

var moduleNameRe = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_.]{0,99}$`)

// Publisher receives design.changed events.
type Publisher interface {
	Publish(key, typ string, data any)
}

// Service manages scripts.
type Service struct {
	Events Publisher

	mu    sync.Mutex
	cache map[string]cached // key: workspace/script id
}

type cached struct {
	version int64
	proto   *lua.FunctionProto
}

const cols = `id, name, kind, owner_id, source, version, updated_at, updated_by`

func scan(rows *sqlitex.Rows) (Script, error) {
	var s Script
	var owner *string
	var at string
	if err := rows.Scan(&s.ID, &s.Name, &s.Kind, &owner, &s.Source, &s.Version, &at, &s.UpdatedBy); err != nil {
		return s, err
	}
	if owner != nil {
		s.OwnerID = *owner
	}
	s.UpdatedAt, _ = sqlitex.ParseTime(at)
	return s, nil
}

// List returns scripts (optionally of one kind), ordered by name.
func (svc *Service) List(ctx context.Context, l *workspace.Lease, kind string) ([]Script, error) {
	out := []Script{}
	err := l.Read(ctx, func(q sqlitex.Querier) error {
		sql := `SELECT ` + cols + ` FROM _sys_scripts`
		var args []any
		if kind != "" {
			sql += ` WHERE kind = ?`
			args = append(args, kind)
		}
		rows, err := q.Query(ctx, sql+` ORDER BY name COLLATE NOCASE`, args...)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			s, err := scan(rows)
			if err != nil {
				return err
			}
			out = append(out, s)
		}
		return rows.Err()
	})
	return out, err
}

func getQ(ctx context.Context, q sqlitex.Querier, where string, arg any) (Script, error) {
	rows, err := q.Query(ctx, `SELECT `+cols+` FROM _sys_scripts WHERE `+where, arg)
	if err != nil {
		return Script{}, err
	}
	defer rows.Close()
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return Script{}, err
		}
		return Script{}, fault.E(fault.NotFound)
	}
	return scan(rows)
}

// Get returns one script.
func (svc *Service) Get(ctx context.Context, l *workspace.Lease, id string) (Script, error) {
	var s Script
	err := l.Read(ctx, func(q sqlitex.Querier) error {
		var err error
		s, err = getQ(ctx, q, `id = ?`, id)
		return err
	})
	return s, err
}

// ByOwner returns the module script of a form or report, if any.
func ByOwner(ctx context.Context, q sqlitex.Querier, ownerID string) (Script, bool, error) {
	s, err := getQ(ctx, q, `owner_id = ?`, ownerID)
	if err != nil {
		if fault.Classify(err).Code == fault.NotFound {
			return Script{}, false, nil
		}
		return Script{}, false, err
	}
	return s, true, nil
}

func validate(in ScriptInput) error {
	if strings.TrimSpace(in.Name) == "" || len(in.Name) > 200 {
		return fault.New(fault.BadRequest, "Enter a script name of at most 200 characters.")
	}
	switch in.Kind {
	case "module":
		if !moduleNameRe.MatchString(in.Name) {
			return fault.New(fault.BadRequest, "Module names are used with require(); use letters, digits, '_' and '.' only.")
		}
	case "form_module", "report_module":
		if in.OwnerID == "" {
			return fault.New(fault.BadRequest, "Form and report modules belong to a form or report.")
		}
	case "job":
	default:
		return fault.Newf(fault.BadRequest, "Unknown script kind %q.", in.Kind)
	}
	if len(in.Source) > MaxSource {
		return fault.New(fault.BadRequest, "Scripts are limited to 1 MiB.")
	}
	return nil
}

// PutTx creates (id == "") or replaces a script inside a caller's transaction.
func (svc *Service) PutTx(ctx context.Context, tx *sqlitex.Tx, id string, expectedVersion int64, in ScriptInput, user string) (Script, error) {
	if err := validate(in); err != nil {
		return Script{}, err
	}
	s := Script{ID: id, Name: strings.TrimSpace(in.Name), Kind: in.Kind, OwnerID: in.OwnerID, Source: in.Source, UpdatedAt: time.Now(), UpdatedBy: user}
	if id == "" {
		s.ID, s.Version = ids.New(), 1
	} else {
		cur, err := getQ(ctx, tx, `id = ?`, id)
		if err != nil {
			return Script{}, err
		}
		if cur.Version != expectedVersion {
			return Script{}, fault.New(fault.ConcurrentModification, "This script was changed by someone else since you opened it. Nothing was saved.").
				WithDetails(map[string]any{"current_version": cur.Version})
		}
		s.Version = cur.Version + 1
		if s.OwnerID == "" {
			s.OwnerID = cur.OwnerID
		}
	}
	var owner any
	if s.OwnerID != "" {
		owner = s.OwnerID
	}
	_, err := tx.Exec(ctx, `INSERT INTO _sys_scripts(`+cols+`) VALUES (?,?,?,?,?,?,?,?)
		ON CONFLICT(id) DO UPDATE SET name = excluded.name, kind = excluded.kind, owner_id = excluded.owner_id,
		source = excluded.source, version = excluded.version, updated_at = excluded.updated_at, updated_by = excluded.updated_by`,
		s.ID, s.Name, s.Kind, owner, s.Source, s.Version, sqlitex.FormatTime(s.UpdatedAt), s.UpdatedBy)
	if err != nil {
		if fault.Classify(err).Code == fault.DuplicateValue {
			return Script{}, fault.Newf(fault.DuplicateValue, "A script named %q already exists.", s.Name)
		}
		return Script{}, err
	}
	return s, nil
}

// Put creates or replaces a script.
func (svc *Service) Put(ctx context.Context, l *workspace.Lease, id string, expectedVersion int64, in ScriptInput, user string) (Script, error) {
	var out Script
	err := l.Write(ctx, workspace.Interactive, func(tx *sqlitex.Tx) error {
		var err error
		out, err = svc.PutTx(ctx, tx, id, expectedVersion, in, user)
		return err
	})
	if err == nil && svc.Events != nil {
		svc.Events.Publish(string(l.ID()), events.DesignChanged, map[string]any{"kind": "script", "id": out.ID, "version": out.Version})
	}
	return out, err
}

// Delete removes a script (form/report modules are deleted with their owner).
func (svc *Service) Delete(ctx context.Context, l *workspace.Lease, id string, expectedVersion int64) error {
	err := l.Write(ctx, workspace.Interactive, func(tx *sqlitex.Tx) error {
		cur, err := getQ(ctx, tx, `id = ?`, id)
		if err != nil {
			return err
		}
		if cur.Version != expectedVersion {
			return fault.E(fault.ConcurrentModification)
		}
		var jobs int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM _sys_jobs WHERE script_id = ?`, id).Scan(&jobs); err != nil {
			return err
		}
		if jobs > 0 {
			return fault.New(fault.LinkedRecords, "Jobs use this script. Delete or change those jobs first. Nothing was deleted.")
		}
		_, err = tx.Exec(ctx, `DELETE FROM _sys_scripts WHERE id = ?`, id)
		return err
	})
	if err == nil && svc.Events != nil {
		svc.Events.Publish(string(l.ID()), events.DesignChanged, map[string]any{"kind": "script", "id": id, "deleted": true})
	}
	return err
}

// Proto returns the compiled prototype for s, cached by (workspace, id, version).
func (svc *Service) Proto(ws ids.WorkspaceID, s Script) (*lua.FunctionProto, error) {
	key := string(ws) + "/" + s.ID
	svc.mu.Lock()
	if svc.cache == nil {
		svc.cache = map[string]cached{}
	}
	if c, ok := svc.cache[key]; ok && c.version == s.Version {
		svc.mu.Unlock()
		return c.proto, nil
	}
	svc.mu.Unlock()
	p, err := script.Compile(s.Source, s.Name)
	if err != nil {
		return nil, err
	}
	svc.mu.Lock()
	svc.cache[key] = cached{version: s.Version, proto: p}
	svc.mu.Unlock()
	return p, nil
}

// Loader resolves require(name) to shared module scripts of workspace l.
func (svc *Service) Loader(ctx context.Context, l *workspace.Lease) script.ModuleLoader {
	return func(name string) (string, bool) {
		var src string
		err := l.Read(ctx, func(q sqlitex.Querier) error {
			return q.QueryRow(ctx, `SELECT source FROM _sys_scripts WHERE kind = 'module' AND name = ?`, name).Scan(&src)
		})
		return src, err == nil
	}
}
