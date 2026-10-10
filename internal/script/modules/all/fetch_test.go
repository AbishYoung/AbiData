package all_test

import (
	"context"
	"strings"
	"testing"

	"abidata/internal/egress"
	"abidata/internal/fault"
	"abidata/internal/ids"
	"abidata/internal/script"
	"abidata/internal/script/host"
)

func runWith(t *testing.T, kind script.Context, d *host.Data, src string) (script.ExecResult, error) {
	t.Helper()
	proto, err := script.Compile(src, "t")
	if err != nil {
		t.Fatal(err)
	}
	return runner.Exec(context.Background(), script.ExecRequest{Kind: kind, Workspace: ids.NewWorkspaceID(), Proto: proto, Host: d})
}

func TestFetch(t *testing.T) {
	var got egress.Request
	d := &host.Data{Fetch: func(_ context.Context, req egress.Request) (*egress.Response, error) {
		got = req
		if strings.Contains(req.URL, "denied") {
			return nil, fault.New(fault.EgressDenied, "nope")
		}
		return &egress.Response{Status: 201, Headers: map[string]string{"content-type": "application/json"}, Body: []byte(`{"items":[1,2,3],"empty":[],"n":null}`)}, nil
	}}
	res, err := runWith(t, script.Job, d, `
		local r = fetch("https://api.test/x", {method = "POST", headers = {Accept = "application/json"}, body = "hi", timeout_ms = 1500, max_bytes = 1024})
		local j = r:json()
		return r.status, r.ok, r.headers["content-type"], #j.items, json.encode(j.empty), j.n == json.null`)
	if err != nil {
		t.Fatal(err)
	}
	want := []any{int64(201), true, "application/json", int64(3), "[]", true}
	for i, w := range want {
		if res.Values[i] != w {
			t.Fatalf("value %d: %#v, want %#v (all %#v)", i, res.Values[i], w, res.Values)
		}
	}
	if got.Method != "POST" || string(got.Body) != "hi" || got.Headers["Accept"] != "application/json" || got.Timeout.Milliseconds() != 1500 || got.MaxBytes != 1024 {
		t.Fatalf("request %+v", got)
	}

	// Gateway errors keep their code; scripts may catch them.
	_, err = runWith(t, script.Job, d, `fetch("https://denied.test/")`)
	if fault.Classify(err).Code != fault.EgressDenied {
		t.Fatalf("uncaught: %v", err)
	}
	res, err = runWith(t, script.Job, d, `local ok, e = pcall(fetch, "https://denied.test/") return ok, tostring(e):find("nope") ~= nil`)
	if err != nil || res.Values[0] != false || res.Values[1] != true {
		t.Fatalf("pcall: %v %#v", err, res.Values)
	}
	// Options are validated and clamped (and the body never exceeds the string cap).
	for _, bad := range []string{`{timeout_ms = 0/0}`, `{timeout_ms = -5}`, `{max_bytes = 0/0}`, `{max_bytes = -1}`} {
		if _, err := runWith(t, script.Job, d, `fetch("https://api.test/", `+bad+`)`); err == nil || !strings.Contains(err.Error(), "positive") {
			t.Fatalf("%s accepted: %v", bad, err)
		}
	}
	if _, err := runWith(t, script.Job, d, `fetch("https://api.test/", {timeout_ms = 1e300, max_bytes = 1e300})`); err != nil {
		t.Fatal(err)
	}
	if got.Timeout != egress.MaxTimeout || got.MaxBytes != egress.MaxBytes {
		t.Fatalf("not clamped: %+v", got)
	}
	_, err = runWith(t, script.Job, d, `fetch("https://api.test/", {body = {}})`)
	if err == nil || !strings.Contains(err.Error(), "body must be a string") {
		t.Fatalf("bad body: %v", err)
	}

	// Gate 3: fetch in a form event is unavailable.
	res, err = runWith(t, script.UIEvent, d, `return fetch == nil`)
	if err != nil || res.Values[0] != true {
		t.Fatalf("ui_event: %v %#v", err, res.Values)
	}
	// A job without the gateway binding is denied, not crashed.
	_, err = runWith(t, script.Job, &host.Data{}, `fetch("https://api.test/")`)
	if fault.Classify(err).Code != fault.EgressDenied {
		t.Fatalf("no gateway: %v", err)
	}
}
