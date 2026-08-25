// Package cache is a TTL-tiered store backed by SQLite in WAL mode.
//
// WAL is the reason there is no daemon: N concurrent reader processes and a
// writer coexist without a lock dance, so each tmux session stays an
// independent process.
//
// What is deliberately NOT cached:
//   - the focused work item, because the user's agent writes to Jira out of
//     band and stale detail is worse than an extra round trip;
//   - available transitions, because they depend on the item's current status
//     and the caller's permissions, so any project-level map would be quietly
//     wrong.
package cache

import (
	"context"
	"time"
)

// TTL tiers, chosen by how fast the underlying data actually moves.
const (
	// TTLSearch covers JQL results, rendered stale-while-revalidate.
	TTLSearch = 60 * time.Second
	// TTLMetadata covers fields, projects, users and workflow metadata.
	// Refetching these on every open is what makes a TUI feel slow; they
	// change on the order of monthly.
	TTLMetadata = 24 * time.Hour
)

type Cache struct {
	// unexported
}

// Open connects to the cache database, creating it if absent. Safe to call
// concurrently from multiple processes.
func Open(path string) (*Cache, error) {
	panic("not implemented")
}

// Get returns the cached value for key and whether it is still fresh. A stale
// value is returned alongside fresh=false rather than discarded, so callers
// can render it immediately and revalidate underneath.
func (c *Cache) Get(ctx context.Context, key string) (value []byte, fresh bool, err error) {
	panic("not implemented")
}

// Put stores value under key with the given TTL.
func (c *Cache) Put(ctx context.Context, key string, value []byte, ttl time.Duration) error {
	panic("not implemented")
}

// Clear drops everything. Backs the :cache clear command.
func (c *Cache) Clear(ctx context.Context) error {
	panic("not implemented")
}

func (c *Cache) Close() error {
	panic("not implemented")
}

// DefaultPath is the cache location under the user's XDG cache directory.
func DefaultPath() (string, error) {
	panic("not implemented")
}
