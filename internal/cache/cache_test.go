package cache_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/nikhil-sharma-b/jira-tui/internal/cache"
)

func open(t *testing.T, path string) *cache.Cache {
	t.Helper()
	c, err := cache.Open(path)
	if err != nil {
		t.Fatalf("opening the cache: %v", err)
	}
	t.Cleanup(func() { c.Close() })
	return c
}

func TestPutThenGetReturnsAFreshValue(t *testing.T) {
	ctx := context.Background()
	c := open(t, filepath.Join(t.TempDir(), "cache.db"))

	if err := c.Put(ctx, "search:mine", []byte("rows"), cache.TTLSearch); err != nil {
		t.Fatalf("putting: %v", err)
	}
	value, fresh, err := c.Get(ctx, "search:mine")
	if err != nil {
		t.Fatalf("getting: %v", err)
	}
	if !fresh {
		t.Error("a value stored a moment ago is not fresh")
	}
	if string(value) != "rows" {
		t.Errorf("got %q, want %q", value, "rows")
	}
}

func TestGetMissReturnsNothing(t *testing.T) {
	value, fresh, err := open(t, filepath.Join(t.TempDir(), "cache.db")).Get(context.Background(), "absent")
	if err != nil {
		t.Fatalf("getting: %v", err)
	}
	if value != nil || fresh {
		t.Errorf("got (%q, %v), want (nil, false)", value, fresh)
	}
}

// An expired value is the whole point of the store: it is what gets rendered
// while the network is asked for a newer one.
func TestExpiredValueIsReturnedButNotFresh(t *testing.T) {
	ctx := context.Background()
	c := open(t, filepath.Join(t.TempDir(), "cache.db"))

	if err := c.Put(ctx, "k", []byte("stale"), -time.Second); err != nil {
		t.Fatalf("putting: %v", err)
	}
	value, fresh, err := c.Get(ctx, "k")
	if err != nil {
		t.Fatalf("getting: %v", err)
	}
	if fresh {
		t.Error("a value past its TTL reports as fresh")
	}
	if string(value) != "stale" {
		t.Errorf("got %q, want the stale value back", value)
	}
}

// The two tiers differ only in how long they last, which is exactly what a
// test can pin: metadata outlives a search result by a wide margin.
func TestTiersExpireIndependently(t *testing.T) {
	ctx := context.Background()
	c := open(t, filepath.Join(t.TempDir(), "cache.db"))

	now := time.Now()
	c.SetNow(func() time.Time { return now })
	if err := c.Put(ctx, "search", []byte("rows"), cache.TTLSearch); err != nil {
		t.Fatalf("putting the search: %v", err)
	}
	if err := c.Put(ctx, "metadata", []byte("fields"), cache.TTLMetadata); err != nil {
		t.Fatalf("putting the metadata: %v", err)
	}

	c.SetNow(func() time.Time { return now.Add(5 * time.Minute) })
	if _, fresh, _ := c.Get(ctx, "search"); fresh {
		t.Error("a search result is still fresh five minutes on")
	}
	if _, fresh, _ := c.Get(ctx, "metadata"); !fresh {
		t.Error("metadata went stale five minutes on")
	}

	c.SetNow(func() time.Time { return now.Add(25 * time.Hour) })
	if _, fresh, _ := c.Get(ctx, "metadata"); fresh {
		t.Error("metadata is still fresh a day and an hour on")
	}
}

func TestPutOverwrites(t *testing.T) {
	ctx := context.Background()
	c := open(t, filepath.Join(t.TempDir(), "cache.db"))

	for _, want := range []string{"first", "second"} {
		if err := c.Put(ctx, "k", []byte(want), cache.TTLSearch); err != nil {
			t.Fatalf("putting %q: %v", want, err)
		}
		value, _, err := c.Get(ctx, "k")
		if err != nil {
			t.Fatalf("getting: %v", err)
		}
		if string(value) != want {
			t.Errorf("got %q, want %q", value, want)
		}
	}
}

func TestClearEmptiesTheStore(t *testing.T) {
	ctx := context.Background()
	c := open(t, filepath.Join(t.TempDir(), "cache.db"))

	if err := c.Put(ctx, "k", []byte("rows"), cache.TTLMetadata); err != nil {
		t.Fatalf("putting: %v", err)
	}
	if err := c.Clear(ctx); err != nil {
		t.Fatalf("clearing: %v", err)
	}
	value, fresh, err := c.Get(ctx, "k")
	if err != nil {
		t.Fatalf("getting: %v", err)
	}
	if value != nil || fresh {
		t.Errorf("got (%q, %v) after a clear, want (nil, false)", value, fresh)
	}
}

// The store is opened by every session, so it has to survive a directory that
// does not exist yet.
func TestOpenCreatesTheDirectory(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "dir", "cache.db")
	open(t, path)
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("the database was not created: %v", err)
	}
}

// A nil cache is what every caller holds when caching is unavailable, so it
// behaves as a store that never hits rather than as a crash.
func TestNilCacheIsAStoreThatNeverHits(t *testing.T) {
	ctx := context.Background()
	var c *cache.Cache

	if err := c.Put(ctx, "k", []byte("rows"), cache.TTLSearch); err != nil {
		t.Errorf("putting into a nil cache: %v", err)
	}
	value, fresh, err := c.Get(ctx, "k")
	if err != nil || value != nil || fresh {
		t.Errorf("got (%q, %v, %v), want (nil, false, nil)", value, fresh, err)
	}
	if err := c.Clear(ctx); err != nil {
		t.Errorf("clearing a nil cache: %v", err)
	}
	if err := c.Close(); err != nil {
		t.Errorf("closing a nil cache: %v", err)
	}
}

// A cache file that is not a database is a recoverable condition: the data in
// it is by definition reconstructible from Jira, so it is thrown away rather
// than reported.
func TestCorruptFileIsRecreated(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "cache.db")
	if err := os.WriteFile(path, []byte("this is not a SQLite database at all"), 0o600); err != nil {
		t.Fatalf("writing the corrupt file: %v", err)
	}

	c := open(t, path)
	if err := c.Put(ctx, "k", []byte("rows"), cache.TTLSearch); err != nil {
		t.Fatalf("putting into the recreated store: %v", err)
	}
	value, fresh, err := c.Get(ctx, "k")
	if err != nil || !fresh || string(value) != "rows" {
		t.Errorf("got (%q, %v, %v), want the value back from a recreated store", value, fresh, err)
	}
}

// The store is shared by every session the user has open, with no daemon and
// no coordination beyond WAL. Readers must not block on the writer, and none
// of them may see a torn value.
func TestConcurrentConnectionsShareTheStore(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "cache.db")

	const (
		connections = 4
		writes      = 40
	)
	// Each connection is what a separate process holds: an independent handle
	// on the same file, opened the same way.
	stores := make([]*cache.Cache, connections)
	for i := range stores {
		stores[i] = open(t, path)
	}

	var wg sync.WaitGroup
	errs := make(chan error, connections*writes*2)
	for i, c := range stores {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for n := range writes {
				key := fmt.Sprintf("conn-%d-%d", i, n)
				want := fmt.Sprintf("value-%d-%d", i, n)
				if err := c.Put(ctx, key, []byte(want), cache.TTLMetadata); err != nil {
					errs <- fmt.Errorf("putting %s: %w", key, err)
					return
				}
				// Read back through a different connection, which is the case
				// that a lock held wrongly would break.
				other := stores[(i+1)%connections]
				got, _, err := other.Get(ctx, key)
				if err != nil {
					errs <- fmt.Errorf("getting %s from another connection: %w", key, err)
					return
				}
				if string(got) != want {
					errs <- fmt.Errorf("got %q for %s from another connection, want %q", got, key, want)
					return
				}
			}
		}()
	}
	wg.Wait()
	close(errs)
	if err := <-errs; err != nil {
		t.Fatal(errors.Join(err))
	}
}

func TestDefaultPathIsUnderTheCacheDirectory(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", "/home/someone/.cache")
	path, err := cache.DefaultPath()
	if err != nil {
		t.Fatalf("resolving the default path: %v", err)
	}
	if want := filepath.Join("/home/someone/.cache", "jt", "cache.db"); path != want {
		t.Errorf("got %q, want %q", path, want)
	}
}

// WAL is the reason there is no daemon, and the property that buys is that a
// reader never waits behind a writer. Without it a busy_timeout of ten seconds
// turns another session's write into a TUI that has stopped repainting, which
// is a far worse failure than an error would have been.
func TestReadersDoNotWaitForAWriter(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "cache.db")
	writer, reader := open(t, path), open(t, path)

	if err := writer.Put(ctx, "row", []byte("value"), cache.TTLMetadata); err != nil {
		t.Fatalf("seeding: %v", err)
	}

	// A payload the size of a real page of search results, written without
	// pause, so the writer genuinely holds the lock for much of the run.
	page := make([]byte, 256*1024)
	done := make(chan struct{})
	stop := make(chan struct{})
	go func() {
		defer close(done)
		for {
			select {
			case <-stop:
				return
			default:
			}
			if err := writer.Put(ctx, "page", page, cache.TTLSearch); err != nil {
				t.Errorf("writing: %v", err)
				return
			}
		}
	}()

	const reads = 200
	started := time.Now()
	for range reads {
		if _, _, err := reader.Get(ctx, "row"); err != nil {
			close(stop)
			<-done
			t.Fatalf("reading beside a writer: %v", err)
		}
	}
	elapsed := time.Since(started)
	close(stop)
	<-done

	// Generously loose: the point is to catch reads serialising behind the
	// writer's lock, which costs seconds, not to measure SQLite.
	if elapsed > 5*time.Second {
		t.Errorf("%d reads beside a writer took %s, which is a reader waiting for a lock", reads, elapsed)
	}
}
