package catalog

import (
	"fmt"
	"regexp"
	"strings"
	"unicode"

	"abidata/internal/fault"
)

var physRe = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_]{0,62}$`)

// sqliteKeywords is SQLite's keyword list (sqlite.org/lang_keywords.html).
var sqliteKeywords = func() map[string]bool {
	m := map[string]bool{}
	for _, k := range strings.Fields(`ABORT ACTION ADD AFTER ALL ALTER ALWAYS ANALYZE AND AS ASC ATTACH AUTOINCREMENT
	BEFORE BEGIN BETWEEN BY CASCADE CASE CAST CHECK COLLATE COLUMN COMMIT CONFLICT CONSTRAINT CREATE CROSS CURRENT
	CURRENT_DATE CURRENT_TIME CURRENT_TIMESTAMP DATABASE DEFAULT DEFERRABLE DEFERRED DELETE DESC DETACH DISTINCT DO
	DROP EACH ELSE END ESCAPE EXCEPT EXCLUDE EXCLUSIVE EXISTS EXPLAIN FAIL FILTER FIRST FOLLOWING FOR FOREIGN FROM
	FULL GENERATED GLOB GROUP GROUPS HAVING IF IGNORE IMMEDIATE IN INDEX INDEXED INITIALLY INNER INSERT INSTEAD
	INTERSECT INTO IS ISNULL JOIN KEY LAST LEFT LIKE LIMIT MATCH MATERIALIZED NATURAL NO NOT NOTHING NOTNULL NULL
	NULLS OF OFFSET ON OR ORDER OTHERS OUTER OVER PARTITION PLAN PRAGMA PRECEDING PRIMARY QUERY RAISE RANGE RECURSIVE
	REFERENCES REGEXP REINDEX RELEASE RENAME REPLACE RESTRICT RETURNING RIGHT ROLLBACK ROW ROWS SAVEPOINT SELECT SET
	TABLE TEMP TEMPORARY THEN TIES TO TRANSACTION TRIGGER UNBOUNDED UNION UNIQUE UPDATE USING VACUUM VALUES VIEW
	VIRTUAL WHEN WHERE WINDOW WITH WITHOUT`) {
		m[k] = true
	}
	return m
}()

// reservedColumns are physical names AbiData itself uses in every table.
var reservedColumns = map[string]bool{"id": true, "_version": true, "rowid": true, "oid": true, "_rowid_": true}

// ValidatePhysicalName checks a physical table/column/index name: the brief's
// regex, not a SQLite keyword, not a reserved system name, and not prefixed
// with an internal namespace (`_sys_`, `_occ_`, `_adm_`, `sqlite_`).
func ValidatePhysicalName(name string) error {
	if !physRe.MatchString(name) {
		return fault.Newf(fault.BadRequest, "%q is not a valid name: use a letter followed by letters, digits or underscores (max 63).", name)
	}
	if sqliteKeywords[strings.ToUpper(name)] {
		return fault.Newf(fault.BadRequest, "%q is a reserved SQL word; choose another name.", name)
	}
	lower := strings.ToLower(name)
	for _, p := range []string{"sqlite_", "_sys_", "_occ_", "_adm_", "_abidata"} {
		if strings.HasPrefix(lower, p) {
			return fault.Newf(fault.BadRequest, "Names starting with %q are reserved.", p)
		}
	}
	return nil
}

// ValidateColumnName additionally rejects reserved column names.
func ValidateColumnName(name string) error {
	if err := ValidatePhysicalName(name); err != nil {
		return err
	}
	if reservedColumns[strings.ToLower(name)] {
		return fault.Newf(fault.BadRequest, "%q is reserved for the system.", name)
	}
	return nil
}

// Quote quotes an identifier for SQL ("" doubling). Callers must still validate
// names against the catalog; quoting only prevents syntax injection.
func Quote(name string) string {
	return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
}

// DerivePhysicalName turns a display name into a candidate physical name:
// non-alphanumerics become "_", runs collapse, a leading non-letter gets an
// "f_" prefix, keywords get a "_f" suffix, and the result is truncated to 63.
func DerivePhysicalName(display string) string {
	var b strings.Builder
	lastUnderscore := false
	for _, r := range strings.TrimSpace(display) {
		switch {
		case r < unicode.MaxASCII && (unicode.IsLetter(r) || unicode.IsDigit(r)):
			b.WriteRune(r)
			lastUnderscore = false
		default:
			if !lastUnderscore && b.Len() > 0 {
				b.WriteByte('_')
				lastUnderscore = true
			}
		}
	}
	s := strings.Trim(b.String(), "_")
	if s == "" {
		s = "field"
	}
	if c := s[0]; !(c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z') {
		s = "f_" + s
	}
	if sqliteKeywords[strings.ToUpper(s)] || reservedColumns[strings.ToLower(s)] {
		s += "_f"
	}
	for _, p := range []string{"sqlite_", "_sys_", "_occ_", "_adm_", "_abidata"} {
		if strings.HasPrefix(strings.ToLower(s), p) {
			s = "f" + s
		}
	}
	if len(s) > 63 {
		s = strings.TrimRight(s[:63], "_")
	}
	return s
}

// uniqueName appends _2, _3 … until taken(name) is false.
func uniqueName(base string, taken func(string) bool) string {
	if !taken(base) {
		return base
	}
	for i := 2; ; i++ {
		suffix := fmt.Sprintf("_%d", i)
		cand := base
		if len(cand)+len(suffix) > 63 {
			cand = cand[:63-len(suffix)]
		}
		cand += suffix
		if !taken(cand) {
			return cand
		}
	}
}
