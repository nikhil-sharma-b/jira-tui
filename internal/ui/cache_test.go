package ui_test

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nikhil-sharma-b/jira-tui/internal/cache"
	"github.com/nikhil-sharma-b/jira-tui/internal/config"
	"github.com/nikhil-sharma-b/jira-tui/internal/jira"
	"github.com/nikhil-sharma-b/jira-tui/internal/ui"
)

// openCache is a store in a directory that dies with the test, standing in for
// the one shared by every session the user has open.
func openCache(t *testing.T) *cache.Cache {
	t.Helper()
	c, err := cache.Open(filepath.Join(t.TempDir(), "cache.db"))
	if err != nil {
		t.Fatalf("opening the cache: %v", err)
	}
	t.Cleanup(func() { c.Close() })
	return c
}

// warm runs a whole session against the store, which is how the cache a later
// session finds gets there in the first place.
func warm(t *testing.T, c *cache.Cache, cfg *config.Config, issues []jira.Issue) {
	t.Helper()
	d := newPausedDriver(t, ui.Options{Client: &fakeClient{issues: issues}, Config: cfg, Cache: c})
	d.flush()
}

// A query run a moment ago is answered from the store, which is the whole
// point of the ~60s tier: re-running a familiar query costs nothing.
func TestAFreshlyCachedQueryRendersWithoutAskingJira(t *testing.T) {
	cfg := testConfig(t, nil)
	c := openCache(t)
	warm(t, c, cfg, sampleIssues(3))

	client := &fakeClient{issues: sampleIssues(3)}
	d := newPausedDriver(t, ui.Options{Client: client, Config: cfg, Cache: c})
	d.flush()

	if !strings.Contains(d.view(), "ENG-1") {
		t.Errorf("the cached rows were not rendered:\n%s", d.view())
	}
	if n := len(client.requests()); n != 0 {
		t.Errorf("%d searches were made against a cache that was still fresh, want 0", n)
	}
	if n := client.fieldRequests(); n != 0 {
		t.Errorf("%d field fetches were made against cached metadata, want 0", n)
	}
}

// The ~24h tier is what removes the metadata round trip from startup; the
// ~60s one is short enough that a search a few minutes later is refetched.
func TestStaleSearchRefetchesWhileCachedMetadataDoesNot(t *testing.T) {
	cfg := testConfig(t, nil)
	c := openCache(t)
	warm(t, c, cfg, sampleIssues(3))
	c.SetNow(func() time.Time { return time.Now().Add(5 * time.Minute) })

	client := &fakeClient{issues: sampleIssues(3)}
	d := newPausedDriver(t, ui.Options{Client: client, Config: cfg, Cache: c})
	d.flush()

	if n := len(client.requests()); n != 1 {
		t.Errorf("%d searches were made for a result five minutes old, want 1", n)
	}
	if n := client.fieldRequests(); n != 0 {
		t.Errorf("%d field fetches were made five minutes on, want 0: metadata lasts a day", n)
	}
}

// Stale-while-revalidate is a claim about what is on screen *before* the
// response lands, so the test has to look while the request is still queued.
func TestStaleRowsRenderBeforeTheSearchIsEvenSent(t *testing.T) {
	cfg := testConfig(t, nil)
	c := openCache(t)
	warm(t, c, cfg, sampleIssues(3))
	c.SetNow(func() time.Time { return time.Now().Add(5 * time.Minute) })

	client := &fakeClient{issues: sampleIssues(3)}
	d := newPausedDriver(t, ui.Options{Client: client, Config: cfg, Cache: c})

	d.stepUntil("ENG-1")
	if n := len(client.requests()); n != 0 {
		t.Errorf("%d searches had already completed, so nothing was proved about the cache", n)
	}
	// The refresh is in flight and says so, without hiding the rows behind a
	// loading line.
	if got := strings.ToLower(d.view()); !strings.Contains(got, "refresh") {
		t.Errorf("nothing indicated that a refresh was under way:\n%s", d.view())
	}
	if strings.Contains(strings.ToLower(d.view()), "loading") {
		t.Errorf("stale rows were replaced by a loading line:\n%s", d.view())
	}
}

// The user is reading a row while the refresh lands underneath. Re-anchoring
// on the key rather than the index is what keeps them where they were when the
// fresh result set is a different length.
func TestFreshDataReplacesStaleRowsWithoutMovingTheSelection(t *testing.T) {
	cfg := testConfig(t, nil)
	c := openCache(t)
	warm(t, c, cfg, sampleIssues(30))
	c.SetNow(func() time.Time { return time.Now().Add(5 * time.Minute) })

	// The fresh result set has an extra item at the top, so every row the user
	// was looking at has moved down by one index.
	fresh := append(sampleIssues(1), sampleIssues(30)...)
	fresh[0].Key = "ENG-99"
	fresh[0].Summary = "Filed since the cache was written"

	client := &fakeClient{issues: fresh}
	d := newPausedDriver(t, ui.Options{Client: client, Config: cfg, Cache: c})
	d.stepUntil("ENG-1")

	d.send(keyMsg("j"))
	d.send(keyMsg("j"))
	d.send(keyMsg("j"))
	before, top := d.selected(), d.visibleKeys()[0]
	if before != "ENG-4" {
		t.Fatalf("the selection started at %q, want ENG-4", before)
	}

	d.flush()
	if got := d.selected(); got != before {
		t.Errorf("the refresh moved the selection to %q, want it left on %q", got, before)
	}
	if got := d.visibleKeys()[0]; got != top {
		t.Errorf("the refresh scrolled the viewport to %q, want it left at %q", got, top)
	}

	// The newly filed item is in the result set, one row above where the user
	// was: the refresh landed, it just did not drag them along with it.
	d.keys("g", "g")
	if got := d.visibleKeys()[0]; got != "ENG-99" {
		t.Errorf("the fresh result never replaced the stale one; the list starts at %q:\n%s", got, d.view())
	}
}

// A refresh that fails is not a reason to take away rows the user was reading.
func TestFailedRevalidationLeavesTheStaleRowsUp(t *testing.T) {
	cfg := testConfig(t, nil)
	c := openCache(t)
	warm(t, c, cfg, sampleIssues(3))
	c.SetNow(func() time.Time { return time.Now().Add(5 * time.Minute) })

	client := &fakeClient{searchErr: errors.New("jira said no")}
	d := newPausedDriver(t, ui.Options{Client: client, Config: cfg, Cache: c})
	d.flush()

	if !strings.Contains(d.view(), "ENG-1") {
		t.Errorf("a failed refresh cleared the pane:\n%s", d.view())
	}
	if !strings.Contains(d.view(), "jira said no") {
		t.Errorf("the failure was not noted anywhere:\n%s", d.view())
	}
}

// Offline is a state, not an event: it stays on the status line for as long as
// the site is unreachable, and the cached rows stay browsable behind it.
func TestOfflineMarkerPersistsWhileCachedRowsStayBrowsable(t *testing.T) {
	cfg := testConfig(t, nil)
	c := openCache(t)
	warm(t, c, cfg, sampleIssues(30))
	c.SetNow(func() time.Time { return time.Now().Add(5 * time.Minute) })

	client := &fakeClient{searchErr: &jira.OfflineError{Err: errors.New("dial tcp: network is unreachable")}}
	d := newPausedDriver(t, ui.Options{Client: client, Config: cfg, Cache: c})
	d.flush()

	if !strings.Contains(strings.ToLower(d.view()), "offline") {
		t.Errorf("nothing marked the session as offline:\n%s", d.view())
	}
	d.keys("j", "j")
	if got := d.selected(); got != "ENG-3" {
		t.Errorf("the cached rows stopped being browsable offline: selection is %q, want ENG-3", got)
	}
	if !strings.Contains(strings.ToLower(d.view()), "offline") {
		t.Errorf("the offline marker did not survive a keypress:\n%s", d.view())
	}
}

// R exists precisely for the moment the user does not believe what is on
// screen, so it must not be answerable from the store.
func TestReloadBypassesTheCache(t *testing.T) {
	cfg := testConfig(t, nil)
	c := openCache(t)
	warm(t, c, cfg, sampleIssues(3))

	client := &fakeClient{issues: sampleIssues(3)}
	d := newPausedDriver(t, ui.Options{Client: client, Config: cfg, Cache: c})
	d.flush()
	if n := len(client.requests()); n != 0 {
		t.Fatalf("%d searches were made before R was pressed, want 0", n)
	}

	d.keys("R")
	if n := len(client.requests()); n != 1 {
		t.Errorf("R made %d searches, want 1: a forced refetch may not be served from the cache", n)
	}
}

// :cache clear is bound in ticket 07; what it binds to is this, and what it
// has to guarantee is that the next view goes to Jira.
func TestClearingTheCacheForcesTheNextViewToRefetch(t *testing.T) {
	cfg := testConfig(t, nil)
	c := openCache(t)
	warm(t, c, cfg, sampleIssues(3))

	client := &fakeClient{issues: sampleIssues(3)}
	d := newPausedDriver(t, ui.Options{Client: client, Config: cfg, Cache: c})
	d.flush()

	d.command(":cache clear")

	if n := len(client.requests()); n != 1 {
		t.Errorf("clearing the cache made %d searches, want 1", n)
	}

	// A later session finds an empty store rather than the entry this one
	// cleared.
	later := &fakeClient{issues: sampleIssues(3)}
	e := newPausedDriver(t, ui.Options{Client: later, Config: cfg, Cache: c})
	e.flush()
	if n := later.fieldRequests(); n == 0 {
		t.Error("the next session still found cached metadata after a clear")
	}
}

// The cache is an optimisation, so a session without one is a session that
// works, not a session that crashes.
func TestASessionWithoutACacheStillRuns(t *testing.T) {
	client := &fakeClient{issues: sampleIssues(3)}
	d := newPausedDriver(t, ui.Options{Client: client, Config: testConfig(t, nil), Cache: nil})
	d.flush()

	if !strings.Contains(d.view(), "ENG-1") {
		t.Errorf("an uncached session rendered nothing:\n%s", d.view())
	}
	if n := len(client.requests()); n != 1 {
		t.Errorf("%d searches were made without a cache, want 1", n)
	}
}

// A different query is a different entry: answering it from the last one's
// rows would be the worst kind of wrong.
func TestADifferentQueryIsNotAnsweredFromAnotherQuerysCache(t *testing.T) {
	cfg := testConfig(t, nil)
	c := openCache(t)
	warm(t, c, cfg, sampleIssues(3))

	other := testConfig(t, nil)
	other.DefaultQuery = "project = OPS ORDER BY created DESC"
	client := &fakeClient{issues: sampleIssues(3)}
	d := newPausedDriver(t, ui.Options{Client: client, Config: other, Cache: c})
	d.flush()

	if n := len(client.requests()); n != 1 {
		t.Errorf("%d searches were made for an uncached query, want 1", n)
	}
}

// A custom field's id can move under a cached column -- the site is
// administered by someone else. The rows the user ends up looking at must be
// the ones fetched against the field set the site actually has.
func TestColumnsThatMovedUnderTheCacheAreRefetched(t *testing.T) {
	cfg := testConfig(t, []string{"key", "summary", "Story Points"})
	c := openCache(t)
	warm(t, c, cfg, sampleIssues(3))
	c.SetNow(func() time.Time { return time.Now().Add(48 * time.Hour) })

	// The same column now resolves to a different id.
	moved := append([]jira.Field(nil), siteFields()...)
	for i := range moved {
		if moved[i].Name == "Story Points" {
			moved[i].ID = "customfield_10099"
		}
	}
	client := &fakeClient{issues: sampleIssues(3), fields: moved}
	d := newPausedDriver(t, ui.Options{Client: client, Config: cfg, Cache: c})
	d.flush()

	reqs := client.requests()
	if len(reqs) == 0 {
		t.Fatal("no search was made at all")
	}
	if len(reqs) != 2 {
		t.Errorf("%d searches were made, want 2: one against the cached columns, one against the site's", len(reqs))
	}
	last := reqs[len(reqs)-1]
	if !contains(last.Fields, "customfield_10099") {
		t.Errorf("the last search asked for %v, want the field id the site now has", last.Fields)
	}
	if !strings.Contains(d.view(), "ENG-1") {
		t.Errorf("the refetched rows are not on screen:\n%s", d.view())
	}
}
