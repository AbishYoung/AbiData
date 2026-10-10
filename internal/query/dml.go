package query

import (
	"strings"

	"abidata/internal/fault"
)

// tokens lexes SQL into upper-cased keywords/identifiers (quoted identifiers
// unquoted), skipping string literals, blobs, numbers and comments.
func tokens(sql string) []string {
	var out []string
	for i := 0; i < len(sql); {
		c := sql[i]
		switch {
		case c == '-' && i+1 < len(sql) && sql[i+1] == '-':
			for i < len(sql) && sql[i] != '\n' {
				i++
			}
		case c == '/' && i+1 < len(sql) && sql[i+1] == '*':
			end := strings.Index(sql[i+2:], "*/")
			if end < 0 {
				return out
			}
			i += end + 4
		case c == '\'':
			i++
			for i < len(sql) {
				if sql[i] == '\'' {
					if i+1 < len(sql) && sql[i+1] == '\'' {
						i += 2
						continue
					}
					break
				}
				i++
			}
			i++
		case c == '"' || c == '`' || c == '[':
			closer := c
			if c == '[' {
				closer = ']'
			}
			j := i + 1
			var b strings.Builder
			for j < len(sql) && sql[j] != closer {
				b.WriteByte(sql[j])
				j++
			}
			out = append(out, strings.ToUpper(b.String()))
			i = j + 1
		case c == '_' || c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z':
			j := i
			for j < len(sql) && (sql[j] == '_' || sql[j] == '$' || sql[j] >= 'A' && sql[j] <= 'Z' || sql[j] >= 'a' && sql[j] <= 'z' || sql[j] >= '0' && sql[j] <= '9') {
				j++
			}
			out = append(out, strings.ToUpper(sql[i:j]))
			i = j
		default:
			i++
		}
	}
	return out
}

// CheckDML rejects write statements that would bypass optimistic concurrency:
// REPLACE conflict resolution (silently deletes and re-creates rows) and
// INSERTs that set _version. UPDATEs of id/_version are denied by the authorizer.
func CheckDML(sql string) error {
	toks := tokens(sql)
	insert := false
	for i, t := range toks {
		switch t {
		case "INSERT":
			insert = true
		case "VACUUM", "REINDEX", "ANALYZE":
			// Whole-database maintenance rewrites or locks every table; the
			// platform runs it (maintenance window), never scripts.
			if i == 0 {
				return fault.Newf(fault.SQLNotAllowed, "Not allowed: %s is run by the platform's maintenance.", t)
			}
		case "REPLACE":
			// REPLACE INTO …, INSERT OR REPLACE …, ON CONFLICT REPLACE; a
			// replace(…) function call is followed by "(" which the lexer drops,
			// so distinguish by the preceding keyword or statement start.
			if i == 0 || (i > 0 && (toks[i-1] == "OR" || toks[i-1] == "CONFLICT")) {
				return fault.New(fault.SQLNotAllowed, "Not allowed: REPLACE replaces rows without version checks; use UPDATE or records.update.")
			}
		}
	}
	if insert {
		for _, t := range toks {
			if t == "_VERSION" {
				return fault.New(fault.SQLNotAllowed, "Not allowed: _version is managed by the system.")
			}
		}
	}
	return nil
}

// HasExpressions reports whether the model contains builder-authored SQL expressions.
func (m Model) HasExpressions() bool {
	for _, f := range m.Fields {
		if f.ExprSQL != "" {
			return true
		}
	}
	return false
}
