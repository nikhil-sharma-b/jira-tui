package ui

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/nikhil-sharma-b/jira-tui/internal/cache"
	"github.com/nikhil-sharma-b/jira-tui/internal/jira"
)

// keyVersion is bumped when a cached payload's shape changes. Entries written
// by an older build then miss rather than decode into something subtly wrong,
// which is cheaper than a migration for data that is refetchable anyway.
const keyVersion = "v1"

// store is the cache as the UI sees it: which calls are cacheable, what each
// one's key is, and how its result serialises. Deciding what is stale is kept
// here rather than in the list pane, whose job is rows on a screen.
//
// Every failure is a miss. The cache is an optimisation over a network call
// that is about to be made anyway, so a store that cannot answer is not worth
// a sentence in the status line.
type store struct {
	cache *cache.Cache
	// site scopes every key, so pointing the config at another Jira site does
	// not read back the first one's fields under the same name.
	site string
}

// fields returns the cached field metadata, whether it is still fresh, and
// whether there was anything cached at all.
func (s store) fields() (fields []jira.Field, fresh, ok bool) {
	return get[[]jira.Field](s, s.key("fields"))
}

func (s store) putFields(fields []jira.Field) {
	s.put(s.key("fields"), fields, cache.TTLMetadata)
}

// page returns a cached first page. Continuations are never cached: their key
// would have to be the opaque page token, which is minted per result set and
// meaningless to a later session.
func (s store) page(opts jira.SearchOptions) (result *jira.SearchResult, fresh, ok bool) {
	if opts.PageToken != "" {
		return nil, false, false
	}
	return get[*jira.SearchResult](s, s.searchKey(opts))
}

// get decodes one entry. Every failure -- an unreadable store, a payload
// written by a build that spelled the type differently -- is a miss, because
// the caller's answer to a miss is to ask Jira, which is the right answer to
// all of them.
func get[T any](s store, key string) (value T, fresh, ok bool) {
	encoded, fresh, err := s.cache.Get(context.Background(), key)
	if err != nil || encoded == nil {
		return value, false, false
	}
	if err := json.Unmarshal(encoded, &value); err != nil {
		return value, false, false
	}
	return value, fresh, true
}

func (s store) putPage(opts jira.SearchOptions, result *jira.SearchResult) {
	if opts.PageToken != "" {
		return
	}
	s.put(s.searchKey(opts), result, cache.TTLSearch)
}

// clear empties the store, so the next view of anything goes to Jira.
func (s store) clear() error { return s.cache.Clear(context.Background()) }

func (s store) put(key string, value any, ttl time.Duration) {
	encoded, err := json.Marshal(value)
	if err != nil {
		return
	}
	s.cache.Put(context.Background(), key, encoded, ttl)
}

// searchKey identifies a result set by what was asked for: the JQL and the
// field set. A page size is in there too, because a cached page of fifty does
// not answer a request for a hundred.
func (s store) searchKey(opts jira.SearchOptions) string {
	return s.key("search", opts.JQL, strings.Join(opts.Fields, ","), strconv.Itoa(opts.MaxResults))
}

// key hashes the request into a fixed-width name. The parts are joined with a
// separator that cannot occur in a hash, so two different requests cannot
// spell the same key by concatenation.
func (s store) key(kind string, parts ...string) string {
	sum := sha256.Sum256([]byte(strings.Join(append([]string{s.site}, parts...), "\x00")))
	return keyVersion + ":" + kind + ":" + hex.EncodeToString(sum[:])
}

// The cache is read as a message like any other, so the model renders between
// reading it and asking Jira. That gap is stale-while-revalidate: without a
// frame in between, there is no moment at which the stale rows are on screen.

// cachedFieldsMsg carries cached field metadata, fields nil on a miss.
type cachedFieldsMsg struct {
	fields []jira.Field
	// fresh reports that the entry is inside its TTL, so the metadata need not
	// be fetched at all.
	fresh bool
}

// cachedPageMsg carries a cached first page, result nil on a miss. The query
// is echoed back so an answer that arrives after the query changed can be
// recognised as belonging to a list that is no longer on screen.
type cachedPageMsg struct {
	query  string
	result *jira.SearchResult
	fresh  bool
}

// loadFields reads the metadata from the cache. A miss and a stale hit both
// end in a fetch; the difference is whether anything can be drawn first.
func (m *Model) loadFields() tea.Cmd {
	st := m.store
	return func() tea.Msg {
		fields, fresh, _ := st.fields()
		return cachedFieldsMsg{fields: fields, fresh: fresh}
	}
}

// loadPage reads the first page from the cache before the network is asked.
// Only the first page is cacheable: a continuation is identified by an opaque
// token minted for one result set, which means nothing to a later session.
func (m *Model) loadPage() tea.Cmd {
	st, query := m.store, m.query
	opts := m.searchOptions("")
	return func() tea.Msg {
		result, fresh, _ := st.page(opts)
		return cachedPageMsg{query: query, result: result, fresh: fresh}
	}
}

// handleCachedFields puts cached columns to work at once. Resolving them is
// what makes the first search expressible, and it is the round trip this tier
// exists to remove from startup.
func (m *Model) handleCachedFields(msg cachedFieldsMsg) tea.Cmd {
	if msg.fields == nil {
		return m.fetchFields()
	}
	columns, err := ResolveColumns(m.cfg.Columns, msg.fields)
	if err != nil {
		// Cached metadata that no longer resolves is not a config error worth
		// ending the session over -- the site may have changed under it. Go
		// and ask.
		return m.fetchFields()
	}
	m.columns = columns
	if msg.fresh {
		return m.loadPage()
	}
	// The search does not wait for the metadata to be revalidated: the cached
	// columns are good enough to ask with, and a field set that turns out to
	// have moved re-runs it.
	return tea.Batch(m.loadPage(), m.fetchFields())
}

// handleCachedPage renders a cached result set, and revalidates underneath it
// unless the entry is still inside its TTL.
func (m *Model) handleCachedPage(msg cachedPageMsg) tea.Cmd {
	if msg.query != m.query {
		return nil
	}
	if msg.result == nil {
		return m.fetchPage("")
	}
	m.applyPage(msg.result, true)
	if msg.fresh {
		m.loading, m.refreshing = false, false
		return nil
	}
	// Both flags are set here: refreshing is what the status line says, and
	// loading is what stops a motion from firing a second request underneath
	// this one.
	m.loading, m.refreshing = true, true
	return m.fetchPage("")
}
