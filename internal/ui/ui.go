// Package ui is the bubbletea layer. It depends on jira.Client and nothing
// else from that package, so UI tests substitute a fake client.
//
// Every Jira call is an asynchronous tea.Cmd; the loop never blocks. Cached
// data renders stale-while-revalidate (show cache immediately, replace
// silently when the response lands); uncached data shows a spinner in the
// affected pane only, leaving the rest interactive.
package ui

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/nikhil-sharma-b/jira-tui/internal/cache"
	"github.com/nikhil-sharma-b/jira-tui/internal/config"
	"github.com/nikhil-sharma-b/jira-tui/internal/jira"
)

// Mode is the vim modal state. Esc returns to ModeNormal from anywhere; no
// widget may consume it.
type Mode int

const (
	ModeNormal Mode = iota
	ModeCommand
	ModeSearch
	ModePicker
)

// Pane identifies a focus target. Panes are addressed directly by chord, never
// cycled -- Tab-cycling is the specific friction this tool exists to remove.
type Pane int

const (
	PaneList Pane = iota
	PaneDetail
)

// Section is a region within the detail pane. Comments and attachments live
// inside detail rather than as sibling panes: three panes is unreadable at 80
// columns in a tmux split.
type Section int

const (
	SectionDescription Section = iota
	SectionComments
	SectionAttachments
)

// Options wires a Model. Pin is the work item this session is bound to, empty
// when unpinned.
type Options struct {
	Client jira.Client
	Cache  *cache.Cache
	Config *config.Config
	Pin    string
}

// Model is the root bubbletea model. It owns the query, the loaded result set
// and the failure of the last request; everything a keypress means arrives
// from the dispatcher as a named action, so no key is bound here.
type Model struct {
	client jira.Client
	cfg    *config.Config
	store  store

	dispatch *Dispatcher
	help     *Help

	// query is the JQL currently on screen.
	query string
	// columns is nil until the site's field metadata has resolved the
	// configured column names, which is also when the first search can be
	// built -- there is nothing to ask for before then.
	columns []Column

	list list

	width, height int

	// loading counts nothing more than "a request is out": one page at a time
	// is requested, and the flag is what stops a held-down j from asking for
	// the same page repeatedly.
	loading bool
	// refreshing records that a request is revalidating data already on
	// screen. It is not loading: rows the user can read stay up and stay
	// usable, and the indicator says only that something newer is on its way.
	refreshing bool
	// offline records that the site could not be reached at all. It is a
	// state rather than an event -- cached rows stay browsable behind it --
	// so it is shown until a request succeeds, not until a key is pressed.
	offline bool
	// pageFailed records that fetching a further page failed. Without it every
	// motion near the end would re-fire the same failing request, turning a
	// held-down j into a retry storm the user never asked for. R clears it.
	pageFailed bool

	// nextPage continues the result set, empty when there is no page after
	// what is loaded. The endpoint reports no total, so how many work items
	// match is not knowable without walking every page -- which is exactly
	// what paging on demand exists to avoid.
	nextPage string
	isLast   bool

	// status is the last failure, shown in the status line rather than in a
	// modal so the list underneath stays readable and usable.
	status error
	// err is a failure that startup cannot continue past -- today, only a
	// configured column that names no field on this site. It is returned from
	// Run so the message lands on stderr after the terminal is restored.
	err error

	now func() time.Time
}

// New builds the root model. When Pin is set, detail opens full-width with the
// list hidden; gl reveals it.
func New(opts Options) (*Model, error) {
	if opts.Client == nil {
		return nil, errors.New("ui: no Jira client")
	}
	cfg := opts.Config
	if cfg == nil {
		cfg = config.Defaults()
	}
	bindings, err := cfg.Bindings()
	if err != nil {
		return nil, err
	}
	return &Model{
		client:   opts.Client,
		cfg:      cfg,
		store:    store{cache: opts.Cache, site: cfg.SiteURL()},
		dispatch: NewDispatcher(bindings),
		help:     NewHelp(bindings),
		query:    cfg.DefaultQuery,
		loading:  true,
		now:      time.Now,
	}, nil
}

// SetNow replaces the clock. Relative timestamps are the only thing in the UI
// that depends on the time, and a test that asserts on "3h" needs to say when
// now is.
func (m *Model) SetNow(now func() time.Time) { m.now = now }

// Err is the failure that ended the session, nil for a clean quit.
func (m *Model) Err() error { return m.err }

// Init asks the cache for the field metadata. Columns are configured by
// display name and the API wants ids, so nothing can be searched for until
// this lands -- which is why the ~24h tier is what makes startup instant: the
// alternative is a round trip before the first row can even be requested.
func (m *Model) Init() tea.Cmd { return m.loadFields() }

// fieldsMsg carries the site's field metadata, or the failure to get it.
type fieldsMsg struct {
	fields []jira.Field
	err    error
}

// pageMsg carries one page of search results. The query is echoed back so a
// response that arrives after the query changed can be recognised as stale,
// and first distinguishes a fresh result set from a continuation.
type pageMsg struct {
	query string
	// fields is what was asked for. Cached columns can be superseded by the
	// metadata that revalidated them, and a page fetched against the old field
	// set is then answering a question no longer being asked.
	fields []string
	first  bool
	result *jira.SearchResult
	err    error
}

func (m *Model) fetchFields() tea.Cmd {
	client := m.client
	return func() tea.Msg {
		fields, err := client.Fields(context.Background())
		return fieldsMsg{fields: fields, err: err}
	}
}

// search asks Jira for one page, and caches a first page that came back.
func (m *Model) search(pageToken string) tea.Cmd {
	client, query, st := m.client, m.query, m.store
	opts := m.searchOptions(pageToken)
	first := pageToken == ""
	return func() tea.Msg {
		result, err := client.Search(context.Background(), opts)
		if err == nil {
			st.putPage(opts, result)
		}
		return pageMsg{query: query, fields: opts.Fields, first: first, result: result, err: err}
	}
}

// searchOptions is what this model asks for. Only the fields behind displayed
// columns are requested: that coupling is the point of resolving columns at
// all, and it means adding a column to config fetches it without a code
// change. It is also half of the cache key, so the two cannot drift.
func (m *Model) searchOptions(pageToken string) jira.SearchOptions {
	return jira.SearchOptions{
		JQL:        m.query,
		Fields:     ColumnFields(m.columns),
		PageToken:  pageToken,
		MaxResults: pageSize,
	}
}

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.list.rows = m.rows()
		m.list.clamp()
		return m, nil

	case tea.KeyMsg:
		return m, m.handleKey(msg)

	case cachedFieldsMsg:
		return m, m.handleCachedFields(msg)

	case cachedPageMsg:
		return m, m.handleCachedPage(msg)

	case clearedMsg:
		return m, m.handleCleared(msg)

	case fieldsMsg:
		return m, m.handleFields(msg)

	case pageMsg:
		return m, m.handlePage(msg)
	}
	return m, nil
}

// handleKey routes a keypress through the dispatcher and acts on what it
// resolved to. Ctrl-C is the one exception, because a program that can be
// wedged out of quitting is a program that has to be killed from another
// terminal.
func (m *Model) handleKey(msg tea.KeyMsg) tea.Cmd {
	if msg.Type == tea.KeyCtrlC {
		return tea.Quit
	}
	result := m.dispatch.Dispatch(msg.String())
	if result.Kind != ResultAction {
		return nil
	}
	return m.handleAction(result.Action, result.Count)
}

func (m *Model) handleAction(action config.Action, count int) tea.Cmd {
	// The overlay gets first refusal, so that the help key and Esc mean the
	// same thing while it is up as they do anywhere else.
	if m.help.HandleAction(action) {
		return nil
	}
	switch action {
	case config.ActionClosePane:
		// With only the list on screen there is no pane to close but the
		// application itself.
		return tea.Quit
	case config.ActionReload:
		return m.reload()
	}

	before := m.list.cursor
	m.list.move(action, count)
	if m.list.cursor == before {
		return nil
	}
	return m.maybePage()
}

// reload discards the loaded rows and runs the query again from the top,
// going to Jira rather than to the cache: R is what the user presses when they
// do not believe what is on screen, and a stored copy of it is no answer.
func (m *Model) reload() tea.Cmd {
	if m.columns == nil {
		// The field metadata never arrived, so there is nothing to reload but
		// the metadata itself.
		m.loading, m.status = true, nil
		return m.fetchFields()
	}
	m.list = list{rows: m.rows()}
	m.nextPage, m.isLast, m.status, m.loading = "", false, nil, true
	m.pageFailed, m.refreshing = false, false
	return m.search("")
}

// ClearCache empties the store and runs the current view again, so the next
// thing on screen came from Jira. It is what :cache clear binds to; the
// commandline that types those words is a later ticket.
//
// The delete runs in a command rather than here: another session may hold the
// write lock, and waiting for it on the update loop is a UI that has stopped
// repainting for as long as that takes.
func (m *Model) ClearCache() tea.Cmd {
	st := m.store
	return tea.Batch(func() tea.Msg { return clearedMsg{err: st.clear()} }, m.reload())
}

// clearedMsg reports what emptying the store came to. The refetch it triggers
// does not wait for it: a cache that refused to empty is a cache the refetch
// overwrites anyway.
type clearedMsg struct{ err error }

func (m *Model) handleCleared(msg clearedMsg) tea.Cmd {
	if msg.err != nil {
		m.status = msg.err
	}
	return nil
}

// maybePage requests the next page when the selection has come close to the
// end of what is loaded. One request is allowed to be outstanding, which is
// what keeps a held-down motion from asking for the same page several times.
func (m *Model) maybePage() tea.Cmd {
	if m.loading || m.isLast || m.pageFailed || m.columns == nil || !m.list.wantsMore() {
		return nil
	}
	m.loading = true
	return m.search(m.nextPage)
}

func (m *Model) handleFields(msg fieldsMsg) tea.Cmd {
	if msg.err != nil {
		if jira.IsOffline(msg.err) {
			m.offline = true
		}
		if m.columns != nil {
			// A failed revalidation of metadata we already have is not worth a
			// status line: the columns on screen are still usable, and the
			// search that matters reports its own failure.
			return nil
		}
		// Metadata we could not fetch is a transient failure, not a broken
		// config: say so, leave the UI up, and let R try again.
		m.loading, m.status = false, msg.err
		return nil
	}
	m.offline = false
	columns, err := ResolveColumns(m.cfg.Columns, msg.fields)
	if err != nil {
		// A column naming no field is a config mistake that every subsequent
		// search would repeat, so it ends the session naming the offending
		// column rather than being retried forever in a status line.
		m.err = err
		return tea.Quit
	}
	before := m.columns
	m.columns = columns
	m.store.putFields(msg.fields)
	if before == nil {
		return m.loadPage()
	}
	if slices.Equal(ColumnFields(before), ColumnFields(columns)) {
		// The cached columns were right, so the search already in flight is
		// asking for the correct fields and nothing needs re-requesting.
		return nil
	}
	// The site's fields moved under the cached ones, so what was asked for is
	// not what is wanted.
	m.list.rows = m.rows()
	return m.search("")
}

func (m *Model) handlePage(msg pageMsg) tea.Cmd {
	if msg.query != m.query || !slices.Equal(msg.fields, ColumnFields(m.columns)) {
		// The query or the columns moved on while this was in flight; its rows
		// belong to a list that is no longer on screen.
		return nil
	}
	m.loading, m.refreshing = false, false
	if msg.err != nil {
		// The rows are left alone. A failed refresh over data the user is
		// reading is a note in the status line, not an empty pane.
		m.status = msg.err
		m.offline = jira.IsOffline(msg.err)
		m.pageFailed = !msg.first
		return nil
	}
	m.status, m.offline = nil, false
	// Taken as given: the client already reconciles IsLast with whether there
	// is a token to follow, and re-deriving it here would only invite the two
	// answers to drift apart.
	m.applyPage(msg.result, msg.first)
	return m.maybePage()
}

// applyPage puts a result on screen. A first page replaces what is there,
// keeping the user on the row they were reading: the fresh result set is a
// different list, so the selection is restored by work item key rather than by
// index, which would silently move them to a neighbour.
func (m *Model) applyPage(result *jira.SearchResult, first bool) {
	m.nextPage, m.isLast = result.NextPageToken, result.IsLast

	var where anchor
	if first {
		where = m.list.anchor()
		m.list.issues = nil
	}
	for i := range result.Issues {
		m.list.issues = append(m.list.issues, &result.Issues[i])
	}
	m.list.rows = m.rows()
	if first {
		m.list.restore(where)
		return
	}
	m.list.clamp()
}

// rows is how many issues fit between the header and the status line.
func (m *Model) rows() int { return max(m.height-2, 0) }

func (m *Model) View() string {
	if m.help.Visible() {
		return m.help.String()
	}
	// Measured over every loaded row rather than the visible ones: widths
	// computed from what is on screen change as the screen scrolls, which
	// re-flows the whole table under a user who only moved the cursor.
	widths := layout(m.columns, m.list.issues, m.now(), max(m.width-gutterWidth, 0))

	lines := make([]string, 0, m.height)
	if len(m.columns) > 0 {
		lines = append(lines, header(m.columns, widths))
	}
	lines = append(lines, m.body(widths)...)

	// The status line is pinned to the bottom, so its position does not move
	// with the number of rows that happened to load.
	for len(lines) < max(m.height-1, 0) {
		lines = append(lines, "")
	}
	return strings.Join(append(lines, m.statusLine()), "\n")
}

// body is the rows, or the one line that explains why there are none.
func (m *Model) body(widths []int) []string {
	switch {
	case len(m.list.issues) > 0:
		return m.rowLines(widths)
	case m.loading:
		return []string{m.fit("Loading…")}
	case m.status != nil:
		// The reason is in the status line; the pane says why it is bare so
		// that an empty result and a failed request do not look alike.
		return []string{m.fit("The query did not run.")}
	default:
		return []string{m.fit("No work items match this query.")}
	}
}

func (m *Model) rowLines(widths []int) []string {
	now := m.now()
	visible := m.list.visible()
	lines := make([]string, 0, len(visible))
	for i, issue := range visible {
		text := row(m.columns, widths, func(c int) string {
			return m.columns[c].Render(issue, now)
		})
		if m.list.top+i == m.list.cursor {
			lines = append(lines, selectedStyle.Render(SelectedMarker+text))
			continue
		}
		lines = append(lines, strings.Repeat(" ", gutterWidth)+text)
	}
	return lines
}

// statusLine reports the failure if there was one, and otherwise where the
// selection is and what query put it there. Errors live here rather than in a
// modal: a modal has to be dismissed before the list can be looked at again.
func (m *Model) statusLine() string {
	if m.status != nil {
		return errorStyle.Render(m.fit(m.withOfflineMarker(m.status.Error())))
	}
	var b strings.Builder
	if n := len(m.list.issues); n > 0 {
		// A trailing + says there is more behind the last loaded row. The
		// endpoint reports no total, and inventing one by counting loaded rows
		// would claim a result set is smaller than it is.
		fmt.Fprintf(&b, "%d/%d", m.list.cursor+1, n)
		if !m.isLast {
			b.WriteString("+")
		}
	}
	if m.loading {
		if b.Len() > 0 {
			b.WriteString(" ")
		}
		// Refreshing and loading are different claims: one says rows are on
		// the way, the other says the rows already up are being checked.
		if m.refreshing {
			b.WriteString("refreshing…")
		} else {
			b.WriteString("loading…")
		}
	}
	if b.Len() > 0 {
		b.WriteString("  ")
	}
	b.WriteString(m.query)
	return statusStyle.Render(m.fit(m.withOfflineMarker(b.String())))
}

// withOfflineMarker prefixes the offline state, which outlives any one failure: while the
// site is unreachable the rows on screen are cached ones, and that is worth
// saying for as long as it is true.
func (m *Model) withOfflineMarker(line string) string {
	if !m.offline {
		return line
	}
	return "[offline] " + line
}

// fit truncates a free-text line to the terminal width, so a long JQL or a
// long error message cannot wrap and push the layout off screen.
func (m *Model) fit(s string) string {
	if m.width <= 0 {
		return s
	}
	return ansi.Truncate(s, m.width, "…")
}

var _ tea.Model = (*Model)(nil)

// Run starts the event loop. The alternate screen is entered and left by
// bubbletea, which is what restores the terminal on a clean quit, on a failed
// query, and on a panic alike -- there is no ANSI cleanup of our own to get
// wrong.
func Run(opts Options) error {
	m, err := New(opts)
	if err != nil {
		return err
	}
	if _, err := tea.NewProgram(m, tea.WithAltScreen()).Run(); err != nil {
		return err
	}
	return m.Err()
}
