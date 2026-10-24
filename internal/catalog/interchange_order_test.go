package catalog

import (
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"testing"
)

// TestSortSysTablesParentsFirst: the import copies system tables in
// sortSysTables' order with foreign keys checked as each row goes in, so a
// table must come after every system table it references.
func TestSortSysTablesParentsFirst(t *testing.T) {
	files, err := filepath.Glob(filepath.Join("migrations", "sqlite", "*.sql"))
	if err != nil || len(files) == 0 {
		t.Fatalf("migrations: %v %v", files, err)
	}
	create := regexp.MustCompile(`(?i)CREATE TABLE (_sys_\w+)`)
	ref := regexp.MustCompile(`(?i)REFERENCES (_sys_\w+)`)
	refs := map[string][]string{}
	var tables []string
	for _, f := range files {
		b, err := os.ReadFile(f) //nolint:gosec // the package's own migrations
		if err != nil {
			t.Fatal(err)
		}
		// Split the script at each CREATE TABLE, so a REFERENCES belongs to
		// the table being created.
		locs := create.FindAllSubmatchIndex(b, -1)
		for i, l := range locs {
			name := string(b[l[2]:l[3]])
			end := len(b)
			if i+1 < len(locs) {
				end = locs[i+1][0]
			}
			if !slices.Contains(tables, name) {
				tables = append(tables, name)
			}
			for _, m := range ref.FindAllSubmatch(b[l[1]:end], -1) {
				refs[name] = append(refs[name], string(m[1]))
			}
		}
	}
	sorted := slices.Clone(tables)
	slices.Reverse(sorted) // the order the import is given must not matter
	sortSysTables(sorted)
	pos := map[string]int{}
	for i, n := range sorted {
		pos[n] = i
	}
	checked := 0
	for child, parents := range refs {
		for _, p := range parents {
			if p == child || child == "_sys_columns_new" {
				continue
			}
			checked++
			if pos[p] > pos[child] {
				t.Errorf("%s references %s but is copied before it: %v", child, p, sorted)
			}
		}
	}
	if checked < 5 {
		t.Fatalf("only %d references found", checked)
	}
}
