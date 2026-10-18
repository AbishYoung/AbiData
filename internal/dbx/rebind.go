package dbx

import (
	"fmt"
	"strconv"
	"strings"
	"sync"
)

// Rebind rewrites SQL written with SQLite-style placeholders (?, ?NNN, :name,
// @name, $name) to PostgreSQL's numbered $n form, and orders args to match.
// Positional args fill ? in order; ?NNN refers to the NNNth positional arg;
// Named args fill the parameter of that name, which may appear several times
// and is bound once. Placeholders inside string literals, quoted identifiers,
// comments and dollar-quoted bodies are left alone, as is the :: cast.
//
// The parse of each distinct SQL text is cached, since services send the same
// statements over and over.
func Rebind(sql string, args []any) (string, []any, error) {
	p, err := parsed(sql)
	if err != nil {
		return "", nil, err
	}
	var pos []any
	named := map[string]any{}
	for _, a := range args {
		if n, ok := a.(Named); ok {
			named[strings.TrimLeft(n.Name, ":@$")] = n.Value
			continue
		}
		pos = append(pos, a)
	}
	if len(pos) != p.next-1 {
		return "", nil, fmt.Errorf("dbx: statement takes %d positional parameters, %d given", p.next-1, len(pos))
	}
	if len(named) != p.names {
		return "", nil, fmt.Errorf("dbx: statement takes %d named parameters, %d given", p.names, len(named))
	}
	out := make([]any, len(p.slots))
	for i, s := range p.slots {
		switch {
		case s.name != "":
			v, ok := named[s.name]
			if !ok {
				return "", nil, fmt.Errorf("dbx: no value for parameter :%s", s.name)
			}
			out[i] = v
		default:
			out[i] = pos[s.pos-1]
		}
	}
	return p.sql, out, nil
}

type slot struct {
	pos  int    // 1-based positional arg, or 0
	name string // named parameter, or ""
}

type parse struct {
	sql   string
	slots []slot // one per $n, in order
	next  int    // the number a bare ? takes: 1 + the highest positional seen (SQLite's rule)
	names int    // distinct named parameters
}

var rebindCache sync.Map // string → *parse

// rebindCacheMax bounds the cache; user SQL is varied, so past this size new
// texts are parsed each time instead of growing it without limit.
const rebindCacheMax = 4096

var rebindCacheLen int64
var rebindCacheMu sync.Mutex

func parsed(sql string) (*parse, error) {
	if v, ok := rebindCache.Load(sql); ok {
		return v.(*parse), nil
	}
	p, err := rebind(sql)
	if err != nil {
		return nil, err
	}
	rebindCacheMu.Lock()
	if rebindCacheLen < rebindCacheMax {
		if _, loaded := rebindCache.LoadOrStore(sql, p); !loaded {
			rebindCacheLen++
		}
	}
	rebindCacheMu.Unlock()
	return p, nil
}

func rebind(sql string) (*parse, error) {
	p := &parse{next: 1}
	var b strings.Builder
	b.Grow(len(sql) + 8)
	byPos := map[int]int{}     // positional arg → $n
	byName := map[string]int{} // name → $n
	emit := func(s slot) {
		var n int
		var ok bool
		if s.name != "" {
			n, ok = byName[s.name]
		} else {
			n, ok = byPos[s.pos]
		}
		if !ok {
			p.slots = append(p.slots, s)
			n = len(p.slots)
			if s.name != "" {
				byName[s.name] = n
				p.names++
			} else {
				byPos[s.pos] = n
			}
		}
		b.WriteByte('$')
		b.WriteString(strconv.Itoa(n))
	}
	for i := 0; i < len(sql); {
		c := sql[i]
		switch {
		case c == '\'' || c == '"' || c == '`':
			j := closeQuote(sql, i, c)
			b.WriteString(sql[i:j])
			i = j
		case c == '-' && i+1 < len(sql) && sql[i+1] == '-':
			j := strings.IndexByte(sql[i:], '\n')
			if j < 0 {
				j = len(sql) - i
			}
			b.WriteString(sql[i : i+j])
			i += j
		case c == '/' && i+1 < len(sql) && sql[i+1] == '*':
			j := strings.Index(sql[i+2:], "*/")
			if j < 0 {
				return nil, fmt.Errorf("dbx: unterminated comment")
			}
			b.WriteString(sql[i : i+2+j+2])
			i += 2 + j + 2
		case c == '?':
			j := i + 1
			for j < len(sql) && isDigit(sql[j]) {
				j++
			}
			if j > i+1 {
				n, _ := strconv.Atoi(sql[i+1 : j])
				if n < 1 {
					return nil, fmt.Errorf("dbx: invalid parameter %s", sql[i:j])
				}
				emit(slot{pos: n})
				p.next = max(p.next, n+1)
			} else {
				emit(slot{pos: p.next})
				p.next++
			}
			i = j
		case c == ':' && i+1 < len(sql) && sql[i+1] == ':':
			b.WriteString("::")
			i += 2
		case (c == ':' || c == '@') && i+1 < len(sql) && isIdentStart(sql[i+1]):
			j := identEnd(sql, i+1)
			emit(slot{name: sql[i+1 : j]})
			i = j
		case c == '$':
			if tag, ok := dollarTag(sql, i); ok {
				end := strings.Index(sql[i+len(tag):], tag)
				if end < 0 {
					return nil, fmt.Errorf("dbx: unterminated dollar-quoted string")
				}
				j := i + len(tag) + end + len(tag)
				b.WriteString(sql[i:j])
				i = j
				continue
			}
			if i+1 < len(sql) && isIdentStart(sql[i+1]) {
				j := identEnd(sql, i+1)
				emit(slot{name: sql[i+1 : j]})
				i = j
				continue
			}
			b.WriteByte(c)
			i++
		default:
			b.WriteByte(c)
			i++
		}
	}
	p.sql = b.String()
	return p, nil
}

// closeQuote returns the index just past the quote that closes the one at i;
// a doubled quote is an escaped quote. An unterminated quote runs to the end,
// where the backend reports it.
func closeQuote(s string, i int, q byte) int {
	for j := i + 1; j < len(s); j++ {
		if s[j] == q {
			if j+1 < len(s) && s[j+1] == q {
				j++
				continue
			}
			return j + 1
		}
	}
	return len(s)
}

// dollarTag reports a PostgreSQL dollar-quote opener ($$ or $tag$) at i.
func dollarTag(s string, i int) (string, bool) {
	j := i + 1
	if j < len(s) && s[j] == '$' {
		return "$$", true
	}
	if j >= len(s) || !isIdentStart(s[j]) {
		return "", false
	}
	k := identEnd(s, j)
	if k < len(s) && s[k] == '$' {
		return s[i : k+1], true
	}
	return "", false
}

func identEnd(s string, i int) int {
	for i < len(s) && (isIdentStart(s[i]) || isDigit(s[i])) {
		i++
	}
	return i
}

func isDigit(c byte) bool      { return c >= '0' && c <= '9' }
func isIdentStart(c byte) bool { return c == '_' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' }
