package all_test

import (
	"context"
	"strings"
	"testing"

	"abidata/internal/ids"
	"abidata/internal/script"
)

func TestURLModule(t *testing.T) {
	// url is available in UIEvent, Report, and Job contexts
	for _, ctx := range script.AllContexts {
		res, err := runWith(t, ctx, nil, `
			local u = url.parse("https://user@api.example.com:8443/v1/search?q=test&limit=10&tag=a&tag=b#heading")
			local b = url.build({scheme = "https", host = "api.example.com", path = "/v1/test", query = {a = 1, b = "hello"}})
			local enc = url.encode("hello world & goodbye")
			local dec = url.decode(enc)
			local qenc = url.query_encode({foo = "bar", num = 42})
			local qdec = url.query_decode(qenc)
			return u.scheme, u.hostname, u.port, u.path, u.query.q, u.query.tag[2], u.fragment, b, dec, qdec.foo, qdec.num
		`)
		if err != nil {
			t.Fatalf("context %s: %v", ctx, err)
		}
		if res.Values[0] != "https" || res.Values[1] != "api.example.com" || res.Values[2] != "8443" || res.Values[3] != "/v1/search" {
			t.Fatalf("context %s: parse values mismatch: %v", ctx, res.Values)
		}
		if res.Values[4] != "test" || res.Values[5] != "b" || res.Values[6] != "heading" {
			t.Fatalf("context %s: query/fragment values mismatch: %v", ctx, res.Values)
		}
		if res.Values[7] != "https://api.example.com/v1/test?a=1&b=hello" {
			t.Fatalf("context %s: build mismatch: %v", ctx, res.Values[7])
		}
		if res.Values[8] != "hello world & goodbye" {
			t.Fatalf("context %s: decode mismatch: %v", ctx, res.Values[8])
		}
		if res.Values[9] != "bar" || res.Values[10] != "42" {
			t.Fatalf("context %s: query_decode mismatch: %v", ctx, res.Values)
		}
	}
}

func TestCryptoModule(t *testing.T) {
	for _, ctx := range script.AllContexts {
		res, err := runWith(t, ctx, nil, `
			local s256 = crypto.sha256("hello")
			local s512 = crypto.sha512("hello")
			local h256 = crypto.hmac_sha256("key", "hello")
			local h512 = crypto.hmac_sha512("key", "hello")
			local b64 = crypto.base64_encode("hello world")
			local b64dec = crypto.base64_decode(b64)
			local b64url = crypto.base64url_encode("hello ? world")
			local b64urldec = crypto.base64url_decode(b64url)
			local hexenc = crypto.hex_encode("abc")
			local hexdec = crypto.hex_decode(hexenc)
			local rhex = crypto.random_hex(16)
			local uid = crypto.uuid()
			return #s256, #s512, #h256, #h512, b64dec, b64urldec, hexdec, #rhex, #uid
		`)
		if err != nil {
			t.Fatalf("context %s: %v", ctx, err)
		}
		if res.Values[0] != int64(64) || res.Values[1] != int64(128) || res.Values[2] != int64(64) || res.Values[3] != int64(128) {
			t.Fatalf("context %s: hash lengths: %v", ctx, res.Values)
		}
		if res.Values[4] != "hello world" || res.Values[5] != "hello ? world" || res.Values[6] != "abc" {
			t.Fatalf("context %s: decodes: %v", ctx, res.Values)
		}
		if res.Values[7] != int64(32) || res.Values[8] != int64(36) {
			t.Fatalf("context %s: random/uuid lengths: %v", ctx, res.Values)
		}
	}

	// Test deterministic values
	res, err := runWith(t, script.Job, nil, `
		return crypto.sha256(""), crypto.base64_encode("Antigravity")
	`)
	if err != nil {
		t.Fatal(err)
	}
	// sha256("") is e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855
	if res.Values[0] != "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855" {
		t.Fatalf("sha256 mismatch: %v", res.Values[0])
	}
	if res.Values[1] != "QW50aWdyYXZpdHk=" {
		t.Fatalf("base64 mismatch: %v", res.Values[1])
	}
}

func TestCryptoStringLimits(t *testing.T) {
	proto, err := script.Compile(`return crypto.random_hex(2000)`, "t")
	if err == nil {
		_, err = runner.Exec(context.Background(), script.ExecRequest{
			Kind:      script.UIEvent,
			Workspace: ids.NewWorkspaceID(),
			Proto:     proto,
			Budget:    script.Budget{MaxString: 100},
		})
		if err == nil || !strings.Contains(err.Error(), "between 1 and 1024") {
			t.Fatalf("expected error for random_hex > 1024: %v", err)
		}
	}
}
