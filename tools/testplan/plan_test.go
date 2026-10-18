package main

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// fakeRepo is a small module shaped like the real one: dbx at the bottom, catalog with
// embedded migrations, a feature package, the HTTP layer and the binary on top.
func fakeRepo() Repo {
	return Repo{
		Packages: map[string]Package{
			"internal/dbx":     {ImportPath: "abidata/internal/dbx"},
			"internal/ids":     {ImportPath: "abidata/internal/ids"},
			"internal/catalog": {ImportPath: "abidata/internal/catalog", Deps: []string{"abidata/internal/dbx"}, Embeds: []string{"migrations/sqlite/"}},
			"internal/forms":   {ImportPath: "abidata/internal/forms", Deps: []string{"abidata/internal/catalog"}},
			"internal/pgdb":    {ImportPath: "abidata/internal/pgdb", Deps: []string{"abidata/internal/dbx"}},
			// ids is imported only by httpapi's tests.
			"internal/httpapi": {ImportPath: "abidata/internal/httpapi", Deps: []string{"abidata/internal/forms", "abidata/internal/ids"}},
			"cmd/abidata":      {ImportPath: "abidata/cmd/abidata", Deps: []string{"abidata/internal/httpapi"}},
			"web":              {ImportPath: "abidata/web", Embeds: []string{"dist"}},
			"docs":             {ImportPath: "abidata/docs", Embeds: []string{"content"}},
		},
		Specs: map[string][]string{
			"forms-runtime": {"forms-runtime-helpers.ts", "helpers.ts", "tabledesign-helpers.ts"},
			"queries":       {"helpers.ts", "queries-helpers.ts"},
			"jobs":          {"helpers.ts", "queries-helpers.ts"},
			"geo":           {"helpers.ts"},
		},
	}
}

func pkgs(names ...string) []string {
	out := make([]string, len(names))
	for i, n := range names {
		out[i] = "abidata/" + n
	}
	slices.Sort(out)
	return out
}

func compute(paths ...string) Plan {
	var cs []Change
	for _, p := range paths {
		if d, ok := strings.CutPrefix(p, "-"); ok {
			cs = append(cs, Change{Path: d, Deleted: true})
		} else {
			cs = append(cs, Change{Path: p})
		}
	}
	return Compute(cs, fakeRepo())
}

func TestDocumentationSelectsNothing(t *testing.T) {
	p := compute("docs/adr/0016-layered-testing.md", "README.md", "web/src/components/DESIGN.md")
	if p.Go() || p.Web() || p.WebStatic || p.E2E() || p.Image || p.Postgres || p.GoLint || p.Full {
		t.Fatalf("documentation selected checks: %+v", p)
	}
}

func TestGoSourceSelectsReverseDependencies(t *testing.T) {
	p := compute("internal/forms/runtime.go")
	if want := pkgs("internal/forms", "internal/httpapi", "cmd/abidata"); !slices.Equal(p.GoPackages, want) {
		t.Fatalf("go packages = %v, want %v", p.GoPackages, want)
	}
	if !p.GoLint || !slices.Equal(p.GoLintDirs, []string{"internal/forms"}) {
		t.Fatalf("lint = %v %v", p.GoLint, p.GoLintDirs)
	}
	if want := []string{"apps", "files-geo", "forms"}; !slices.Equal(p.E2EAreas, want) {
		t.Fatalf("areas = %v, want %v", p.E2EAreas, want)
	}
	if !p.Image || p.Web() || p.WebStatic || p.Postgres {
		t.Fatalf("unexpected layers: %+v", p)
	}
}

func TestGoTestOnlyChangeTestsJustThatPackage(t *testing.T) {
	p := compute("internal/catalog/apply_test.go", "internal/catalog/testdata/schema.golden")
	if want := pkgs("internal/catalog"); !slices.Equal(p.GoPackages, want) {
		t.Fatalf("go packages = %v, want %v", p.GoPackages, want)
	}
	if p.E2E() || p.Image {
		t.Fatalf("a test-only change selected e2e or the image: %+v", p)
	}
}

func TestTestOnlyImportCounts(t *testing.T) {
	// httpapi imports ids only from its tests; it is still affected.
	p := compute("internal/ids/ids.go")
	if want := pkgs("internal/ids", "internal/httpapi", "cmd/abidata"); !slices.Equal(p.GoPackages, want) {
		t.Fatalf("go packages = %v, want %v", p.GoPackages, want)
	}
}

func TestSharedSQLLayerWidens(t *testing.T) {
	for _, f := range []string{"internal/dbx/tx.go", "internal/catalog/migrations/sqlite/0007_x.sql"} {
		p := compute(f)
		if !p.Postgres {
			t.Errorf("%s: postgres not selected", f)
		}
		if !p.E2EAll || len(p.E2EAreas) != len(areas) {
			t.Errorf("%s: e2e = %v, want every area", f, p.E2EAreas)
		}
		if !slices.Contains(p.GoPackages, "abidata/cmd/abidata") || !slices.Contains(p.GoPackages, "abidata/internal/catalog") {
			t.Errorf("%s: go packages = %v", f, p.GoPackages)
		}
	}
	p := compute("internal/dbx/tx.go")
	if !slices.Contains(p.GoPackages, "abidata/internal/pgdb") {
		t.Errorf("dbx: pgdb not selected: %v", p.GoPackages)
	}
	if p := compute("internal/pgdb/caps.go"); !p.Postgres || p.E2E() {
		t.Errorf("pgdb: postgres=%v e2e=%v", p.Postgres, p.E2EAreas)
	}
}

func TestEmbeddedContentBelongsToItsPackage(t *testing.T) {
	p := compute("docs/content/guide/forms.md")
	if want := pkgs("docs"); !slices.Equal(p.GoPackages, want) {
		t.Fatalf("go packages = %v, want %v", p.GoPackages, want)
	}
	if !slices.Equal(p.E2EAreas, []string{"shell"}) || !p.Image {
		t.Fatalf("areas = %v image = %v", p.E2EAreas, p.Image)
	}
}

func TestWebSourceIsNotTheWebGoPackage(t *testing.T) {
	// web/ is a Go package that embeds web/dist; web/src is not part of it.
	p := compute("web/src/features/forms/FormView.tsx")
	if p.Go() {
		t.Fatalf("web source selected Go packages %v", p.GoPackages)
	}
	if !p.WebStatic || p.WebAll || !slices.Equal(p.WebFiles, []string{"src/features/forms/FormView.tsx"}) {
		t.Fatalf("web = %v %v %v", p.WebStatic, p.WebAll, p.WebFiles)
	}
	if want := []string{"a11y", "apps", "files-geo", "forms"}; !slices.Equal(p.E2EAreas, want) {
		t.Fatalf("areas = %v, want %v", p.E2EAreas, want)
	}
}

func TestWebTestOnlyChange(t *testing.T) {
	p := compute("web/src/features/forms/expr.test.ts")
	if !slices.Equal(p.WebFiles, []string{"src/features/forms/expr.test.ts"}) || p.E2E() || p.Image {
		t.Fatalf("plan = %+v", p)
	}
}

func TestDeletedWebFileRunsAllWebTests(t *testing.T) {
	p := compute("-web/src/lib/old.ts")
	if !p.WebAll || p.WebFiles != nil {
		t.Fatalf("web all = %v files = %v", p.WebAll, p.WebFiles)
	}
}

func TestWebConfigRunsAllWebAndE2E(t *testing.T) {
	for _, f := range []string{"web/package.json", "web/package-lock.json", "web/vite.config.ts", "web/tsconfig.json", "web/vitest.setup.ts"} {
		p := compute(f)
		if !p.WebAll || !p.WebStatic || !p.E2EAll {
			t.Errorf("%s: web all = %v static = %v e2e all = %v", f, p.WebAll, p.WebStatic, p.E2EAll)
		}
	}
}

func TestSharedWebCodeRunsEveryArea(t *testing.T) {
	for _, f := range []string{"web/src/lib/api.ts", "web/src/features/nav/NavPane.tsx", "web/src/components/Toasts.tsx", "web/src/generated/api.d.ts"} {
		if p := compute(f); !p.E2EAll {
			t.Errorf("%s: areas = %v, want all", f, p.E2EAreas)
		}
	}
	if p := compute("web/src/lib/geo/wkt.ts"); !slices.Equal(p.E2EAreas, []string{"files-geo"}) {
		t.Errorf("lib/geo: areas = %v", p.E2EAreas)
	}
}

func TestSpecSelectsItsArea(t *testing.T) {
	p := compute("tests/e2e/forms-runtime.spec.ts")
	if !slices.Equal(p.E2EAreas, []string{"forms"}) || p.Go() || p.Web() || p.Image {
		t.Fatalf("plan = %+v", p)
	}
}

func TestHelperSelectsTheSpecsThatImportIt(t *testing.T) {
	p := compute("tests/e2e/queries-helpers.ts")
	if want := []string{"automation", "queries"}; !slices.Equal(p.E2EAreas, want) {
		t.Fatalf("areas = %v, want %v", p.E2EAreas, want)
	}
	p = compute("tests/e2e/helpers.ts")
	if want := []string{"automation", "files-geo", "forms", "queries"}; !slices.Equal(p.E2EAreas, want) {
		t.Fatalf("areas = %v, want %v", p.E2EAreas, want)
	}
	// Nothing in the scan imports it: run everything rather than trust the scan.
	if p := compute("tests/e2e/orphan-helpers.ts"); !p.E2EAll {
		t.Fatalf("orphan helper: areas = %v", p.E2EAreas)
	}
}

func TestE2EInfrastructureRunsEveryArea(t *testing.T) {
	for _, f := range []string{"tests/e2e/server.mjs", "tests/e2e/playwright.config.ts", "tests/e2e/package-lock.json"} {
		if p := compute(f); !p.E2EAll || p.Go() || p.Image {
			t.Errorf("%s: plan = %+v", f, p)
		}
	}
}

func TestUnknownSpecRunsEveryArea(t *testing.T) {
	if p := compute("tests/e2e/brand-new.spec.ts"); !p.E2EAll {
		t.Fatalf("areas = %v", p.E2EAreas)
	}
}

func TestContainerSmokeFiles(t *testing.T) {
	for _, f := range []string{"tests/e2e/smoke.spec.ts", "tests/e2e/compose.smoke.yaml", "Dockerfile", "compose.yaml", "scripts/container-smoke.sh"} {
		p := compute(f)
		if !p.Image || p.E2E() || p.Go() || p.Web() {
			t.Errorf("%s: plan = %+v", f, p)
		}
	}
}

func TestPostgresScript(t *testing.T) {
	if p := compute("scripts/test-postgres.sh"); !p.Postgres || p.Go() || p.E2E() {
		t.Fatalf("plan = %+v", p)
	}
}

func TestCrossCuttingFilesSelectEverything(t *testing.T) {
	for _, f := range []string{"go.mod", "go.sum", "Makefile", ".github/workflows/ci.yml", "tools/testplan/plan.go", "third_party/gopher-lua/state.go"} {
		p := compute(f, "README.md")
		if !p.Full || !p.GoAll || !p.WebAll || !p.E2EAll || !p.Image || !p.Postgres || !p.GoLint || !p.WebStatic {
			t.Errorf("%s: plan = %+v", f, p)
		}
	}
}

func TestUnmappedSourceRunsEveryArea(t *testing.T) {
	// A Go file in a directory go list did not report (a new or removed package).
	p := compute("internal/brandnew/x.go")
	if !p.GoAll || !p.E2EAll || len(p.GoPackages) != len(fakeRepo().Packages) {
		t.Fatalf("plan = %+v", p)
	}
}

func TestLongestPrefixWins(t *testing.T) {
	if p := compute("internal/httpapi/routes_admin_overview.go"); !slices.Equal(p.E2EAreas, []string{"admin"}) {
		t.Errorf("routes_admin_overview: %v", p.E2EAreas)
	}
	if p := compute("internal/httpapi/routes_admin.go"); !slices.Equal(p.E2EAreas, []string{"admin", "apps", "auth", "files-geo"}) {
		t.Errorf("routes_admin: %v", p.E2EAreas)
	}
	if p := compute("internal/httpapi/server.go"); !p.E2EAll {
		t.Errorf("server.go: %v", p.E2EAreas)
	}
}

func TestOpenAPIDocumentIsWebStaticOnly(t *testing.T) {
	p := compute("internal/httpapi/openapi.json")
	if !p.WebStatic || p.E2E() {
		t.Fatalf("plan = %+v", p)
	}
	// It sits in the httpapi directory, so that package and its importers are tested.
	if want := pkgs("internal/httpapi", "cmd/abidata"); !slices.Equal(p.GoPackages, want) {
		t.Fatalf("go packages = %v, want %v", p.GoPackages, want)
	}
}

func TestSpecArgsAreAnchored(t *testing.T) {
	got := specArgs([]string{"forms"})
	want := []string{`[\\/]forms-designer\.spec\.ts$`, `[\\/]forms-runtime\.spec\.ts$`, `[\\/]forms-runtime-live\.spec\.ts$`}
	if !slices.Equal(got, want) {
		t.Fatalf("specArgs = %v, want %v", got, want)
	}
}

// The tests below check the area table against the real checkout.

const repoRootRel = "../.."

func TestEverySpecHasOneArea(t *testing.T) {
	entries, err := os.ReadDir(filepath.Join(repoRootRel, "tests", "e2e"))
	if err != nil {
		t.Fatal(err)
	}
	onDisk := map[string]bool{}
	for _, e := range entries {
		if spec, ok := strings.CutSuffix(e.Name(), ".spec.ts"); ok && spec != "smoke" {
			onDisk[spec] = true
		}
	}
	listed := map[string]string{}
	for _, a := range areas {
		for _, s := range a.Specs {
			if prev, ok := listed[s]; ok {
				t.Errorf("%s.spec.ts is in both %s and %s", s, prev, a.Name)
			}
			listed[s] = a.Name
			if !onDisk[s] {
				t.Errorf("area %s lists %s.spec.ts, which does not exist", a.Name, s)
			}
		}
	}
	for s := range onDisk {
		if _, ok := listed[s]; !ok {
			t.Errorf("tests/e2e/%s.spec.ts is in no area: add it to areas in tools/testplan/areas.go", s)
		}
	}
}

// pendingPrefixes are mapped ahead of branches that add them; drop an entry once it exists.
var pendingPrefixes = map[string]bool{}

func TestSourceAreasPointAtRealPathsAndAreas(t *testing.T) {
	names := areaNames()
	for pre, as := range sourceAreas {
		matches, _ := filepath.Glob(filepath.Join(repoRootRel, filepath.FromSlash(pre)) + "*")
		if len(matches) == 0 && !pendingPrefixes[pre] {
			t.Errorf("sourceAreas prefix %q matches nothing (renamed?)", pre)
		}
		for _, a := range as {
			if a != "*" && !slices.Contains(names, a) {
				t.Errorf("sourceAreas[%q] names unknown area %q", pre, a)
			}
		}
	}
}

func TestLoadRealRepo(t *testing.T) {
	root, err := filepath.Abs(repoRootRel)
	if err != nil {
		t.Fatal(err)
	}
	repo, err := loadRepo(root)
	if err != nil {
		t.Fatal(err)
	}
	cat, ok := repo.Packages["internal/catalog"]
	if !ok || cat.ImportPath != "abidata/internal/catalog" || !slices.Contains(cat.Embeds, "migrations/sqlite/") {
		t.Fatalf("internal/catalog = %+v", cat)
	}
	if !slices.Contains(cat.Deps, "abidata/internal/dbx") {
		t.Errorf("catalog deps = %v, want dbx among them", cat.Deps)
	}
	if web := repo.Packages["web"]; !slices.Contains(web.Embeds, "dist") {
		t.Errorf("web embeds = %v", web.Embeds)
	}
	if h := repo.Specs["forms-runtime"]; !slices.Contains(h, "helpers.ts") || !slices.Contains(h, "forms-runtime-helpers.ts") {
		t.Errorf("forms-runtime helpers = %v", h)
	}
	// A change to dbx reaches the binary through the real graph.
	p := Compute([]Change{{Path: "internal/dbx/dbx.go"}}, repo)
	if !slices.Contains(p.GoPackages, "abidata/cmd/abidata") {
		t.Errorf("dbx change misses cmd/abidata: %v", p.GoPackages)
	}
}
