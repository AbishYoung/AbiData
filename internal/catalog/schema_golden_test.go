package catalog

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// maskIDs replaces ULIDs (catalog IDs in constraint and trigger names and in
// trigger bodies) so a schema can be compared across runs.
var maskIDs = regexp.MustCompile(`[0-9A-HJKMNP-TV-Za-hjkmnp-tv-z]{26}`)

// TestSchemaGolden pins the DDL the catalog generates, byte for byte, for every
// field type, an index, a lookup, attachments triggers, added and renamed
// columns, a renamed table and a rebuild. Existing workspaces hold this text in
// sqlite_schema, so a renderer change that alters it is a schema change.
// Regenerate with ABIDATA_UPDATE_GOLDEN=1 only for an intended change.
func TestSchemaGolden(t *testing.T) {
	f := newFixture(t)
	f.apply(allTypesTable())
	cust, _ := f.schema().TableByPhysical("Customers")
	name, _ := cust.ColumnByPhysical("Name")
	f.apply(Op{Kind: OpCreateIndex, Index: &IndexSpec{TableID: cust.ID, Unique: true, ColumnIDs: []string{name.ID}}})
	f.apply(Op{Kind: OpCreateTable, Table: &TableSpec{DisplayName: "Sites", Columns: []ColumnSpec{
		{DisplayName: "Customer", Type: Lookup, Lookup: &LookupSpec{ParentTableID: cust.ID}},
		{DisplayName: "Where", Type: GeoPoint},
		{DisplayName: "Area", Type: GeoArea},
		{DisplayName: "Photos", Type: Attachments},
		{DisplayName: "Next", Type: Calculated, Options: TypeOptions{Expression: "Customer + 1", ResultType: "integer"}},
	}}})
	sites, _ := f.schema().TableByPhysical("Sites")
	f.apply(Op{Kind: OpAddColumn, TableID: sites.ID, Column: &ColumnSpec{DisplayName: "Docs", Type: Attachments}})
	// Required with a validation rule cannot be added in place: a rebuild.
	f.apply(Op{Kind: OpAddColumn, TableID: sites.ID, Column: &ColumnSpec{DisplayName: "Code", Type: ShortText, Required: true,
		DefaultSQL: "'x'", ValidationSQL: "length(Code) > 0"}})
	f.apply(Op{Kind: OpRenameTable, TableID: sites.ID, DisplayName: "Places", PhysicalName: "Places"})
	f.apply(Op{Kind: OpRenameColumn, TableID: sites.ID, ColumnID: sites.Columns[2].ID, DisplayName: "Location", PhysicalName: "Location"})
	// A type conversion with data: a rebuild through abidata_convert.
	notes, _ := cust.ColumnByPhysical("Notes")
	f.exec(`INSERT INTO Customers(Name, Notes) VALUES ('Ada', '12'), ('Bob', 'x')`)
	f.apply(Op{Kind: OpAlterColumn, TableID: cust.ID, ColumnID: notes.ID, Column: &ColumnSpec{DisplayName: "Notes", Type: ShortText,
		Required: true, DefaultSQL: "'n'"}})

	var lines []string
	for _, r := range f.query(`SELECT type, name, tbl_name, sql FROM sqlite_schema
		WHERE tbl_name NOT LIKE '\_sys\_%' ESCAPE '\' AND tbl_name NOT LIKE 'sqlite\_%' ESCAPE '\'`) {
		lines = append(lines, maskIDs.ReplaceAllString(fmt.Sprintln(r...), "<ID>"))
	}
	sort.Strings(lines)
	got := strings.Join(lines, "")
	path := filepath.Join("testdata", "schema_all_types.golden")
	if os.Getenv("ABIDATA_UPDATE_GOLDEN") != "" {
		if err := os.WriteFile(path, []byte(got), 0o600); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if got != string(want) {
		t.Errorf("generated schema differs from %s:\n got:\n%s\nwant:\n%s", path, got, want)
	}
}
