package catalog

import (
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5/pgconn"

	"abidata/internal/dbx"
	"abidata/internal/sqlgen"
)

// The table rebuild on PostgreSQL (ADR 0012 Phase 3). It follows SQLite's: a
// new table in the new shape, the rows copied over, the old table dropped and
// the new one renamed. Two things differ.
//
//   - Foreign keys stay on. Children's keys to the table would keep it from
//     being dropped, so they are dropped first; they and the table's own keys
//     are added back NOT VALID after the copy, and impact checks the rows
//     against them, as SQLite's pragma_foreign_key_check does, before
//     validating them.
//   - There is no INSERT OR IGNORE. The copy runs as one statement in a
//     savepoint; when a row breaks a rule, abi_copy_rows copies row by row
//     instead and reports the rows it could not copy.

func (p *planner) pgRebuild(before, after *Table, conv map[string]convSpec) error {
	var children []sqlgen.AddForeignKey
	for _, r := range p.s.Relationships {
		if r.ParentTableID != after.ID || r.ChildTableID == after.ID || !r.Enforce {
			continue
		}
		child, ok := p.s.Table(r.ChildTableID)
		if !ok {
			return fmt.Errorf("catalog: relationship %s child table %s missing", r.ID, r.ChildTableID)
		}
		col, ok := child.Column(r.ChildColumnID)
		if !ok {
			return fmt.Errorf("catalog: relationship %s child column %s missing", r.ID, r.ChildColumnID)
		}
		if err := p.execStmt(&sqlgen.DropConstraint{Table: child.PhysicalName, Name: fkName(r.ID), IfExists: true}); err != nil {
			return ddlError(err)
		}
		children = append(children, sqlgen.AddForeignKey{Table: child.PhysicalName, NotValid: true, FK: sqlgen.ForeignKey{
			Name: fkName(r.ID), Column: col.PhysicalName, RefTable: after.PhysicalName, OnDelete: sqlgen.RefAction(r.OnDelete)}})
	}

	ct, err := createTableStmt(p.s, after, rebuildName)
	if err != nil {
		return err
	}
	own := ct.ForeignKeys
	ct.ForeignKeys = nil
	if err := p.execStmt(ct); err != nil {
		return ddlError(err)
	}
	dst, src := copyColumns(before, after, nil)
	for i, name := range dst {
		if c, ok := after.ColumnByPhysical(name); ok && conv[c.ID] != (convSpec{}) {
			pc := p.pgConv[c.ID]
			if pc == nil {
				return fmt.Errorf("catalog: conversion of %s was not prepared", c.PhysicalName)
			}
			src[i] = sqlgen.Column{Expr: pc.expr(before.PhysicalName)}
		}
	}
	total, err := p.countRows(before.PhysicalName, nil)
	if err != nil {
		return err
	}
	copied, err := p.pgCopy(before, dst, src, total)
	if err != nil {
		return err
	}
	p.report.RowsAffected += copied

	var last *int64
	if err := p.tx.QueryRow(p.ctx, sqlPlannerPGRebuild.SQL, Quote(before.PhysicalName)).Scan(&last); err != nil {
		return err
	}
	if err := p.execStmt(&sqlgen.DropTable{Name: before.PhysicalName}); err != nil {
		return ddlError(err)
	}
	if err := p.execStmt(&sqlgen.RenameTable{From: rebuildName, To: after.PhysicalName}); err != nil {
		return ddlError(err)
	}
	if err := p.keepIdentity(after, last); err != nil {
		return err
	}
	if err := p.grant(after.PhysicalName); err != nil {
		return err
	}
	for _, x := range after.Indexes {
		ddl, err := createIndexSQL(p.d, after, x)
		if err != nil {
			return err
		}
		if err := p.execChecked(ddl); err != nil {
			if isConstraint(err) {
				p.violations = true
				p.report.Breaking = append(p.report.Breaking, Issue{Kind: "constraint_violation", Object: "table", ID: after.ID,
					Message: fmt.Sprintf("Unique index %q no longer holds after this change.", x.PhysicalName)})
				continue
			}
			return ddlError(err)
		}
	}
	for _, fk := range own {
		if fk.RefTable == rebuildName {
			fk.RefTable = after.PhysicalName // a self-reference follows the rename
		}
		children = append(children, sqlgen.AddForeignKey{Table: after.PhysicalName, FK: fk, NotValid: true})
	}
	for i := range children {
		if err := p.execStmt(&children[i]); err != nil {
			return ddlError(err)
		}
	}
	if err := p.execStmt(occTrigger(after)); err != nil {
		return err
	}
	return p.syncAttachTriggers(after)
}

// pgCopy copies before's rows into the rebuild table and reports the rows
// that break the new design. It returns how many it copied.
func (p *planner) pgCopy(before *Table, dst []string, src []sqlgen.Column, total int64) (int64, error) {
	from := sqlgen.Table{Name: before.PhysicalName}
	copySQL, err := render(p.d, &sqlgen.Insert{Table: rebuildName, Columns: dst, From: &sqlgen.Select{Columns: src, From: from}})
	if err != nil {
		return 0, err
	}
	var copied int64
	err = p.tx.Savepoint(p.ctx, func() error {
		res, err := p.tx.Exec(p.ctx, copySQL)
		copied = res.Changes
		return err
	})
	var pe *pgconn.PgError
	if err == nil || !errors.As(err, &pe) || (pe.Code[:2] != "23" && pe.Code[:2] != "22") {
		if err != nil {
			return 0, ddlError(err)
		}
		return copied, nil
	}
	// A row breaks a rule: copy row by row to find which.
	rowSQL, err := render(p.d, &sqlgen.Insert{Table: rebuildName, Columns: dst,
		From: &sqlgen.Select{Columns: src, From: from, Where: sqlgen.Raw{SQL: `"id" = $1`}}})
	if err != nil {
		return 0, err
	}
	var skipped int64
	var sample []int64
	if err := p.tx.QueryRow(p.ctx, sqlPlannerPGCopy.SQL, Quote(before.PhysicalName), rowSQL).Scan(&copied, &skipped, &sample); err != nil {
		return 0, err
	}
	if skipped > 0 {
		p.lostRows(before, skipped, sample)
	} else if copied+skipped < total {
		return 0, fmt.Errorf("catalog: rebuild of %s copied %d of %d rows", before.PhysicalName, copied, total)
	}
	return copied, nil
}

// keepIdentity carries the old table's identity past its last value, as
// SQLite's AUTOINCREMENT keeps sqlite_sequence: an ID is never reused, even
// of a row deleted before the rebuild.
func (p *planner) keepIdentity(t *Table, last *int64) error {
	r, err := sqlgen.Render(p.d, &sqlgen.Select{Columns: []sqlgen.Column{{Expr: sqlgen.Agg{Fn: sqlgen.AggMax, X: sqlgen.Col{Name: "id"}}}},
		From: sqlgen.Table{Name: t.PhysicalName}})
	if err != nil {
		return fmt.Errorf("catalog: %w", err)
	}
	var top *int64
	if err := p.tx.QueryRow(p.ctx, r.SQL, r.Args...).Scan(&top); err != nil {
		return err
	}
	v := int64(0)
	for _, x := range []*int64{last, top} {
		if x != nil && *x > v {
			v = *x
		}
	}
	if v == 0 {
		return nil
	}
	_, err = p.tx.Exec(p.ctx, sqlPlannerPGRebuild2.SQL, Quote(t.PhysicalName), v)
	return err
}

// pgCheckForeignKeys checks the rows against every foreign key a rebuild
// added NOT VALID, reports those that refer to missing rows, and validates
// the keys that hold.
func (p *planner) pgCheckForeignKeys() error {
	for _, r := range p.s.Relationships {
		if !r.Enforce {
			continue
		}
		child, ok1 := p.s.Table(r.ChildTableID)
		parent, ok2 := p.s.Table(r.ParentTableID)
		if !ok1 || !ok2 {
			continue
		}
		col, ok := child.Column(r.ChildColumnID)
		if !ok {
			continue
		}
		var pending bool
		err := p.tx.QueryRow(p.ctx, sqlPlannerPGForeignKeys.SQL, Quote(child.PhysicalName), fkName(r.ID)).Scan(&pending)
		if err != nil || !pending {
			if err != nil && !dbx.IsNoRows(err) {
				return err
			}
			continue
		}
		ref := sqlgen.Col{Name: col.PhysicalName}
		n, err := p.countRows(child.PhysicalName, sqlgen.And{sqlgen.IsNull{X: ref, Negate: true},
			sqlgen.Not{X: sqlgen.In{X: ref, Select: &sqlgen.Select{Columns: []sqlgen.Column{{Expr: sqlgen.Col{Name: "id"}}},
				From: sqlgen.Table{Name: parent.PhysicalName}}}}})
		if err != nil {
			return err
		}
		if n > 0 {
			p.violations = true
			p.report.Breaking = append(p.report.Breaking, Issue{Kind: "fk_violation", Object: "table",
				Message: fmt.Sprintf("%d rows in %q refer to %q rows that do not exist.", n, child.DisplayName, parent.DisplayName)})
			continue
		}
		if err := p.execStmt(&sqlgen.ValidateConstraint{Table: child.PhysicalName, Name: fkName(r.ID)}); err != nil {
			return ddlError(err)
		}
	}
	return nil
}
