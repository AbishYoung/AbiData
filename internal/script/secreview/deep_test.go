// Package secreview holds regression tests for the Phase 3 security review.
package secreview

import (
	"strings"
	"testing"
	"time"

	"abidata/internal/script"
)

// M1 (fixed; regression): a builder-sized (<1 MiB) source with deep nesting
// could overflow the Go stack or take tens of seconds in the gopher-lua
// parser/compiler. Compile now rejects it up front (script.MaxNesting).
func TestDeepNestingCompile(t *testing.T) {
	for _, shape := range []string{"table", "unary", "not", "pow", "concat", "call", "do", "if", "func", "do50k", "paren"} {
		t.Run(shape, func(t *testing.T) { deepCompile(t, shape) })
	}
}

func deepCompile(t *testing.T, shape string) {
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
	if el := time.Since(start); err == nil || el > time.Second {
		t.Fatalf("%s (%d bytes): compile took %s, err=%v", shape, len(src), el, err)
	}
}
