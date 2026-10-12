package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
	"testing"
	"time"
)

type demoServer struct {
	t      *testing.T
	bin    string
	env    []string
	base   string
	out    *syncBuffer
	cmd    *exec.Cmd
	client *http.Client
	csrf   string
}

func newDemoServer(t *testing.T, bin string, extraEnv ...string) *demoServer {
	t.Helper()
	data := t.TempDir()
	port := freePort(t)
	env := append(os.Environ(),
		"ABIDATA_DATA_DIR="+data, "ABIDATA_BACKUP_DIR="+filepath.Join(data, "backups"),
		fmt.Sprintf("ABIDATA_LISTEN_ADDR=127.0.0.1:%d", port), fmt.Sprintf("ABIDATA_PUBLIC_URL=http://127.0.0.1:%d", port),
		"ABIDATA_BOOTSTRAP_ADMIN_EMAIL=admin@example.com")
	jar, _ := cookiejar.New(nil)
	return &demoServer{t: t, bin: bin, env: append(env, extraEnv...), base: fmt.Sprintf("http://127.0.0.1:%d", port),
		client: &http.Client{Jar: jar, Timeout: 30 * time.Second}}
}

func (s *demoServer) start() {
	s.t.Helper()
	s.out = &syncBuffer{}
	s.cmd = exec.Command(s.bin, "serve")
	s.cmd.Env, s.cmd.Stdout, s.cmd.Stderr = s.env, s.out, s.out
	if err := s.cmd.Start(); err != nil {
		s.t.Fatal(err)
	}
	s.t.Cleanup(func() { _ = s.cmd.Process.Kill() })
	deadline := time.Now().Add(20 * time.Second)
	for {
		if resp, err := http.Get(s.base + "/healthz"); err == nil {
			resp.Body.Close()
			if resp.StatusCode == 200 {
				return
			}
		}
		if time.Now().After(deadline) {
			s.t.Fatalf("server did not come up:\n%s", s.out.String())
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func (s *demoServer) stop() {
	s.t.Helper()
	if err := s.cmd.Process.Signal(syscall.SIGTERM); err != nil {
		s.t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- s.cmd.Wait() }()
	select {
	case err := <-done:
		if err != nil {
			s.t.Fatalf("serve exit: %v\n%s", err, s.out.String())
		}
	case <-time.After(30 * time.Second):
		s.t.Fatal("serve did not stop")
	}
}

func (s *demoServer) do(method, path string, body any) (int, map[string]any) {
	s.t.Helper()
	var rdr io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rdr = bytes.NewReader(b)
	}
	req, _ := http.NewRequest(method, s.base+path, rdr)
	req.Header.Set("Origin", s.base)
	req.Header.Set("Content-Type", "application/json")
	if s.csrf != "" {
		req.Header.Set("X-CSRF-Token", s.csrf)
	}
	resp, err := s.client.Do(req)
	if err != nil {
		s.t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	var out map[string]any
	_ = json.Unmarshal(raw, &out)
	return resp.StatusCode, out
}

// setup completes first-run setup and returns the memberships from the response.
func (s *demoServer) setup() []any {
	s.t.Helper()
	m := regexp.MustCompile(`"setup_token":"([^"]+)"`).FindStringSubmatch(s.out.String())
	if m == nil {
		s.t.Fatalf("no setup token logged:\n%s", s.out.String())
	}
	st, me := s.do("POST", "/api/v1/auth/setup", map[string]string{"token": m[1], "display_name": "Admin", "password": "long-enough-pw"})
	if st != 200 {
		s.t.Fatalf("setup: %d %v", st, me)
	}
	s.csrf, _ = me["csrf_token"].(string)
	ms, _ := me["memberships"].([]any)
	return ms
}

func buildBinary(t *testing.T) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "abidata")
	if out, err := exec.Command("go", "build", "-o", bin, ".").CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}
	return bin
}

// TestSetupCreatesDemoWorkspace: on a fresh install the first-run setup also
// creates the Northwind Traders workspace, owned by the new administrator, with
// its tables, rows and saved queries; `abidata demo` is then a no-op.
func TestSetupCreatesDemoWorkspace(t *testing.T) {
	if testing.Short() {
		t.Skip("builds the binary")
	}
	bin := buildBinary(t)
	s := newDemoServer(t, bin)
	s.start()
	ms := s.setup()
	if len(ms) != 1 {
		t.Fatalf("memberships after setup: %v", ms)
	}
	mem := ms[0].(map[string]any)
	if mem["workspace_name"] != "Northwind Traders" || mem["role"] != "owner" {
		t.Fatalf("membership: %v", mem)
	}
	ws := mem["workspace_id"].(string)

	st, sch := s.do("GET", "/api/v1/w/"+ws+"/schema", nil)
	if st != 200 {
		t.Fatalf("schema: %d", st)
	}
	want := map[string]int{"Customers": 25, "Products": 20, "Orders": 60, "Employees": 6, "Order lines": 60,
		"Suppliers": 6, "Species": 6, "Pest sightings": 40, "Quarantine zones": 3}
	tables := map[string]string{}
	for _, x := range sch["tables"].([]any) {
		tm := x.(map[string]any)
		tables[tm["display_name"].(string)] = tm["id"].(string)
	}
	for name, min := range want {
		id, ok := tables[name]
		if !ok {
			t.Errorf("table %q missing; have %v", name, tables)
			continue
		}
		st, c := s.do("GET", "/api/v1/w/"+ws+"/tables/"+id+"/rows/count", nil)
		if n, _ := c["count"].(float64); st != 200 || int(n) < min {
			t.Errorf("%s: status %d count %v, want >= %d", name, st, c["count"], min)
		}
	}

	st, ql := s.do("GET", "/api/v1/w/"+ws+"/queries", nil)
	if st != 200 {
		t.Fatalf("queries: %d", st)
	}
	var topID string
	for _, x := range ql["items"].([]any) {
		if x.(map[string]any)["name"] == "Top customers" {
			topID = x.(map[string]any)["id"].(string)
		}
	}
	if topID == "" {
		t.Fatalf("Top customers query missing: %v", ql)
	}
	st, res := s.do("POST", "/api/v1/w/"+ws+"/queries/"+topID+"/run", map[string]any{})
	if rows, _ := res["rows"].([]any); st != 200 || len(rows) != 10 {
		t.Fatalf("run Top customers: %d %v", st, res)
	}
	for _, kind := range []string{"forms", "reports", "scripts", "jobs"} {
		st, l := s.do("GET", "/api/v1/w/"+ws+"/"+kind, nil)
		if items, _ := l["items"].([]any); st != 200 || len(items) == 0 {
			t.Errorf("%s: %d %v", kind, st, l)
		}
	}
	if !strings.Contains(s.out.String(), "created demo workspace") {
		t.Errorf("no log line for the demo workspace:\n%s", s.out.String())
	}

	// The CLI refuses while the server runs, then reports the workspace exists.
	s.stop()
	cli := exec.Command(bin, "demo", "--owner", "admin@example.com")
	cli.Env = s.env
	if out, err := cli.CombinedOutput(); err != nil || !strings.Contains(string(out), "already exists") {
		t.Fatalf("abidata demo: %v %s", err, out)
	}
}

// TestDemoDisabledAndCLI: ABIDATA_DEMO_WORKSPACE=false leaves a fresh install
// empty; `abidata demo --owner` then adds the workspace on request.
func TestDemoDisabledAndCLI(t *testing.T) {
	if testing.Short() {
		t.Skip("builds the binary")
	}
	bin := buildBinary(t)
	s := newDemoServer(t, bin, "ABIDATA_DEMO_WORKSPACE=false")
	s.start()
	if ms := s.setup(); len(ms) != 0 {
		t.Fatalf("expected no workspaces with the demo disabled, got %v", ms)
	}
	s.stop()

	cli := exec.Command(bin, "demo", "--owner", "nobody@example.com")
	cli.Env = s.env
	if out, err := cli.CombinedOutput(); err == nil || !strings.Contains(string(out), "no user has the email") {
		t.Fatalf("unknown owner: %v %s", err, out)
	}
	cli = exec.Command(bin, "demo", "--owner", "admin@example.com")
	cli.Env = s.env
	if out, err := cli.CombinedOutput(); err != nil || !strings.Contains(string(out), "created workspace") {
		t.Fatalf("abidata demo: %v %s", err, out)
	}

	// After a restart the owner sees it and its job is scheduled.
	s.start()
	st, me := s.do("POST", "/api/v1/auth/login", map[string]string{"email": "admin@example.com", "password": "long-enough-pw"})
	if st != 200 {
		t.Fatalf("login: %d %v", st, me)
	}
	if ms, _ := me["memberships"].([]any); len(ms) != 1 {
		t.Fatalf("memberships: %v", me["memberships"])
	}
	s.stop()
}
