// Package secreview holds Phase 3 security-review repro tests. Tests that prove
// a still-open finding are gated behind environment variables so the normal
// suite stays green; run them with the variable named in each test.
package secreview

import (
	"os"
	"strings"
	"testing"
	"time"

	"abidata/internal/script"
)

// Run with ABIDATA_SECREVIEW_CRASH=<shape>: a builder-sized (<1 MiB) source with
// deep nesting overflows the Go stack in the gopher-lua parser/compiler, which is
// a fatal runtime error (not recoverable) that kills the whole server process.
// Reachable from POST /scripts/check, script/form/report save, and calc/group
// expressions.
func TestDeepNestingCompileCrashesProcess(t *testing.T) {
	shape := os.Getenv("ABIDATA_SECREVIEW_CRASH")
	if shape == "" {
		t.Skip("set ABIDATA_SECREVIEW_CRASH=<shape>; this test is expected to crash the test binary")
	}
	n := 300_000
	var src string
	switch shape {
	case "table":
		src = "return " + strings.Repeat("{", n) + strings.Repeat("}", n)
	case "unary":
		src = "return " + strings.Repeat("- ", n) + "1"
	case "not":
		src = "return " + strings.Repeat("not ", 200_000) + "1"
	case "pow":
		src = "return 2" + strings.Repeat("^2", n)
	case "concat":
		src = "return 'a'" + strings.Repeat("..'a'", 150_000)
	case "call":
		src = "return " + strings.Repeat("f(", n) + strings.Repeat(")", n)
	case "do":
		src = strings.Repeat("do ", 140_000) + strings.Repeat("end ", 140_000)
	case "if":
		src = strings.Repeat("if x then ", 70_000) + strings.Repeat("end ", 70_000)
	case "func":
		src = "return " + strings.Repeat("function() return ", 45_000) + "1" + strings.Repeat(" end", 45_000)
	case "do50k":
		src = strings.Repeat("do ", 50_000) + strings.Repeat("end ", 50_000)
	case "paren":
		src = "return " + strings.Repeat("(", n) + "1" + strings.Repeat(")", n)
	}
	if len(src) > 1<<20 {
		t.Fatalf("source %d bytes exceeds MaxSource", len(src))
	}
	start := time.Now()
	_, err := script.Compile(src, "deep")
	t.Logf("%s (%d bytes): compile took %s, err=%v (no crash)", shape, len(src), time.Since(start), err)
}
