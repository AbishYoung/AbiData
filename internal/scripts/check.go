package scripts

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"abidata/internal/catalog"
	"abidata/internal/script"
)

// Marker is a diagnostic for the editor (Monaco marker semantics: 1-based).
type Marker struct {
	Severity  string `json:"severity" enum:"error,warning"`
	Message   string `json:"message"`
	Line      int    `json:"line"`
	Column    int    `json:"column"`
	EndColumn int    `json:"end_column"`
}

var parseErrRe = regexp.MustCompile(`line:(\d+)\(column:(\d+)\) near '([^']*)':\s*(.*)`)

// Check returns syntax errors and lint warnings for a script of kind.
// Lint: calls to unknown functions of host modules (from the API manifest),
// host modules not available in the script's context, and string literals in
// db/records calls that name tables or fields the catalog does not have.
func Check(source, kind string, sch *catalog.Schema) []Marker {
	out := []Marker{}
	if _, err := script.Compile(source, "check"); err != nil {
		msg := strings.TrimSpace(err.Error())
		m := Marker{Severity: "error", Message: msg, Line: 1, Column: 1}
		if g := parseErrRe.FindStringSubmatch(msg); g != nil {
			m.Line, _ = strconv.Atoi(g[1])
			m.Column, _ = strconv.Atoi(g[2])
			m.Message = strings.TrimSpace(g[4]) + " near '" + g[3] + "'"
		} else if strings.Contains(msg, "EOF") {
			m.Line = strings.Count(source, "\n") + 1
			m.Message = strings.TrimSpace(msg[strings.Index(msg, "EOF")+4:])
		}
		m.EndColumn = m.Column + 1
		out = append(out, m)
		return out
	}
	ctx := contextOf(kind)
	mods := map[string]script.ModuleDoc{}
	for _, m := range script.Manifest() {
		mods[m.Name] = m
	}
	for _, tok := range memberAccesses(source) {
		m, ok := mods[tok.module]
		if !ok || m.Global {
			continue
		}
		available := false
		for _, c := range m.Contexts {
			available = available || c == ctx
		}
		if !available {
			out = append(out, Marker{Severity: "warning", Line: tok.line, Column: tok.col, EndColumn: tok.col + len(tok.module),
				Message: fmt.Sprintf("module %q is not available in %s scripts", tok.module, ctx)})
			continue
		}
		known := false
		for _, f := range m.Funcs {
			if f.Name == tok.member {
				known = true
				if len(f.Contexts) > 0 {
					ok := false
					for _, c := range f.Contexts {
						ok = ok || c == ctx
					}
					if !ok {
						out = append(out, Marker{Severity: "warning", Line: tok.line, Column: tok.col, EndColumn: tok.col + len(tok.module) + 1 + len(tok.member),
							Message: fmt.Sprintf("%s.%s is not available in %s scripts", tok.module, tok.member, ctx)})
					}
				}
			}
		}
		for _, v := range m.Values {
			known = known || v.Name == tok.member
		}
		if !known {
			out = append(out, Marker{Severity: "warning", Line: tok.line, Column: tok.col, EndColumn: tok.col + len(tok.module) + 1 + len(tok.member),
				Message: fmt.Sprintf("unknown function %s.%s", tok.module, tok.member)})
		}
		if sch != nil && tok.firstArg != "" && (tok.module == "records" || tok.module == "db" && tok.member != "query" && tok.member != "exec" &&
			tok.member != "query_one" && tok.member != "query_value" && tok.member != "iter") {
			if _, ok := sch.TableByPhysical(tok.firstArg); !ok {
				out = append(out, Marker{Severity: "warning", Line: tok.line, Column: tok.col, EndColumn: tok.col + len(tok.module) + 1 + len(tok.member),
					Message: fmt.Sprintf("table %q does not exist", tok.firstArg)})
			}
		}
	}
	return out
}

func contextOf(kind string) script.Context {
	switch kind {
	case "job":
		return script.Job
	case "report_module":
		return script.Report
	}
	return script.UIEvent
}

type access struct {
	module, member, firstArg string
	line, col                int
}

// memberAccesses scans Lua source (skipping strings and comments) for
// `name.member` tokens and, for calls, a leading string-literal argument.
func memberAccesses(src string) []access {
	var out []access
	line, col := 1, 1
	i := 0
	adv := func(n int) {
		for k := 0; k < n && i < len(src); k++ {
			if src[i] == '\n' {
				line++
				col = 1
			} else {
				col++
			}
			i++
		}
	}
	isIdent := func(c byte) bool {
		return c == '_' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9'
	}
	longClose := func(start int) int { // returns index after a [[ ]] / [==[ ]==] block, or -1
		j := start + 1
		eq := 0
		for j < len(src) && src[j] == '=' {
			eq++
			j++
		}
		if j >= len(src) || src[j] != '[' {
			return -1
		}
		end := "]" + strings.Repeat("=", eq) + "]"
		k := strings.Index(src[j+1:], end)
		if k < 0 {
			return len(src)
		}
		return j + 1 + k + len(end)
	}
	for i < len(src) {
		c := src[i]
		switch {
		case c == '-' && i+1 < len(src) && src[i+1] == '-':
			if i+2 < len(src) && src[i+2] == '[' {
				if e := longClose(i + 2); e > 0 {
					adv(e - i)
					continue
				}
			}
			for i < len(src) && src[i] != '\n' {
				adv(1)
			}
		case c == '[':
			if e := longClose(i); e > 0 {
				adv(e - i)
				continue
			}
			adv(1)
		case c == '"' || c == '\'':
			adv(1)
			for i < len(src) && src[i] != c && src[i] != '\n' {
				if src[i] == '\\' {
					adv(1)
				}
				adv(1)
			}
			adv(1)
		case isIdent(c) && (c < '0' || c > '9'):
			if i > 0 && (src[i-1] == '.' || src[i-1] == ':') {
				for i < len(src) && isIdent(src[i]) {
					adv(1)
				}
				continue
			}
			start, sl, sc := i, line, col
			for i < len(src) && isIdent(src[i]) {
				adv(1)
			}
			name := src[start:i]
			if i < len(src) && src[i] == '.' && i+1 < len(src) && isIdent(src[i+1]) {
				adv(1)
				ms := i
				for i < len(src) && isIdent(src[i]) {
					adv(1)
				}
				a := access{module: name, member: src[ms:i], line: sl, col: sc}
				// First argument literal: name.member("Literal"
				j := i
				for j < len(src) && (src[j] == ' ' || src[j] == '\t') {
					j++
				}
				if j < len(src) && src[j] == '(' {
					j++
					for j < len(src) && (src[j] == ' ' || src[j] == '\t') {
						j++
					}
					if j < len(src) && (src[j] == '"' || src[j] == '\'') {
						q := src[j]
						if k := strings.IndexByte(src[j+1:], q); k >= 0 {
							a.firstArg = src[j+1 : j+1+k]
						}
					}
				}
				out = append(out, a)
			}
		default:
			adv(1)
		}
	}
	return out
}
