package files

import (
	"path"
	"strings"
	"unicode"
	"unicode/utf8"
)

// MaxNameRunes is the longest display name kept for a file.
const MaxNameRunes = 255

// SanitizeName turns a client-supplied file name into a safe display name: any
// directory part is dropped (both / and \ separate), control characters and the
// invisible bidirectional overrides that disguise extensions are removed, runs
// of white space collapse, and the result is cut to MaxNameRunes while keeping
// the extension. A name with nothing left becomes "file". The name is metadata
// only: the bytes are stored under their hash, never under this name.
func SanitizeName(name string) string {
	if !utf8.ValidString(name) {
		name = strings.ToValidUTF8(name, "�")
	}
	if i := strings.LastIndexAny(name, `/\`); i >= 0 {
		name = name[i+1:]
	}
	var b strings.Builder
	space := false
	for _, r := range name {
		switch {
		case unicode.IsControl(r) || isInvisibleFormat(r):
			continue
		case unicode.IsSpace(r):
			space = true
			continue
		}
		if space && b.Len() > 0 {
			b.WriteByte(' ')
		}
		space = false
		b.WriteRune(r)
	}
	out := strings.Trim(b.String(), ". ")
	if out == "" {
		return "file"
	}
	if utf8.RuneCountInString(out) > MaxNameRunes {
		ext := path.Ext(out)
		if utf8.RuneCountInString(ext) > 32 {
			ext = ""
		}
		keep := MaxNameRunes - utf8.RuneCountInString(ext)
		runes := []rune(strings.TrimSuffix(out, ext))
		out = string(runes[:keep]) + ext
	}
	return out
}

// isInvisibleFormat reports bidi controls, zero-width characters and the byte
// order mark, which can make "txt.exe" read as "exe.txt" in a file list.
func isInvisibleFormat(r rune) bool {
	switch {
	case r >= 0x200B && r <= 0x200F, r >= 0x202A && r <= 0x202E, r >= 0x2060 && r <= 0x2064, r >= 0x2066 && r <= 0x2069:
		return true
	case r == 0xFEFF, r == 0x061C:
		return true
	}
	return false
}
