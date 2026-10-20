// Package crypto provides safe, in-memory cryptographic hashing, HMAC signatures,
// encoding and secure random generators. It performs no networking or DNS operations.
package crypto

import (
	"crypto/hmac"
	crand "crypto/rand"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/base64"
	"encoding/hex"
	"fmt"

	lua "github.com/yuin/gopher-lua"

	"abidata/internal/script"
)

func init() {
	script.Register(script.ModuleDef{Doc: doc, Build: build})
}

var allContexts = []script.Context{script.UIEvent, script.Report, script.Job}

var doc = script.ModuleDoc{
	Name:     "crypto",
	Doc:      "Pure cryptographic hashing, HMAC signatures, binary encoding and secure random generators. Evaluated in-memory with zero network or DNS lookups.",
	Contexts: allContexts,
	Funcs: []script.FuncDoc{
		{
			Name: "sha256", Signature: "crypto.sha256(data) → string",
			Params: []script.ParamDoc{
				{Name: "data", Type: "string", Doc: "Data to hash."},
			},
			Returns: "hex-encoded SHA-256 digest (64 characters)",
			Doc:     "Calculates the SHA-256 hash of a string.",
			Example: `local hash = crypto.sha256("hello world")`,
		},
		{
			Name: "sha512", Signature: "crypto.sha512(data) → string",
			Params: []script.ParamDoc{
				{Name: "data", Type: "string", Doc: "Data to hash."},
			},
			Returns: "hex-encoded SHA-512 digest (128 characters)",
			Doc:     "Calculates the SHA-512 hash of a string.",
			Example: `local hash = crypto.sha512("hello world")`,
		},
		{
			Name: "hmac_sha256", Signature: "crypto.hmac_sha256(key, message) → string",
			Params: []script.ParamDoc{
				{Name: "key", Type: "string", Doc: "Secret key."},
				{Name: "message", Type: "string", Doc: "Message payload."},
			},
			Returns: "hex-encoded HMAC-SHA256 signature",
			Doc:     "Calculates an HMAC-SHA256 signature.",
			Example: `local sig = crypto.hmac_sha256("secret-key", "payload")`,
		},
		{
			Name: "hmac_sha512", Signature: "crypto.hmac_sha512(key, message) → string",
			Params: []script.ParamDoc{
				{Name: "key", Type: "string", Doc: "Secret key."},
				{Name: "message", Type: "string", Doc: "Message payload."},
			},
			Returns: "hex-encoded HMAC-SHA512 signature",
			Doc:     "Calculates an HMAC-SHA512 signature.",
			Example: `local sig = crypto.hmac_sha512("secret-key", "payload")`,
		},
		{
			Name: "base64_encode", Signature: "crypto.base64_encode(data) → string",
			Params: []script.ParamDoc{
				{Name: "data", Type: "string", Doc: "Data to encode."},
			},
			Returns: "standard Base64-encoded string (RFC 4648 §4)",
			Doc:     "Encodes a string using standard Base64 with padding.",
			Example: `local b64 = crypto.base64_encode("credentials:secret")`,
		},
		{
			Name: "base64_decode", Signature: "crypto.base64_decode(data) → string|nil, err",
			Params: []script.ParamDoc{
				{Name: "data", Type: "string", Doc: "Base64-encoded string."},
			},
			Returns: "decoded string (or nil, error)",
			Doc:     "Decodes a standard Base64 string.",
			Example: `local plain, err = crypto.base64_decode("Y3JlZGVudGlhbHM6c2VjcmV0")`,
		},
		{
			Name: "base64url_encode", Signature: "crypto.base64url_encode(data) → string",
			Params: []script.ParamDoc{
				{Name: "data", Type: "string", Doc: "Data to encode."},
			},
			Returns: "unpadded URL-safe Base64-encoded string (RFC 4648 §5)",
			Doc:     "Encodes a string using unpadded URL-safe Base64.",
			Example: `local enc = crypto.base64url_encode('{"alg":"none"}')`,
		},
		{
			Name: "base64url_decode", Signature: "crypto.base64url_decode(data) → string|nil, err",
			Params: []script.ParamDoc{
				{Name: "data", Type: "string", Doc: "URL-safe Base64-encoded string."},
			},
			Returns: "decoded string (or nil, error)",
			Doc:     "Decodes an unpadded or padded URL-safe Base64 string.",
			Example: `local plain, err = crypto.base64url_decode("eyJhbGciOiJub25lIn0")`,
		},
		{
			Name: "hex_encode", Signature: "crypto.hex_encode(data) → string",
			Params: []script.ParamDoc{
				{Name: "data", Type: "string", Doc: "Raw bytes or string."},
			},
			Returns: "lowercase hexadecimal string",
			Doc:     "Encodes bytes to a hexadecimal string.",
			Example: `local h = crypto.hex_encode("abc")`,
		},
		{
			Name: "hex_decode", Signature: "crypto.hex_decode(data) → string|nil, err",
			Params: []script.ParamDoc{
				{Name: "data", Type: "string", Doc: "Hexadecimal string."},
			},
			Returns: "decoded string (or nil, error)",
			Doc:     "Decodes a hexadecimal string to raw bytes.",
			Example: `local raw, err = crypto.hex_decode("616263")`,
		},
		{
			Name: "random_hex", Signature: "crypto.random_hex(n) → string",
			Params: []script.ParamDoc{
				{Name: "n", Type: "number", Doc: "Number of random bytes (at least 1, at most 1024)."},
			},
			Returns: "hexadecimal string of length 2n",
			Doc:     "Generates cryptographically secure random bytes as a hex string.",
			Example: `local token = crypto.random_hex(16)`,
		},
		{
			Name: "uuid", Signature: "crypto.uuid() → string",
			Returns: "canonical lowercase RFC 4122 v4 UUID string",
			Doc:     "Generates a cryptographically random RFC 4122 v4 UUID.",
			Example: `local id = crypto.uuid()`,
		},
	},
}

func checkStringBudget(r *script.Run, length int, op string) {
	if lim := r.Budget.MaxString; lim > 0 && length > lim {
		r.RaiseLimit(fmt.Sprintf("%s result exceeds the maximum string length of %d bytes", op, lim))
	}
}

func build(r *script.Run) lua.LValue {
	L := r.L
	t := L.NewTable()

	t.RawSetString("sha256", L.NewFunction(func(L *lua.LState) int {
		data := L.CheckString(1)
		h := sha256.Sum256([]byte(data))
		res := hex.EncodeToString(h[:])
		checkStringBudget(r, len(res), "crypto.sha256")
		L.Push(lua.LString(res))
		return 1
	}))

	t.RawSetString("sha512", L.NewFunction(func(L *lua.LState) int {
		data := L.CheckString(1)
		h := sha512.Sum512([]byte(data))
		res := hex.EncodeToString(h[:])
		checkStringBudget(r, len(res), "crypto.sha512")
		L.Push(lua.LString(res))
		return 1
	}))

	t.RawSetString("hmac_sha256", L.NewFunction(func(L *lua.LState) int {
		key := L.CheckString(1)
		msg := L.CheckString(2)
		m := hmac.New(sha256.New, []byte(key))
		m.Write([]byte(msg))
		res := hex.EncodeToString(m.Sum(nil))
		checkStringBudget(r, len(res), "crypto.hmac_sha256")
		L.Push(lua.LString(res))
		return 1
	}))

	t.RawSetString("hmac_sha512", L.NewFunction(func(L *lua.LState) int {
		key := L.CheckString(1)
		msg := L.CheckString(2)
		m := hmac.New(sha512.New, []byte(key))
		m.Write([]byte(msg))
		res := hex.EncodeToString(m.Sum(nil))
		checkStringBudget(r, len(res), "crypto.hmac_sha512")
		L.Push(lua.LString(res))
		return 1
	}))

	t.RawSetString("base64_encode", L.NewFunction(func(L *lua.LState) int {
		data := L.CheckString(1)
		res := base64.StdEncoding.EncodeToString([]byte(data))
		checkStringBudget(r, len(res), "crypto.base64_encode")
		L.Push(lua.LString(res))
		return 1
	}))

	t.RawSetString("base64_decode", L.NewFunction(func(L *lua.LState) int {
		data := L.CheckString(1)
		raw, err := base64.StdEncoding.DecodeString(data)
		if err != nil {
			// Also attempt RawStdEncoding
			var err2 error
			raw, err2 = base64.RawStdEncoding.DecodeString(data)
			if err2 != nil {
				L.Push(lua.LNil)
				L.Push(lua.LString(err.Error()))
				return 2
			}
		}
		checkStringBudget(r, len(raw), "crypto.base64_decode")
		L.Push(lua.LString(string(raw)))
		return 1
	}))

	t.RawSetString("base64url_encode", L.NewFunction(func(L *lua.LState) int {
		data := L.CheckString(1)
		res := base64.RawURLEncoding.EncodeToString([]byte(data))
		checkStringBudget(r, len(res), "crypto.base64url_encode")
		L.Push(lua.LString(res))
		return 1
	}))

	t.RawSetString("base64url_decode", L.NewFunction(func(L *lua.LState) int {
		data := L.CheckString(1)
		raw, err := base64.RawURLEncoding.DecodeString(data)
		if err != nil {
			var err2 error
			raw, err2 = base64.URLEncoding.DecodeString(data)
			if err2 != nil {
				L.Push(lua.LNil)
				L.Push(lua.LString(err.Error()))
				return 2
			}
		}
		checkStringBudget(r, len(raw), "crypto.base64url_decode")
		L.Push(lua.LString(string(raw)))
		return 1
	}))

	t.RawSetString("hex_encode", L.NewFunction(func(L *lua.LState) int {
		data := L.CheckString(1)
		res := hex.EncodeToString([]byte(data))
		checkStringBudget(r, len(res), "crypto.hex_encode")
		L.Push(lua.LString(res))
		return 1
	}))

	t.RawSetString("hex_decode", L.NewFunction(func(L *lua.LState) int {
		data := L.CheckString(1)
		raw, err := hex.DecodeString(data)
		if err != nil {
			L.Push(lua.LNil)
			L.Push(lua.LString(err.Error()))
			return 2
		}
		checkStringBudget(r, len(raw), "crypto.hex_decode")
		L.Push(lua.LString(string(raw)))
		return 1
	}))

	t.RawSetString("random_hex", L.NewFunction(func(L *lua.LState) int {
		n := L.CheckInt(1)
		if n < 1 || n > 1024 {
			L.ArgError(1, "n must be between 1 and 1024")
		}
		buf := make([]byte, n)
		if _, err := crand.Read(buf); err != nil {
			L.RaiseError("crypto.random_hex: %s", err.Error())
		}
		res := hex.EncodeToString(buf)
		checkStringBudget(r, len(res), "crypto.random_hex")
		L.Push(lua.LString(res))
		return 1
	}))

	t.RawSetString("uuid", L.NewFunction(func(L *lua.LState) int {
		var b [16]byte
		if _, err := crand.Read(b[:]); err != nil {
			L.RaiseError("crypto.uuid: %s", err.Error())
		}
		b[6] = (b[6] & 0x0f) | 0x40 // RFC 4122 version 4
		b[8] = (b[8] & 0x3f) | 0x80 // RFC 4122 variant
		res := fmt.Sprintf("%08x-%04x-%04x-%04x-%012x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
		checkStringBudget(r, len(res), "crypto.uuid")
		L.Push(lua.LString(res))
		return 1
	}))

	return t
}
