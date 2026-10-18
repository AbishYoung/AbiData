package exprlang

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

type tokKind int

const (
	tEOF tokKind = iota
	tNumber
	tString
	tIdent  // bare name or keyword; keywords are matched case-insensitively
	tQIdent // "quoted name"
	tOp     // operators and punctuation
)

type token struct {
	kind tokKind
	text string // number text, string value, name, or operator
	pos  int    // byte offset in the source
}

// keyword reports whether t is the bare keyword kw (upper case).
func (t token) keyword(kw string) bool { return t.kind == tIdent && strings.EqualFold(t.text, kw) }

func (t token) op(s string) bool { return t.kind == tOp && t.text == s }

// lex splits src into tokens. It rejects what an expression may never contain,
// with the reason: statement separators, comments, parameters, and the
// bracket and backtick quoting the grammar does not use.
func lex(src string) ([]token, error) {
	var out []token
	i := 0
	for i < len(src) {
		c := src[i]
		switch {
		case c == ' ' || c == '\t' || c == '\n' || c == '\r':
			i++
		case c == 0:
			return nil, errAt(src, i, "NUL characters are not allowed")
		case c == '\'':
			s, n, ok := quoted(src, i, '\'')
			if !ok {
				return nil, errAt(src, i, "this text is not closed with '")
			}
			out = append(out, token{tString, s, i})
			i = n
		case c == '"':
			s, n, ok := quoted(src, i, '"')
			if !ok {
				return nil, errAt(src, i, `this name is not closed with "`)
			}
			if s == "" {
				return nil, errAt(src, i, "a name cannot be empty")
			}
			out = append(out, token{tQIdent, s, i})
			i = n
		case c >= '0' && c <= '9' || c == '.' && i+1 < len(src) && src[i+1] >= '0' && src[i+1] <= '9':
			n := number(src, i)
			if n < len(src) && isIdentByte(src[n]) {
				return nil, errAt(src, i, "a number cannot run into a name; put a space or an operator between them")
			}
			out = append(out, token{tNumber, src[i:n], i})
			i = n
		case c == '-' && i+1 < len(src) && src[i+1] == '-', c == '/' && i+1 < len(src) && src[i+1] == '*':
			return nil, errAt(src, i, "comments are not allowed")
		case c == ';':
			return nil, errAt(src, i, "';' is not allowed: an expression is a single value")
		case c == '?' || c == ':' || c == '@' || c == '$':
			return nil, errAt(src, i, "parameters are not allowed")
		case c == '[' || c == '`':
			return nil, errAt(src, i, `use double quotes for names ("Unit price"), not brackets or backticks`)
		case isIdentByte(c) || c >= utf8.RuneSelf:
			n := i
			for n < len(src) {
				r, w := utf8.DecodeRuneInString(src[n:])
				if r != '_' && !unicode.IsLetter(r) && !unicode.IsDigit(r) {
					break
				}
				n += w
			}
			if n == i {
				r, _ := utf8.DecodeRuneInString(src[i:])
				return nil, errAt(src, i, "%q is not allowed here", r)
			}
			out = append(out, token{tIdent, src[i:n], i})
			i = n
		default:
			op := ""
			for _, o := range []string{"||", "<=", ">=", "<>", "!=", "==", "+", "-", "*", "/", "%", "=", "<", ">", "(", ")", ",", "."} {
				if strings.HasPrefix(src[i:], o) {
					op = o
					break
				}
			}
			if op == "" {
				return nil, errAt(src, i, "%q is not allowed here", rune(c))
			}
			out = append(out, token{tOp, op, i})
			i += len(op)
		}
	}
	return append(out, token{tEOF, "", len(src)}), nil
}

func isIdentByte(c byte) bool {
	return c == '_' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9'
}

// quoted reads a q-quoted run starting at i; a doubled q is one q.
func quoted(s string, i int, q byte) (string, int, bool) {
	var b strings.Builder
	for j := i + 1; j < len(s); j++ {
		if s[j] == q {
			if j+1 < len(s) && s[j+1] == q {
				b.WriteByte(q)
				j++
				continue
			}
			return b.String(), j + 1, true
		}
		b.WriteByte(s[j])
	}
	return "", 0, false
}

// number returns the end of the numeric literal at i: decimal with optional
// fraction and exponent, or 0x hexadecimal.
func number(s string, i int) int {
	if strings.HasPrefix(s[i:], "0x") || strings.HasPrefix(s[i:], "0X") {
		j := i + 2
		for j < len(s) && strings.IndexByte("0123456789abcdefABCDEF", s[j]) >= 0 {
			j++
		}
		return j
	}
	j := i
	digits := func() {
		for j < len(s) && s[j] >= '0' && s[j] <= '9' {
			j++
		}
	}
	digits()
	if j < len(s) && s[j] == '.' {
		j++
		digits()
	}
	if j < len(s) && (s[j] == 'e' || s[j] == 'E') {
		k := j + 1
		if k < len(s) && (s[k] == '+' || s[k] == '-') {
			k++
		}
		if k < len(s) && s[k] >= '0' && s[k] <= '9' {
			j = k
			digits()
		}
	}
	return j
}
