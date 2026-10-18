package catalog

import (
	"context"
	"strconv"
	"sync"

	"abidata/internal/dbx"
	"abidata/internal/ids"
	"abidata/internal/workspace"
)

// Cache holds the latest catalog snapshot per workspace, revalidated against
// _sys_meta.schema_version (one cheap read) on every Get.
type Cache struct {
	mu sync.Mutex
	m  map[ids.WorkspaceID]*Schema
}

// NewCache returns an empty cache.
func NewCache() *Cache { return &Cache{m: map[ids.WorkspaceID]*Schema{}} }

// Get returns the current schema for the lease's workspace. Schemas are shared
// and must be treated as read-only.
func (c *Cache) Get(ctx context.Context, l *workspace.Lease) (*Schema, error) {
	var s *Schema
	err := l.Read(ctx, func(q dbx.Querier) error {
		var v string
		if err := q.QueryRow(ctx, sqlCacheGet.SQL).Scan(&v); err != nil {
			return err
		}
		ver, _ := strconv.ParseInt(v, 10, 64)
		c.mu.Lock()
		cached := c.m[l.ID()]
		c.mu.Unlock()
		if cached != nil && cached.Version == ver {
			s = cached
			return nil
		}
		loaded, err := Load(ctx, q)
		if err != nil {
			return err
		}
		s = loaded
		c.mu.Lock()
		c.m[l.ID()] = loaded
		c.mu.Unlock()
		return nil
	})
	return s, err
}

// Put stores s (e.g. right after a committed apply).
func (c *Cache) Put(id ids.WorkspaceID, s *Schema) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.m[id] = s
}

// Drop forgets a workspace (after restore).
func (c *Cache) Drop(id ids.WorkspaceID) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.m, id)
}
