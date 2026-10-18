package httpapi

import (
	"context"
	"errors"
	"testing"

	"abidata/internal/demo"
)

type fakeProvisioner struct {
	calls   int
	kinds   []demo.Kind
	started chan struct{}
	release chan struct{}
}

func (f *fakeProvisioner) ProvisionAll(_ context.Context, kind demo.Kind) (demo.Provisioned, error) {
	f.calls++
	f.kinds = append(f.kinds, kind)
	if f.started != nil {
		close(f.started)
		<-f.release
	}
	return demo.Provisioned{Created: []string{"a@example.com"}, Skipped: []string{"admin@example.com"},
		Failed: []demo.ProvisionError{{Email: "b@example.com", Err: errors.New("disk full at /secret/path")}}}, nil
}

func TestAdminProvisionDemos(t *testing.T) {
	e := newEnv(t)
	fake := &fakeProvisioner{}
	e.s.deps.Demo = fake
	e.user("admin@example.com", true)
	e.user("editor@example.com", false)
	const path = "/api/v1/admin/demo-workspaces"

	traders := map[string]any{"kind": "traders"}
	if st, body, _ := e.login("editor@example.com").do("POST", path, traders); st != 403 || fake.calls != 0 {
		t.Fatalf("non-admin: status %d (%v), %d calls; want 403 and no run", st, body, fake.calls)
	}
	adm := e.login("admin@example.com")
	for _, bad := range []map[string]any{nil, {"kind": "everything"}} {
		if st, body, _ := adm.do("POST", path, bad); st != 400 && st != 422 {
			t.Errorf("kind %v: status %d (%v), want a validation error", bad, st, body)
		}
	}
	if fake.calls != 0 {
		t.Fatalf("an invalid kind ran the provisioner %d times", fake.calls)
	}
	st, body, _ := adm.do("POST", path, traders)
	if st != 200 {
		t.Fatalf("admin: status %d (%v)", st, body)
	}
	created, _ := body["created"].([]any)
	skipped, _ := body["skipped"].([]any)
	failed, _ := body["failed"].([]any)
	if len(created) != 1 || created[0] != "a@example.com" || len(skipped) != 1 || len(failed) != 1 {
		t.Fatalf("body = %v", body)
	}
	// The failure names the user but not the internal error.
	f0, _ := failed[0].(map[string]any)
	if f0["email"] != "b@example.com" || f0["message"] == "" || f0["message"] == "disk full at /secret/path" {
		t.Fatalf("failure entry = %v", f0)
	}

	// A second press while a run is in progress is refused, not run twice.
	fake.started, fake.release = make(chan struct{}), make(chan struct{})
	done := make(chan int)
	go func() { st, _, _ := adm.do("POST", path, map[string]any{"kind": "invasives"}); done <- st }()
	<-fake.started
	if st, body, _ := adm.do("POST", path, traders); st != 503 {
		t.Errorf("concurrent run: status %d (%v), want 503", st, body)
	}
	close(fake.release)
	if st := <-done; st != 200 {
		t.Errorf("first run: status %d", st)
	}
	if fake.calls != 2 || fake.kinds[0] != demo.Traders || fake.kinds[1] != demo.Invasives {
		t.Errorf("ProvisionAll ran %d times with %v, want traders then invasives", fake.calls, fake.kinds)
	}

	e.s.deps.Demo = nil
	if st, _, _ := adm.do("POST", path, traders); st != 404 {
		t.Errorf("without a provisioner: status %d, want 404", st)
	}
}
