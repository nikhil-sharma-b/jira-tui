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
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	_ "modernc.org/sqlite"
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

// schema is applied on every open. The store is one flat keyspace: callers
// encode the tier and the request into the key, so adding a cached call needs
// no migration.
const schema = `
CREATE TABLE IF NOT EXISTS entries (
	key        TEXT PRIMARY KEY,
	value      BLOB NOT NULL,
	expires_at INTEGER NOT NULL
) STRICT;`

// driverName is the modernc.org/sqlite driver, which is cgo-free so release
// builds cross-compile without a C toolchain for each target.
const driverName = "sqlite"

// Cache is a handle on the store. The zero value is not usable, but a nil
// *Cache is: it behaves as a store that never hits, so a session that could
// not open its cache runs uncached rather than not at all.
type Cache struct {
	db *sql.DB

	mu  sync.RWMutex
	now func() time.Time
}

// Open connects to the cache database, creating it if absent. Safe to call
// concurrently from multiple processes.
func Open(path string) (*Cache, error) {
	if dir := filepath.Dir(path); dir != "" {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return nil, fmt.Errorf("creating the cache directory: %w", err)
		}
	}

	db, err := connect(path)
	if err != nil {
		// Everything in here is reconstructible from Jira, so a file we cannot
		// read is thrown away rather than reported: a corrupt cache must not
		// be the reason the tool will not start.
		if rmErr := remove(path); rmErr != nil {
			return nil, errors.Join(err, rmErr)
		}
		if db, err = connect(path); err != nil {
			return nil, err
		}
	}

	return &Cache{db: db, now: time.Now}, nil
}

// connect opens the file and brings it to a usable state, which is also what
// proves it is a database at all -- a corrupt file fails here rather than at
// the first Get.
func connect(path string) (*sql.DB, error) {
	// busy_timeout is what turns a concurrent writer from an error into a
	// wait; WAL is what keeps readers from waiting at all. synchronous=NORMAL
	// trades a durability guarantee we do not need -- the worst case is a
	// refetch -- for not fsyncing on every write.
	dsn := "file:" + path +
		"?_pragma=journal_mode(WAL)" +
		"&_pragma=busy_timeout(10000)" +
		"&_pragma=synchronous(NORMAL)"
	db, err := sql.Open(driverName, dsn)
	if err != nil {
		return nil, fmt.Errorf("opening the cache: %w", err)
	}
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("preparing the cache: %w", err)
	}
	return db, nil
}

// remove deletes the database and the two sidecar files WAL leaves beside it.
// Leaving those behind makes the fresh database unreadable in turn.
func remove(path string) error {
	var errs []error
	for _, suffix := range []string{"", "-wal", "-shm"} {
		if err := os.Remove(path + suffix); err != nil && !os.IsNotExist(err) {
			errs = append(errs, fmt.Errorf("discarding the unreadable cache: %w", err))
		}
	}
	return errors.Join(errs...)
}

// SetNow replaces the clock. Freshness is the only thing in the store that
// depends on the time, and a test that asserts on a tier boundary needs to say
// when now is.
func (c *Cache) SetNow(now func() time.Time) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = now
}

func (c *Cache) clock() time.Time {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.now()
}

// Get returns the cached value for key and whether it is still fresh. A stale
// value is returned alongside fresh=false rather than discarded, so callers
// can render it immediately and revalidate underneath.
func (c *Cache) Get(ctx context.Context, key string) (value []byte, fresh bool, err error) {
	if c == nil {
		return nil, false, nil
	}
	var expires int64
	row := c.db.QueryRowContext(ctx, `SELECT value, expires_at FROM entries WHERE key = ?`, key)
	switch err := row.Scan(&value, &expires); {
	case errors.Is(err, sql.ErrNoRows):
		return nil, false, nil
	case err != nil:
		return nil, false, fmt.Errorf("reading the cache: %w", err)
	}
	return value, c.clock().UnixNano() < expires, nil
}

// Put stores value under key with the given TTL.
func (c *Cache) Put(ctx context.Context, key string, value []byte, ttl time.Duration) error {
	if c == nil {
		return nil
	}
	expires := c.clock().Add(ttl).UnixNano()
	_, err := c.db.ExecContext(ctx, `
		INSERT INTO entries (key, value, expires_at) VALUES (?, ?, ?)
		ON CONFLICT(key) DO UPDATE SET value = excluded.value, expires_at = excluded.expires_at`,
		key, value, expires)
	if err != nil {
		return fmt.Errorf("writing the cache: %w", err)
	}
	return nil
}

// Clear drops everything. Backs the :cache clear command.
func (c *Cache) Clear(ctx context.Context) error {
	if c == nil {
		return nil
	}
	if _, err := c.db.ExecContext(ctx, `DELETE FROM entries`); err != nil {
		return fmt.Errorf("clearing the cache: %w", err)
	}
	return nil
}

func (c *Cache) Close() error {
	if c == nil {
		return nil
	}
	return c.db.Close()
}

// DefaultPath is the cache location under the user's XDG cache directory.
func DefaultPath() (string, error) {
	dir, err := os.UserCacheDir()
	if err != nil {
		return "", fmt.Errorf("locating cache directory: %w", err)
	}
	return filepath.Join(dir, "jt", "cache.db"), nil
}
