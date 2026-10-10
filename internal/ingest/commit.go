package ingest

import (
	"context"
	"fmt"

	"abidata/internal/catalog"
	"abidata/internal/fault"
	"abidata/internal/ids"
	"abidata/internal/ops"
	"abidata/internal/workspace"
)

// StartCommit validates the stored mapping and starts the import operation.
func (s *Service) StartCommit(ctx context.Context, l *workspace.Lease, id, by string) (ops.Op, error) {
	path, err := s.importPaths(l.ID(), id)
	if err != nil {
		return ops.Op{}, err
	}
	c, err := openStaging(ctx, path)
	if err != nil {
		return ops.Op{}, err
	}
	meta, err := readMeta(ctx, c)
	if err != nil {
		_ = c.Close()
		return ops.Op{}, errImportNotFound
	}
	if meta.Mapping == nil {
		_ = c.Close()
		return ops.Op{}, fault.New(fault.BadRequest, "Set the column mapping first.")
	}
	sch, err := s.Schemas.Get(ctx, l)
	if err == nil {
		_, err = planFor(sch, *meta.Mapping, len(meta.Columns))
	}
	if err != nil {
		_ = c.Close()
		return ops.Op{}, err
	}
	moved, err := transition(ctx, c, []string{StatusMapped}, StatusCommitting)
	_ = c.Close()
	if err != nil {
		return ops.Op{}, err
	}
	if !moved {
		return ops.Op{}, fault.New(fault.BadRequest, "This import is already being committed or can no longer be committed.")
	}
	op, err := s.Ops.Start(ctx, l, "import", by, func(ctx context.Context, ol *workspace.Lease, p *ops.Progress) (any, error) {
		return s.runImport(ctx, ol, id, by, p)
	})
	if err != nil {
		s.setStatus(context.WithoutCancel(ctx), l.ID(), id, StatusMapped)
	}
	return op, err
}

func (s *Service) setStatus(ctx context.Context, ws ids.WorkspaceID, id, status string) {
	path, err := s.importPaths(ws, id)
	if err != nil {
		return
	}
	c, err := openStaging(ctx, path)
	if err != nil {
		return
	}
	defer func() { _ = c.Close() }()
	_, _ = c.Exec(ctx, `UPDATE meta SET value = ? WHERE key = 'status'`, `"`+status+`"`)
}

func (s *Service) runImport(ctx context.Context, l *workspace.Lease, id, by string, prog *ops.Progress) (out any, err error) {
	ws := l.ID()
	path, err := s.importPaths(ws, id)
	if err != nil {
		return nil, err
	}
	src, err := openStaging(ctx, path)
	if err != nil {
		return nil, err
	}
	var res MergeResult
	defer func() {
		_ = src.Close()
		switch {
		case err == nil:
			s.removeFiles(ws, id)
		case res.Inserted+res.Updated == 0 && !res.TableCreated:
			s.setStatus(context.WithoutCancel(ctx), ws, id, StatusMapped)
		default:
			s.setStatus(context.WithoutCancel(ctx), ws, id, StatusFailed)
		}
	}()
	meta, err := readMeta(ctx, src)
	if err != nil {
		return nil, err
	}
	sch, err := s.Schemas.Get(ctx, l)
	if err != nil {
		return nil, err
	}
	p, err := planFor(sch, *meta.Mapping, len(meta.Columns))
	if err != nil {
		return nil, err
	}
	if p.NewSpec != nil {
		prog.Set(0, "Creating the table")
		if _, err := s.Catalog.Run(ctx, l, []catalog.Op{{Kind: catalog.OpCreateTable, Table: p.NewSpec}}, catalog.Apply, catalog.ApplyOptions{UserID: by}); err != nil {
			return nil, err
		}
		if sch, err = s.Schemas.Get(ctx, l); err != nil {
			return nil, err
		}
		t, ok := sch.Table(p.NewSpec.ID)
		if !ok {
			return nil, fault.New(fault.Internal, "The new table was not found after creating it.")
		}
		p.Table, res.TableCreated = t, true
		// A retry must target the table that now exists.
		em := ImportMapping{Mode: ModeAppend, Transaction: meta.Mapping.Transaction, TableID: t.ID}
		for i := range p.Cols {
			c, _ := t.Column(p.NewSpec.Columns[i].ID)
			p.Cols[i].Col = *c
			em.Columns = append(em.Columns, ColumnMap{Source: p.Cols[i].Src, ColumnID: c.ID})
		}
		p.NewSpec = nil
		origMapping := *meta.Mapping
		defer func() {
			// An import that wrote nothing (all-or-nothing rollback, or failure
			// before the first chunk) removes the table it created, so it
			// rolls back fully and a retry creates the table again.
			if err == nil || res.Inserted+res.Updated > 0 {
				return
			}
			dctx := context.WithoutCancel(ctx)
			if _, derr := s.Catalog.Run(dctx, l, []catalog.Op{{Kind: catalog.OpDropTable, TableID: t.ID}}, catalog.Apply,
				catalog.ApplyOptions{UserID: by, Force: true, SkipRestorePoint: true}); derr != nil {
				return
			}
			res.TableCreated = false
			meta.Mapping = &origMapping
			_ = saveMeta(dctx, src, meta)
		}()
		meta.Mapping = &em
		_ = saveMeta(ctx, src, meta)
	}
	defer func() {
		if p.Table != nil && (res.Inserted+res.Updated > 0 || p.Mode == ModeReplace) {
			s.publish(string(ws), p.Table.ID)
		}
	}()
	created := res.TableCreated
	res, err = s.merge(ctx, l, src, p, meta.RowCount, ChunkRows, func(done int64) {
		prog.Set(float64(done)/float64(max(meta.RowCount, 1)), fmt.Sprintf("Imported %d of %d rows", done, meta.RowCount))
	})
	res.TableCreated = created
	if err != nil {
		return nil, err
	}
	return res, nil
}
