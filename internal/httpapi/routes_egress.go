package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/danielgtaylor/huma/v2"

	"abidata/internal/admin"
	"abidata/internal/egress"
	"abidata/internal/fault"
	"abidata/internal/sqlitex"
	"abidata/internal/workspace"
)

// EgressSettingKey is the _adm_settings key holding the global egress policy.
const EgressSettingKey = "egress_policy"

// maxAllowlist bounds a workspace's allowlist.
const maxAllowlist = 200

// EgressEntry is one workspace allowlist row.
type EgressEntry struct {
	Pattern string `json:"pattern" maxLength:"255" doc:"Exact host (api.example.com) or *.domain for any subdomain."`
	Ports   string `json:"ports,omitempty" maxLength:"100" doc:"Comma-separated ports (default 443; list 80 to allow plain http when the platform permits it)."`
	Note    string `json:"note,omitempty" maxLength:"500"`
}

// WorkspaceEgress is a workspace's allowlist plus the platform policy it is intersected with.
type WorkspaceEgress struct {
	Items    []EgressEntry `json:"items"`
	Platform egress.Global `json:"platform" readOnly:"true"`
}

// LoadGlobalEgress reads the platform egress policy (DefaultGlobal when unset).
func LoadGlobalEgress(ctx context.Context, a *admin.Store) (egress.Global, error) {
	v, err := a.GetSetting(ctx, EgressSettingKey)
	if err != nil {
		if errors.Is(err, admin.ErrNotFound) {
			return egress.DefaultGlobal, nil
		}
		return egress.Global{}, err
	}
	var g egress.Global
	if err := json.Unmarshal([]byte(v), &g); err != nil {
		return egress.Global{}, err
	}
	return g, nil
}

// ReadAllowlist reads a workspace's _sys_egress_allowlist.
func ReadAllowlist(ctx context.Context, l *workspace.Lease) ([]EgressEntry, error) {
	out := []EgressEntry{}
	err := l.Read(ctx, func(q sqlitex.Querier) error {
		rows, err := q.Query(ctx, `SELECT pattern, ports, note FROM _sys_egress_allowlist ORDER BY pattern`)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var e EgressEntry
			if err := rows.Scan(&e.Pattern, &e.Ports, &e.Note); err != nil {
				return err
			}
			out = append(out, e)
		}
		return rows.Err()
	})
	return out, err
}

func validateEgress(items []EgressEntry) ([]EgressEntry, error) {
	if len(items) > maxAllowlist {
		return nil, fault.Newf(fault.BadRequest, "The allowlist can hold at most %d entries.", maxAllowlist)
	}
	seen := map[string]bool{}
	out := make([]EgressEntry, 0, len(items))
	for i, e := range items {
		e.Pattern = strings.ToLower(strings.TrimSuffix(strings.TrimSpace(e.Pattern), "."))
		e.Ports = strings.ReplaceAll(strings.TrimSpace(e.Ports), " ", "")
		if e.Ports == "" {
			e.Ports = "443"
		}
		if !egress.ValidPattern(e.Pattern) {
			return nil, fault.Newf(fault.BadRequest, "Entry %d: %q is not a host name or *.domain pattern.", i+1, e.Pattern).WithDetails(map[string]any{"index": i, "field": "pattern"})
		}
		if !egress.ValidPorts(e.Ports) {
			return nil, fault.Newf(fault.BadRequest, "Entry %d: ports must be numbers from 1 to 65535, separated by commas.", i+1).WithDetails(map[string]any{"index": i, "field": "ports"})
		}
		if seen[e.Pattern] {
			return nil, fault.Newf(fault.BadRequest, "Entry %d: %q is listed twice.", i+1, e.Pattern).WithDetails(map[string]any{"index": i, "field": "pattern"})
		}
		seen[e.Pattern] = true
		out = append(out, e)
	}
	return out, nil
}

func (s *Server) registerEgress(api huma.API) {
	owner := map[string]any{metaRole: string(admin.RoleOwner)}
	adminOnly := map[string]any{metaAccess: "platform_admin"}
	tags := []string{"workspace"}

	get := func(ctx context.Context) (*struct{ Body WorkspaceEgress }, error) {
		l, err := s.lease(ctx)
		if err != nil {
			return nil, err
		}
		defer l.Release()
		items, err := ReadAllowlist(ctx, l)
		if err != nil {
			return nil, err
		}
		g, err := LoadGlobalEgress(ctx, s.deps.Admin)
		if err != nil {
			return nil, err
		}
		return &struct{ Body WorkspaceEgress }{Body: WorkspaceEgress{Items: items, Platform: g}}, nil
	}
	huma.Register(api, huma.Operation{OperationID: "getEgressAllowlist", Method: http.MethodGet, Path: "/api/v1/w/{ws}/settings/egress", Tags: tags, Metadata: owner,
		Summary: "The workspace's outbound HTTP allowlist (owner)", Errors: []int{401, 403, 404}},
		func(ctx context.Context, _ *struct {
			WS string `path:"ws"`
		}) (*struct{ Body WorkspaceEgress }, error) {
			return get(ctx)
		})
	huma.Register(api, huma.Operation{OperationID: "putEgressAllowlist", Method: http.MethodPut, Path: "/api/v1/w/{ws}/settings/egress", Tags: tags, Metadata: owner,
		Summary: "Replace the workspace's outbound HTTP allowlist (owner)", Errors: []int{400, 401, 403, 404}},
		func(ctx context.Context, in *struct {
			WS   string `path:"ws"`
			Body struct {
				Items []EgressEntry `json:"items" maxItems:"200"`
			}
		}) (*struct{ Body WorkspaceEgress }, error) {
			items, err := validateEgress(in.Body.Items)
			if err != nil {
				return nil, err
			}
			l, err := s.lease(ctx)
			if err != nil {
				return nil, err
			}
			err = l.Write(ctx, workspace.Interactive, func(tx *sqlitex.Tx) error {
				if _, err := tx.Exec(ctx, `DELETE FROM _sys_egress_allowlist`); err != nil {
					return err
				}
				for _, e := range items {
					if _, err := tx.Exec(ctx, `INSERT INTO _sys_egress_allowlist (pattern, ports, note) VALUES (?,?,?)`, e.Pattern, e.Ports, e.Note); err != nil {
						return err
					}
				}
				return nil
			})
			l.Release()
			if err != nil {
				return nil, err
			}
			s.audit(ctx, "egress.update", "workspace", "", map[string]any{"entries": len(items)})
			return get(ctx)
		})

	huma.Register(api, huma.Operation{OperationID: "adminGetEgressPolicy", Method: http.MethodGet, Path: "/api/v1/admin/settings/egress", Tags: []string{"admin"}, Metadata: adminOnly,
		Summary: "The platform egress policy", Errors: []int{401, 403}},
		func(ctx context.Context, _ *struct{}) (*struct{ Body egress.Global }, error) {
			g, err := LoadGlobalEgress(ctx, s.deps.Admin)
			if err != nil {
				return nil, err
			}
			return &struct{ Body egress.Global }{Body: g}, nil
		})
	huma.Register(api, huma.Operation{OperationID: "adminPutEgressPolicy", Method: http.MethodPut, Path: "/api/v1/admin/settings/egress", Tags: []string{"admin"}, Metadata: adminOnly,
		Summary: "Replace the platform egress policy", Errors: []int{400, 401, 403}},
		func(ctx context.Context, in *struct{ Body egress.Global }) (*struct{ Body egress.Global }, error) {
			g := in.Body
			hosts := make([]string, 0, len(g.Hosts))
			for _, h := range g.Hosts {
				h = strings.ToLower(strings.TrimSpace(h))
				if h != "*" && !egress.ValidPattern(h) {
					return nil, fault.Newf(fault.BadRequest, "%q is not a host name, *.domain pattern or *.", h)
				}
				hosts = append(hosts, h)
			}
			g.Hosts = hosts
			b, _ := json.Marshal(g)
			if err := s.deps.Admin.PutSetting(ctx, EgressSettingKey, string(b)); err != nil {
				return nil, err
			}
			s.audit(ctx, "egress.policy", "settings", EgressSettingKey, map[string]any{"hosts": len(hosts), "allow_http": g.AllowHTTP, "disabled": g.Disabled})
			return &struct{ Body egress.Global }{Body: g}, nil
		})
}
