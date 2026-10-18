// Command testplan selects and runs the tests a change needs (ADR 0016).
//
//	testplan plan [-base origin/main] [-json] [-github-output FILE]   print the selection
//	testplan run  [-base origin/main] [-layers static,go,web,e2e] [-go-flags -short] [-dry]
//	testplan e2e  [AREA...] [-- PLAYWRIGHT ARGS]                       run e2e areas (all by default)
//	testplan areas [-json]                                            list the e2e areas
//
// The changes are the files that differ between the merge base of -base and HEAD and the
// working tree, untracked files included. -full ignores them and selects everything.
package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
)

func main() {
	if len(os.Args) < 2 {
		usage()
	}
	root, err := repoRoot()
	if err != nil {
		fatal(err)
	}
	cmd, args := os.Args[1], os.Args[2:]
	switch cmd {
	case "plan":
		err = cmdPlan(root, args)
	case "run":
		err = cmdRun(root, args)
	case "e2e":
		err = cmdE2E(root, args)
	case "areas":
		err = cmdAreas(args)
	default:
		usage()
	}
	if err != nil {
		fatal(err)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, "usage: testplan plan|run|e2e|areas [flags]; see the package comment or docs/adr/0016-layered-testing.md")
	os.Exit(2)
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "testplan:", err)
	os.Exit(1)
}

func repoRoot() (string, error) {
	out, err := output(".", "git", "rev-parse", "--show-toplevel")
	return strings.TrimSpace(out), err
}

type selection struct {
	base, paths string
	full        bool
}

func (s *selection) flags(fs *flag.FlagSet) {
	fs.StringVar(&s.base, "base", "origin/main", "compare against the merge base with this ref")
	fs.StringVar(&s.paths, "paths", "", "read the changed paths from this file (- for stdin) instead of git")
	fs.BoolVar(&s.full, "full", false, "select everything")
}

func (s *selection) plan(root string) (Plan, error) {
	if s.full {
		return fullPlan([]string{"-full"}), nil
	}
	var changes []Change
	var err error
	if s.paths != "" {
		changes, err = readPaths(root, s.paths)
	} else {
		changes, err = changedPaths(root, s.base)
	}
	if err != nil {
		return Plan{}, err
	}
	repo, err := loadRepo(root)
	if err != nil {
		return Plan{}, err
	}
	return Compute(changes, repo), nil
}

// readPaths reads one path per line; a path that no longer exists counts as deleted.
func readPaths(root, file string) ([]Change, error) {
	f := os.Stdin
	if file != "-" {
		var err error
		if f, err = os.Open(file); err != nil {
			return nil, err
		}
		defer f.Close()
	}
	var out []Change
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		p := strings.TrimSpace(sc.Text())
		if p == "" {
			continue
		}
		_, err := os.Stat(filepath.Join(root, p))
		out = append(out, Change{Path: p, Deleted: os.IsNotExist(err)})
	}
	return out, sc.Err()
}

func cmdPlan(root string, args []string) error {
	fs := flag.NewFlagSet("plan", flag.ExitOnError)
	var sel selection
	sel.flags(fs)
	asJSON := fs.Bool("json", false, "print the plan as JSON")
	ghOut := fs.String("github-output", "", "append the plan as GitHub Actions step outputs to this file")
	_ = fs.Parse(args)
	p, err := sel.plan(root)
	if err != nil {
		return err
	}
	if *ghOut != "" {
		if err := writeGitHubOutput(*ghOut, p); err != nil {
			return err
		}
	}
	if *asJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(p)
	}
	describe(os.Stdout, p)
	return nil
}

func writeGitHubOutput(file string, p Plan) error {
	areasJSON, _ := json.Marshal(p.E2EAreas)
	f, err := os.OpenFile(file, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	pkgs := strings.Join(p.GoPackages, " ")
	if p.GoAll {
		pkgs = "./..."
	}
	_, err = fmt.Fprintf(f, "full=%t\ngo=%t\ngo_packages=%s\ngo_lint=%t\nweb=%t\nweb_static=%t\ne2e=%t\ne2e_areas=%s\nimage=%t\npostgres=%t\n",
		p.Full, p.Go(), pkgs, p.GoLint, p.Web(), p.WebStatic, p.E2E(), areasJSON, p.Image, p.Postgres)
	return err
}

func describe(w *os.File, p Plan) {
	line := func(name string, on bool, detail string) {
		state := "skip"
		if on {
			state = "run "
		}
		fmt.Fprintf(w, "%-9s %s %s\n", name, state, detail)
	}
	for i, r := range p.Reasons {
		if i == 8 {
			fmt.Fprintf(w, "why: ... and %d more\n", len(p.Reasons)-i)
			break
		}
		fmt.Fprintln(w, "why:", r)
	}
	line("go lint", p.GoLint, strings.Join(p.GoLintDirs, " "))
	line("web lint", p.WebStatic, "typecheck + eslint")
	goDetail := fmt.Sprintf("%d packages", len(p.GoPackages))
	if p.GoAll {
		goDetail = "all packages"
	}
	line("go", p.Go(), goDetail)
	webDetail := fmt.Sprintf("related to %d files", len(p.WebFiles))
	if p.WebAll {
		webDetail = "all files"
	}
	line("vitest", p.Web(), webDetail)
	line("e2e", p.E2E(), strings.Join(p.E2EAreas, " "))
	line("postgres", p.Postgres, "make test-postgres (CI)")
	line("image", p.Image, "docker build + container smoke (CI)")
}

func cmdRun(root string, args []string) error {
	fs := flag.NewFlagSet("run", flag.ExitOnError)
	var sel selection
	sel.flags(fs)
	layers := fs.String("layers", "static,go,web,e2e", "comma-separated layers to run")
	goFlags := fs.String("go-flags", "-short", "flags for go test")
	dry := fs.Bool("dry", false, "print the commands instead of running them")
	_ = fs.Parse(args)
	p, err := sel.plan(root)
	if err != nil {
		return err
	}
	describe(os.Stdout, p)
	want := strings.Split(*layers, ",")
	var steps []step
	if slices.Contains(want, "static") {
		steps = append(steps, staticSteps(root, p)...)
	}
	if slices.Contains(want, "go") && p.Go() {
		args := append([]string{"test"}, strings.Fields(*goFlags)...)
		if p.GoAll {
			args = append(args, "./...")
		} else {
			args = append(args, p.GoPackages...)
		}
		steps = append(steps, step{root, "go", args})
	}
	if slices.Contains(want, "web") && p.Web() {
		web := filepath.Join(root, "web")
		if p.WebAll {
			steps = append(steps, step{web, "npx", []string{"vitest", "run"}})
		} else {
			steps = append(steps, step{web, "npx", append([]string{"vitest", "related", "--run", "--passWithNoTests"}, p.WebFiles...)})
		}
	}
	if slices.Contains(want, "e2e") && p.E2E() {
		steps = append(steps, step{filepath.Join(root, "tests", "e2e"), "npx", append([]string{"playwright", "test"}, specArgs(p.E2EAreas)...)})
	}
	if len(steps) == 0 {
		fmt.Println("nothing to run")
	}
	for _, s := range steps {
		if err := s.run(*dry); err != nil {
			return err
		}
	}
	return nil
}

func staticSteps(root string, p Plan) []step {
	var out []step
	if p.GoLint {
		if _, err := exec.LookPath("golangci-lint"); err != nil {
			fmt.Println("golangci-lint not found: skipping the Go lint (CI runs it)")
		} else {
			args := []string{"run"}
			for _, d := range p.GoLintDirs {
				if d == "." {
					args = append(args, "./...")
				} else {
					args = append(args, "./"+d)
				}
			}
			out = append(out, step{root, "golangci-lint", args})
		}
	}
	if p.WebStatic {
		web := filepath.Join(root, "web")
		out = append(out, step{web, "npm", []string{"run", "--silent", "typecheck"}}, step{web, "npm", []string{"run", "--silent", "lint"}})
	}
	return out
}

func cmdE2E(root string, args []string) error {
	var names, extra []string
	if i := slices.Index(args, "--"); i >= 0 {
		names, extra = args[:i], args[i+1:]
	} else {
		names = args
	}
	for _, n := range names {
		if !slices.Contains(areaNames(), n) {
			return fmt.Errorf("unknown e2e area %q; known: %s", n, strings.Join(areaNames(), " "))
		}
	}
	if len(names) == 0 {
		names = areaNames()
	}
	s := step{filepath.Join(root, "tests", "e2e"), "npx", slices.Concat([]string{"playwright", "test"}, specArgs(names), extra)}
	return s.run(false)
}

// specArgs turns areas into Playwright file filters, which are regular expressions.
func specArgs(names []string) []string {
	var out []string
	for _, a := range areas {
		if !slices.Contains(names, a.Name) {
			continue
		}
		for _, s := range a.Specs {
			out = append(out, `[\\/]`+regexp.QuoteMeta(s+".spec.ts")+"$")
		}
	}
	return out
}

func cmdAreas(args []string) error {
	fs := flag.NewFlagSet("areas", flag.ExitOnError)
	asJSON := fs.Bool("json", false, "print the area names as a JSON array")
	_ = fs.Parse(args)
	if *asJSON {
		return json.NewEncoder(os.Stdout).Encode(areaNames())
	}
	for _, a := range areas {
		fmt.Printf("%-11s %s\n", a.Name, strings.Join(a.Specs, " "))
	}
	return nil
}

type step struct {
	dir, name string
	args      []string
}

func (s step) run(dry bool) error {
	where := ""
	if root, err := repoRoot(); err == nil && root != s.dir {
		if rel, err := filepath.Rel(root, s.dir); err == nil {
			where = "(cd " + rel + ") "
		}
	}
	fmt.Printf("+ %s%s %s\n", where, s.name, strings.Join(s.args, " "))
	if dry {
		return nil
	}
	cmd := exec.Command(s.name, s.args...)
	cmd.Dir = s.dir
	cmd.Stdout, cmd.Stderr, cmd.Stdin = os.Stdout, os.Stderr, os.Stdin
	return cmd.Run()
}
