package httpapi

import (
	"bufio"
	"context"
	"net/http"
	"strings"
	"testing"
	"time"
)

// job.run events (which carry script error text) reach builders only, like
// every other job route (Phase 7 review, info item).
func TestJobRunEventsBuildersOnly(t *testing.T) {
	e := newEnv(t)
	ws, _, _, b, ed, _ := roles(t, e)
	base := "/api/v1/w/" + ws
	stream := func(c *client) <-chan string {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		t.Cleanup(cancel)
		req, _ := http.NewRequestWithContext(ctx, "GET", e.srv.URL+base+"/events", nil)
		resp, err := c.http.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = resp.Body.Close() })
		got := make(chan string, 16)
		go func() {
			sc := bufio.NewScanner(resp.Body)
			for sc.Scan() {
				if ev, ok := strings.CutPrefix(sc.Text(), "event: "); ok {
					got <- ev
				}
			}
			close(got)
		}()
		if ev := <-got; ev != "hello" {
			t.Fatalf("first event %q", ev)
		}
		return got
	}
	edEvents, bEvents := stream(ed), stream(b)

	_, sc, _ := b.do("POST", base+"/scripts", map[string]any{"name": "Fail", "kind": "job", "source": `error("secret detail")`})
	_, job, _ := b.do("POST", base+"/jobs", map[string]any{"name": "Fail", "script_id": sc["id"]})
	if st, _, _ := b.do("POST", base+"/jobs/"+job["id"].(string)+"/run", nil); st != 202 {
		t.Fatalf("run %d", st)
	}
	sawJob := func(ch <-chan string, wait time.Duration) bool {
		deadline := time.After(wait)
		for {
			select {
			case ev, ok := <-ch:
				if !ok {
					return false
				}
				if ev == "job.run" {
					return true
				}
			case <-deadline:
				return false
			}
		}
	}
	if !sawJob(bEvents, 3*time.Second) {
		t.Fatal("builder did not get job.run")
	}
	if sawJob(edEvents, 500*time.Millisecond) {
		t.Fatal("editor got job.run")
	}
}
