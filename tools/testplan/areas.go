package main

// Areas group the Playwright specs in tests/e2e by feature (ADR 0016). Every spec except
// smoke.spec.ts (the container smoke) belongs to exactly one area; TestEverySpecHasOneArea
// fails when a new spec is not listed here. CI runs one job per area.
var areas = []area{
	{"auth", []string{"account", "password-reset", "members", "apikeys"}},
	{"admin", []string{"admin-overview", "backups-admin", "demo-provision"}},
	{"shell", []string{"shell", "palette", "docs", "pwa", "ui-behaviour"}},
	{"a11y", []string{"a11y", "mobile"}},
	{"tables", []string{"tabledesign", "tabledesign-indexes", "history"}},
	{"datasheet", []string{"datasheet"}},
	{"files-geo", []string{"attachments", "geo"}},
	{"queries", []string{"queries", "import-export"}},
	{"forms", []string{"forms-designer", "forms-runtime", "forms-runtime-live"}},
	{"reports", []string{"reports", "reports-designer"}},
	{"automation", []string{"scripts", "jobs", "script-templates"}},
	{"apps", []string{"public-apps", "invasive-species"}},
	{"blogs", []string{"blogs"}},
}

type area struct {
	Name  string
	Specs []string // file names in tests/e2e without .spec.ts
}

// all marks a source prefix whose change runs every area.
var all = []string{"*"}

// sourceAreas maps source paths to the e2e areas that exercise them. The longest matching
// prefix wins. A path under a source tree (see sourceTrees) that matches no prefix runs every
// area, so a new package or feature directory is covered until it is mapped.
var sourceAreas = map[string][]string{
	// The React app.
	"web/src/features/account/":     {"auth"},
	"web/src/features/admin/":       {"admin", "auth", "apps", "files-geo", "a11y"},
	"web/src/features/apikeys/":     {"auth"},
	"web/src/features/apps/":        {"apps"},
	"web/src/features/auth/":        all, // every spec signs in
	"web/src/features/blog/":        {"blogs"},
	"web/src/features/backups/":     {"admin"},
	"web/src/features/datasheet/":   {"datasheet", "tables", "files-geo", "a11y"},
	"web/src/features/docs/":        {"shell"},
	"web/src/features/files/":       {"files-geo"},
	"web/src/features/forms/":       {"forms", "apps", "files-geo", "a11y"},
	"web/src/features/history/":     {"tables"},
	"web/src/features/imports/":     {"queries", "a11y"},
	"web/src/features/jobs/":        {"automation", "a11y"},
	"web/src/features/members/":     {"auth", "a11y"},
	"web/src/features/nav/":         all,
	"web/src/features/palette/":     {"shell"},
	"web/src/features/queries/":     {"queries", "files-geo", "a11y"},
	"web/src/features/reports/":     {"reports", "apps", "files-geo", "a11y"},
	"web/src/features/scripts/":     {"automation", "shell", "a11y"},
	"web/src/features/settings/":    {"admin", "auth", "files-geo", "automation", "a11y"},
	"web/src/features/tabledesign/": {"tables", "a11y"},
	"web/src/features/workspace/":   all,
	"web/src/components/designer/":  {"forms", "reports", "a11y"},
	"web/src/components/maps/":      {"files-geo"},
	"web/src/components/charts/":    {"reports"},
	"web/src/lib/geo/":              {"files-geo"},
	"web/index.html":                all,
	"web/public/":                   {"shell", "a11y"},

	// The server. Packages every request goes through (auth, catalog, records, dbx, sqlgen,
	// sqlitex, workspace, events, the HTTP plumbing) are deliberately unmapped: they run all.
	"internal/admin/":                           {"admin", "auth"},
	"internal/apps/":                            {"apps"},
	"internal/artifacts/":                       {"automation"},
	"internal/backup/":                          {"admin"},
	"internal/blogs/":                           {"blogs", "auth", "admin"},
	"internal/demo/":                            {"apps", "admin"},
	"internal/docsrv/":                          {"shell"},
	"internal/egress/":                          {"automation", "admin"},
	"internal/exprlang/":                        {"tables", "queries", "forms", "reports"}, // ADR 0013 Phase D
	"internal/files/":                           {"files-geo"},
	"internal/forms/":                           {"forms", "apps", "files-geo"},
	"internal/geo/":                             {"files-geo", "queries"},
	"internal/ingest/":                          {"queries"},
	"internal/jobs/":                            {"automation"},
	"internal/obs/":                             {"admin"},
	"internal/ops/":                             {"admin"},
	"internal/pgdb/":                            {}, // the e2e server runs on SQLite
	"internal/query/":                           {"queries", "forms", "reports", "apps", "files-geo"},
	"internal/reports/":                         {"reports", "apps"},
	"internal/script/":                          {"automation", "forms"},
	"internal/scripts/":                         {"automation"},
	"internal/httpapi/openapi.json":             {},
	"internal/httpapi/routes_admin.go":          {"admin", "auth", "apps", "files-geo"},
	"internal/httpapi/routes_admin_overview.go": {"admin"},
	"internal/httpapi/routes_apikeys.go":        {"auth"},
	"internal/httpapi/routes_apps.go":           {"apps"},
	"internal/httpapi/routes_backups.go":        {"admin"},
	"internal/httpapi/routes_blog.go":           {"blogs"},
	"internal/httpapi/blog_public.go":           {"blogs"},
	"internal/httpapi/routes_docs.go":           {"shell"},
	"internal/httpapi/routes_egress.go":         {"automation", "admin"},
	"internal/httpapi/routes_files.go":          {"files-geo"},
	"internal/httpapi/routes_forms.go":          {"forms", "apps"},
	"internal/httpapi/routes_ingest.go":         {"queries"},
	"internal/httpapi/routes_jobs.go":           {"automation"},
	"internal/httpapi/routes_logs.go":           {"admin"},
	"internal/httpapi/routes_maps.go":           {"files-geo"},
	"internal/httpapi/routes_queries.go":        {"queries", "forms", "reports"},
	"internal/httpapi/routes_reports.go":        {"reports", "apps"},
	"cmd/abidata/demo":                          {"apps", "admin"},
	"docs/content/":                             {"shell"},
	"docs/embed.go":                             {"shell"},
}

// sourceTrees are the roots whose unmapped paths run every area.
var sourceTrees = []string{"web/src/", "web/", "internal/", "cmd/", "docs/"}
