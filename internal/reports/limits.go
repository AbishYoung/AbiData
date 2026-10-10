package reports

import (
	"runtime"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"abidata/internal/fault"
	"abidata/internal/ids"
)

// Concurrent report runs materialise their rows in memory, so they are capped
// per workspace and per process; excess runs fail fast with WORKSPACE_BUSY.
const (
	MaxRunsPerWorkspace = 2
	// maxGroupKeyLen bounds Lua group-key text (keys are compared while sorting).
	maxGroupKeyLen = 1024
	// rowOverhead and fieldOverhead approximate Go's per-row and per-value
	// cost (row struct, maps, interface boxing) on top of the raw value size.
	rowOverhead   = 160
	fieldOverhead = 64
)

// MaxRuns is the process-wide cap on concurrent report runs (each may hold up
// to maxSourceBytes of rows).
var MaxRuns = min(4, max(2, runtime.GOMAXPROCS(0)))

type runSlots struct {
	mu    sync.Mutex
	total int
	byWS  map[ids.WorkspaceID]int
}

var slots = runSlots{byWS: map[ids.WorkspaceID]int{}}

func (s *runSlots) acquire(ws ids.WorkspaceID) (func(), error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.byWS[ws] >= MaxRunsPerWorkspace || s.total >= MaxRuns {
		return nil, &fault.Error{Code: fault.WorkspaceBusy, RetryAfter: 2 * time.Second,
			Message: "Too many reports are running right now. Try again in a moment."}
	}
	s.byWS[ws]++
	s.total++
	return func() {
		s.mu.Lock()
		defer s.mu.Unlock()
		s.total--
		if s.byWS[ws]--; s.byWS[ws] <= 0 {
			delete(s.byWS, ws)
		}
	}, nil
}

// foldCompare compares strings case-insensitively without allocating.
func foldCompare(a, b string) int {
	if a == b {
		return 0
	}
	for a != "" && b != "" {
		ra, na := utf8.DecodeRuneInString(a)
		rb, nb := utf8.DecodeRuneInString(b)
		if ra != rb {
			la, lb := unicode.ToLower(ra), unicode.ToLower(rb)
			if la != lb {
				if la < lb {
					return -1
				}
				return 1
			}
		}
		a, b = a[na:], b[nb:]
	}
	switch {
	case a == "" && b == "":
		return 0
	case a == "":
		return -1
	}
	return 1
}
