package egress

// Phase 5 security review repros (assert current, vulnerable behaviour).

import (
	"context"
	"net/http"
	"net/netip"
	"os"
	"sync/atomic"
	"testing"
)

// On a redirect to a different allowlisted host, net/http drops only
// Authorization/Cookie/WWW-Authenticate; any other credential header a job set
// (X-Api-Key, a bearer token in a custom header) is replayed to the new host.
// One rate-limit token also pays for the whole redirect chain (up to 6 requests).
func TestSecReview_RedirectForwardsCustomCredentialHeaders(t *testing.T) {
	if os.Getenv("ABIDATA_SECREVIEW") == "" {
		t.Skip("security review repro; set ABIDATA_SECREVIEW=1")
	}
	e := newEnv(t)
	var leaked atomic.Value
	port2, _ := server(t, func(w http.ResponseWriter, r *http.Request) {
		leaked.Store(r.Header.Get("X-Api-Key") + "|" + r.Header.Get("Authorization"))
		_, _ = w.Write([]byte("ok"))
	})
	port1, hits1 := server(t, func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "http://other.test:"+port2+"/collect", http.StatusFound)
	})
	e.permit("api.test", port1)
	e.permit("other.test", port2)
	e.lookup = func(string) []netip.Addr { return []netip.Addr{netip.MustParseAddr("127.0.0.1")} }
	_, err := e.g.Do(context.Background(), ws, Request{URL: "http://api.test:" + port1 + "/", Headers: map[string]string{
		"X-Api-Key": "secret-key", "Authorization": "Bearer secret-token"}})
	if err != nil {
		t.Fatal(err)
	}
	got, _ := leaked.Load().(string)
	if got != "secret-key|" || hits1.Load() != 1 {
		t.Fatalf("expected X-Api-Key (but not Authorization) to reach the redirect target, got %q", got)
	}
}
