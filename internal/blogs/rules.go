package blogs

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"

	"golang.org/x/text/unicode/norm"

	"abidata/internal/catalog"
	"abidata/internal/dbx"
	"abidata/internal/fault"
	"abidata/internal/pgdb"
	"abidata/internal/records"
	"abidata/internal/sqlgen"
)

// reservedSlugs cannot be a post's slug: they are, or may become, routes
// under /b/{username}/. (feed.xml and _files cannot come out of Slugify.)
var reservedSlugs = map[string]bool{"rss": true, "atom": true, "index": true, "new": true, "edit": true, "drafts": true}

var translit = map[rune]string{'ß': "ss", 'æ': "ae", 'Æ': "ae", 'ø': "o", 'Ø': "o", 'œ': "oe", 'Œ': "oe", 'đ': "d", 'Đ': "d",
	'ł': "l", 'Ł': "l", 'þ': "th", 'Þ': "th", 'ð': "d", 'Ð': "d", 'ı': "i", '&': " and "}

// Slugify makes a slug from a title or a typed slug: accents removed, lower
// case, every run of other characters one hyphen, at most MaxSlug
// characters. It returns "" when nothing usable is left.
func Slugify(s string) string {
	var pre strings.Builder
	for _, r := range s {
		if t, ok := translit[r]; ok {
			pre.WriteString(t)
		} else {
			pre.WriteRune(r)
		}
	}
	var b strings.Builder
	hyphen := false
	for _, r := range norm.NFD.String(pre.String()) {
		switch {
		case unicode.Is(unicode.Mn, r):
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			if hyphen && b.Len() > 0 {
				b.WriteByte('-')
			}
			b.WriteRune(r)
			hyphen = false
		case r >= 'A' && r <= 'Z':
			if hyphen && b.Len() > 0 {
				b.WriteByte('-')
			}
			b.WriteRune(unicode.ToLower(r))
			hyphen = false
		default:
			hyphen = true
		}
	}
	return trimSlug(b.String(), MaxSlug)
}

// trimSlug cuts s to at most n characters, at a hyphen when it can.
func trimSlug(s string, n int) string {
	if len(s) > n {
		s = s[:n]
		if i := strings.LastIndexByte(s, '-'); i > n/2 {
			s = s[:i]
		}
	}
	return strings.Trim(s, "-")
}

// ValidSlug reports whether s is a slug the public routes serve.
func ValidSlug(s string) bool {
	return s != "" && Slugify(s) == s && !reservedSlugs[s]
}

// Rules applies the post rules (ADR 0017) to every write of a blog's posts
// table, whichever path it comes from: the blog API, the datasheet, a script
// or an import. Wire Hook as records.Service.Hook.
type Rules struct {
	Records *records.Service
	Now     func() time.Time
}

func (r *Rules) now() time.Time {
	if r.Now != nil {
		return r.Now()
	}
	return time.Now()
}

// Hook is the records.WriteHook. On the pinned posts table it normalises the
// slug and makes it unique, sets published_at on first publish, checks the
// body size, and keeps a redirect when a public post's slug changes.
func (r *Rules) Hook(ctx context.Context, tx dbx.Tx, sch *catalog.Schema, t *catalog.Table, id int64, fields map[string]any,
	current func() (records.Row, error)) (map[string]any, bool, error) {
	if t.PhysicalName != "posts" {
		return nil, false, nil
	}
	pins, ok, err := ReadPins(ctx, tx)
	if err != nil || !ok || pins.Posts != t.ID {
		return nil, false, err
	}
	var cur *records.Row
	if id != 0 {
		row, err := current()
		if errors.Is(err, fault.E(fault.NotFound)) {
			return nil, false, nil // the update reports it
		}
		if err != nil {
			return nil, false, err
		}
		cur = &row
	}
	return r.apply(ctx, tx, sch, pins, t, cur, fields)
}

func (r *Rules) apply(ctx context.Context, tx dbx.Tx, sch *catalog.Schema, pins Pins, t *catalog.Table, cur *records.Row,
	fields map[string]any) (map[string]any, bool, error) {
	col := pins.Columns
	get := func(role string) (any, bool) {
		if v, ok := fields[col[role]]; ok {
			return v, true
		}
		if cur != nil {
			return cur.Fields[col[role]], false
		}
		return nil, false
	}
	str := func(v any) string { s, _ := v.(string); return s }
	out := make(map[string]any, len(fields)+2)
	for k, v := range fields {
		out[k] = v
	}
	changed := false

	body, bodyGiven := get("body_md")
	if len(str(body)) > MaxBodySize {
		return nil, false, fault.Newf(fault.BadRequest, "A post can be at most %d KB.", MaxBodySize>>10)
	}
	// The images column holds the files the body shows, so the file sweep
	// keeps them and backups carry them (ADR 0011).
	if imagesCol, ok := col["images"]; ok && (bodyGiven || cur == nil) {
		files, err := existingFiles(ctx, tx, BodyFileIDs(str(body)))
		if err != nil {
			return nil, false, err
		}
		var v any
		if len(files) > 0 {
			v = files
		}
		out[imagesCol], changed = v, true
	}

	title, _ := get("title")
	slugV, slugGiven := get("slug")
	var id int64
	var oldSlug string
	if cur != nil {
		id, oldSlug = cur.ID, str(cur.Fields[col["slug"]])
	}
	if cur == nil || slugGiven {
		base := Slugify(str(slugV))
		if base == "" {
			base = Slugify(str(title))
		}
		if base == "" {
			base = "post"
		}
		slug := base
		if slug != oldSlug {
			var err error
			if slug, err = uniqueSlug(ctx, tx, t, col["slug"], base, id); err != nil {
				return nil, false, err
			}
		}
		if slug != str(slugV) || !slugGiven {
			out[col["slug"]], changed = slug, true
		}
		slugV = slug
	}

	status, _ := get("status")
	st := str(status)
	if st == "" {
		st = StatusDraft
	}
	public := st == StatusPublished || st == StatusUnlisted
	if pub, _ := get("published_at"); public && pub == nil {
		out[col["published_at"]], changed = r.now().UTC().Format(time.RFC3339Nano), true
	}

	if cur != nil && str(slugV) != oldSlug {
		was := str(cur.Fields[col["status"]])
		if err := r.redirect(ctx, tx, sch, pins, oldSlug, str(slugV), cur.ID, was == StatusPublished || was == StatusUnlisted); err != nil {
			return nil, false, err
		}
	}
	return out, changed, nil
}

// uniqueSlug returns base, or base-2, base-3 and so on: the first that no
// other post uses and that is not reserved.
func uniqueSlug(ctx context.Context, tx dbx.Tx, t *catalog.Table, slugCol, base string, self int64) (string, error) {
	c, ok := t.Column(slugCol)
	if !ok {
		return "", fmt.Errorf("blogs: posts has no slug column")
	}
	for n := 1; n < 10000; n++ {
		s := base
		if n > 1 {
			suffix := "-" + strconv.Itoa(n)
			s = trimSlug(base, MaxSlug-len(suffix)) + suffix
		}
		if reservedSlugs[s] {
			continue
		}
		taken, err := slugTaken(ctx, tx, t.PhysicalName, c.PhysicalName, s, self)
		if err != nil {
			return "", err
		}
		if !taken {
			return s, nil
		}
	}
	return "", fault.New(fault.DuplicateValue, "No free slug is left for this title. Choose another slug.")
}

func slugTaken(ctx context.Context, q dbx.Querier, table, slugCol, slug string, self int64) (bool, error) {
	stmt, err := sqlgen.Render(pgdb.DialectOf(q), &sqlgen.Select{
		Columns: []sqlgen.Column{{Expr: sqlgen.Col{Name: "id"}}},
		From:    sqlgen.Table{Name: table},
		Where: sqlgen.And{
			sqlgen.Binary{Op: sqlgen.OpEq, L: sqlgen.Col{Name: slugCol}, R: sqlgen.Arg{Value: slug}},
			sqlgen.Binary{Op: sqlgen.OpNe, L: sqlgen.Col{Name: "id"}, R: sqlgen.Arg{Value: self}},
		},
		Limit: sqlgen.Lit{Value: 1},
	})
	if err != nil {
		return false, err
	}
	var id int64
	err = q.QueryRow(ctx, stmt.SQL, stmt.Args...).Scan(&id)
	if dbx.IsNoRows(err) {
		return false, nil
	}
	return err == nil, err
}

// redirect keeps the redirects in step with a slug change: a public post's
// old slug now points at it, and a redirect that the new slug shadows goes.
func (r *Rules) redirect(ctx context.Context, tx dbx.Tx, sch *catalog.Schema, pins Pins, oldSlug, newSlug string, postID int64, wasPublic bool) error {
	t, ok := sch.Table(pins.Redirects)
	if !ok || r.Records == nil {
		return nil // a damaged blog; the renderer refuses it
	}
	oldCol, postCol := pins.RedirectCols["old_slug"], pins.RedirectCols["post_id"]
	find := func(slug string) (records.Row, bool, error) {
		c, _ := t.Column(oldCol)
		stmt, err := sqlgen.Render(pgdb.DialectOf(tx), &sqlgen.Select{
			Columns: []sqlgen.Column{{Expr: sqlgen.Col{Name: "id"}}},
			From:    sqlgen.Table{Name: t.PhysicalName},
			Where:   sqlgen.Binary{Op: sqlgen.OpEq, L: sqlgen.Col{Name: c.PhysicalName}, R: sqlgen.Arg{Value: slug}},
		})
		if err != nil {
			return records.Row{}, false, err
		}
		var id int64
		if err := tx.QueryRow(ctx, stmt.SQL, stmt.Args...).Scan(&id); dbx.IsNoRows(err) {
			return records.Row{}, false, nil
		} else if err != nil {
			return records.Row{}, false, err
		}
		row, err := records.GetTx(ctx, tx, sch, t, id)
		return row, err == nil, err
	}
	actor := records.Actor{}
	if row, ok, err := find(newSlug); err != nil {
		return err
	} else if ok {
		if _, err := r.Records.DeleteTx(ctx, tx, "", sch, t, row.ID, row.Version, actor); err != nil {
			return err
		}
	}
	if !wasPublic || oldSlug == "" {
		return nil
	}
	row, ok, err := find(oldSlug)
	if err != nil {
		return err
	}
	if ok {
		_, err = r.Records.UpdateTx(ctx, tx, "", sch, t, row.ID, row.Version, map[string]any{postCol: json.Number(strconv.FormatInt(postID, 10))}, actor)
		return err
	}
	_, err = r.Records.InsertTx(ctx, tx, "", sch, t, map[string]any{oldCol: oldSlug, postCol: json.Number(strconv.FormatInt(postID, 10))}, actor)
	return err
}

// bodyFileRef matches a body's references to workspace files.
var bodyFileRef = regexp.MustCompile(`attachment:([0-9A-HJKMNP-TV-Z]{26})`)

// BodyFileIDs lists the file ids a post body refers to, first occurrence
// first, at most catalog.MaxMaxFiles.
func BodyFileIDs(body string) []string {
	var out []string
	seen := map[string]bool{}
	for _, m := range bodyFileRef.FindAllStringSubmatch(body, -1) {
		if id := m[1]; !seen[id] && len(out) < catalog.MaxMaxFiles {
			seen[id] = true
			out = append(out, id)
		}
	}
	return out
}

// existingFiles keeps the ids that are files of this workspace, in order.
func existingFiles(ctx context.Context, q dbx.Querier, list []string) ([]string, error) {
	if len(list) == 0 {
		return nil, nil
	}
	args := make([]sqlgen.Expr, len(list))
	for i, id := range list {
		args[i] = sqlgen.Arg{Value: id}
	}
	stmt, err := sqlgen.Render(pgdb.DialectOf(q), &sqlgen.Select{
		Columns: []sqlgen.Column{{Expr: sqlgen.Col{Name: "id"}}},
		From:    sqlgen.Table{Name: "_sys_files"},
		Where:   sqlgen.In{X: sqlgen.Col{Name: "id"}, List: args},
	})
	if err != nil {
		return nil, err
	}
	rows, err := q.Query(ctx, stmt.SQL, stmt.Args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	have := map[string]bool{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		have[id] = true
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	out := list[:0:0]
	for _, id := range list {
		if have[id] {
			out = append(out, id)
		}
	}
	return out, nil
}
