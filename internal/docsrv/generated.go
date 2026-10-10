package docsrv

import (
	"fmt"
	"strings"

	"abidata/internal/fault"
	"abidata/internal/script"
)

// Generated reference page slugs.
const (
	LuaAPISlug = "reference/lua-api"
	ErrorsSlug = "reference/errors"
)

// Anchor returns the heading id used for a Lua function or module name
// ("db.query" → "db-query").
func Anchor(name string) string {
	return strings.NewReplacer(".", "-", "_", "-", ":", "-").Replace(strings.ToLower(name))
}

// ErrorAnchor returns the heading id of an error code's section
// ("CONCURRENT_MODIFICATION" → "concurrent-modification").
func ErrorAnchor(c fault.Code) string { return Anchor(string(c)) }

// Generated returns the pages built from code: the Lua API reference (from the
// manifest) and the error reference (from the error catalog).
func Generated() []Source {
	return []Source{{Slug: LuaAPISlug, Markdown: luaAPIMarkdown()}, {Slug: ErrorsSlug, Markdown: errorsMarkdown()}}
}

func luaAPIMarkdown() string {
	var b strings.Builder
	b.WriteString("---\ntitle: Lua API reference\norder: 10\nsummary: Every function scripts can call, with parameters, errors and examples.\n---\n\n")
	b.WriteString("# Lua API reference\n\nThis page is generated from the server's function registry. Each function lists the contexts it is available in: ")
	b.WriteString("`ui_event` (form events), `report` (report expressions) and `job` (scheduled and on-demand jobs).\n\n")
	for _, m := range script.Manifest() {
		fmt.Fprintf(&b, "## %s {#%s}\n\n%s\n\n", m.Name, Anchor("module-"+m.Name), m.Doc)
		fmt.Fprintf(&b, "Available in: %s.\n\n", contexts(m.Contexts))
		for _, v := range m.Values {
			fmt.Fprintf(&b, "- `%s` (%s): %s\n", v.Name, v.Type, v.Doc)
		}
		if len(m.Values) > 0 {
			b.WriteString("\n")
		}
		for _, f := range m.Funcs {
			fmt.Fprintf(&b, "### %s {#%s}\n\n`%s`\n\n%s\n\n", f.Name, Anchor(f.Name), f.Signature, f.Doc)
			if len(f.Contexts) > 0 {
				fmt.Fprintf(&b, "Available in: %s.\n\n", contexts(f.Contexts))
			}
			if len(f.Params) > 0 {
				b.WriteString("| Parameter | Type | Description |\n|---|---|---|\n")
				for _, p := range f.Params {
					opt := ""
					if p.Opt {
						opt = " (optional)"
					}
					fmt.Fprintf(&b, "| `%s`%s | %s | %s |\n", p.Name, opt, cell(p.Type), cell(p.Doc))
				}
				b.WriteString("\n")
			}
			if f.Returns != "" {
				fmt.Fprintf(&b, "Returns: %s\n\n", f.Returns)
			}
			if len(f.Errors) > 0 {
				b.WriteString("Errors:\n\n")
				for _, e := range f.Errors {
					fmt.Fprintf(&b, "- %s\n", errorLink(e))
				}
				b.WriteString("\n")
			}
			if f.Example != "" {
				fmt.Fprintf(&b, "```lua\n%s\n```\n\n", f.Example)
			}
		}
	}
	return b.String()
}

// errorLink links a leading error code ("EGRESS_DENIED: …") to its section.
func errorLink(e string) string {
	code, rest, ok := strings.Cut(e, ":")
	if ok {
		for _, c := range fault.Codes() {
			if string(c) == strings.TrimSpace(code) {
				return fmt.Sprintf("[`%s`](%s%s#%s):%s", c, LinkPrefix, ErrorsSlug, ErrorAnchor(c), rest)
			}
		}
	}
	return e
}

func contexts(cs []script.Context) string {
	if len(cs) == 0 {
		return "all contexts"
	}
	parts := make([]string, len(cs))
	for i, c := range cs {
		parts[i] = "`" + string(c) + "`"
	}
	return strings.Join(parts, ", ")
}

func cell(s string) string { return strings.ReplaceAll(strings.ReplaceAll(s, "|", `\|`), "\n", " ") }

func errorsMarkdown() string {
	var b strings.Builder
	b.WriteString("---\ntitle: Error reference\norder: 20\nsummary: What each error code means and how to recover.\n---\n\n")
	b.WriteString("# Error reference\n\nEvery error the server returns carries one of these codes. Error dialogs link here.\n\n")
	for _, c := range fault.Codes() {
		e := fault.Lookup(c)
		fmt.Fprintf(&b, "## %s {#%s}\n\n**%s** (HTTP %d)\n\n%s\n\n", c, ErrorAnchor(c), e.Title, e.HTTPStatus, e.Message)
		fmt.Fprintf(&b, "What to do: %s\n\n", e.Recovery)
		if e.Retryable {
			b.WriteString("Retrying after a short wait can succeed.\n\n")
		}
	}
	return b.String()
}
