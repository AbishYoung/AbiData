package catalog

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"abidata/internal/sqlitex"
)

func FuzzIdentifier(f *testing.F) {
	for _, s := range []string{"Customer Name", "select", "1st place", "_sys_x", `a"b`, "", "   ", "ÅÄÖ", "id", "_version",
		"x; DROP TABLE t; --", "a]b", "`x`", strings.Repeat("y", 80), "sqlite_master", "Order", "Field_", "__", "a\x00b"} {
		f.Add(s)
	}
	ctx := context.Background()
	c, err := sqlitex.Open(ctx, filepath.Join(f.TempDir(), "fz.db"), sqlitex.Options{Kind: sqlitex.Writer})
	if err != nil {
		f.Fatal(err)
	}
	f.Cleanup(func() { _ = c.Close() })
	f.Fuzz(func(t *testing.T, display string) {
		// A derived name always validates as a column name.
		d := DerivePhysicalName(display)
		if err := ValidateColumnName(d); err != nil {
			t.Fatalf("derived %q from %q is invalid: %v", d, display, err)
		}
		// Validated names never collide with internal namespaces.
		if ValidatePhysicalName(display) == nil {
			l := strings.ToLower(display)
			for _, p := range []string{"sqlite_", "_sys_", "_occ_", "_adm_"} {
				if strings.HasPrefix(l, p) {
					t.Fatalf("%q accepted with reserved prefix", display)
				}
			}
		}
		// Quote yields exactly one identifier whose name round-trips, for any input.
		if strings.ContainsRune(display, 0) {
			return
		}
		cols, err := c.Prepare(ctx, "SELECT 1 AS "+Quote(display))
		if err != nil {
			t.Fatalf("quoted %q failed to prepare: %v", display, err)
		}
		if len(cols) != 1 || cols[0] != display {
			t.Fatalf("quoted %q round-tripped as %v", display, cols)
		}
	})
}

func TestRenameIdent(t *testing.T) {
	cases := []struct{ in, old, new, want string }{
		{`Qty * Price`, "Qty", "Quantity", `"Quantity" * Price`},
		{`"Qty" > 0 AND qty < 10`, "Qty", "N", `"N" > 0 AND "N" < 10`},
		{`'Qty' || Qty`, "Qty", "N", `'Qty' || "N"`},
		{`length(Qty) > max(1, Qty2)`, "Qty", "N", `length("N") > max(1, Qty2)`},
		{`Qty(1)`, "Qty", "N", `Qty(1)`},
	}
	for _, c := range cases {
		if got := renameIdent(c.in, c.old, c.new); got != c.want {
			t.Errorf("renameIdent(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
