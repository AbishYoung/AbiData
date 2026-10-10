package artifacts

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"abidata/internal/fault"
	"abidata/internal/ids"
)

func TestSaveListOpen(t *testing.T) {
	s := &Store{Dir: t.TempDir(), Quota: func(ids.WorkspaceID) int64 { return 100 }}
	ws, run := ids.NewWorkspaceID(), ids.New()
	info, err := s.Save(ws, run, "report.csv", "", strings.NewReader("a,b\n1,2\n"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Size != 8 || info.MIME != "text/csv; charset=utf-8" {
		t.Fatalf("%+v", info)
	}
	if _, err := s.Save(ws, run, "data.bin", "application/x-custom", strings.NewReader("xyz")); err != nil {
		t.Fatal(err)
	}
	list, err := s.List(ws, run)
	if err != nil || len(list) != 2 || list[0].Name != "data.bin" || list[0].MIME != "application/x-custom" {
		t.Fatalf("%+v %v", list, err)
	}
	f, got, err := s.Open(ws, run, "report.csv")
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(f)
	_ = f.Close()
	if string(b) != "a,b\n1,2\n" || got.Size != 8 {
		t.Fatal(string(b))
	}
	// Overwrite counts only the delta toward the quota.
	if _, err := s.Save(ws, run, "report.csv", "", strings.NewReader(strings.Repeat("x", 90))); err != nil {
		t.Fatal(err)
	}
	// Over quota: refused and no partial file left.
	_, err = s.Save(ws, run, "big.txt", "", bytes.NewReader(make([]byte, 50)))
	if fault.Classify(err).Code != fault.QuotaExceeded {
		t.Fatalf("quota: %v", err)
	}
	dir := filepath.Join(s.Dir, string(ws), run)
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".tmp") || e.Name() == "big.txt" {
			t.Fatalf("leftover %s", e.Name())
		}
	}
}

func TestNamesAndTraversal(t *testing.T) {
	s := &Store{Dir: t.TempDir()}
	ws, run := ids.NewWorkspaceID(), ids.New()
	for _, n := range []string{"", "../x", "a/b", ".hidden", "x.meta.json", "a\x00b", strings.Repeat("a", 200), "..", "a..b"} {
		if _, err := s.Save(ws, run, n, "", strings.NewReader("x")); fault.Classify(err).Code != fault.BadRequest {
			t.Errorf("%q: %v", n, err)
		}
	}
	if _, err := s.Save(ws, "../../etc", "x.txt", "", strings.NewReader("x")); err == nil {
		t.Error("owner traversal")
	}
	if _, err := s.Save("not-a-ws", run, "x.txt", "", strings.NewReader("x")); err == nil {
		t.Error("bad workspace")
	}
	if _, _, err := s.Open(ws, run, "../../secret"); fault.Classify(err).Code != fault.NotFound {
		t.Error("open traversal")
	}
	if _, _, err := s.Open(ws, run, "missing.txt"); fault.Classify(err).Code != fault.NotFound {
		t.Error("missing")
	}
}

func TestSweep(t *testing.T) {
	s := &Store{Dir: t.TempDir()}
	ws := ids.NewWorkspaceID()
	oldRun, newRun := ids.New(), ids.New()
	_, _ = s.Save(ws, oldRun, "a.txt", "", strings.NewReader("x"))
	_, _ = s.Save(ws, newRun, "b.txt", "", strings.NewReader("y"))
	past := time.Now().Add(-48 * time.Hour)
	_ = os.Chtimes(filepath.Join(s.Dir, string(ws), oldRun), past, past)
	n, err := s.Sweep(24*time.Hour, time.Now())
	if err != nil || n != 1 {
		t.Fatalf("%d %v", n, err)
	}
	if l, _ := s.List(ws, newRun); len(l) != 1 {
		t.Fatal("new run swept")
	}
}
