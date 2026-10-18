package statements_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// sqlText is what a string that is a SQL statement starts with.
var sqlText = regexp.MustCompile(`(?is)^\s*(SELECT|INSERT|UPDATE|DELETE|WITH|CREATE|DROP|ALTER|PRAGMA|REPLACE|VACUUM|ANALYZE|BEGIN|COMMIT|ROLLBACK|SAVEPOINT|RELEASE|ATTACH|DETACH|REINDEX)\b`)

// TestNoInlineSQL keeps hand-written SQL in the registry: a SQL string
// literal passed straight to Exec, Query, QueryRow (or a package's exec
// helper) belongs in the package's statements.go as a sqlgen.Register, where
// the conformance test prepares it. The drivers and the SQL generator are
// exempt, as are tests and migration scripts (ExecScript). SQL assembled at
// run time (identifiers quoted in) is not a literal and is left to the
// models of ADR 0013 Phase F.
func TestNoInlineSQL(t *testing.T) {
	exempt := []string{"internal/sqlitex/", "internal/sqlgen/", "internal/dbx/", "internal/pgdb/"}
	root := filepath.Join("..", "..")
	fset := token.NewFileSet()
	for _, dir := range []string{"internal", "cmd"} {
		err := filepath.WalkDir(filepath.Join(root, dir), func(path string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return err
			}
			rel, _ := filepath.Rel(root, path)
			rel = filepath.ToSlash(rel)
			for _, e := range exempt {
				if strings.HasPrefix(rel, e) {
					return nil
				}
			}
			f, err := parser.ParseFile(fset, path, nil, 0)
			if err != nil {
				return err
			}
			ast.Inspect(f, func(n ast.Node) bool {
				c, ok := n.(*ast.CallExpr)
				if !ok {
					return true
				}
				sel, ok := c.Fun.(*ast.SelectorExpr)
				if !ok {
					return true
				}
				switch sel.Sel.Name {
				case "Exec", "Query", "QueryRow", "exec":
				default:
					return true
				}
				for _, a := range c.Args[:min(2, len(c.Args))] {
					l, ok := a.(*ast.BasicLit)
					if !ok || l.Kind != token.STRING {
						continue
					}
					if v, err := strconv.Unquote(l.Value); err == nil && sqlText.MatchString(v) {
						t.Errorf("%s: SQL written inline; declare it in the package's statements.go with sqlgen.Register", fset.Position(l.Pos()))
					}
				}
				return true
			})
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
}
