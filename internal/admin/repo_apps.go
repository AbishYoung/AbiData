package admin

import (
	"context"
	"errors"
	"time"

	"abidata/internal/dbx"
	"abidata/internal/ids"
)

// ErrTooManyApps is returned when a workspace already has MaxAppsPerWorkspace apps.
var ErrTooManyApps = errors.New("admin: too many apps")

// Limits of public apps (ADR 0014).
const (
	MaxAppsPerWorkspace = 20
	MaxAppItems         = 30
)

// App is a row of _adm_apps with its items in menu order. Token is the secret
// link; TokenHash is its SHA-256, the lookup key.
type App struct {
	ID            string
	WorkspaceID   ids.WorkspaceID
	Name          string
	Token         string
	TokenHash     []byte
	StartItem     string
	Enabled       bool
	TakenDownAt   *time.Time
	TakenDownBy   string
	CreatedBy     string
	CreatedByName string // joined from _adm_users on reads; empty if the user is gone
	CreatedAt     time.Time
	UpdatedAt     time.Time
	Items         []AppItem
}

// AppItem is a form or report shown by an app.
type AppItem struct {
	ID          string
	Kind        string // "form" or "report"
	ObjectID    string
	Title       string
	Fingerprint string // the versions an owner approved
}

// Item returns the item with the given id.
func (a App) Item(id string) (AppItem, bool) {
	for _, it := range a.Items {
		if it.ID == id {
			return it, true
		}
	}
	return AppItem{}, false
}

const appCols = `a.id, a.workspace_id, a.name, a.token, a.token_hash, a.start_item, a.enabled, a.taken_down_at, COALESCE(a.taken_down_by, ''),
	a.created_by, COALESCE(u.display_name, ''), a.created_at, a.updated_at`
const appFrom = ` FROM _adm_apps a LEFT JOIN _adm_users u ON u.id = a.created_by `

func scanApp(r scanner) (App, error) {
	var a App
	var ws, created, updated string
	var down *string
	if err := r.Scan(&a.ID, &ws, &a.Name, &a.Token, &a.TokenHash, &a.StartItem, &a.Enabled, &down, &a.TakenDownBy,
		&a.CreatedBy, &a.CreatedByName, &created, &updated); err != nil {
		return App{}, err
	}
	a.WorkspaceID = ids.WorkspaceID(ws)
	var err error
	if a.CreatedAt, err = parseTime(created); err != nil {
		return App{}, err
	}
	if a.UpdatedAt, err = parseTime(updated); err != nil {
		return App{}, err
	}
	if down != nil {
		t, err := parseTime(*down)
		if err != nil {
			return App{}, err
		}
		a.TakenDownAt = &t
	}
	return a, nil
}

func loadAppItems(ctx context.Context, c dbx.Querier, apps []App) error {
	for i := range apps {
		a := &apps[i]
		rows, err := c.Query(ctx, sqlLoadAppItems.SQL, a.ID)
		if err != nil {
			return err
		}
		items := []AppItem{}
		for rows.Next() {
			var it AppItem
			if err := rows.Scan(&it.ID, &it.Kind, &it.ObjectID, &it.Title, &it.Fingerprint); err != nil {
				rows.Close()
				return err
			}
			items = append(items, it)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return err
		}
		a.Items = items
	}
	return nil
}

func writeAppItems(ctx context.Context, tx dbx.Tx, a App) error {
	if len(a.Items) > MaxAppItems {
		return errors.New("admin: too many app items")
	}
	if _, err := tx.Exec(ctx, sqlWriteAppItems.SQL, a.ID); err != nil {
		return err
	}
	for i, it := range a.Items {
		if _, err := tx.Exec(ctx, sqlWriteAppItems2.SQL,
			a.ID, it.ID, it.Kind, it.ObjectID, it.Title, i, it.Fingerprint); err != nil {
			return wrapWrite(err, "app item")
		}
	}
	return nil
}

// CreateApp inserts a with its items, refusing with ErrTooManyApps when the
// workspace already has MaxAppsPerWorkspace apps (counted in the same transaction).
func (s *Store) CreateApp(ctx context.Context, a App) error {
	return s.Write(ctx, func(tx dbx.Tx) error {
		var n int
		if err := tx.QueryRow(ctx, sqlCreateApp.SQL, string(a.WorkspaceID)).Scan(&n); err != nil {
			return err
		}
		if n >= MaxAppsPerWorkspace {
			return ErrTooManyApps
		}
		_, err := tx.Exec(ctx, sqlCreateApp2.SQL, a.ID, string(a.WorkspaceID), a.Name, a.Token, a.TokenHash, a.StartItem, a.Enabled, a.CreatedBy,
			dbx.FormatTime(a.CreatedAt), dbx.FormatTime(a.UpdatedAt))
		if err != nil {
			return wrapWrite(err, "create app")
		}
		return writeAppItems(ctx, tx, a)
	})
}

func (s *Store) getApp(ctx context.Context, where string, arg any) (App, error) {
	var out App
	err := s.Read(ctx, func(c dbx.Querier) error {
		a, err := scanApp(c.QueryRow(ctx, `SELECT `+appCols+appFrom+where, arg))
		if err != nil {
			return err
		}
		apps := []App{a}
		if err := loadAppItems(ctx, c, apps); err != nil {
			return err
		}
		out = apps[0]
		return nil
	})
	if err != nil {
		return App{}, notFoundIfNoRows(err, "app")
	}
	return out, nil
}

// GetApp returns one app with its items.
func (s *Store) GetApp(ctx context.Context, id string) (App, error) {
	return s.getApp(ctx, `WHERE a.id = ?`, id)
}

// GetAppByTokenHash returns the app whose link token hashes to h.
func (s *Store) GetAppByTokenHash(ctx context.Context, h []byte) (App, error) {
	return s.getApp(ctx, `WHERE a.token_hash = ?`, h)
}

// ListApps returns a workspace's apps by name; "" lists every workspace's apps.
func (s *Store) ListApps(ctx context.Context, ws ids.WorkspaceID) ([]App, error) {
	out := []App{}
	err := s.Read(ctx, func(c dbx.Querier) error {
		rows, err := c.Query(ctx, `SELECT `+appCols+appFrom+`WHERE ?1 = '' OR a.workspace_id = ?1 ORDER BY a.name, a.id LIMIT 1000`, string(ws))
		if err != nil {
			return err
		}
		for rows.Next() {
			a, err := scanApp(rows)
			if err != nil {
				rows.Close()
				return err
			}
			out = append(out, a)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return err
		}
		return loadAppItems(ctx, c, out)
	})
	return out, err
}

// UpdateApp stores the owner's settings: name, start item, enabled and the
// items. It never touches the token or an administrator's take-down, so an
// owner's edit cannot undo either.
func (s *Store) UpdateApp(ctx context.Context, a App) error {
	return s.Write(ctx, func(tx dbx.Tx) error {
		res, err := tx.Exec(ctx, sqlUpdateApp.SQL,
			a.Name, a.StartItem, a.Enabled, dbx.FormatTime(a.UpdatedAt), a.ID)
		if err != nil {
			return wrapWrite(err, "update app")
		}
		if err := requireChanged(res, "app"); err != nil {
			return err
		}
		return writeAppItems(ctx, tx, a)
	})
}

// SetAppToken replaces an app's link; the old one stops working at once.
func (s *Store) SetAppToken(ctx context.Context, id, token string, hash []byte, at time.Time) error {
	return s.Write(ctx, func(tx dbx.Tx) error {
		res, err := tx.Exec(ctx, sqlSetAppToken.SQL, token, hash, dbx.FormatTime(at), id)
		if err != nil {
			return wrapWrite(err, "rotate app link")
		}
		return requireChanged(res, "app")
	})
}

// SetAppTakenDown records an administrator's take-down (at != nil) or lifts it.
func (s *Store) SetAppTakenDown(ctx context.Context, id string, at *time.Time, by string) error {
	return s.Write(ctx, func(tx dbx.Tx) error {
		var when, who any
		if at != nil {
			when, who = dbx.FormatTime(*at), by
		}
		res, err := tx.Exec(ctx, sqlSetAppTakenDown.SQL, when, who, id)
		if err != nil {
			return err
		}
		return requireChanged(res, "app")
	})
}

// DeleteApp removes an app and its items.
func (s *Store) DeleteApp(ctx context.Context, id string) error {
	return s.Write(ctx, func(tx dbx.Tx) error {
		res, err := tx.Exec(ctx, sqlDeleteApp.SQL, id)
		if err != nil {
			return err
		}
		return requireChanged(res, "app")
	})
}
