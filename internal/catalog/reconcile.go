package catalog

import (
	"context"
	"fmt"
	"strings"

	"abidata/internal/sqlitex"
)

// Reconcile compares the catalog snapshot against the physical schema (D5) and
// returns human-readable drift descriptions (empty when consistent).
func Reconcile(ctx context.Context, q sqlitex.Querier, s *Schema) ([]string, error) {
	var drift []string
	for _, t := range s.Tables {
		rows, err := q.Query(ctx, `SELECT name, hidden FROM pragma_table_xinfo(?)`, t.PhysicalName)
		if err != nil {
			return nil, err
		}
		phys := map[string]int{}
		for rows.Next() {
			var name string
			var hidden int
			if err := rows.Scan(&name, &hidden); err != nil {
				rows.Close()
				return nil, err
			}
			phys[strings.ToLower(name)] = hidden
		}
		if err := rows.Err(); err != nil {
			return nil, err
		}
		if len(phys) == 0 {
			drift = append(drift, fmt.Sprintf("table %q (%s) is missing", t.DisplayName, t.PhysicalName))
			continue
		}
		if _, ok := phys["_version"]; !ok {
			drift = append(drift, fmt.Sprintf("table %q has no _version column", t.DisplayName))
		}
		want := map[string]bool{"_version": true}
		for _, c := range t.Columns {
			key := strings.ToLower(c.PhysicalName)
			want[key] = true
			h, ok := phys[key]
			switch {
			case !ok:
				drift = append(drift, fmt.Sprintf("field %q.%q (%s) is missing", t.DisplayName, c.DisplayName, c.PhysicalName))
			case c.Type == Calculated && h == 0:
				drift = append(drift, fmt.Sprintf("field %q.%q should be generated", t.DisplayName, c.DisplayName))
			}
		}
		for name := range phys {
			if !want[name] {
				drift = append(drift, fmt.Sprintf("table %q has uncataloged column %s", t.DisplayName, name))
			}
		}
		var trig int
		if err := q.QueryRow(ctx, `SELECT count(*) FROM sqlite_schema WHERE type = 'trigger' AND name = ?`, occTriggerName(&t)).Scan(&trig); err != nil {
			return nil, err
		}
		if trig == 0 {
			drift = append(drift, fmt.Sprintf("table %q is missing its version trigger", t.DisplayName))
		}
		if HasAttachTriggers(&t) {
			for _, name := range attachTriggerNames(&t) {
				var n int
				if err := q.QueryRow(ctx, `SELECT count(*) FROM sqlite_schema WHERE type = 'trigger' AND name = ?`, name).Scan(&n); err != nil {
					return nil, err
				}
				if n == 0 {
					drift = append(drift, fmt.Sprintf("table %q is missing its file reference trigger %s", t.DisplayName, name))
				}
			}
		}
		for _, x := range t.Indexes {
			var n int
			if err := q.QueryRow(ctx, `SELECT count(*) FROM sqlite_schema WHERE type = 'index' AND name = ?`, x.PhysicalName).Scan(&n); err != nil {
				return nil, err
			}
			if n == 0 {
				drift = append(drift, fmt.Sprintf("index %s is missing", x.PhysicalName))
			}
		}
	}
	// User tables that exist physically but not in the catalog.
	rows, err := q.Query(ctx, `SELECT name FROM sqlite_schema WHERE type = 'table' AND name NOT LIKE 'sqlite\_%' ESCAPE '\'
		AND name NOT LIKE '\_sys\_%' ESCAPE '\'`)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			rows.Close()
			return nil, err
		}
		if _, ok := s.TableByPhysical(name); !ok {
			drift = append(drift, fmt.Sprintf("table %s exists but is not in the catalog", name))
		}
	}
	return drift, rows.Err()
}

// identTokens scans a SQL expression and calls fn for each identifier token with
// its byte span; string literals are skipped.
func identTokens(expr string, fn func(start, end int, name string)) {
	for i := 0; i < len(expr); {
		c := expr[i]
		switch {
		case c == '\'':
			j := i + 1
			for j < len(expr) {
				if expr[j] == '\'' {
					if j+1 < len(expr) && expr[j+1] == '\'' {
						j += 2
						continue
					}
					break
				}
				j++
			}
			i = j + 1
		case c == '"':
			j := i + 1
			var b strings.Builder
			for j < len(expr) {
				if expr[j] == '"' {
					if j+1 < len(expr) && expr[j+1] == '"' {
						b.WriteByte('"')
						j += 2
						continue
					}
					break
				}
				b.WriteByte(expr[j])
				j++
			}
			fn(i, min(j+1, len(expr)), b.String())
			i = j + 1
		case c == '_' || c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z':
			j := i
			for j < len(expr) && (expr[j] == '_' || expr[j] >= 'A' && expr[j] <= 'Z' || expr[j] >= 'a' && expr[j] <= 'z' || expr[j] >= '0' && expr[j] <= '9') {
				j++
			}
			// A word followed by "(" is a function name, not a column.
			k := j
			for k < len(expr) && expr[k] == ' ' {
				k++
			}
			// Words after AS (CAST(x AS text)) are type names, not columns.
			p := i - 1
			for p >= 0 && expr[p] == ' ' {
				p--
			}
			afterAs := p >= 1 && strings.EqualFold(expr[p-1:p+1], "as") && (p < 2 || !isWordByte(expr[p-2]))
			if (k >= len(expr) || expr[k] != '(') && !afterAs {
				fn(i, j, expr[i:j])
			}
			i = j
		case c >= '0' && c <= '9':
			j := i
			for j < len(expr) && (expr[j] >= '0' && expr[j] <= '9' || expr[j] == '.' || expr[j] == 'e' || expr[j] == 'E' || expr[j] == 'x' || expr[j] == 'X' ||
				expr[j] >= 'a' && expr[j] <= 'f' || expr[j] >= 'A' && expr[j] <= 'F') {
				j++
			}
			i = j
		default:
			i++
		}
	}
}

// referencesIdent reports whether expr refers to identifier name (case-insensitive).
func referencesIdent(expr, name string) bool {
	found := false
	identTokens(expr, func(_, _ int, n string) {
		if strings.EqualFold(n, name) {
			found = true
		}
	})
	return found
}

// renameIdent replaces references to identifier old with the quoted new name.
func renameIdent(expr, old, new string) string {
	if expr == "" {
		return expr
	}
	var b strings.Builder
	last := 0
	identTokens(expr, func(start, end int, n string) {
		if strings.EqualFold(n, old) {
			b.WriteString(expr[last:start])
			b.WriteString(Quote(new))
			last = end
		}
	})
	b.WriteString(expr[last:])
	return b.String()
}

func isWordByte(c byte) bool {
	return c == '_' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9'
}
