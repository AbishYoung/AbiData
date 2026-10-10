package script

import (
	"crypto/sha256"
	"sync"

	lua "github.com/yuin/gopher-lua"
)

// protoCacheSize bounds the compiled-chunk cache (entries, not bytes; sources
// are at most MaxSource each).
const protoCacheSize = 256

var protoCache = struct {
	sync.Mutex
	m map[[32]byte]*lua.FunctionProto
}{m: map[[32]byte]*lua.FunctionProto{}}

// CompileCached is Compile with a process-wide cache keyed by chunk name and
// source, for generated chunks compiled on every run (report and form
// calculations). FunctionProtos are immutable and shared safely across VMs.
// Compile errors are not cached.
func CompileCached(source, chunkName string) (*lua.FunctionProto, error) {
	h := sha256.New()
	h.Write([]byte(chunkName))
	h.Write([]byte{0})
	h.Write([]byte(source))
	var k [32]byte
	h.Sum(k[:0])
	protoCache.Lock()
	p, ok := protoCache.m[k]
	protoCache.Unlock()
	if ok {
		return p, nil
	}
	p, err := Compile(source, chunkName)
	if err != nil {
		return nil, err
	}
	protoCache.Lock()
	if len(protoCache.m) >= protoCacheSize {
		for old := range protoCache.m { // evict an arbitrary entry
			delete(protoCache.m, old)
			break
		}
	}
	protoCache.m[k] = p
	protoCache.Unlock()
	return p, nil
}
