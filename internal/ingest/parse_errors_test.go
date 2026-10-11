package ingest

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"abidata/internal/fault"
)

// A bare quote in an unquoted CSV field is a problem with the file, not with form values: the error
// names the line and column and advises fixing the file or the delimiter.
func TestCSVParseErrorIsAFileFault(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bad.csv")
	if err := os.WriteFile(path, []byte("name,note\nAda,fine\nBo,5\" pipe\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	src, err := openCSV(path, ParseOptions{HasHeader: true})
	for err == nil {
		_, err = src.next()
	}
	if errors.Is(err, io.EOF) {
		t.Fatal("expected a parse error")
	}
	var fe *fault.Error
	if !errors.As(err, &fe) {
		t.Fatalf("not a fault: %v", err)
	}
	if fe.Code != fault.BadRequest || fe.DisplayTitle() != "The file could not be read" {
		t.Errorf("code/title: %s %q", fe.Code, fe.DisplayTitle())
	}
	if !strings.Contains(fe.Message, "line 3, column") || !strings.Contains(fe.Message, "quotation mark") {
		t.Errorf("message: %q", fe.Message)
	}
	if !strings.Contains(fe.Recovery, "quote fields that contain quotes") || !strings.Contains(fe.Recovery, "delimiter") {
		t.Errorf("recovery: %q", fe.Recovery)
	}
}
