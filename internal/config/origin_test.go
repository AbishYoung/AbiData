package config

import "testing"

func TestOriginAllowed(t *testing.T) {
	loop := Config{PublicURL: "http://localhost:18100"}
	lan := Config{PublicURL: "http://localhost:18100", AllowedOrigins: []string{"http://192.168.1.20:18100"}}
	prod := Config{PublicURL: "https://data.example.com"}
	cases := []struct {
		name   string
		c      Config
		origin string
		want   bool
	}{
		{"public URL", loop, "http://localhost:18100", true},
		{"127.0.0.1 is the same loopback origin", loop, "http://127.0.0.1:18100", true},
		{"[::1] is the same loopback origin", loop, "http://[::1]:18100", true},
		{"another loopback port is another origin", loop, "http://localhost:5173", false},
		{"another scheme is another origin", loop, "https://127.0.0.1:18100", false},
		{"a LAN address is refused unless listed", loop, "http://192.168.1.20:18100", false},
		{"a listed LAN address", lan, "http://192.168.1.20:18100", true},
		{"a look-alike host", loop, "http://localhost.evil.test:18100", false},
		{"no Origin", loop, "", false},
		{"null Origin", loop, "null", false},
		{"production public URL", prod, "https://data.example.com", true},
		{"loopback is not special when the public URL is not loopback", prod, "http://localhost", false},
		{"production refuses other hosts", prod, "https://evil.test", false},
	}
	for _, tc := range cases {
		if got := tc.c.OriginAllowed(tc.origin); got != tc.want {
			t.Errorf("%s: OriginAllowed(%q) = %v, want %v", tc.name, tc.origin, got, tc.want)
		}
	}
}

func TestLoadAllowedOrigins(t *testing.T) {
	env := map[string]string{"ABIDATA_ALLOWED_ORIGINS": " http://192.168.1.20:18100/ , https://lab.example.com "}
	c, err := Load(func(k string) string { return env[k] }, nil)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"http://192.168.1.20:18100", "https://lab.example.com"}
	if len(c.AllowedOrigins) != 2 || c.AllowedOrigins[0] != want[0] || c.AllowedOrigins[1] != want[1] {
		t.Fatalf("AllowedOrigins = %q, want %q", c.AllowedOrigins, want)
	}
	for _, bad := range []string{"192.168.1.20", "ftp://x.test", "http://x.test/path", "http://*.example.com", "http://u@x.test", " , "} {
		env["ABIDATA_ALLOWED_ORIGINS"] = bad
		if _, err := Load(func(k string) string { return env[k] }, nil); err == nil {
			t.Errorf("ABIDATA_ALLOWED_ORIGINS=%q: want an error", bad)
		}
	}
}
