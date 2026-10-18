package httpapi

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"abidata/internal/admin"
	"abidata/internal/apps"
	"abidata/internal/auth"
	"abidata/internal/catalog"
	"abidata/internal/fault"
	"abidata/internal/forms"
	"abidata/internal/ids"
	"abidata/internal/obs"
	"abidata/internal/query"
	"abidata/internal/records"
	"abidata/internal/reports"
	"abidata/internal/workspace"
)

// Public apps (ADR 0014): forms and reports behind a secret link.

// settingPublicApps is the admin setting that switches public apps on or off ("true"/"false").
const settingPublicApps = "public_apps"

// maxLookupOptions caps the choices a public combo box offers.
const maxLookupOptions = 1000

// publicAppsOn reports whether public apps are allowed on this install: the
// environment can forbid them for good, the administrator setting turns them off.
func (s *Server) publicAppsOn(ctx context.Context) (on, forced bool) {
	if !s.deps.Config.PublicApps {
		return false, true
	}
	v, err := s.deps.Admin.GetSetting(ctx, settingPublicApps)
	if errors.Is(err, admin.ErrNotFound) {
		return true, false
	} else if err != nil {
		return false, false // fail closed
	}
	return v != "false", false
}

func appFrom(ctx context.Context) (admin.App, bool) {
	a, ok := ctx.Value(keyApp).(admin.App)
	return a, ok
}

// authenticateApp resolves the link token of an app route. Every reason a link
// does not work (malformed, unknown, rotated, off, taken down, feature off,
// workspace suspended or deleted) is the same NOT_FOUND.
func (s *Server) authenticateApp(hctx huma.Context, next func(huma.Context)) {
	hctx.SetHeader("Referrer-Policy", "no-referrer")
	hctx.SetHeader("X-Robots-Tag", "noindex, nofollow")
	hctx.SetHeader("Cache-Control", "no-store")
	if isUnsafe(hctx.Method()) && !s.deps.Config.OriginAllowed(hctx.Header("Origin")) {
		writeHumaError(hctx, s.log, fault.New(fault.Forbidden, "The request origin is not allowed."))
		return
	}
	ctx := hctx.Context()
	a, m, err := s.resolveApp(ctx, hctx.Param("token"))
	if err != nil {
		writeHumaError(hctx, s.log, err)
		return
	}
	if ok, wait := s.appLimits.Allow("read:"+a.ID+":"+clientIP(ctx), apps.Reads); !ok {
		writeHumaError(hctx, s.log, &fault.Error{Code: fault.RateLimited, RetryAfter: wait})
		return
	}
	p := auth.Principal{UserID: apps.ActorID(a.ID), DisplayName: apps.DisplayName(a.Name)}
	ctx = obs.WithWorkspace(obs.WithUser(ctx, p.UserID), string(a.WorkspaceID))
	ctx = context.WithValue(context.WithValue(context.WithValue(ctx, keyPrincipal, p), keyMembership, m), keyApp, a)
	next(huma.WithContext(hctx, ctx))
}

func (s *Server) resolveApp(ctx context.Context, token string) (admin.App, Membership, error) {
	gone := fault.E(fault.NotFound)
	if on, _ := s.publicAppsOn(ctx); !on || !apps.ValidToken(token) {
		return admin.App{}, Membership{}, gone
	}
	a, err := s.deps.Admin.GetAppByTokenHash(ctx, apps.HashToken(token))
	if errors.Is(err, admin.ErrNotFound) {
		return admin.App{}, Membership{}, gone
	} else if err != nil {
		return admin.App{}, Membership{}, err
	}
	if !a.Enabled || a.TakenDownAt != nil {
		return admin.App{}, Membership{}, gone
	}
	// Visitors act with the editor role: enough to submit through the form
	// lifecycle, and every route below narrows what they can reach.
	m, err := s.openWorkspace(ctx, a.WorkspaceID, admin.RoleEditor)
	if err != nil {
		if fault.Classify(err).Code == fault.WorkspaceMaintenance {
			return admin.App{}, Membership{}, err
		}
		return admin.App{}, Membership{}, gone
	}
	return a, m, nil
}

// --- approval ---------------------------------------------------------------

// itemState is an item checked against the workspace: its current fingerprint,
// or why it cannot be shown.
type itemState struct {
	fingerprint string
	form        forms.Form
	report      reports.Report
	problem     error // the object is gone or no longer publishable
}

func (s *Server) queryVersions(ctx context.Context, l *workspace.Lease, idsList []string) (map[string]int64, error) {
	out := map[string]int64{}
	for _, id := range idsList {
		q, err := s.deps.Queries.Get(ctx, l, id)
		if err != nil {
			return nil, err
		}
		out[id] = q.Version
	}
	return out, nil
}

// inspect loads an item's object and works out its fingerprint and problems.
func (s *Server) inspect(ctx context.Context, l *workspace.Lease, kind, objectID string) itemState {
	switch kind {
	case "form":
		f, err := s.deps.Forms.Get(ctx, l, objectID)
		if err != nil {
			return itemState{problem: fault.Newf(fault.BadRequest, "The form no longer exists.")}
		}
		if err := apps.CheckForm(f); err != nil {
			return itemState{form: f, problem: err}
		}
		qs, err := s.queryVersions(ctx, l, apps.FormQueries(f))
		if err != nil {
			return itemState{form: f, problem: fault.Newf(fault.BadRequest, "A query the form %q uses no longer exists.", f.Name)}
		}
		return itemState{form: f, fingerprint: apps.Fingerprint("form", f.ID, f.Version, f.ModuleVersion, qs)}
	case "report":
		r, err := s.deps.Reports.Get(ctx, l, objectID)
		if err != nil {
			return itemState{problem: fault.Newf(fault.BadRequest, "The report no longer exists.")}
		}
		if err := apps.CheckReport(r); err != nil {
			return itemState{report: r, problem: err}
		}
		qs, err := s.queryVersions(ctx, l, apps.ReportQueries(r))
		if err != nil {
			return itemState{report: r, problem: fault.Newf(fault.BadRequest, "The query behind the report %q no longer exists.", r.Name)}
		}
		return itemState{report: r, fingerprint: apps.Fingerprint("report", r.ID, r.Version, r.ModuleVersion, qs)}
	}
	return itemState{problem: fault.Newf(fault.BadRequest, "Unknown item kind %q.", kind)}
}

// publicItem returns an item of the calling app, refusing it when its kind is
// wrong, it can no longer be published, or it changed since an owner approved it.
func (s *Server) publicItem(ctx context.Context, l *workspace.Lease, kind, itemID string) (itemState, error) {
	a, _ := appFrom(ctx)
	it, ok := a.Item(itemID)
	if !ok || it.Kind != kind {
		return itemState{}, fault.E(fault.NotFound)
	}
	st := s.inspect(ctx, l, it.Kind, it.ObjectID)
	if st.problem != nil || st.fingerprint != it.Fingerprint {
		return itemState{}, fault.E(fault.AppItemChanged)
	}
	return st, nil
}

// --- shapes -------------------------------------------------------------------

// AppItemOut is an item as its owner sees it.
type AppItemOut struct {
	ID       string `json:"id"`
	Kind     string `json:"kind" enum:"form,report"`
	ObjectID string `json:"object_id"`
	Title    string `json:"title"`
	Status   string `json:"status" enum:"live,changed,broken" doc:"live: visitors see it; changed: edited since approval and waiting for an owner; broken: cannot be published (see problem)."`
	Problem  string `json:"problem,omitempty"`
}

// AppOut is an app as its owner (or a platform administrator) sees it.
type AppOut struct {
	ID            string       `json:"id"`
	WorkspaceID   string       `json:"workspace_id"`
	WorkspaceName string       `json:"workspace_name,omitempty"`
	Name          string       `json:"name"`
	Link          string       `json:"link,omitempty" doc:"The secret link. Shown to owners only."`
	Enabled       bool         `json:"enabled"`
	TakenDown     bool         `json:"taken_down" doc:"A platform administrator took the app down; only they can restore it."`
	StartItem     string       `json:"start_item"`
	Items         []AppItemOut `json:"items"`
	CreatedBy     string       `json:"created_by_name"`
	CreatedAt     time.Time    `json:"created_at"`
	UpdatedAt     time.Time    `json:"updated_at"`
}

// AppItemInput names an item of an app. Items with an id keep their approval;
// new items are approved as they are now.
type AppItemInput struct {
	ID       string `json:"id,omitempty"`
	Kind     string `json:"kind" enum:"form,report"`
	ObjectID string `json:"object_id" minLength:"1" maxLength:"64"`
	Title    string `json:"title" minLength:"1" maxLength:"100"`
}

// AppInput creates or replaces an app's settings.
type AppInput struct {
	Name      string         `json:"name" minLength:"1" maxLength:"100"`
	Enabled   *bool          `json:"enabled,omitempty"`
	StartItem string         `json:"start_item,omitempty" doc:"Item id of the first page; empty: the first item."`
	Items     []AppItemInput `json:"items" maxItems:"30"`
}

func (s *Server) appLink(token string) string {
	return strings.TrimRight(s.deps.Config.PublicURL, "/") + "/a/" + token
}

func (s *Server) appOut(ctx context.Context, l *workspace.Lease, a admin.App, withLink bool) AppOut {
	out := AppOut{ID: a.ID, WorkspaceID: string(a.WorkspaceID), Name: a.Name, Enabled: a.Enabled, TakenDown: a.TakenDownAt != nil,
		StartItem: a.StartItem, Items: []AppItemOut{}, CreatedBy: a.CreatedByName, CreatedAt: a.CreatedAt, UpdatedAt: a.UpdatedAt}
	if withLink {
		out.Link = s.appLink(a.Token)
	}
	for _, it := range a.Items {
		o := AppItemOut{ID: it.ID, Kind: it.Kind, ObjectID: it.ObjectID, Title: it.Title, Status: "live"}
		if l != nil {
			st := s.inspect(ctx, l, it.Kind, it.ObjectID)
			switch {
			case st.problem != nil:
				o.Status, o.Problem = "broken", fault.Classify(st.problem).DisplayMessage()
			case st.fingerprint != it.Fingerprint:
				o.Status = "changed"
			}
		}
		out.Items = append(out.Items, o)
	}
	return out
}

// buildItems checks the requested items against the workspace. Items the app
// already has keep their approved fingerprint; new ones are approved now.
func (s *Server) buildItems(ctx context.Context, l *workspace.Lease, prev admin.App, in []AppItemInput) ([]admin.AppItem, error) {
	if len(in) > admin.MaxAppItems {
		return nil, fault.Newf(fault.BadRequest, "An app can show at most %d forms and reports.", admin.MaxAppItems)
	}
	out := make([]admin.AppItem, 0, len(in))
	seen := map[string]bool{}
	for _, x := range in {
		if old, ok := prev.Item(x.ID); ok && x.ID != "" {
			if old.Kind != x.Kind || old.ObjectID != x.ObjectID {
				return nil, fault.New(fault.BadRequest, "An item cannot change what it shows. Remove it and add a new one.")
			}
			old.Title = x.Title
			if seen[old.ID] {
				return nil, fault.New(fault.BadRequest, "An item is listed twice.")
			}
			seen[old.ID] = true
			out = append(out, old)
			continue
		}
		st := s.inspect(ctx, l, x.Kind, x.ObjectID)
		if st.problem != nil {
			return nil, st.problem
		}
		out = append(out, admin.AppItem{ID: ids.New(), Kind: x.Kind, ObjectID: x.ObjectID, Title: x.Title, Fingerprint: st.fingerprint})
	}
	return out, nil
}

func startItem(items []admin.AppItem, want string) (string, error) {
	if want == "" {
		if len(items) > 0 {
			return items[0].ID, nil
		}
		return "", nil
	}
	for _, it := range items {
		if it.ID == want {
			return want, nil
		}
	}
	return "", fault.New(fault.BadRequest, "The start page must be one of the app's items.")
}

func appFault(err error) error {
	switch {
	case errors.Is(err, admin.ErrTooManyApps):
		return fault.Newf(fault.BadRequest, "This workspace already has %d apps. Delete one first.", admin.MaxAppsPerWorkspace)
	case errors.Is(err, admin.ErrNotFound):
		return fault.E(fault.NotFound)
	}
	return err
}

// ownedApp returns the app id of the caller's workspace, or NOT_FOUND.
func (s *Server) ownedApp(ctx context.Context, id string) (admin.App, error) {
	m, _ := MembershipFrom(ctx)
	a, err := s.deps.Admin.GetApp(ctx, id)
	if err != nil || a.WorkspaceID != m.Workspace.ID {
		return admin.App{}, fault.E(fault.NotFound)
	}
	return a, nil
}

func (s *Server) registerApps(api huma.API) {
	s.registerPublicApps(api)
	s.registerOwnerApps(api)
	s.registerAdminApps(api)
}

// --- owner routes -------------------------------------------------------------

func (s *Server) registerOwnerApps(api huma.API) {
	owner := map[string]any{metaRole: string(admin.RoleOwner), metaSessionOnly: true}
	tags := []string{"apps"}
	type appOne struct{ Body AppOut }
	type appList struct {
		Body struct {
			Items   []AppOut `json:"items"`
			Enabled bool     `json:"enabled" doc:"Whether public apps are allowed on this install."`
		}
	}

	huma.Register(api, huma.Operation{OperationID: "listApps", Method: http.MethodGet, Path: "/api/v1/w/{ws}/apps", Tags: tags, Metadata: owner,
		Summary: "The workspace's public apps, with their links and which items wait for approval", Errors: []int{401, 403, 404}},
		func(ctx context.Context, _ *struct {
			WS string `path:"ws"`
		}) (*appList, error) {
			m, _ := MembershipFrom(ctx)
			as, err := s.deps.Admin.ListApps(ctx, m.Workspace.ID)
			if err != nil {
				return nil, err
			}
			l, err := s.lease(ctx)
			if err != nil {
				return nil, err
			}
			defer l.Release()
			out := &appList{}
			out.Body.Items = make([]AppOut, 0, len(as))
			for _, a := range as {
				out.Body.Items = append(out.Body.Items, s.appOut(ctx, l, a, true))
			}
			out.Body.Enabled, _ = s.publicAppsOn(ctx)
			return out, nil
		})

	huma.Register(api, huma.Operation{OperationID: "createApp", Method: http.MethodPost, Path: "/api/v1/w/{ws}/apps", Tags: tags, Metadata: owner,
		Summary: "Publish forms and reports as an app behind a new secret link", DefaultStatus: http.StatusCreated, Errors: []int{400, 401, 403, 404}},
		func(ctx context.Context, in *struct {
			WS   string `path:"ws"`
			Body AppInput
		}) (*appOne, error) {
			if on, _ := s.publicAppsOn(ctx); !on {
				return nil, fault.New(fault.Forbidden, "Public apps are turned off on this server.")
			}
			m, _ := MembershipFrom(ctx)
			p, _ := PrincipalFrom(ctx)
			l, err := s.lease(ctx)
			if err != nil {
				return nil, err
			}
			defer l.Release()
			items, err := s.buildItems(ctx, l, admin.App{}, in.Body.Items)
			if err != nil {
				return nil, err
			}
			start, err := startItem(items, "")
			if err != nil {
				return nil, err
			}
			tok, hash := apps.NewToken()
			now := time.Now()
			a := admin.App{ID: ids.New(), WorkspaceID: m.Workspace.ID, Name: strings.TrimSpace(in.Body.Name), Token: tok, TokenHash: hash, StartItem: start,
				Enabled: in.Body.Enabled == nil || *in.Body.Enabled, CreatedBy: p.UserID, CreatedAt: now, UpdatedAt: now, Items: items}
			if err := s.deps.Admin.CreateApp(ctx, a); err != nil {
				return nil, appFault(err)
			}
			s.audit(ctx, "app.create", "app", a.ID, map[string]any{"workspace_id": string(a.WorkspaceID), "name": a.Name, "items": len(items)})
			a, _ = s.deps.Admin.GetApp(ctx, a.ID)
			return &appOne{Body: s.appOut(ctx, l, a, true)}, nil
		})

	huma.Register(api, huma.Operation{OperationID: "updateApp", Method: http.MethodPut, Path: "/api/v1/w/{ws}/apps/{id}", Tags: tags, Metadata: owner,
		Summary: "Rename an app, change its menu or start page, or turn it on or off", Errors: []int{400, 401, 403, 404}},
		func(ctx context.Context, in *struct {
			WS   string `path:"ws"`
			ID   string `path:"id"`
			Body AppInput
		}) (*appOne, error) {
			a, err := s.ownedApp(ctx, in.ID)
			if err != nil {
				return nil, err
			}
			l, err := s.lease(ctx)
			if err != nil {
				return nil, err
			}
			defer l.Release()
			items, err := s.buildItems(ctx, l, a, in.Body.Items)
			if err != nil {
				return nil, err
			}
			start, err := startItem(items, in.Body.StartItem)
			if err != nil {
				return nil, err
			}
			a.Name, a.Items, a.StartItem, a.UpdatedAt = strings.TrimSpace(in.Body.Name), items, start, time.Now()
			if in.Body.Enabled != nil {
				a.Enabled = *in.Body.Enabled
			}
			if err := s.deps.Admin.UpdateApp(ctx, a); err != nil {
				return nil, appFault(err)
			}
			s.audit(ctx, "app.update", "app", a.ID, map[string]any{"workspace_id": string(a.WorkspaceID), "name": a.Name, "enabled": a.Enabled, "items": len(items)})
			a, _ = s.deps.Admin.GetApp(ctx, a.ID)
			return &appOne{Body: s.appOut(ctx, l, a, true)}, nil
		})

	huma.Register(api, huma.Operation{OperationID: "approveApp", Method: http.MethodPost, Path: "/api/v1/w/{ws}/apps/{id}/approve", Tags: tags, Metadata: owner,
		Summary: "Publish changes: visitors see the items as they are now", Errors: []int{400, 401, 403, 404}},
		func(ctx context.Context, in *struct {
			WS   string `path:"ws"`
			ID   string `path:"id"`
			Body struct {
				Items []string `json:"items,omitempty" doc:"Item ids to approve; omit for every item."`
			}
		}) (*appOne, error) {
			a, err := s.ownedApp(ctx, in.ID)
			if err != nil {
				return nil, err
			}
			l, err := s.lease(ctx)
			if err != nil {
				return nil, err
			}
			defer l.Release()
			want := map[string]bool{}
			for _, id := range in.Body.Items {
				want[id] = true
			}
			approved := []string{}
			for i, it := range a.Items {
				if len(want) > 0 && !want[it.ID] {
					continue
				}
				st := s.inspect(ctx, l, it.Kind, it.ObjectID)
				if st.problem != nil {
					return nil, st.problem
				}
				if a.Items[i].Fingerprint != st.fingerprint {
					a.Items[i].Fingerprint = st.fingerprint
					approved = append(approved, it.ID)
				}
			}
			a.UpdatedAt = time.Now()
			if err := s.deps.Admin.UpdateApp(ctx, a); err != nil {
				return nil, appFault(err)
			}
			s.audit(ctx, "app.approve", "app", a.ID, map[string]any{"workspace_id": string(a.WorkspaceID), "items": approved})
			a, _ = s.deps.Admin.GetApp(ctx, a.ID)
			return &appOne{Body: s.appOut(ctx, l, a, true)}, nil
		})

	huma.Register(api, huma.Operation{OperationID: "rotateAppLink", Method: http.MethodPost, Path: "/api/v1/w/{ws}/apps/{id}/rotate", Tags: tags, Metadata: owner,
		Summary: "Replace the app's link; the old link stops working at once", Errors: []int{401, 403, 404}},
		func(ctx context.Context, in *struct {
			WS string `path:"ws"`
			ID string `path:"id"`
		}) (*appOne, error) {
			a, err := s.ownedApp(ctx, in.ID)
			if err != nil {
				return nil, err
			}
			tok, hash := apps.NewToken()
			if err := s.deps.Admin.SetAppToken(ctx, a.ID, tok, hash, time.Now()); err != nil {
				return nil, appFault(err)
			}
			s.audit(ctx, "app.rotate", "app", a.ID, map[string]any{"workspace_id": string(a.WorkspaceID)})
			a, _ = s.deps.Admin.GetApp(ctx, a.ID)
			return &appOne{Body: s.appOut(ctx, nil, a, true)}, nil
		})

	huma.Register(api, huma.Operation{OperationID: "deleteApp", Method: http.MethodDelete, Path: "/api/v1/w/{ws}/apps/{id}", Tags: tags, Metadata: owner,
		Summary: "Delete an app; its link stops working (the forms and reports stay)", DefaultStatus: http.StatusNoContent, Errors: []int{401, 403, 404}},
		func(ctx context.Context, in *struct {
			WS string `path:"ws"`
			ID string `path:"id"`
		}) (*struct{}, error) {
			a, err := s.ownedApp(ctx, in.ID)
			if err != nil {
				return nil, err
			}
			if err := s.deps.Admin.DeleteApp(ctx, a.ID); err != nil {
				return nil, appFault(err)
			}
			s.audit(ctx, "app.delete", "app", a.ID, map[string]any{"workspace_id": string(a.WorkspaceID), "name": a.Name})
			return nil, nil
		})
}

// --- administrator routes -----------------------------------------------------

// PublicAppsSetting is the install-wide switch.
type PublicAppsSetting struct {
	Enabled bool `json:"enabled"`
	Forced  bool `json:"forced_off" readOnly:"true" doc:"ABIDATA_PUBLIC_APPS=false turned public apps off; the setting cannot turn them on."`
}

func (s *Server) registerAdminApps(api huma.API) {
	adminOnly := map[string]any{metaAccess: "platform_admin", metaSessionOnly: true}
	tags := []string{"admin"}

	huma.Register(api, huma.Operation{OperationID: "adminListApps", Method: http.MethodGet, Path: "/api/v1/admin/apps", Tags: tags, Metadata: adminOnly,
		Summary: "Every public app on the server (without links)", Errors: []int{401, 403}},
		func(ctx context.Context, _ *struct{}) (*struct {
			Body struct {
				Items []AppOut `json:"items"`
			}
		}, error) {
			as, err := s.deps.Admin.ListApps(ctx, "")
			if err != nil {
				return nil, err
			}
			names := map[ids.WorkspaceID]string{}
			if ws, err := s.deps.Admin.ListWorkspaces(ctx); err == nil {
				for _, w := range ws {
					names[w.ID] = w.Name
				}
			}
			out := &struct {
				Body struct {
					Items []AppOut `json:"items"`
				}
			}{}
			out.Body.Items = make([]AppOut, 0, len(as))
			for _, a := range as {
				o := s.appOut(ctx, nil, a, false)
				o.WorkspaceName = names[a.WorkspaceID]
				out.Body.Items = append(out.Body.Items, o)
			}
			return out, nil
		})

	for _, act := range []struct{ path, op, summary, audit string }{
		{"take-down", "adminTakeDownApp", "Take an app down: its link stops working and its owners cannot turn it back on", "app.take_down"},
		{"restore", "adminRestoreApp", "Lift a take-down (the owner's own on/off switch still applies)", "app.restore"},
	} {
		huma.Register(api, huma.Operation{OperationID: act.op, Method: http.MethodPost, Path: "/api/v1/admin/apps/{id}/" + act.path, Tags: tags, Metadata: adminOnly,
			Summary: act.summary, DefaultStatus: http.StatusNoContent, Errors: []int{401, 403, 404}},
			func(ctx context.Context, in *struct {
				ID string `path:"id"`
			}) (*struct{}, error) {
				p, _ := PrincipalFrom(ctx)
				var at *time.Time
				if act.path == "take-down" {
					now := time.Now()
					at = &now
				}
				if err := s.deps.Admin.SetAppTakenDown(ctx, in.ID, at, p.UserID); err != nil {
					return nil, appFault(err)
				}
				s.audit(ctx, act.audit, "app", in.ID, nil)
				return nil, nil
			})
	}

	huma.Register(api, huma.Operation{OperationID: "adminGetPublicApps", Method: http.MethodGet, Path: "/api/v1/admin/settings/public-apps", Tags: tags, Metadata: adminOnly,
		Summary: "Whether owners can publish public apps", Errors: []int{401, 403}},
		func(ctx context.Context, _ *struct{}) (*struct{ Body PublicAppsSetting }, error) {
			on, forced := s.publicAppsOn(ctx)
			return &struct{ Body PublicAppsSetting }{Body: PublicAppsSetting{Enabled: on, Forced: forced}}, nil
		})
	huma.Register(api, huma.Operation{OperationID: "adminPutPublicApps", Method: http.MethodPut, Path: "/api/v1/admin/settings/public-apps", Tags: tags, Metadata: adminOnly,
		Summary: "Turn public apps on or off for the whole server; off makes every app link answer as unknown", Errors: []int{400, 401, 403}},
		func(ctx context.Context, in *struct{ Body PublicAppsSetting }) (*struct{ Body PublicAppsSetting }, error) {
			if in.Body.Enabled && !s.deps.Config.PublicApps {
				return nil, fault.New(fault.BadRequest, "ABIDATA_PUBLIC_APPS=false turns public apps off on this server. Change the environment setting first.")
			}
			v := "false"
			if in.Body.Enabled {
				v = "true"
			}
			if err := s.deps.Admin.PutSetting(ctx, settingPublicApps, v); err != nil {
				return nil, err
			}
			s.audit(ctx, "settings.public_apps", "settings", settingPublicApps, map[string]any{"enabled": in.Body.Enabled})
			on, forced := s.publicAppsOn(ctx)
			return &struct{ Body PublicAppsSetting }{Body: PublicAppsSetting{Enabled: on, Forced: forced}}, nil
		})
}

// --- public routes ------------------------------------------------------------

// PublicApp is what a visitor learns about an app.
type PublicApp struct {
	Name      string          `json:"name"`
	StartItem string          `json:"start_item"`
	Items     []PublicAppItem `json:"items"`
}

// PublicAppItem is one entry of an app's menu.
type PublicAppItem struct {
	ID    string `json:"id"`
	Kind  string `json:"kind" enum:"form,report"`
	Title string `json:"title"`
}

// PublicSubmitResult is the answer to a submission. It never carries the stored record.
type PublicSubmitResult struct {
	Submitted     bool              `json:"submitted"`
	Cancel        *forms.CancelInfo `json:"cancel,omitempty"`
	Notifications []PublicNotice    `json:"notifications"`
}

// PublicNotice is a message a form handler shows the visitor.
type PublicNotice struct {
	Kind  string `json:"kind"`
	Level string `json:"level"`
	Text  string `json:"text"`
}

// LookupOption is one choice of a public combo box.
type LookupOption struct {
	Value any    `json:"value"`
	Label string `json:"label"`
}

func (s *Server) registerPublicApps(api huma.API) {
	app := map[string]any{metaAccess: "app"}
	tags := []string{"public apps"}
	base := "/api/v1/apps/{token}"
	type tokenIn struct {
		Token string `path:"token" maxLength:"64"`
	}

	huma.Register(api, huma.Operation{OperationID: "publicApp", Method: http.MethodGet, Path: base, Tags: tags, Metadata: app, Security: []map[string][]string{},
		Summary: "An app's name and menu", Errors: []int{404, 429}},
		func(ctx context.Context, _ *tokenIn) (*struct{ Body PublicApp }, error) {
			a, _ := appFrom(ctx)
			out := PublicApp{Name: a.Name, StartItem: a.StartItem, Items: make([]PublicAppItem, 0, len(a.Items))}
			for _, it := range a.Items {
				out.Items = append(out.Items, PublicAppItem{ID: it.ID, Kind: it.Kind, Title: it.Title})
			}
			return &struct{ Body PublicApp }{Body: out}, nil
		})

	type itemIn struct {
		Token string `path:"token" maxLength:"64"`
		Item  string `path:"item" maxLength:"64"`
	}

	huma.Register(api, huma.Operation{OperationID: "publicForm", Method: http.MethodGet, Path: base + "/forms/{item}", Tags: tags, Metadata: app, Security: []map[string][]string{},
		Summary: "A published form: its layout and the fields it binds", Errors: []int{404, 429, 503}},
		func(ctx context.Context, in *itemIn) (*struct{ Body apps.PublicForm }, error) {
			l, err := s.lease(ctx)
			if err != nil {
				return nil, err
			}
			defer l.Release()
			st, err := s.publicItem(ctx, l, "form", in.Item)
			if err != nil {
				return nil, err
			}
			sch, err := s.deps.Schemas.Get(ctx, l)
			if err != nil {
				return nil, err
			}
			t, ok := sch.Table(st.form.SourceID)
			if !ok {
				return nil, fault.E(fault.AppItemChanged)
			}
			return &struct{ Body apps.PublicForm }{Body: apps.ViewForm(st.form, t)}, nil
		})

	huma.Register(api, huma.Operation{OperationID: "publicFormLookup", Method: http.MethodGet, Path: base + "/forms/{item}/lookups/{control}", Tags: tags, Metadata: app,
		Security: []map[string][]string{}, Summary: "The choices of one combo box of a published form", Errors: []int{404, 429, 503}},
		func(ctx context.Context, in *struct {
			Token   string `path:"token" maxLength:"64"`
			Item    string `path:"item" maxLength:"64"`
			Control string `path:"control" maxLength:"100"`
		}) (*struct {
			Body struct {
				Options []LookupOption `json:"options"`
			}
		}, error) {
			l, err := s.lease(ctx)
			if err != nil {
				return nil, err
			}
			defer l.Release()
			st, err := s.publicItem(ctx, l, "form", in.Item)
			if err != nil {
				return nil, err
			}
			var lk *forms.Lookup
			for _, c := range st.form.Layout.AllControls() {
				if c.Name == in.Control && c.Lookup != nil {
					lk = c.Lookup
				}
			}
			if lk == nil {
				return nil, fault.E(fault.NotFound)
			}
			opts, err := s.lookupOptions(ctx, l, lk)
			if err != nil {
				return nil, err
			}
			out := &struct {
				Body struct {
					Options []LookupOption `json:"options"`
				}
			}{}
			out.Body.Options = opts
			return out, nil
		})

	huma.Register(api, huma.Operation{OperationID: "publicFormEvent", Method: http.MethodPost, Path: base + "/forms/{item}/events", Tags: tags, Metadata: app,
		Security: []map[string][]string{}, Summary: "A form event on the visitor's new record (load, change, confirm)", Errors: []int{400, 404, 422, 429, 503}},
		func(ctx context.Context, in *struct {
			Token   string `path:"token" maxLength:"64"`
			Item    string `path:"item" maxLength:"64"`
			Body    forms.EventRequest
			RawBody []byte
		}) (*struct{ Body *forms.EventResponse }, error) {
			var req forms.EventRequest
			if err := decodeJSON(in.RawBody, &req); err != nil {
				return nil, err
			}
			switch req.Event {
			case "load", "current", "change", "confirm":
			default:
				return nil, fault.Newf(fault.BadRequest, "The event %q is not available on public forms.", req.Event)
			}
			l, err := s.lease(ctx)
			if err != nil {
				return nil, err
			}
			defer l.Release()
			st, err := s.publicItem(ctx, l, "form", in.Item)
			if err != nil {
				return nil, err
			}
			if err := boundOnly(st.form, req.Record); err != nil {
				return nil, err
			}
			// A visitor only ever has a new, unsaved record.
			req.RecordID, req.RecordVersion, req.IsNew, req.OpenArgs = 0, 0, true, nil
			bctx, stop := l.Bind(ctx)
			defer stop()
			resp, err := s.deps.Forms.Event(bctx, l, s.caller(ctx, l), st.form.ID, req)
			if err != nil {
				return nil, err
			}
			resp.Navigate, resp.Requery, resp.Logs, resp.Saved, resp.Close = nil, nil, nil, nil, false
			return &struct{ Body *forms.EventResponse }{Body: resp}, nil
		})

	huma.Register(api, huma.Operation{OperationID: "publicFormSubmit", Method: http.MethodPost, Path: base + "/forms/{item}/submit", Tags: tags, Metadata: app,
		Security: []map[string][]string{}, Summary: "Submit a new record through a published form", Errors: []int{400, 404, 409, 422, 429, 503, 507}},
		func(ctx context.Context, in *struct {
			Token          string `path:"token" maxLength:"64"`
			Item           string `path:"item" maxLength:"64"`
			IdempotencyKey string `header:"Idempotency-Key" maxLength:"100"`
			Body           struct {
				Fields map[string]any `json:"fields" doc:"Field values by column id; only fields the form shows."`
			}
			RawBody []byte
		}) (*struct{ Body PublicSubmitResult }, error) {
			var body struct {
				Fields map[string]any `json:"fields"`
			}
			if err := decodeJSON(in.RawBody, &body); err != nil {
				return nil, err
			}
			a, _ := appFrom(ctx)
			ip := clientIP(ctx)
			if ok, wait := s.appLimits.Allow("submit:"+a.ID+":"+ip, apps.Submits); !ok {
				return nil, &fault.Error{Code: fault.RateLimited, RetryAfter: wait}
			}
			if ok, wait := s.appLimits.Allow("submit:"+a.ID, apps.AppSubmits); !ok {
				return nil, &fault.Error{Code: fault.RateLimited, RetryAfter: wait}
			}
			l, err := s.lease(ctx)
			if err != nil {
				return nil, err
			}
			defer l.Release()
			st, err := s.publicItem(ctx, l, "form", in.Item)
			if err != nil {
				return nil, err
			}
			if err := boundOnly(st.form, body.Fields); err != nil {
				return nil, err
			}
			bctx, stop := l.Bind(ctx)
			defer stop()
			resp, err := s.deps.Forms.Save(bctx, l, s.caller(ctx, l), st.form.ID, forms.SaveRequest{IsNew: true, Fields: body.Fields, IdempotencyKey: in.IdempotencyKey})
			if err != nil {
				return nil, err
			}
			out := PublicSubmitResult{Submitted: resp.Saved != nil, Cancel: resp.Cancel, Notifications: []PublicNotice{}}
			for _, n := range resp.Notifications {
				out.Notifications = append(out.Notifications, PublicNotice{Kind: n.Kind, Level: n.Level, Text: n.Text})
			}
			return &struct{ Body PublicSubmitResult }{Body: out}, nil
		})

	huma.Register(api, huma.Operation{OperationID: "publicReport", Method: http.MethodGet, Path: base + "/reports/{item}", Tags: tags, Metadata: app,
		Security: []map[string][]string{}, Summary: "A published report's layout and paper", Errors: []int{404, 429, 503}},
		func(ctx context.Context, in *itemIn) (*struct{ Body apps.PublicReport }, error) {
			l, err := s.lease(ctx)
			if err != nil {
				return nil, err
			}
			defer l.Release()
			st, err := s.publicItem(ctx, l, "report", in.Item)
			if err != nil {
				return nil, err
			}
			var params []apps.PublicParam
			if st.report.SourceKind == "query" {
				if q, err := s.deps.Queries.Get(ctx, l, st.report.SourceID); err == nil {
					for _, p := range q.Parameters {
						params = append(params, apps.PublicParam{Name: p.Name, Type: p.Type})
					}
				}
			}
			return &struct{ Body apps.PublicReport }{Body: apps.ViewReport(st.report, params)}, nil
		})

	huma.Register(api, huma.Operation{OperationID: "publicReportRun", Method: http.MethodPost, Path: base + "/reports/{item}/run", Tags: tags, Metadata: app,
		Security: []map[string][]string{}, Summary: "Run a published report (NDJSON, as runReport)", Errors: []int{400, 404, 422, 429, 503}},
		func(ctx context.Context, in *struct {
			Token   string `path:"token" maxLength:"64"`
			Item    string `path:"item" maxLength:"64"`
			Body    RunRequest
			RawBody []byte
		}) (*huma.StreamResponse, error) {
			params, _, _, _, err := decodeParams(in.RawBody)
			if err != nil {
				return nil, err
			}
			l, err := s.lease(ctx)
			if err != nil {
				return nil, err
			}
			st, err := s.publicItem(ctx, l, "report", in.Item)
			if err != nil {
				l.Release()
				return nil, err
			}
			sch, err := s.deps.Schemas.Get(ctx, l)
			if err != nil {
				l.Release()
				return nil, err
			}
			p, _ := PrincipalFrom(ctx)
			return s.streamReport(l, sch, st.report, params, reports.Caller{UserID: p.UserID, TZ: workspaceTZ(ctx, l)}), nil
		})
}

// boundOnly refuses values for fields the form does not show: a visitor can
// fill in the form, not the rest of the table.
func boundOnly(f forms.Form, fields map[string]any) error {
	bound := map[string]bool{}
	for _, c := range f.Layout.AllControls() {
		if c.Bind != nil {
			bound[c.Bind.ColumnID] = true
		}
	}
	for id := range fields {
		if !bound[id] {
			return fault.New(fault.BadRequest, "The submission has a field the form does not show.")
		}
	}
	return nil
}

// lookupOptions lists a combo box's choices, as the workspace form does.
func (s *Server) lookupOptions(ctx context.Context, l *workspace.Lease, lk *forms.Lookup) ([]LookupOption, error) {
	out := []LookupOption{}
	label := func(v, t any) string {
		if t == nil || t == "" {
			return stringify(v)
		}
		return stringify(t)
	}
	if lk.TableID != "" {
		sch, err := s.deps.Schemas.Get(ctx, l)
		if err != nil {
			return nil, err
		}
		sort := []records.Sort{}
		if _, ok := tableColumn(sch, lk.TableID, lk.DisplayColumnID); ok {
			sort = append(sort, records.Sort{Col: lk.DisplayColumnID})
		}
		rows, _, err := s.deps.Records.List(ctx, l, lk.TableID, records.ListQuery{Sort: sort, Limit: records.MaxPage})
		if err != nil {
			return nil, err
		}
		pick := func(r records.Row, col string) any {
			if col == "id" {
				return r.ID
			}
			return r.Fields[col]
		}
		for _, r := range rows {
			out = append(out, LookupOption{Value: pick(r, lk.ValueColumnID), Label: label(pick(r, lk.ValueColumnID), pick(r, lk.DisplayColumnID))})
		}
		return out, nil
	}
	res, err := s.deps.Queries.RunSaved(ctx, l, query.Policy{}, lk.SourceQueryID, query.Params{}, maxLookupOptions)
	if err != nil {
		return nil, err
	}
	vi, di := indexOf(res.Columns, lk.ValueColumnID), indexOf(res.Columns, lk.DisplayColumnID)
	for _, cells := range res.Rows {
		v, t := cells[max(vi, 0)], cells[max(di, 0)]
		out = append(out, LookupOption{Value: v, Label: label(v, t)})
	}
	return out, nil
}

func tableColumn(sch *catalog.Schema, tableID, colID string) (catalog.Column, bool) {
	t, ok := sch.Table(tableID)
	if !ok {
		return catalog.Column{}, false
	}
	c, ok := t.Column(colID)
	if !ok {
		return catalog.Column{}, false
	}
	return *c, true
}

func stringify(v any) string {
	switch x := v.(type) {
	case nil:
		return ""
	case string:
		return x
	}
	return fmt.Sprint(v)
}

func indexOf(names []string, want string) int {
	for i, n := range names {
		if n == want {
			return i
		}
	}
	return -1
}
