package httpapi

import "testing"

// A dev server on loopback accepts every spelling of its own address, so a
// browser at http://127.0.0.1:port can sign in; other origins stay refused.
func TestOriginCheckLoopbackAliases(t *testing.T) {
	e := newEnv(t)
	e.user("dev@example.com", true)
	e.s.deps.Config.PublicURL = "http://localhost:18100"
	e.s.deps.Config.AllowedOrigins = []string{"http://192.168.1.20:18100"}
	login := map[string]any{"email": "dev@example.com", "password": "long-enough-pw"}
	for origin, want := range map[string]int{
		"http://localhost:18100":       200,
		"http://127.0.0.1:18100":       200,
		"http://[::1]:18100":           200,
		"http://192.168.1.20:18100":    200,
		"http://localhost:5173":        403,
		"http://192.168.1.21:18100":    403,
		"http://abidata.test":          403,
		"http://localhost.evil.test:1": 403,
	} {
		if st, body, _ := e.client().do("POST", "/api/v1/auth/login", login, withOrigin(origin)); st != want {
			t.Errorf("login with Origin %s: status %d, want %d (%v)", origin, st, want, body)
		}
	}
	if st, _, _ := e.client().do("POST", "/api/v1/auth/login", login, noOrigin); st != 403 {
		t.Errorf("login without Origin: status %d, want 403", st)
	}
}
