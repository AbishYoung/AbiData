package catalog

import (
	"strings"
	"testing"

	"abidata/internal/dbx"
	"abidata/internal/fault"
	"abidata/internal/workspace"
)

func TestExpressionsAreParsedBeforeDDL(t *testing.T) {
	f := newFixture(t)
	create := func(spec ColumnSpec) error {
		_, err := f.run(Apply, ApplyOptions{UserID: "u1"}, Op{Kind: OpCreateTable, Table: &TableSpec{DisplayName: "T" + spec.DisplayName,
			Columns: []ColumnSpec{{DisplayName: "Qty", Type: Integer}, spec}}})
		return err
	}
	for _, c := range []struct {
		spec ColumnSpec
		want string
	}{
		{ColumnSpec{DisplayName: "A", Type: Integer, ValidationSQL: "Qtty >= 0"}, `at character 1: there is no column named "Qtty"; did you mean "Qty"?`},
		{ColumnSpec{DisplayName: "B", Type: Integer, ValidationSQL: "CAST(Qty AS NUMERIC) > 0"}, "use REAL or INTEGER"},
		{ColumnSpec{DisplayName: "C", Type: Calculated, Options: TypeOptions{Expression: "random()", ResultType: "integer"}}, "gives a different value each time"},
		{ColumnSpec{DisplayName: "D", Type: Integer, DefaultSQL: "Qty + 1"}, "cannot refer to columns"},
		{ColumnSpec{DisplayName: "E", Type: Integer, ValidationSQL: "Qty > 0); DROP TABLE x; --"}, "';' is not allowed"},
		{ColumnSpec{DisplayName: "F", Type: DateTime, ValidationSQL: "F < datetime('now')"}, "only be used in a default value"},
	} {
		err := create(c.spec)
		if fault.Classify(err).Code != fault.BadRequest || !strings.Contains(fault.Classify(err).DisplayMessage(), c.want) {
			t.Errorf("%s: %v, want a bad request containing %q", c.spec.DisplayName, err, c.want)
		}
	}
	// A rule may refer to the field it is on, and a default may read the clock.
	if err := create(ColumnSpec{DisplayName: "G", Type: Integer, ValidationSQL: "g BETWEEN 0 AND qty", DefaultSQL: "0"}); err != nil {
		t.Fatal(err)
	}
	if err := create(ColumnSpec{DisplayName: "H", Type: DateTime, DefaultSQL: "strftime('%Y-%m-%dT%H:%M:%fZ', 'now')"}); err != nil {
		t.Fatal(err)
	}
	f.exec(`INSERT INTO TG(Qty, G) VALUES (5, 3)`)
	if err := f.l.Write(f.ctx, workspace.Interactive, func(tx dbx.Tx) error {
		_, err := tx.Exec(f.ctx, `INSERT INTO TG(Qty, G) VALUES (2, 3)`)
		return err
	}); err == nil {
		t.Fatal("the parsed rule does not apply")
	}
}

func TestLegacyExpressionsNeedAttentionButKeepWorking(t *testing.T) {
	f := newFixture(t)
	f.apply(Op{Kind: OpCreateTable, Table: &TableSpec{DisplayName: "Stock", Columns: []ColumnSpec{
		{DisplayName: "Qty", Type: Integer, ValidationSQL: "Qty >= 0"},
	}}})
	tbl, _ := f.schema().TableByPhysical("Stock")
	qty, _ := tbl.ColumnByPhysical("Qty")
	// An expression saved before the grammar, valid SQLite but outside it.
	const legacy = "CAST(Qty AS NUMERIC) >= 0"
	f.exec(`UPDATE _sys_columns SET validation_sql = ? WHERE id = ?`, legacy, qty.ID)
	tbl, _ = f.schema().TableByPhysical("Stock")
	qty, _ = tbl.ColumnByPhysical("Qty")
	if msg := qty.ExprIssues["validation_sql"]; !strings.Contains(msg, "NUMERIC") {
		t.Fatalf("issues %v", qty.ExprIssues)
	}

	// An unrelated change that rebuilds the table embeds it as written.
	f.apply(Op{Kind: OpAddColumn, TableID: tbl.ID, Column: &ColumnSpec{DisplayName: "Code", Type: ShortText, Required: true, DefaultSQL: "'x'",
		ValidationSQL: "length(Code) > 0"}})
	ddl := f.query(`SELECT sql FROM sqlite_schema WHERE name = 'Stock'`)[0][0].(string)
	if !strings.Contains(ddl, legacy) {
		t.Fatalf("legacy rule not kept: %s", ddl)
	}
	f.exec(`INSERT INTO Stock(Qty) VALUES (1)`)
	if err := f.l.Write(f.ctx, workspace.Interactive, func(tx dbx.Tx) error {
		_, err := tx.Exec(f.ctx, `INSERT INTO Stock(Qty) VALUES (-1)`)
		return err
	}); err == nil {
		t.Fatal("the legacy rule no longer applies")
	}

	// Changing it to something else outside the grammar is refused; to a
	// valid expression, accepted and no longer flagged.
	spec := ColumnSpec{DisplayName: "Qty", Type: Integer, ValidationSQL: "CAST(Qty AS NUMERIC) > 0"}
	if _, err := f.run(Apply, ApplyOptions{UserID: "u1"}, Op{Kind: OpAlterColumn, TableID: tbl.ID, ColumnID: qty.ID, Column: &spec}); fault.Classify(err).Code != fault.BadRequest {
		t.Fatalf("changed legacy rule: %v", err)
	}
	spec.ValidationSQL = "Qty > 0"
	f.apply(Op{Kind: OpAlterColumn, TableID: tbl.ID, ColumnID: qty.ID, Column: &spec})
	tbl, _ = f.schema().TableByPhysical("Stock")
	qty, _ = tbl.ColumnByPhysical("Qty")
	if qty.ExprIssues != nil {
		t.Fatalf("still flagged: %v", qty.ExprIssues)
	}
}
