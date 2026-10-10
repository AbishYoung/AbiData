package script

import (
	"strings"
	"testing"
)

func TestLogCountsFields(t *testing.T) {
	r := &Run{}
	big := strings.Repeat("x", 100<<10)
	for range 10 {
		r.Log("info", "m", map[string]any{"f": big})
	}
	total := 0
	for _, e := range r.Logs() {
		total += logSize(e)
	}
	if total > maxLogBytes {
		t.Fatalf("log holds %d bytes (cap %d)", total, maxLogBytes)
	}
	r.Log("info", "huge", map[string]any{"f": strings.Repeat("y", 1<<20)})
	if l := r.Logs(); l[len(l)-1].Fields["_truncated"] == nil {
		t.Fatalf("oversized entry kept its fields")
	}
}
