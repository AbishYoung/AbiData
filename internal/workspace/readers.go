package workspace

import (
	"context"
	"sync"
	"time"

	"abidata/internal/sqlitex"
)

// readerPool holds up to max query_only connections, opened lazily and closed
// when idle. WAL readers never block on the writer.
type readerPool struct {
	path string
	sem  chan struct{}
	mu   sync.Mutex
	idle []pooled
	shut bool
}

type pooled struct {
	c        *sqlitex.Conn
	lastUsed time.Time
}

func newReaderPool(path string, max int) *readerPool {
	return &readerPool{path: path, sem: make(chan struct{}, max)}
}

func (p *readerPool) get(ctx context.Context) (*sqlitex.Conn, error) {
	select {
	case p.sem <- struct{}{}:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	p.mu.Lock()
	if n := len(p.idle); n > 0 {
		c := p.idle[n-1].c
		p.idle = p.idle[:n-1]
		p.mu.Unlock()
		return c, nil
	}
	p.mu.Unlock()
	c, err := sqlitex.Open(ctx, p.path, sqlitex.Options{Kind: sqlitex.Reader})
	if err != nil {
		<-p.sem
		return nil, err
	}
	return c, nil
}

func (p *readerPool) put(c *sqlitex.Conn) {
	p.mu.Lock()
	if p.shut || c.Broken() {
		p.mu.Unlock()
		_ = c.Close()
	} else {
		p.idle = append(p.idle, pooled{c: c, lastUsed: time.Now()})
		p.mu.Unlock()
	}
	<-p.sem
}

// trim closes idle connections unused for longer than d.
func (p *readerPool) trim(d time.Duration) {
	cutoff := time.Now().Add(-d)
	p.mu.Lock()
	var keep, drop []pooled
	for _, x := range p.idle {
		if x.lastUsed.Before(cutoff) {
			drop = append(drop, x)
		} else {
			keep = append(keep, x)
		}
	}
	p.idle = keep
	p.mu.Unlock()
	for _, x := range drop {
		_ = x.c.Close()
	}
}

// close closes idle connections and makes later puts close theirs. Callers must
// ensure no reads are in flight (refcount drained) when they need all FDs gone.
func (p *readerPool) close() {
	p.mu.Lock()
	p.shut = true
	idle := p.idle
	p.idle = nil
	p.mu.Unlock()
	for _, x := range idle {
		_ = x.c.Close()
	}
}
