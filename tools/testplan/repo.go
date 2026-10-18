package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
)

// loadRepo reads the module's package graph with `go list` and the helper imports of the
// e2e specs. root is the repository root.
func loadRepo(root string) (Repo, error) {
	mod, err := output(root, "go", "list", "-m")
	if err != nil {
		return Repo{}, err
	}
	mod = strings.TrimSpace(mod)
	out, err := output(root, "go", "list", "-e",
		"-json=ImportPath,Dir,Imports,TestImports,XTestImports,EmbedPatterns,TestEmbedPatterns,XTestEmbedPatterns", "./...")
	if err != nil {
		return Repo{}, err
	}
	repo := Repo{Packages: map[string]Package{}}
	dec := json.NewDecoder(strings.NewReader(out))
	for {
		var p struct {
			ImportPath, Dir                                      string
			Imports, TestImports, XTestImports                   []string
			EmbedPatterns, TestEmbedPatterns, XTestEmbedPatterns []string
		}
		if err := dec.Decode(&p); errors.Is(err, io.EOF) {
			break
		} else if err != nil {
			return Repo{}, fmt.Errorf("go list: %w", err)
		}
		rel, err := filepath.Rel(root, p.Dir)
		if err != nil {
			return Repo{}, err
		}
		pkg := Package{ImportPath: p.ImportPath}
		for _, imp := range slices.Concat(p.Imports, p.TestImports, p.XTestImports) {
			if (imp == mod || strings.HasPrefix(imp, mod+"/")) && imp != p.ImportPath && !slices.Contains(pkg.Deps, imp) {
				pkg.Deps = append(pkg.Deps, imp)
			}
		}
		for _, e := range slices.Concat(p.EmbedPatterns, p.TestEmbedPatterns, p.XTestEmbedPatterns) {
			pkg.Embeds = append(pkg.Embeds, embedPrefix(e))
		}
		repo.Packages[filepath.ToSlash(rel)] = pkg
	}
	specs, err := specHelpers(filepath.Join(root, "tests", "e2e"))
	if err != nil {
		return Repo{}, err
	}
	repo.Specs = specs
	return repo, nil
}

// embedPrefix turns an embed pattern into the path prefix of the files it can match.
func embedPrefix(pattern string) string {
	pattern = strings.TrimPrefix(pattern, "all:")
	if i := strings.IndexAny(pattern, "*?[\\"); i >= 0 {
		pattern = pattern[:i]
	}
	return pattern
}

var localImport = regexp.MustCompile(`(?m)(?:from|import)\s*\(?\s*["']\./([^"']+)["']`)

// specHelpers maps every spec in dir to the local files it imports, transitively.
func specHelpers(dir string) (map[string][]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	imports := map[string][]string{}
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".ts") && !strings.HasSuffix(name, ".mjs") {
			continue
		}
		b, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			return nil, err
		}
		for _, m := range localImport.FindAllSubmatch(b, -1) {
			imp := string(m[1])
			if filepath.Ext(imp) == "" {
				imp += ".ts"
			}
			imports[name] = append(imports[name], imp)
		}
	}
	out := map[string][]string{}
	for name := range imports {
		if !strings.HasSuffix(name, ".spec.ts") {
			continue
		}
		seen := map[string]bool{}
		queue := slices.Clone(imports[name])
		for len(queue) > 0 {
			f := queue[0]
			queue = queue[1:]
			if seen[f] {
				continue
			}
			seen[f] = true
			queue = append(queue, imports[f]...)
		}
		out[strings.TrimSuffix(name, ".spec.ts")] = sortedKeys(seen)
	}
	// Specs that import nothing local still exist.
	for _, e := range entries {
		if spec, ok := strings.CutSuffix(e.Name(), ".spec.ts"); ok {
			if _, ok := out[spec]; !ok {
				out[spec] = nil
			}
		}
	}
	return out, nil
}

// changedPaths lists what differs between the merge base of base and HEAD and the working
// tree: committed, staged, unstaged and untracked (not ignored) files.
func changedPaths(root, base string) ([]Change, error) {
	mb, err := output(root, "git", "merge-base", base, "HEAD")
	if err != nil {
		return nil, fmt.Errorf("finding the merge base with %s (fetch it, or pass BASE=<ref>): %w", base, err)
	}
	diff, err := output(root, "git", "diff", "--name-status", "--no-renames", "-z", strings.TrimSpace(mb))
	if err != nil {
		return nil, err
	}
	var changes []Change
	seen := map[string]bool{}
	add := func(c Change) {
		if c.Path != "" && !seen[c.Path] {
			seen[c.Path] = true
			changes = append(changes, c)
		}
	}
	fields := strings.Split(strings.TrimSuffix(diff, "\x00"), "\x00")
	for i := 0; i+1 < len(fields); i += 2 {
		add(Change{Path: fields[i+1], Deleted: fields[i] == "D"})
	}
	untracked, err := output(root, "git", "ls-files", "--others", "--exclude-standard", "-z")
	if err != nil {
		return nil, err
	}
	for _, f := range strings.Split(untracked, "\x00") {
		add(Change{Path: f})
	}
	return changes, nil
}

func output(dir, name string, args ...string) (string, error) {
	cmd := exec.CommandContext(context.Background(), name, args...)
	cmd.Dir = dir
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("%s %s: %w: %s", name, strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return string(out), nil
}
