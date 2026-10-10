package docsrv

import (
	"strings"
	"testing"
)

// The code theme follows a forced [data-theme], not only the system setting.
func TestHighlightCSSFollowsDataTheme(t *testing.T) {
	css, err := HighlightCSS()
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`:root:not([data-theme="dark"]) .chroma`, `:root[data-theme="dark"] .chroma`, `:root:not([data-theme="light"]) .chroma`} {
		if !strings.Contains(css, want) {
			t.Errorf("missing %s", want)
		}
	}
	for _, line := range strings.Split(css, "\n") {
		if strings.Contains(line, "{") && !strings.Contains(line, ":root") && strings.TrimSpace(line) != "@media (prefers-color-scheme: dark) {" {
			t.Fatalf("unscoped rule: %s", line)
		}
	}
	t.Log(css[:300])
}
