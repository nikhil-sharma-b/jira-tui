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
	"os"
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
	// EditorExec defaults to tea.ExecProcess, which releases and restores the
	// terminal around the configured editor.
	EditorExec EditorExec
	// SearchDebounce is how long a keystroke in a picker that searches the
	// server waits for the next one. Zero takes defaultSearchDebounce; a test
	// sets it short so that what is being asserted is the ordering rather than
	// the wait.
	SearchDebounce time.Duration
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

	list   list
	detail detailPane
	focus  Pane

	listVisible   bool
	detailVisible bool
	zoomed        bool
	pin           string
	jumps         jumplist

	// prompt is the commandline or the search line while one is open, nil in
	// normal mode. It is paired with the dispatcher's modal state, which is
	// what routes keys to it; openPrompt and closePrompt are the only two
	// places that pairing is made or broken, so it has somewhere to be got
	// right rather than being everyone's business.
	prompt *prompt
	// history is the commandline's, for this session. Search keeps none: the
	// ticket asks for a command history, and a second one is a second thing
	// to explain for a line that is usually retyped in full anyway.
	history history
	// search is the in-pane search: what is being looked for in the rows that
	// are loaded.
	search paneSearch

	// picker is the choice on screen while one is being made, nil otherwise.
	// Like prompt it is paired with the dispatcher's modal state, and
	// beginTransitionPicker and closePicker are the only two places that
	// pairing is made or broken.
	picker *picker
	// transitionRequest orders every transition fetch and write, so a response
	// that outlived its question -- the picker closed, the focus moved, another
	// transition started -- is recognised and dropped.
	transitionRequest uint64
	// transitionNames are the live names a single completion keystroke is
	// completing over. They are set when the fetch lands and cleared in the
	// same call: transitions are never cached, and a completion offering a name
	// from a minute ago would be the same lie a cache would tell.
	transitionNames []string

	// assignRequest orders every user search and every assignment, so a
	// response that outlived its question -- the picker closed, another letter
	// typed, an assignment already started -- is recognised and dropped. It is
	// what makes the search debounced rather than merely delayed.
	assignRequest uint64
	// searchDebounce is how long a keystroke waits for the next one before the
	// server is asked about it.
	searchDebounce time.Duration
	// myself is the authenticated account, fetched once on the first assignment
	// of a session and kept: who the user is does not change while jt is open,
	// and the picker pins them to its top every time it opens.
	myself *jira.User

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
	// pageFailed records that a request for rows failed. Without it every
	// motion near the end would re-fire the same failing request, turning a
	// held-down j into a retry storm the user never asked for. R clears it,
	// and so does asking for a different query.
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

	editorCommand             []string
	editorExec                EditorExec
	editorRequest             uint64
	drafts                    map[draftKey]string
	pendingDescription        string
	pendingDescriptionRequest uint64
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
	editorCommand, err := cfg.ResolveEditor(os.Getenv)
	if err != nil {
		return nil, err
	}
	editorExec := opts.EditorExec
	if editorExec == nil {
		editorExec = tea.ExecProcess
	}
	debounce := opts.SearchDebounce
	if debounce <= 0 {
		debounce = defaultSearchDebounce
	}
	m := &Model{
		client:        opts.Client,
		cfg:           cfg,
		store:         store{cache: opts.Cache, site: cfg.SiteURL()},
		dispatch:      NewDispatcher(bindings),
		help:          NewHelp(bindings),
		query:         cfg.DefaultQuery,
		loading:       true,
		listVisible:   true,
		pin:           opts.Pin,
		jumps:         newJumplist(),
		now:           time.Now,
		editorCommand: editorCommand,
		editorExec:    editorExec,
		drafts:        make(map[draftKey]string),

		searchDebounce: debounce,
	}
	if opts.Pin != "" {
		m.listVisible = false
		m.detailVisible = true
		m.focus = PaneDetail
	}
	return m, nil
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
func (m *Model) Init() tea.Cmd {
	if m.pin == "" {
		return m.loadFields()
	}
	return tea.Batch(m.loadFields(), m.openKey(m.pin, true))
}

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

// fetchPage asks Jira for one page, and caches a first page that came back.
func (m *Model) fetchPage(pageToken string) tea.Cmd {
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
		m.resizePanes()
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

	case rowMsg:
		return m, m.handleRow(msg)

	case detailMsg:
		accepted := m.detail.handle(msg)
		if accepted && msg.err == nil {
			// The detail read is the live one; the row beside it came from a
			// cached search. Where they disagree, the live read is right.
			m.list.refresh(msg.issue)
		}
		if accepted && m.pendingDescription != "" && msg.request != m.pendingDescriptionRequest {
			m.pendingDescription = ""
		}
		if accepted && msg.err == nil && msg.request == m.pendingDescriptionRequest && m.pendingDescription != "" && m.detail.issue != nil && m.detail.issue.Key == m.pendingDescription {
			key := m.pendingDescription
			m.pendingDescription = ""
			return m, m.openEditor(writeDescription, key)
		}
		if accepted && msg.err != nil {
			m.pendingDescription = ""
		}
		return m, nil

	case commentsMsg:
		if m.detail.handleComments(msg) && msg.err != nil {
			m.status = msg.err
		}
		return m, nil

	case detailTickMsg:
		return m, m.detail.handleTick(msg)

	case editorStartMsg:
		return m, m.handleEditorStart(msg)

	case editorDoneMsg:
		return m, m.handleEditorDone(msg)

	case editorReadMsg:
		return m, m.handleEditorRead(msg)

	case writeDoneMsg:
		return m, m.handleWriteDone(msg)

	case transitionsMsg:
		return m, m.handleTransitions(msg)

	case transitionDoneMsg:
		return m, m.handleTransitionDone(msg)

	case usersMsg:
		return m, m.handleUsers(msg)

	case assignSearchMsg:
		return m, m.handleAssignSearch(msg)

	case assignDoneMsg:
		return m, m.handleAssignDone(msg)
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
	key := msg.String()
	// Terminals encode Ctrl-i as Tab. In normal mode the configured vim
	// binding wins; in text modes the same byte remains a completion key.
	if msg.Type == tea.KeyCtrlI && !m.dispatch.InText() {
		key = "ctrl+i"
	}
	switch result := m.dispatch.Dispatch(key); result.Kind {
	case ResultText:
		return m.handleText(result.Key)
	case ResultAction:
		return m.handleAction(result.Action, result.Count)
	}
	return nil
}

// handleText feeds a keypress to whichever widget is taking keys: the picker
// while one is open, otherwise the prompt. The dispatcher reports text only
// while a mode was entered by an action, and the actions that enter one are the
// actions that open a widget.
func (m *Model) handleText(key string) tea.Cmd {
	if m.picker != nil {
		before := m.picker.text()
		if m.picker.handle(key) {
			return m.chooseFromPicker()
		}
		if m.picker.serverFiltered && m.picker.text() != before {
			// The set on screen belongs to the filter that produced it, so a
			// changed filter is a question for the server rather than something
			// to narrow locally.
			return m.debounceUserSearch()
		}
		return nil
	}
	if m.prompt == nil {
		// The mode and the widget have come apart. The mode is the wrong one,
		// since there is nothing on screen for these keys to be typed into.
		m.dispatch.Reset()
		return nil
	}
	if key == "tab" {
		// Some arguments can only be completed against Jira. The fetch is asked
		// for before the widget sees the key, so that completion itself stays
		// synchronous and the answer is always a live one.
		if cmd := m.liveCompletion(m.prompt.text()); cmd != nil {
			return cmd
		}
	}
	if !m.prompt.handle(key) {
		return nil
	}
	line, mode := m.prompt.text(), m.prompt.mode
	m.closePrompt()
	if mode == ModeSearch {
		return m.runSearch(line)
	}
	m.history.add(line)
	return m.runCommand(line)
}

// openPrompt puts a line up to be typed into. Whatever was last reported has
// been read or it has not; either way it is not what the user is typing about.
func (m *Model) openPrompt(p *prompt) tea.Cmd {
	// A line typed under a full-screen overlay is a line typed blind.
	m.help.Hide()
	m.status = nil
	m.prompt = p
	return nil
}

// closePrompt puts the widget away and the dispatcher back in normal mode, so
// that submitting and cancelling leave the same state behind. Reset is
// idempotent, which is what lets Esc -- already resolved to normal mode by the
// dispatcher before any widget saw it -- come through here like anything else.
func (m *Model) closePrompt() {
	m.prompt = nil
	m.dispatch.Reset()
}

func (m *Model) handleAction(action config.Action, count int) tea.Cmd {
	// The overlay gets first refusal, so that the help key and Esc mean the
	// same thing while it is up as they do anywhere else.
	if m.help.HandleAction(action) {
		return nil
	}
	switch action {
	case config.ActionNormalMode:
		// Esc abandons whatever was being typed or chosen and leaves everything
		// else -- the rows, the query, the search pattern -- alone. Nothing is
		// applied on the way out.
		m.closePicker()
		m.closePrompt()
		return nil
	case config.ActionCommandline:
		return m.openPrompt(m.newCommandPrompt())
	case config.ActionSearchInPane:
		return m.openPrompt(m.newSearchPrompt())
	case config.ActionSearchNext:
		return m.moveToMatch(1, max(count, 1), false)
	case config.ActionSearchPrev:
		return m.moveToMatch(-1, max(count, 1), false)
	case config.ActionClosePane:
		return m.closePane()
	case config.ActionOpen:
		return m.openDetail()
	case config.ActionPaneLeft:
		m.moveFocus(PaneList)
		return nil
	case config.ActionPaneRight:
		m.moveFocus(PaneDetail)
		return nil
	case config.ActionPaneZoom:
		m.toggleZoom()
		return nil
	case config.ActionGoList:
		m.goList()
		return nil
	case config.ActionGoDetail:
		m.goDetail()
		return nil
	case config.ActionGoComments:
		m.goComments()
		return nil
	case config.ActionJumpBack:
		return m.jump(-1, count)
	case config.ActionJumpFwd:
		return m.jump(1, count)
	case config.ActionReload:
		return m.reload()
	case config.ActionComment:
		return m.beginWrite(writeComment)
	case config.ActionEditDesc:
		return m.beginWrite(writeDescription)
	case config.ActionTransition:
		return m.beginTransitionPicker()
	case config.ActionAssign:
		return m.beginAssignPicker()
	}

	// A key bound straight to a transition name carries the name in the action
	// itself, because whether the site has such a transition -- and whether it
	// is available on this item now -- is only answerable live.
	if name, ok := action.TransitionName(); ok {
		return m.beginNamedTransition(name)
	}

	listVisible, detailVisible := m.visiblePanes()
	if m.focus == PaneDetail && detailVisible && m.detail.open && !m.detail.loading {
		m.detail.move(action, count)
		return nil
	}
	if !listVisible {
		return nil
	}
	before := m.list.cursor
	m.list.move(action, count)
	if m.list.cursor == before {
		return nil
	}
	m.detail.selectionMoved()
	return m.maybePage()
}

func (m *Model) openDetail() tea.Cmd {
	if len(m.list.issues) == 0 || m.list.cursor >= len(m.list.issues) {
		return nil
	}
	m.listVisible = true
	return m.openKey(m.list.issues[m.list.cursor].Key, true)
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
	m.beginQuery()
	return m.fetchPage("")
}

// closePane closes detail when it exists and quits when the list is the last
// pane. Both q and :q come through this one decision.
func (m *Model) closePane() tea.Cmd {
	if m.focus == PaneDetail && m.detail.open {
		m.detail.close()
		m.detailVisible = false
		m.listVisible = true
		m.zoomed = false
		m.focus = PaneList
		m.resizePanes()
		return nil
	}
	if m.focus == PaneList && m.detail.open {
		m.listVisible = false
		m.detailVisible = true
		m.zoomed = false
		m.focus = PaneDetail
		m.resizePanes()
		return nil
	}
	return tea.Quit
}

// newCommandPrompt opens the commandline over the session's history and the
// command table's own completions.
func (m *Model) newCommandPrompt() *prompt {
	// The walk belongs to the line about to be typed, not to the session.
	m.history.reset()
	return &prompt{mode: ModeCommand, sigil: ":", history: &m.history, complete: m.completeCommandLine}
}

// newSearchPrompt opens the in-pane search line. It completes nothing: the
// candidates would be the rows themselves, which is what the search is for.
func (m *Model) newSearchPrompt() *prompt {
	return &prompt{mode: ModeSearch, sigil: "/"}
}

// runQuery puts a different JQL on screen. Unlike reload it does not empty the
// list first: a query the server rejects must leave the rows the user was
// reading up, and a query it accepts replaces them when its own page lands.
//
// It goes through the cache like any other view. R is the one thing that
// bypasses it, because R is what the user presses when they do not believe
// what is on screen.
func (m *Model) runQuery(query string) tea.Cmd {
	m.query = query
	m.beginQuery()
	if m.columns == nil {
		// Nothing can be asked for until the field metadata resolves. What is
		// already in flight for it will run this query when it lands, because
		// it reads the query from the model rather than carrying its own.
		return nil
	}
	return m.loadPage()
}

// beginQuery forgets everything that described the result set that was on
// screen, which is what R and :jql both have to do before the first page of
// the next one can arrive. It leaves the rows alone: whether they stay up
// while the answer is fetched is the caller's decision, and the two callers
// decide differently.
func (m *Model) beginQuery() {
	m.nextPage, m.isLast = "", false
	m.status, m.pageFailed = nil, false
	m.loading, m.refreshing = true, false
}

// clearCache empties the store and runs the current view again, so the next
// thing on screen came from Jira. It is what :cache clear binds to.
//
// The delete runs in a command rather than here: another session may hold the
// write lock, and waiting for it on the update loop is a UI that has stopped
// repainting for as long as that takes.
func (m *Model) clearCache() tea.Cmd {
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
	return m.fetchPage(m.nextPage)
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
	return m.fetchPage("")
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
		// Any failure closes the door on re-firing this request from a motion,
		// first page or not. A rejected :jql leaves the previous query's rows
		// up, and a motion over rows that are still there would otherwise ask
		// for the rejected query again on every j.
		m.pageFailed = true
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
	listVisible, detailVisible := m.visiblePanes()
	if listVisible && detailVisible {
		return m.twoPaneView()
	}
	footer := m.footer()
	bodyRows := max(m.height-len(footer), 0)
	var lines []string
	if detailVisible {
		lines = m.detail.view()
	} else {
		lines = m.listPane(m.width, bodyRows)
	}
	for len(lines) < bodyRows {
		lines = append(lines, "")
	}
	if len(lines) > bodyRows {
		lines = lines[:bodyRows]
	}
	return strings.Join(append(lines, footer...), "\n")
}

func (m *Model) paneWidths() (int, int) {
	available := max(m.width-1, 0)
	left := available * 45 / 100
	return left, available - left
}

func (m *Model) resizePanes() {
	if !m.detail.open {
		return
	}
	listVisible, detailVisible := m.visiblePanes()
	width := m.width
	if listVisible && detailVisible {
		_, width = m.paneWidths()
	} else if !detailVisible && m.listVisible && m.detailVisible {
		// The detail is hidden by a list zoom. Keep its split layout current so
		// restoring the split never flashes stale wrapping.
		_, width = m.paneWidths()
	}
	m.detail.resize(width, max(m.height-len(m.footer()), 0))
}

func (m *Model) twoPaneView() string {
	footer := m.footer()
	bodyRows := max(m.height-len(footer), 0)
	leftWidth, rightWidth := m.paneWidths()
	left := m.listPane(leftWidth, bodyRows)
	right := m.detail.view()

	lines := make([]string, bodyRows)
	for i := range bodyRows {
		l, r := "", ""
		if i < len(left) {
			l = left[i]
		}
		if i < len(right) {
			r = right[i]
		}
		if pad := leftWidth - ansi.StringWidth(l); pad > 0 {
			l += strings.Repeat(" ", pad)
		}
		lines[i] = fitWidth(l, leftWidth) + "│" + fitWidth(r, rightWidth)
	}
	return strings.Join(append(lines, footer...), "\n")
}

func (m *Model) listPane(width, rows int) []string {
	widths := layout(m.columns, m.list.issues, m.now(), max(width-gutterWidth, 0))
	lines := make([]string, 0, rows)
	if len(m.columns) > 0 && rows > 0 {
		lines = append(lines, fitWidth(header(m.columns, widths), width))
	}
	switch {
	case len(m.list.issues) > 0:
		for _, line := range m.rowLines(widths) {
			lines = append(lines, fitWidth(line, width))
		}
	case m.loading:
		lines = append(lines, fitWidth("Loading…", width))
	case m.status != nil:
		lines = append(lines, fitWidth("The query did not run.", width))
	default:
		lines = append(lines, fitWidth("No work items match this query.", width))
	}
	if len(lines) > rows {
		lines = lines[:rows]
	}
	return lines
}

// footer is the bottom of the screen: the line being typed if one is, and
// above it the completion candidates when the last Tab could not choose
// between them.
func (m *Model) footer() []string {
	if m.picker != nil {
		// The picker sits above the status line rather than over the panes: the
		// item being transitioned has to stay readable while it is chosen.
		return append(m.picker.lines(m.width), m.statusLine())
	}
	if m.prompt == nil {
		return []string{m.statusLine()}
	}
	line := m.fit(m.prompt.render())
	if note := m.prompt.note; note != "" {
		return []string{m.fit(errorStyle.Render(note)), line}
	}
	if len(m.prompt.candidates) > 0 {
		return []string{m.fit(statusStyle.Render(strings.Join(m.prompt.candidates, "  "))), line}
	}
	return []string{line}
}

func (m *Model) rowLines(widths []int) []string {
	now := m.now()
	visible := m.list.visible()
	lines := make([]string, 0, len(visible))
	for i, issue := range visible {
		text := row(m.columns, widths, func(c int) string {
			return m.columns[c].Render(issue, now)
		})
		base, match := plainStyle, matchStyle
		if m.list.top+i == m.list.cursor {
			text = SelectedMarker + text
			base, match = selectedStyle, selectedMatchStyle
		} else {
			text = strings.Repeat(" ", gutterWidth) + text
		}
		lines = append(lines, highlight(text, m.search.pattern, base, match))
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
