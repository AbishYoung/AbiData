package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/danielgtaylor/huma/v2"

	"abidata/internal/admin"
	"abidata/internal/egress"
	"abidata/internal/fault"
)

// EgressSettingKey is the _adm_settings key holding the platform egress policy.
const EgressSettingKey = "egress_policy"

// maxAllowlist bounds the platform allowlist.
const maxAllowlist = 200

// EgressHost is the read-only view of an allowlist entry shown to workspace
// builders and owners: host pattern and ports only (notes stay with administrators).
type EgressHost struct {
	Pattern string `json:"pattern"`
	Ports   string `json:"ports"`
}

// EgressHosts is what outbound requests from a workspace's jobs may reach.
type EgressHosts struct {
	Items     []EgressHost `json:"items"`
	AllowHTTP bool         `json:"allow_http"`
	Disabled  bool         `json:"disabled"`
}

// LoadGlobalEgress reads the platform egress policy (DefaultGlobal, which
// denies everything, when unset). A policy stored in the pre-ADR-0008 shape is
// converted in memory; MigrateEgressPolicy persists the conversion at startup.
func LoadGlobalEgress(ctx context.Context, a *admin.Store) (egress.Global, error) {
	v, err := a.GetSetting(ctx, EgressSettingKey)
	if err != nil {
		if errors.Is(err, admin.ErrNotFound) {
			return egress.DefaultGlobal, nil
		}
		return egress.Global{}, err
	}
	g, _, _, err := egress.ParseGlobal(v)
	return g, err
}

// MigrateEgressPolicy rewrites a stored policy in the old per-workspace shape
// ("hosts") to the global allowlist shape and logs a warning when a wildcard
// was dropped. It is idempotent and does nothing on a fresh install.
func MigrateEgressPolicy(ctx context.Context, a *admin.Store, log *slog.Logger) error {
	v, err := a.GetSetting(ctx, EgressSettingKey)
	if errors.Is(err, admin.ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	g, migrated, dropped, err := egress.ParseGlobal(v)
	if err != nil {
		return err
	}
	if !migrated {
		return nil
	}
	b, _ := json.Marshal(g)
	if err := a.PutSetting(ctx, EgressSettingKey, string(b)); err != nil {
		return err
	}
	// Every migrated host is now reachable from every workspace, where before
	// each workspace also had to list it, so the result always needs review.
	kept := make([]string, 0, len(g.Allowlist))
	keptSet := map[string]bool{}
	for _, e := range g.Allowlist {
		kept = append(kept, e.Pattern)
		keptSet[e.Pattern] = true
	}
	var old struct {
		Hosts []string `json:"hosts"`
	}
	_ = json.Unmarshal([]byte(v), &old)
	discarded := []string{}
	for _, h := range old.Hosts {
		if n := strings.TrimSuffix(strings.ToLower(strings.TrimSpace(h)), "."); n != "*" && !keptSet[n] {
			discarded = append(discarded, h)
		}
	}
	log.Warn("egress policy migrated to one platform allowlist: every listed host is now reachable from every workspace's jobs and per-workspace allowlists are no longer used; review it in Admin > Settings, especially wildcard entries",
		slog.Any("allowlist", kept), slog.Any("discarded_invalid", discarded), slog.Bool("wildcard_dropped", dropped))
	return nil
}

func validateEgress(items []egress.Entry) ([]egress.Entry, error) {
	if len(items) > maxAllowlist {
		return nil, fault.Newf(fault.BadRequest, "The platform allowlist can hold at most %d entries.", maxAllowlist)
	}
	seen := map[string]bool{}
	out := make([]egress.Entry, 0, len(items))
	for i, e := range items {
		e.Pattern = strings.ToLower(strings.TrimSuffix(strings.TrimSpace(e.Pattern), "."))
		if !egress.ValidPattern(e.Pattern) {
			return nil, fault.Newf(fault.BadRequest, "Entry %d: %q is not a host name or *.domain pattern.", i+1, e.Pattern).WithDetails(map[string]any{"index": i, "field": "pattern"})
		}
		ports, ok := egress.NormPorts(e.Ports)
		e.Ports = ports
		if !ok {
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
	adminOnly := map[string]any{metaAccess: "platform_admin"}
	builder := map[string]any{metaRole: string(admin.RoleBuilder)}

	huma.Register(api, huma.Operation{OperationID: "getEgressHosts", Method: http.MethodGet, Path: "/api/v1/w/{ws}/egress-hosts", Tags: []string{"workspace"}, Metadata: builder,
		Summary: "The outbound hosts the platform allows (read-only)", Errors: []int{401, 403, 404}},
		func(ctx context.Context, _ *struct {
			WS string `path:"ws"`
		}) (*struct{ Body EgressHosts }, error) {
			if _, ok := MembershipFrom(ctx); !ok {
				return nil, errNotFound
			}
			g, err := LoadGlobalEgress(ctx, s.deps.Admin)
			if err != nil {
				return nil, err
			}
			out := EgressHosts{Items: make([]EgressHost, 0, len(g.Allowlist)), AllowHTTP: g.AllowHTTP, Disabled: g.Disabled}
			for _, e := range g.Allowlist {
				out.Items = append(out.Items, EgressHost{Pattern: e.Pattern, Ports: e.Ports})
			}
			return &struct{ Body EgressHosts }{Body: out}, nil
		})

	huma.Register(api, huma.Operation{OperationID: "adminGetEgressPolicy", Method: http.MethodGet, Path: "/api/v1/admin/settings/egress", Tags: []string{"admin"}, Metadata: adminOnly,
		Summary: "The platform egress policy and allowlist", Errors: []int{401, 403}},
		func(ctx context.Context, _ *struct{}) (*struct{ Body egress.Global }, error) {
			g, err := LoadGlobalEgress(ctx, s.deps.Admin)
			if err != nil {
				return nil, err
			}
			return &struct{ Body egress.Global }{Body: g}, nil
		})
	huma.Register(api, huma.Operation{OperationID: "adminPutEgressPolicy", Method: http.MethodPut, Path: "/api/v1/admin/settings/egress", Tags: []string{"admin"}, Metadata: adminOnly,
		Summary: "Replace the platform egress policy and allowlist", Errors: []int{400, 401, 403}},
		func(ctx context.Context, in *struct{ Body egress.Global }) (*struct{ Body egress.Global }, error) {
			g := in.Body
			items, err := validateEgress(g.Allowlist)
			if err != nil {
				return nil, err
			}
			g.Allowlist = items
			b, _ := json.Marshal(g)
			if err := s.deps.Admin.PutSetting(ctx, EgressSettingKey, string(b)); err != nil {
				return nil, err
			}
			s.audit(ctx, "egress.policy", "settings", EgressSettingKey, map[string]any{"entries": len(items), "allow_http": g.AllowHTTP, "disabled": g.Disabled})
			return &struct{ Body egress.Global }{Body: g}, nil
		})
}
