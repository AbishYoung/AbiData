package ingest

import (
	"strings"
	"testing"

	"abidata/internal/catalog"
	"abidata/internal/dbx"
	"abidata/internal/ids"
	"abidata/internal/query"
	"abidata/internal/workspace"
)

// ADR 0011: imports ignore attachments fields and say so; exporting a table
// with an attachments column works (the cell shows the file ids for now).
func TestImportSkipsAttachmentsColumns(t *testing.T) {
	e := newEnv(t)
	tb := e.table("Sightings", catalog.ColumnSpec{DisplayName: "Species", Type: catalog.ShortText},
		catalog.ColumnSpec{DisplayName: "Photos", Type: catalog.Attachments})
	m := e.upload("s.csv", "Species,Photos\nGarlic mustard,a.jpg\nKudzu,b.jpg\n", UploadOptions{})

	meta, err := e.svc.SetMapping(e.ctx, e.l, m.ID, mapTo(tb, ModeAppend, "", "Species", "Photos"))
	if err != nil {
		t.Fatal(err)
	}
	if len(meta.Notes) != 1 || !strings.Contains(meta.Notes[0], "Photos") || !strings.Contains(meta.Notes[0], "attachments") {
		t.Fatalf("mapping notes: %v", meta.Notes)
	}
	// The note is part of the preview response too.
	view, err := e.svc.Get(e.ctx, e.ws, m.ID)
	if err != nil || len(view.Notes) != 1 {
		t.Fatalf("view notes %v %v", view.Notes, err)
	}
	val, err := e.svc.Validate(e.ctx, e.l, m.ID)
	if err != nil || val.ErrorCount != 0 || len(val.Warnings) != 1 {
		t.Fatalf("validate %+v %v", val, err)
	}
	res := result(t, e.commit(m.ID))
	if res.Inserted != 2 || e.count(tb) != 2 {
		t.Fatalf("%+v", res)
	}
	var n int
	if err := e.l.Read(e.ctx, func(q dbx.Querier) error {
		return q.QueryRow(e.ctx, `SELECT count(*) FROM "Sightings" WHERE "Photos" IS NOT NULL`).Scan(&n)
	}); err != nil || n != 0 {
		t.Fatalf("attachments cells were filled: %d %v", n, err)
	}

	// Mapping only the attachments column leaves nothing to import.
	m2 := e.upload("s2.csv", "Photos\na.jpg\n", UploadOptions{})
	if _, err := e.svc.SetMapping(e.ctx, e.l, m2.ID, mapTo(tb, ModeAppend, "", "Photos")); fcode(err) != "BAD_REQUEST" {
		t.Fatalf("only attachments: %v", err)
	}
}

func TestExportWithAttachmentsColumn(t *testing.T) {
	e := newEnv(t)
	tb := e.table("Sightings", catalog.ColumnSpec{DisplayName: "Species", Type: catalog.ShortText},
		catalog.ColumnSpec{DisplayName: "Photos", Type: catalog.Attachments})
	fid := ids.New()
	if err := e.l.Write(e.ctx, workspace.Interactive, func(tx dbx.Tx) error {
		if _, err := tx.Exec(e.ctx, `INSERT INTO _sys_files(id, sha256, size, name, content_type, created_at) VALUES (?, ?, 1, 'a.jpg', 'image/jpeg', '2026-01-01T00:00:00.000Z')`,
			fid, strings.Repeat("a", 64)); err != nil {
			return err
		}
		_, err := tx.Exec(e.ctx, `INSERT INTO "Sightings"("Species", "Photos") VALUES ('Kudzu', ?), ('Reed', NULL)`, `["`+fid+`"]`)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	for _, format := range []string{"csv", "json", "xlsx"} {
		_, body := e.export(ExportRequest{SourceKind: "table", SourceID: tb.ID, Format: format}, query.Policy{})
		if format == "xlsx" {
			if len(body) < 100 {
				t.Fatalf("xlsx too small")
			}
			continue
		}
		if !strings.Contains(body, fid) {
			t.Fatalf("%s export lacks the file id: %s", format, body)
		}
	}
}
