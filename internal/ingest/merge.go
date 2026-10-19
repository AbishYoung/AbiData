package ingest

import (
	"context"
	"fmt"
	"strings"

	"abidata/internal/catalog"
	"abidata/internal/dbx"
	"abidata/internal/fault"
	"abidata/internal/records"
	"abidata/internal/sqlgen"
	"abidata/internal/sqlitex"
	"abidata/internal/workspace"
)

// MergeResult is the outcome of an import commit or a staging merge.
type MergeResult struct {
	Inserted     int64      `json:"inserted"`
	Updated      int64      `json:"updated"`
	Skipped      int64      `json:"skipped" doc:"Rows that were rejected (chunked mode)."`
	ErrorCount   int64      `json:"error_count"`
	Errors       []RowError `json:"errors" doc:"The first 100 rejected rows."`
	TableID      string     `json:"table_id"`
	TableCreated bool       `json:"table_created,omitempty"`
	Warnings     []string   `json:"warnings"`
}

func (r *MergeResult) addError(re RowError) {
	r.Skipped++
	r.ErrorCount++
	if len(r.Errors) < MaxRowErrors {
		r.Errors = append(r.Errors, re)
	}
}

// rowFault reports whether err is a constraint failure caused by the row's data
// (the row can be skipped) rather than by the database or the request.
func rowFault(err error) bool {
	switch fault.Classify(err).Code {
	case fault.DuplicateValue, fault.ValidationFailed, fault.RequiredField, fault.LinkedRecords:
		return true
	}
	return false
}

// mergeSQL renders the explicit-column write statement for plan p. It has one
// positional placeholder per plan column, in plan order, so the applier runs
// it once per row with that row's values.
func mergeSQL(d sqlgen.Dialect, p *plan) (string, error) {
	t := p.Table
	ins := &sqlgen.Insert{Table: t.PhysicalName, Rows: [][]sqlgen.Expr{make([]sqlgen.Expr, len(p.Cols))}}
	for i, mc := range p.Cols {
		ins.Columns = append(ins.Columns, mc.Col.PhysicalName)
		ins.Rows[0][i] = sqlgen.Arg{}
	}
	if p.Mode == ModeUpsert {
		textKey := false
		switch p.Key.Type {
		case catalog.ShortText, catalog.LongText, catalog.Hyperlink:
			textKey = true // text indexes are case-insensitive
		}
		// The existing row's _version, named by its table: bare, PostgreSQL
		// finds it ambiguous with excluded's.
		set := []sqlgen.Assign{{Column: "_version", Value: sqlgen.Binary{Op: sqlgen.OpAdd, L: sqlgen.Col{Table: t.PhysicalName, Name: "_version"}, R: sqlgen.Lit{Value: 1}}}}
		for _, mc := range p.Cols {
			if mc.Col.ID != p.Key.ID {
				set = append(set, sqlgen.Assign{Column: mc.Col.PhysicalName, Value: sqlgen.Excluded(mc.Col.PhysicalName)})
			}
		}
		ins.OnConflict = &sqlgen.OnConflict{Target: []sqlgen.Col{{Name: p.Key.PhysicalName, CI: textKey}}, Set: set}
		ins.Returning = []sqlgen.Column{{Expr: sqlgen.Col{Name: "_version"}}}
	}
	r, err := sqlgen.Render(d, ins)
	if err != nil {
		return "", fmt.Errorf("ingest: %w", err)
	}
	return r.SQL, nil
}

// applier writes converted rows inside one transaction.
type applier struct {
	d      sqlgen.Dialect
	p      *plan
	sql    string
	strict bool // any row failure aborts the whole merge
	keyIdx int
}

func newApplier(d sqlgen.Dialect, p *plan, strict bool) (*applier, error) {
	sql, err := mergeSQL(d, p)
	if err != nil {
		return nil, err
	}
	a := &applier{d: d, p: p, sql: sql, strict: strict, keyIdx: -1}
	for i, mc := range p.Cols {
		if p.Key != nil && mc.Col.ID == p.Key.ID {
			a.keyIdx = i
		}
	}
	return a, nil
}

func (a *applier) apply(ctx context.Context, tx dbx.Tx, res *MergeResult, n int64, vals []any) error {
	if a.d != sqlgen.SQLite {
		args := make([]any, len(vals))
		for i, mc := range a.p.Cols {
			args[i] = records.ArgValue(a.d, mc.Col, vals[i])
		}
		vals = args
	}
	var err error
	write := func(fn func() error) error { return fn() }
	if a.d != sqlgen.SQLite && !a.strict {
		// A failed statement aborts a PostgreSQL transaction; a savepoint
		// lets the row be skipped, as SQLite does.
		write = func(fn func() error) error { return tx.Savepoint(ctx, fn) }
	}
	if a.p.Mode == ModeUpsert {
		if vals[a.keyIdx] == nil {
			re := RowError{Row: n, Column: a.p.Key.DisplayName, Code: string(fault.RequiredField), Message: fmt.Sprintf("%q is empty, so the row cannot be matched.", a.p.Key.DisplayName)}
			if a.strict {
				return rowAbort(re)
			}
			res.addError(re)
			return nil
		}
		var ver int64
		if err = write(func() error { return tx.QueryRow(ctx, a.sql, vals...).Scan(&ver) }); err == nil {
			if ver == 1 {
				res.Inserted++
			} else {
				res.Updated++
			}
			return nil
		}
	} else {
		if err = write(func() error { _, err := tx.Exec(ctx, a.sql, vals...); return err }); err == nil {
			res.Inserted++
			return nil
		}
	}
	if !rowFault(err) {
		return err
	}
	tf := fault.Classify(records.Translate(a.p.Table, err))
	re := RowError{Row: n, Code: string(tf.Code), Message: tf.DisplayMessage()}
	var fieldIDs []string
	if f, _ := tf.Details["field"].(string); f != "" {
		fieldIDs = []string{f}
	}
	if fs, ok := tf.Details["fields"].([]string); ok {
		fieldIDs = fs
	}
	var names []string
	for _, f := range fieldIDs {
		if c, ok := a.p.Table.Column(f); ok {
			names = append(names, c.DisplayName)
		}
	}
	re.Column = strings.Join(names, ", ")
	if a.strict {
		return rowAbort(re)
	}
	res.addError(re)
	return nil
}

// okRow is a converted row and its number in the file.
type okRow struct {
	n    int64
	vals []any
}

// applyChunk applies rows that may be skipped one by one. On PostgreSQL each
// would need its own savepoint, so the chunk is first tried in one; only when
// a row fails is it written again row by row.
func (a *applier) applyChunk(ctx context.Context, tx dbx.Tx, res *MergeResult, rows []okRow) error {
	if a.d != sqlgen.SQLite && !a.strict {
		fast := *a
		fast.strict = true // the first failing row aborts the try
		var trial MergeResult
		err := tx.Savepoint(ctx, func() error {
			for _, r := range rows {
				if err := fast.apply(ctx, tx, &trial, r.n, r.vals); err != nil {
					return err
				}
			}
			return nil
		})
		if err == nil {
			res.Inserted += trial.Inserted
			res.Updated += trial.Updated
			return nil
		}
		if err := ctx.Err(); err != nil {
			return err
		}
	}
	for _, r := range rows {
		if err := a.apply(ctx, tx, res, r.n, r.vals); err != nil {
			return err
		}
	}
	return nil
}

func rowAbort(re RowError) error {
	return &fault.Error{Code: fault.Code(re.Code), Message: fmt.Sprintf("Row %d: %s Nothing was imported.", re.Row, re.Message),
		Details: map[string]any{"row": re.Row, "column": re.Column}}
}

// merge copies the mapped staging rows into p.Table. It reads and converts
// outside the workspace writer and writes chunk-sized background transactions;
// all_or_nothing (and replace of up to ReplaceChunkedAbove rows) uses one.
func (s *Service) merge(ctx context.Context, l *workspace.Lease, src *sqlitex.Conn, p *plan, total int64, chunk int,
	progress func(done int64)) (MergeResult, error) {
	res := MergeResult{TableID: p.Table.ID, Errors: []RowError{}, Warnings: []string{}}
	if chunk <= 0 || chunk > 10_000 {
		chunk = ChunkRows
	}
	strict := p.AllOrNo
	oneTx := strict || (p.Mode == ModeReplace && total <= ReplaceChunkedAbove)
	if p.Mode == ModeReplace && !oneTx {
		res.Warnings = append(res.Warnings, fmt.Sprintf("Replace of more than %d rows runs in chunks: the table is emptied with the first chunk and refilled afterwards, so readers can see a partly filled table until the import finishes.", ReplaceChunkedAbove))
		if s.RestorePoint != nil {
			if err := s.RestorePoint(ctx, l); err != nil {
				return res, fmt.Errorf("ingest: restore point before replace: %w", err)
			}
			res.Warnings = append(res.Warnings, "A restore point was taken before the table was replaced.")
		}
	}
	if strict && total > AllOrNothingWarnRows {
		res.Warnings = append(res.Warnings, "All-or-nothing held the workspace writer for the whole import.")
	}
	delStmt, err := sqlgen.Render(l.Dialect(), &sqlgen.Delete{Table: p.Table.PhysicalName})
	if err != nil {
		return res, fmt.Errorf("ingest: %w", err)
	}
	del := delStmt.SQL
	a, err := newApplier(l.Dialect(), p, strict)
	if err != nil {
		return res, err
	}

	if strict {
		// Conversion errors are found before the writer is taken.
		var bad MergeResult
		err := readChunks(ctx, src, p, chunk, func(rows []stagedRow) error {
			for _, r := range rows {
				if _, re := convertRow(p, r.vals); re != nil {
					re.Row = r.n
					bad.addError(*re)
				}
			}
			return nil
		})
		if err != nil {
			return res, err
		}
		if bad.ErrorCount > 0 {
			return res, (&fault.Error{Code: fault.ImportRowErrors, Message: fmt.Sprintf("%d rows have errors, so nothing was imported.", bad.ErrorCount)}).
				WithDetails(map[string]any{"error_count": bad.ErrorCount, "errors": bad.Errors})
		}
	}

	var done int64
	if oneTx {
		var local MergeResult
		err := l.Write(ctx, workspace.Background, func(tx dbx.Tx) error {
			local = MergeResult{Errors: []RowError{}}
			if p.Mode == ModeReplace {
				if _, err := tx.Exec(ctx, del); err != nil {
					return err
				}
			}
			return readChunks(ctx, src, p, chunk, func(rows []stagedRow) error {
				for _, r := range rows {
					vals, re := convertRow(p, r.vals)
					if re != nil {
						re.Row = r.n
						local.addError(*re)
						continue
					}
					if err := a.apply(ctx, tx, &local, r.n, vals); err != nil {
						return err
					}
				}
				done += int64(len(rows))
				if progress != nil {
					progress(done)
				}
				return nil
			})
		})
		if err != nil {
			return res, translateWrite(p.Table, err)
		}
		local.TableID, local.Warnings = res.TableID, res.Warnings
		return local, nil
	}

	first := true
	err = readChunks(ctx, src, p, chunk, func(rows []stagedRow) error {
		good := make([]okRow, 0, len(rows))
		var conv []RowError
		for _, r := range rows {
			vals, re := convertRow(p, r.vals)
			if re != nil {
				re.Row = r.n
				conv = append(conv, *re)
				continue
			}
			good = append(good, okRow{r.n, vals})
		}
		var local MergeResult
		err := l.Write(ctx, workspace.Background, func(tx dbx.Tx) error {
			local = MergeResult{}
			if first && p.Mode == ModeReplace {
				if _, err := tx.Exec(ctx, del); err != nil {
					return err
				}
			}
			return a.applyChunk(ctx, tx, &local, good)
		})
		if err != nil {
			return translateWrite(p.Table, err)
		}
		first = false
		res.Inserted += local.Inserted
		res.Updated += local.Updated
		for _, re := range conv {
			res.addError(re)
		}
		res.Skipped += local.Skipped
		res.ErrorCount += local.ErrorCount
		for _, re := range local.Errors {
			if len(res.Errors) < MaxRowErrors {
				res.Errors = append(res.Errors, re)
			}
		}
		done += int64(len(rows))
		if progress != nil {
			progress(done)
		}
		return nil
	})
	return res, err
}

func translateWrite(t *catalog.Table, err error) error {
	if rowFault(err) {
		return records.Translate(t, err)
	}
	return err
}

// publish tells open datasheets the table changed.
func (s *Service) publish(ws string, tableID string) {
	if s.Records != nil {
		s.Records.PublishInvalidated(ws, []string{tableID})
	}
}
