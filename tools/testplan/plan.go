package main

import (
	"path"
	"slices"
	"sort"
	"strings"
)

// Change is one changed path, relative to the repository root with forward slashes.
type Change struct {
	Path    string
	Deleted bool
}

// Repo is what the plan needs to know about the checkout. loadRepo fills it from `go list`
// and tests/e2e; the tests build it by hand.
type Repo struct {
	// Packages maps a package directory ("internal/forms") to the package.
	Packages map[string]Package
	// Specs maps a spec name ("forms-runtime") to the tests/e2e files it imports, directly
	// or through other helpers ("forms-runtime-helpers.ts", "helpers.ts").
	Specs map[string][]string
}

type Package struct {
	ImportPath string
	// Deps are the module packages this package or its tests import directly.
	Deps []string
	// Embeds are the //go:embed patterns of the package and its tests, up to the first
	// wildcard and relative to the package directory ("migrations/sqlite/", "dist").
	Embeds []string
}

// Plan is the set of checks a change needs (ADR 0016). The *All fields win over the lists.
type Plan struct {
	Full    bool     `json:"full"`
	Reasons []string `json:"reasons"`

	GoLint      bool     `json:"go_lint"`
	GoLintDirs  []string `json:"go_lint_dirs"`
	GoAll       bool     `json:"go_all"`
	GoPackages  []string `json:"go_packages"`
	WebStatic   bool     `json:"web_static"`
	WebAll      bool     `json:"web_all"`
	WebFiles    []string `json:"web_files"` // relative to web/, for `vitest related`
	E2EAll      bool     `json:"e2e_all"`
	E2EAreas    []string `json:"e2e_areas"`
	Image       bool     `json:"image"`    // docker build and the container smoke
	Postgres    bool     `json:"postgres"` // the PostgreSQL backend tests
	unknownSpec bool
}

// Go reports whether any Go tests are needed.
func (p *Plan) Go() bool { return p.GoAll || len(p.GoPackages) > 0 }

// Web reports whether any Vitest tests are needed.
func (p *Plan) Web() bool { return p.WebAll || len(p.WebFiles) > 0 }

// E2E reports whether any Playwright area is needed.
func (p *Plan) E2E() bool { return p.E2EAll || len(p.E2EAreas) > 0 }

// fullPaths change how everything is built or tested, so they select every layer.
var fullPaths = []string{".github/workflows/", "Makefile", "tools/testplan/", "go.mod", "go.sum", "third_party/"}

// webConfig are the files that configure the web build, the type checker, the linter or
// Vitest itself. A change runs every web test and every e2e area.
var webConfig = []string{"web/package.json", "web/package-lock.json", "web/vite.config.ts", "web/vitest.setup.ts", "web/tsconfig", "web/eslint.config.js"}

// e2eConfig are the tests/e2e files every spec depends on.
var e2eConfig = []string{"tests/e2e/package.json", "tests/e2e/package-lock.json", "tests/e2e/playwright.config.ts", "tests/e2e/server.mjs", "tests/e2e/tsconfig"}

// imagePaths only affect the Docker image and the container smoke.
var imagePaths = []string{"Dockerfile", ".dockerignore", "compose.yaml", "compose.dev.yaml", "Caddyfile", "secrets/", "scripts/container-smoke.sh", "scripts/docker-", "tests/e2e/smoke.spec.ts", "tests/e2e/compose.smoke.yaml"}

// postgresPaths run the PostgreSQL backend tests on top of whatever the Go graph selects:
// the SQL layer that both backends share, and the test script. Migrations too (below).
var postgresPaths = []string{"scripts/test-postgres.sh", "internal/pgdb/", "internal/pgtest/", "internal/dbx/", "internal/sqlgen/",
	"cmd/abidata/backend"}

// postgresPackages have tests that run against PostgreSQL; with
// cmd/abidata's startup checks (a path above, since every package is its
// dependency), they are the packages scripts/test-postgres.sh runs.
var postgresPackages = []string{"abidata/internal/pgdb", "abidata/internal/admin", "abidata/internal/auth", "abidata/internal/catalog", "abidata/internal/workspace", "abidata/internal/records", "abidata/internal/query", "abidata/internal/reports", "abidata/internal/script/modules/db", "abidata/internal/files", "abidata/internal/forms", "abidata/internal/jobs", "abidata/internal/script/modules/geo", "abidata/internal/script/modules/all", "abidata/internal/ops", "abidata/internal/ingest", "abidata/internal/demo"}

// inert paths are documentation and tests: they never change what is built or shipped.
func inert(p string) bool {
	switch {
	case testFile(p):
		return true
	case strings.HasPrefix(p, "tests/e2e/") && !hasAnyPrefix(p, imagePaths):
		return true
	case strings.HasPrefix(p, "docs/adr/"), strings.HasPrefix(p, "docs/images/"):
		return true
	case strings.HasSuffix(p, ".md") && !strings.HasPrefix(p, "docs/content/"):
		return true
	case p == "LICENSE", strings.HasPrefix(p, ".claude/"), p == ".gitignore":
		return true
	}
	return false
}

// testFile reports whether p is test code or test data, which no other package or spec sees.
func testFile(p string) bool {
	base := path.Base(p)
	return strings.HasSuffix(base, "_test.go") || strings.Contains(p, "/testdata/") ||
		strings.HasSuffix(base, ".test.ts") || strings.HasSuffix(base, ".test.tsx")
}

func hasAnyPrefix(p string, prefixes []string) bool {
	for _, pre := range prefixes {
		if strings.HasPrefix(p, pre) {
			return true
		}
	}
	return false
}

// Compute works out the plan for a set of changes. When in doubt it selects more.
func Compute(changes []Change, repo Repo) Plan {
	var p Plan
	var fullReasons []string
	changedPkgs := map[string]bool{} // their reverse dependencies are affected too
	testedPkgs := map[string]bool{}  // only test files changed: just the package itself
	lintDirs := map[string]bool{}
	areaSet := map[string]bool{}
	webFiles := map[string]bool{}
	reason := func(s string) {
		if !slices.Contains(p.Reasons, s) {
			p.Reasons = append(p.Reasons, s)
		}
	}

	for _, c := range changes {
		f := c.Path
		if hasAnyPrefix(f, fullPaths) {
			p.Full = true
			fullReasons = append(fullReasons, f+" affects every layer")
			continue
		}
		if !inert(f) {
			p.Image = true
		}
		if hasAnyPrefix(f, postgresPaths) || strings.Contains(f, "/migrations/") {
			p.Postgres = true
		}
		if f == ".golangci.yml" || f == ".golangci.yaml" {
			p.GoLint = true
			lintDirs["."] = true
		}

		// Go: the package that holds the file (source, embedded asset, migration or test data).
		if dir, ok := packageDir(f, repo); ok {
			ip := repo.Packages[dir].ImportPath
			if testFile(f) {
				testedPkgs[ip] = true
			} else {
				changedPkgs[ip] = true
			}
			if strings.HasSuffix(f, ".go") {
				p.GoLint = true
				lintDirs[dir] = true
			}
		} else if strings.HasSuffix(f, ".go") {
			// A Go file outside any package: a package was removed or added outside
			// `go list ./...`. Test everything rather than guess.
			p.GoAll = true
			p.GoLint = true
			lintDirs["."] = true
			reason(f + " is not in a known Go package")
		}

		// Web.
		switch {
		case hasAnyPrefix(f, webConfig):
			p.WebAll, p.WebStatic = true, true
			reason(f + " configures the web build or tests")
		case strings.HasPrefix(f, "web/src/") && strings.HasSuffix(f, ".md"):
			// Design notes next to the code.
		case strings.HasPrefix(f, "web/src/"):
			p.WebStatic = true
			if c.Deleted {
				// Nothing imports a deleted file any more, so `vitest related` cannot find
				// the tests that used it.
				p.WebAll = true
				reason(f + " was deleted")
			} else {
				webFiles[strings.TrimPrefix(f, "web/")] = true
			}
		case strings.HasPrefix(f, "web/scripts/"), strings.HasPrefix(f, "web/public/"), f == "web/index.html",
			f == "internal/httpapi/openapi.json":
			p.WebStatic = true
		}

		// End to end.
		for _, a := range e2eAreasFor(c, repo, &p) {
			if a == "*" {
				p.E2EAll = true
				reason(f + " runs every e2e area")
			} else {
				areaSet[a] = true
			}
		}
	}

	if p.Full {
		return fullPlan(fullReasons)
	}

	affected := reverseClosure(changedPkgs, repo)
	for ip := range testedPkgs {
		affected[ip] = true
	}
	if p.GoAll {
		affected = map[string]bool{}
		for _, pkg := range repo.Packages {
			affected[pkg.ImportPath] = true
		}
	}
	for _, pkg := range postgresPackages {
		if affected[pkg] {
			p.Postgres = true
		}
	}
	p.GoPackages = sortedKeys(affected)
	p.GoLintDirs = sortedKeys(lintDirs)
	if lintDirs["."] {
		p.GoLintDirs = []string{"."}
	}
	if !p.WebAll {
		p.WebFiles = sortedKeys(webFiles)
	}
	if p.E2EAll || p.unknownSpec {
		p.E2EAll = true
		p.E2EAreas = areaNames()
	} else {
		p.E2EAreas = sortedKeys(areaSet)
	}
	return p
}

func fullPlan(reasons []string) Plan {
	return Plan{
		Full: true, Reasons: reasons,
		GoLint: true, GoLintDirs: []string{"."}, GoAll: true,
		WebStatic: true, WebAll: true,
		E2EAll: true, E2EAreas: areaNames(),
		Image: true, Postgres: true,
	}
}

// packageDir finds the Go package that f belongs to. A Go file belongs to the package in its
// directory. Any other file belongs to the nearest package above it when it sits in that
// package's directory, under its testdata, or under one of its //go:embed patterns
// (migrations, embedded content); web/src, for instance, is not part of the web package,
// which embeds only web/dist.
func packageDir(f string, repo Repo) (string, bool) {
	dir := path.Dir(f)
	if strings.HasSuffix(f, ".go") {
		_, ok := repo.Packages[dir]
		return dir, ok
	}
	for d := dir; d != "." && d != "/"; d = path.Dir(d) {
		pkg, ok := repo.Packages[d]
		if !ok {
			continue
		}
		rel := strings.TrimPrefix(f, d+"/")
		if d == dir || strings.HasPrefix(rel, "testdata/") || strings.Contains(rel, "/testdata/") {
			return d, true
		}
		for _, e := range pkg.Embeds {
			if strings.HasPrefix(rel, e) {
				return d, true
			}
		}
		return "", false
	}
	return "", false
}

// reverseClosure returns the changed packages plus every module package that imports one of
// them, directly or not, from its code or its tests.
func reverseClosure(changed map[string]bool, repo Repo) map[string]bool {
	importers := map[string][]string{}
	for _, pkg := range repo.Packages {
		for _, d := range pkg.Deps {
			importers[d] = append(importers[d], pkg.ImportPath)
		}
	}
	out := map[string]bool{}
	queue := make([]string, 0, len(changed))
	for ip := range changed {
		queue = append(queue, ip)
	}
	for len(queue) > 0 {
		ip := queue[0]
		queue = queue[1:]
		if out[ip] {
			continue
		}
		out[ip] = true
		queue = append(queue, importers[ip]...)
	}
	return out
}

// e2eAreasFor returns the areas a change selects; "*" means all of them.
func e2eAreasFor(c Change, repo Repo, p *Plan) []string {
	f := c.Path
	if testFile(f) {
		return nil
	}
	if strings.HasPrefix(f, "tests/e2e/") {
		name := strings.TrimPrefix(f, "tests/e2e/")
		switch {
		case hasAnyPrefix(f, e2eConfig):
			return all
		case hasAnyPrefix(f, imagePaths), strings.Contains(name, "/"):
			return nil // the container smoke, or test output and fixtures outside the suite
		case strings.HasSuffix(name, ".spec.ts"):
			spec := strings.TrimSuffix(name, ".spec.ts")
			if a := areaOf(spec); a != "" {
				return []string{a}
			}
			// A spec no area lists: run everything, so it runs too.
			p.unknownSpec = true
			return all
		case strings.HasSuffix(name, ".ts"), strings.HasSuffix(name, ".mjs"):
			var out []string
			for spec, helpers := range repo.Specs {
				if slices.Contains(helpers, name) {
					if a := areaOf(spec); a != "" {
						out = append(out, a)
					} else {
						p.unknownSpec = true
					}
				}
			}
			if len(out) == 0 {
				return all // a helper nothing seems to import: do not trust the import scan
			}
			return out
		}
		return nil
	}
	best := ""
	for pre := range sourceAreas {
		if strings.HasPrefix(f, pre) && len(pre) > len(best) {
			best = pre
		}
	}
	if best != "" {
		return sourceAreas[best]
	}
	if inert(f) {
		return nil
	}
	if hasAnyPrefix(f, sourceTrees) {
		return all
	}
	return nil
}

func areaOf(spec string) string {
	for _, a := range areas {
		if slices.Contains(a.Specs, spec) {
			return a.Name
		}
	}
	return ""
}

func areaNames() []string {
	out := make([]string, len(areas))
	for i, a := range areas {
		out[i] = a.Name
	}
	return out
}

func sortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
