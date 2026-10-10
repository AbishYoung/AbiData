package script

import (
	"strings"
	"testing"
)

func TestCheckNestingAcceptsOrdinaryCode(t *testing.T) {
	var b strings.Builder
	for i := range 500 {
		b.WriteString(`log.info("a" .. x .. "b" .. (y + 1) * 2)` + "\n")
		b.WriteString(`local t = {1, 2, {3, 4}, f(g(h(1 + 2)))}` + "\n")
		if i%7 == 0 {
			b.WriteString("-- comment ((((((( [[ unbalanced\n--[==[ long ((((( ]==]\n")
			b.WriteString(`local s = "((((((" .. '{{{{' .. [[ do do do ]] .. 1e-5 - 0x1F` + "\n")
		}
	}
	deep := strings.Repeat("if x then ", 150) + strings.Repeat("end ", 150)
	for _, src := range []string{b.String(), deep, "return ...", "local a = -1 ^ -2"} {
		if err := checkNesting(src); err != nil {
			t.Errorf("rejected: %v", err)
		}
		if _, err := Compile(src, "ok"); err != nil && strings.Contains(err.Error(), "nested too deeply") {
			t.Errorf("Compile rejected: %v", err)
		}
	}
}

func TestCheckNestingRejectsDeepSources(t *testing.T) {
	for name, src := range map[string]string{
		"table":  "return " + strings.Repeat("{", 300) + strings.Repeat("}", 300),
		"do":     strings.Repeat("do ", 300) + strings.Repeat("end ", 300),
		"func":   "return " + strings.Repeat("function() return ", 300) + "1" + strings.Repeat(" end", 300),
		"concat": "return 'a'" + strings.Repeat("..'a'", 300),
		"unary":  "return " + strings.Repeat("- ", 300) + "1",
		"mixed":  "return " + strings.Repeat("(1 + 1 + ", 150) + "1" + strings.Repeat(")", 150),
	} {
		if err := checkNesting(src); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}
