package ui_test

import (
	"errors"
	"strconv"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/nikhil-sharma-b/jira-tui/internal/config"
	"github.com/nikhil-sharma-b/jira-tui/internal/jira"
	"github.com/nikhil-sharma-b/jira-tui/internal/ui"
)

// driver runs a model the way bubbletea does, except that commands are queued
// rather than run the moment they are returned. That is what lets a test
// observe the UI while a request is in flight -- which is when the loading
// indicator exists and when a duplicate page request would be made.
type driver struct {
	t       *testing.T
	model   tea.Model
	pending []tea.Cmd
	// quit records that the model asked bubbletea to exit, which is the only
	// observable form of ":qa" and of closing the last pane.
	quit bool
}

func newDriver(t *testing.T, client jira.Client, cfg *config.Config) *driver {
	t.Helper()
	d := newPausedDriver(t, ui.Options{Client: client, Config: cfg})
	d.flush()
	return d
}

// newPausedDriver builds a driver with the startup commands queued but not
// run, which is the only vantage point from which "what is on screen before
// the network answers" can be asked at all.
func newPausedDriver(t *testing.T, opts ui.Options) *driver {
	t.Helper()
	m, err := ui.New(opts)
	if err != nil {
		t.Fatalf("building the model: %v", err)
	}
	// A fixed clock keeps relative timestamps assertable.
	m.SetNow(func() time.Time { return time.Date(2026, 8, 25, 12, 0, 0, 0, time.UTC) })

	d := &driver{t: t, model: m}
	d.queue(m.Init())
	d.send(tea.WindowSizeMsg{Width: 100, Height: 22})
	return d
}

func (d *driver) queue(cmd tea.Cmd) {
	if cmd != nil {
		d.pending = append(d.pending, cmd)
	}
}

func (d *driver) send(msg tea.Msg) {
	model, cmd := d.model.Update(msg)
	d.model = model
	d.queue(cmd)
}

// flush runs every queued command and feeds what it produced back in, until
// the model stops asking for anything.
func (d *driver) flush() {
	for len(d.pending) > 0 {
		cmd := d.pending[0]
		d.pending = d.pending[1:]
		d.deliver(cmd())
	}
}

// deliver feeds one command's result back in, unwrapping a batch and noting a
// quit, which no model of ours has an Update case for.
func (d *driver) deliver(msg tea.Msg) {
	switch msg := msg.(type) {
	case nil:
	case tea.QuitMsg:
		d.quit = true
	case tea.BatchMsg:
		for _, c := range msg {
			d.queue(c)
		}
	default:
		d.send(msg)
	}
}

// step runs one queued command and feeds back what it produced, so a test can
// advance the model one round trip at a time.
func (d *driver) step() {
	d.t.Helper()
	if len(d.pending) == 0 {
		d.t.Fatal("nothing is queued to step")
	}
	cmd := d.pending[0]
	d.pending = d.pending[1:]
	d.deliver(cmd())
}

// stepUntil advances the model one queued command at a time until the screen
// shows want, which is how a test observes an intermediate state without
// running the requests queued behind it.
func (d *driver) stepUntil(want string) {
	d.t.Helper()
	for len(d.pending) > 0 {
		if strings.Contains(d.view(), want) {
			return
		}
		d.step()
	}
	if !strings.Contains(d.view(), want) {
		d.t.Fatalf("%q never reached the screen:\n%s", want, d.view())
	}
}

// keys types a sequence of already-normalized key names.
func (d *driver) keys(names ...string) {
	for _, name := range names {
		d.send(keyMsg(name))
	}
	d.flush()
}

// typeText types text one rune at a time, which is what a user does and what
// distinguishes a run of characters from a key with a multi-character name.
func (d *driver) typeText(text string) {
	for _, r := range text {
		d.send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	d.flush()
}

// command types a whole commandline, leading colon and all, and submits it --
// which is the only way anything on the commandline is reached.
func (d *driver) command(line string) {
	d.t.Helper()
	d.keys(":")
	d.typeText(strings.TrimPrefix(line, ":"))
	d.keys("enter")
}

func keyMsg(name string) tea.KeyMsg {
	switch name {
	case "ctrl+d":
		return tea.KeyMsg{Type: tea.KeyCtrlD}
	case "ctrl+u":
		return tea.KeyMsg{Type: tea.KeyCtrlU}
	case "ctrl+i":
		return tea.KeyMsg{Type: tea.KeyCtrlI}
	case "ctrl+o":
		return tea.KeyMsg{Type: tea.KeyCtrlO}
	case "ctrl+w":
		return tea.KeyMsg{Type: tea.KeyCtrlW}
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	case "esc":
		return tea.KeyMsg{Type: tea.KeyEsc}
	case "tab":
		return tea.KeyMsg{Type: tea.KeyTab}
	case "backspace":
		return tea.KeyMsg{Type: tea.KeyBackspace}
	case "up":
		return tea.KeyMsg{Type: tea.KeyUp}
	case "down":
		return tea.KeyMsg{Type: tea.KeyDown}
	case "left":
		return tea.KeyMsg{Type: tea.KeyLeft}
	case "right":
		return tea.KeyMsg{Type: tea.KeyRight}
	}
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(name)}
}

// view is the rendered screen with styling removed, since what a test cares
// about is the text a user reads.
func (d *driver) view() string { return ansi.Strip(d.model.View()) }

func (d *driver) lines() []string { return strings.Split(d.view(), "\n") }

// split reports that both panes are on screen, which now reads as two frames
// side by side rather than as a single divider column.
func (d *driver) split() bool {
	return strings.Count(d.lines()[0], "╭") > 1
}

// selected is the key on the marked row, which is the observable form of "the
// selection is here".
func (d *driver) selected() string {
	d.t.Helper()
	for _, line := range d.lines() {
		if i := strings.Index(line, ui.SelectedMarker); i >= 0 {
			fields := strings.Fields(line[i+len(ui.SelectedMarker):])
			// The row opens with its number, which is not what "the selection
			// is here" means to a test.
			if len(fields) > 1 {
				if _, err := strconv.Atoi(fields[0]); err == nil {
					fields = fields[1:]
				}
			}
			return fields[0]
		}
	}
	return ""
}

// visibleKeys lists the issue keys currently drawn, in order, so a test can
// assert that the viewport scrolled rather than that a field changed.
func (d *driver) visibleKeys() []string {
	var keys []string
	for _, line := range d.lines() {
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		for _, f := range fields {
			if strings.HasPrefix(f, "ENG-") {
				keys = append(keys, f)
				break
			}
		}
	}
	return keys
}

func testConfig(t *testing.T, columns []string) *config.Config {
	t.Helper()
	cfg := config.Defaults()
	cfg.Site.URL = "https://example.atlassian.net"
	cfg.Site.Email = "user@example.com"
	if columns != nil {
		cfg.Columns = columns
	}
	if _, err := cfg.Bindings(); err != nil {
		t.Fatalf("compiling bindings: %v", err)
	}
	return cfg
}

func listWith(t *testing.T, n int) *driver {
	t.Helper()
	return newDriver(t, &fakeClient{issues: sampleIssues(n)}, testConfig(t, nil))
}

func TestListShowsTheConfiguredColumns(t *testing.T) {
	d := listWith(t, 3)

	view := d.view()
	for _, want := range []string{"Key", "Summary", "Status", "Assignee", "Priority", "Updated"} {
		if !strings.Contains(view, want) {
			t.Errorf("the header does not show the %q column:\n%s", want, view)
		}
	}
	for _, want := range []string{"ENG-1", "Work item number 1", "In Progress", "Ada Lovelace"} {
		if !strings.Contains(view, want) {
			t.Errorf("the list does not show %q:\n%s", want, view)
		}
	}
}

// Fetching every field is the single biggest avoidable cost on a list load, so
// the request must name the displayed fields and only those.
func TestSearchRequestsOnlyTheDisplayedFields(t *testing.T) {
	client := &fakeClient{issues: sampleIssues(3)}
	newDriver(t, client, testConfig(t, []string{"key", "summary", "status"}))

	reqs := client.requests()
	if len(reqs) == 0 {
		t.Fatal("no search was made")
	}
	if got := strings.Join(reqs[0].Fields, ","); got != "summary,status" {
		t.Errorf("the search requested fields %q, want %q", got, "summary,status")
	}
}

func TestListRunsTheConfiguredDefaultQuery(t *testing.T) {
	client := &fakeClient{issues: sampleIssues(1)}
	cfg := testConfig(t, nil)
	cfg.DefaultQuery = "project = ENG ORDER BY created DESC"
	newDriver(t, client, cfg)

	reqs := client.requests()
	if len(reqs) == 0 {
		t.Fatal("no search was made")
	}
	if reqs[0].JQL != cfg.DefaultQuery {
		t.Errorf("the search ran %q, want %q", reqs[0].JQL, cfg.DefaultQuery)
	}
}

func TestDefaultQueryIsTheCurrentUsersUnresolvedWorkByRecency(t *testing.T) {
	client := &fakeClient{issues: sampleIssues(1)}
	newDriver(t, client, testConfig(t, nil))

	jql := client.requests()[0].JQL
	for _, want := range []string{"currentUser()", "Unresolved", "ORDER BY updated DESC"} {
		if !strings.Contains(jql, want) {
			t.Errorf("the default query %q does not mention %q", jql, want)
		}
	}
}

func TestEmptyResultExplainsItself(t *testing.T) {
	d := newDriver(t, &fakeClient{}, testConfig(t, nil))

	if !strings.Contains(strings.ToLower(d.view()), "no work items") {
		t.Errorf("an empty result set rendered no explanation:\n%s", d.view())
	}
}

// A query that fails must say why and leave the application usable, because
// the alternative -- exiting on a bad JQL -- means retyping it from scratch.
func TestFailedQueryShowsTheReasonAndKeepsTheUIUsable(t *testing.T) {
	client := &fakeClient{searchErr: errors.New("the JQL is malformed")}
	d := newDriver(t, client, testConfig(t, nil))

	if !strings.Contains(d.view(), "the JQL is malformed") {
		t.Errorf("the failure is not in the status line:\n%s", d.view())
	}
	d.keys("?")
	if !strings.Contains(d.view(), "MOTION") {
		t.Errorf("the UI stopped responding after a failed query:\n%s", d.view())
	}
}

// The name in the message is the only thing that tells the user which line of
// their config to fix.
func TestUnknownColumnIsReportedAtStartup(t *testing.T) {
	client := &fakeClient{issues: sampleIssues(1)}
	m, err := ui.New(ui.Options{Client: client, Config: testConfig(t, []string{"key", "Storey Points"})})
	if err != nil {
		t.Fatalf("building the model: %v", err)
	}
	d := &driver{t: t, model: m}
	d.queue(m.Init())
	d.send(tea.WindowSizeMsg{Width: 100, Height: 22})
	d.flush()

	if m.Err() == nil {
		t.Fatal("an unresolvable column did not stop startup")
	}
	if !strings.Contains(m.Err().Error(), `"Storey Points"`) {
		t.Errorf("the startup error %q does not name the offending column", m.Err())
	}
	if len(client.requests()) != 0 {
		t.Error("a search was made despite the columns being unresolvable")
	}
}

func TestMotionsMoveTheSelection(t *testing.T) {
	cases := []struct {
		name string
		keys []string
		want string
	}{
		{"down", []string{"j"}, "ENG-2"},
		{"down then up", []string{"j", "j", "k"}, "ENG-2"},
		{"counted down", []string{"5", "j"}, "ENG-6"},
		{"bottom", []string{"G"}, "ENG-30"},
		{"top from bottom", []string{"G", "g", "g"}, "ENG-1"},
		{"counted bottom is a line number", []string{"1", "2", "G"}, "ENG-12"},
		{"counted top is a line number", []string{"G", "7", "g", "g"}, "ENG-7"},
		{"half page down", []string{"ctrl+d"}, "ENG-10"},
		{"half page down twice", []string{"ctrl+d", "ctrl+d"}, "ENG-19"},
		{"counted half page down", []string{"2", "ctrl+d"}, "ENG-19"},
		{"half page up", []string{"ctrl+d", "ctrl+d", "ctrl+u"}, "ENG-10"},
		{"up stops at the top", []string{"k"}, "ENG-1"},
		{"down stops at the bottom", []string{"G", "j"}, "ENG-30"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			d := listWith(t, 30)
			d.keys(c.keys...)
			if got := d.selected(); got != c.want {
				t.Errorf("after %v the selection is %s, want %s", c.keys, got, c.want)
			}
		})
	}
}

// H, M and L address the viewport rather than the result set, which is only
// observable once the two have come apart.
func TestViewportMotionsAddressTheVisibleRows(t *testing.T) {
	d := listWith(t, 30)
	d.keys("G")
	visible := d.visibleKeys()
	if len(visible) < 3 {
		t.Fatalf("only %d rows are visible, too few to tell viewport motion apart", len(visible))
	}

	d.keys("H")
	if got := d.selected(); got != visible[0] {
		t.Errorf("H selected %s, want the top visible row %s", got, visible[0])
	}
	d.keys("L")
	if got := d.selected(); got != visible[len(visible)-1] {
		t.Errorf("L selected %s, want the bottom visible row %s", got, visible[len(visible)-1])
	}
	d.keys("M")
	if got := d.selected(); got != visible[len(visible)/2] {
		t.Errorf("M selected %s, want the middle visible row %s", got, visible[len(visible)/2])
	}
}

func TestSelectionStaysVisibleAsItMoves(t *testing.T) {
	d := listWith(t, 60)

	for _, keys := range [][]string{{"G"}, {"g", "g"}, {"4", "0", "j"}, {"ctrl+u"}} {
		d.keys(keys...)
		selected := d.selected()
		if selected == "" {
			t.Fatalf("after %v nothing is marked as selected:\n%s", keys, d.view())
		}
		visible := d.visibleKeys()
		if !contains(visible, selected) {
			t.Errorf("after %v the selection %s is off screen; visible: %v", keys, selected, visible)
		}
	}
}

func TestViewportScrollsRatherThanGrowing(t *testing.T) {
	d := listWith(t, 40)
	first := d.visibleKeys()

	d.keys("G")
	last := d.visibleKeys()

	if len(last) != len(first) {
		t.Errorf("the viewport shows %d rows at the bottom and %d at the top", len(last), len(first))
	}
	if last[0] == first[0] {
		t.Errorf("the viewport did not scroll: it still starts at %s", first[0])
	}
	if !contains(last, "ENG-40") {
		t.Errorf("the last row is not visible after G; visible: %v", last)
	}
}

// G goes to the end of what is loaded rather than to the end of the result
// set: the alternative is fetching every page to answer one keypress. The page
// it triggers is what makes the second G reach further.
func TestBottomWalksToTheEndOfALongResultSetOnePageAtATime(t *testing.T) {
	d := listWith(t, 120)

	d.keys("G")
	if got := d.selected(); got != "ENG-50" {
		t.Fatalf("G selected %s, want the last loaded row ENG-50", got)
	}
	d.keys("G")
	if got := d.selected(); got != "ENG-100" {
		t.Errorf("a second G selected %s, want ENG-100", got)
	}
}

// A resize must re-lay out rather than leave rows written to the old width,
// which is what corrupts a display.
func TestResizeRelaysOutWithinTheNewWidth(t *testing.T) {
	d := listWith(t, 10)

	for _, width := range []int{100, 60, 40, 24, 120} {
		d.send(tea.WindowSizeMsg{Width: width, Height: 22})
		d.flush()
		for _, line := range d.lines() {
			if w := ansi.StringWidth(line); w > width {
				t.Errorf("at width %d a line is %d wide: %q", width, w, line)
			}
		}
		if !strings.Contains(d.view(), "ENG-1") {
			t.Errorf("at width %d the list shows no rows:\n%s", width, d.view())
		}
	}
}

func TestNarrowTerminalKeepsTheLeftmostColumns(t *testing.T) {
	d := listWith(t, 10)
	d.send(tea.WindowSizeMsg{Width: 30, Height: 22})
	d.flush()

	view := d.view()
	if !strings.Contains(view, "ENG-1") {
		t.Errorf("the key column was dropped at 30 columns:\n%s", view)
	}
	if strings.Contains(view, "Ada Lovelace") {
		t.Errorf("30 columns is not enough for the assignee, yet it was drawn:\n%s", view)
	}
}

// Paging on approach rather than on arrival is what keeps a long list feeling
// continuous: by the time the selection reaches the end, the next page is
// already there.
func TestLongResultSetsPageAsTheSelectionApproachesTheEnd(t *testing.T) {
	client := &fakeClient{issues: sampleIssues(120)}
	d := newDriver(t, client, testConfig(t, nil))

	if n := len(client.requests()); n != 1 {
		t.Fatalf("%d searches were made on open, want 1", n)
	}
	d.keys("G")
	if n := len(client.requests()); n < 2 {
		t.Fatalf("reaching the end of the loaded rows made %d searches, want a further page", n)
	}
	reqs := client.requests()
	if reqs[0].PageToken != "" {
		t.Errorf("the first search carried a page token %q; there was none to carry", reqs[0].PageToken)
	}
	if reqs[1].PageToken == "" {
		t.Error("the second search asked for the first page again rather than continuing")
	}
}

// Two motions in the time one request takes must not become two requests for
// the same page, or a slow link turns a held-down j into a stampede.
func TestPagingDoesNotDuplicateAnInFlightRequest(t *testing.T) {
	client := &fakeClient{issues: sampleIssues(120)}
	d := newDriver(t, client, testConfig(t, nil))

	// Deliberately not flushed: the page request is queued, so the model is
	// in exactly the state it would be in while the response is on the wire.
	d.send(keyMsg("G"))
	d.send(keyMsg("k"))
	d.send(keyMsg("j"))

	if n := len(client.requests()); n != 1 {
		t.Fatalf("%d searches ran before the queued page was even sent", n)
	}
	d.flush()
	if n := len(client.requests()); n != 2 {
		t.Errorf("%d searches were made, want 2: one page in flight must not be requested twice", n)
	}
}

func TestLoadingIsVisibleWhileTheFirstPageIsInFlight(t *testing.T) {
	client := &fakeClient{issues: sampleIssues(5)}
	m, err := ui.New(ui.Options{Client: client, Config: testConfig(t, nil)})
	if err != nil {
		t.Fatalf("building the model: %v", err)
	}
	d := &driver{t: t, model: m}
	d.queue(m.Init())
	d.send(tea.WindowSizeMsg{Width: 100, Height: 22})

	if !strings.Contains(strings.ToLower(ansi.Strip(m.View())), "loading") {
		t.Errorf("nothing said the list was loading:\n%s", ansi.Strip(m.View()))
	}
	d.flush()
	if strings.Contains(strings.ToLower(ansi.Strip(m.View())), "loading") {
		t.Errorf("the loading indicator outlived the request:\n%s", ansi.Strip(m.View()))
	}
}

func TestClosingTheLastPaneQuits(t *testing.T) {
	d := listWith(t, 3)
	d.send(keyMsg("q"))

	if len(d.pending) == 0 {
		t.Fatal("q asked for nothing; the program would not quit")
	}
	if _, ok := d.pending[0]().(tea.QuitMsg); !ok {
		t.Error("q did not quit")
	}
}

func TestCountsDoNotLeakIntoTheNextMotion(t *testing.T) {
	d := listWith(t, 30)
	d.keys("5", "j")
	d.keys("j")

	if got := d.selected(); got != "ENG-7" {
		t.Errorf("after 5j then j the selection is %s, want ENG-7", got)
	}
}

func contains(haystack []string, needle string) bool {
	for _, s := range haystack {
		if s == needle {
			return true
		}
	}
	return false
}

// Resolution is only half of it: the value has to reach the screen, and a
// custom field's value lives somewhere no typed struct member does.
func TestCustomFieldColumnDisplaysItsValue(t *testing.T) {
	issues := sampleIssues(1)
	issues[0].Raw = map[string]any{"customfield_10016": 8.0}
	client := &fakeClient{issues: issues}
	d := newDriver(t, client, testConfig(t, []string{"key", "Story Points"}))

	if got := client.requests()[0].Fields; strings.Join(got, ",") != "customfield_10016" {
		t.Errorf("the search requested %v, want customfield_10016", got)
	}
	view := d.view()
	if !strings.Contains(view, "Story Points") {
		t.Errorf("the custom column has no header:\n%s", view)
	}
	if !strings.Contains(view, "8") {
		t.Errorf("the custom field's value is not on screen:\n%s", view)
	}
}

// Leaving after a failure is the case a manual terminal reset gets wrong, so
// it is worth stating that the same quit path serves both.
func TestQuittingAfterAFailedQueryStillQuits(t *testing.T) {
	client := &fakeClient{searchErr: errors.New("the JQL is malformed")}
	d := newDriver(t, client, testConfig(t, nil))

	d.send(keyMsg("q"))
	if len(d.pending) == 0 {
		t.Fatal("q asked for nothing after a failed query")
	}
	if _, ok := d.pending[0]().(tea.QuitMsg); !ok {
		t.Error("q did not quit after a failed query")
	}
}

// The header is what says a query ran and came back with nothing, as opposed
// to the pane never having loaded at all.
func TestEmptyResultStillShowsTheColumnHeader(t *testing.T) {
	d := newDriver(t, &fakeClient{}, testConfig(t, nil))

	if !strings.Contains(d.view(), "Summary") {
		t.Errorf("an empty result set drew no column header:\n%s", d.view())
	}
}

// Field metadata we could not fetch is a transient failure, not a broken
// config: it must not end the session the way an unresolvable column does.
func TestFailedFieldMetadataLeavesTheUIUsable(t *testing.T) {
	client := &fakeClient{fieldsErr: errors.New("the site is unreachable")}
	m, err := ui.New(ui.Options{Client: client, Config: testConfig(t, nil)})
	if err != nil {
		t.Fatalf("building the model: %v", err)
	}
	d := &driver{t: t, model: m}
	d.queue(m.Init())
	d.send(tea.WindowSizeMsg{Width: 100, Height: 22})
	d.flush()

	if m.Err() != nil {
		t.Errorf("a transient metadata failure ended the session: %v", m.Err())
	}
	if !strings.Contains(d.view(), "the site is unreachable") {
		t.Errorf("the metadata failure is not in the status line:\n%s", d.view())
	}
	if len(client.requests()) != 0 {
		t.Error("a search ran without the fields to build it from")
	}

	d.keys("?")
	if !strings.Contains(d.view(), "MOTION") {
		t.Error("the UI stopped responding after the metadata fetch failed")
	}
}

// R is the way back from a transient failure, and it has to retry the step
// that actually failed rather than the search that never ran.
func TestReloadRetriesTheMetadataFetchThatFailed(t *testing.T) {
	client := &fakeClient{fieldsErr: errors.New("the site is unreachable"), issues: sampleIssues(2)}
	m, err := ui.New(ui.Options{Client: client, Config: testConfig(t, nil)})
	if err != nil {
		t.Fatalf("building the model: %v", err)
	}
	d := &driver{t: t, model: m}
	d.queue(m.Init())
	d.send(tea.WindowSizeMsg{Width: 100, Height: 22})
	d.flush()

	client.fieldsErr = nil
	d.keys("R")

	if !strings.Contains(d.view(), "ENG-1") {
		t.Errorf("reloading after a metadata failure loaded nothing:\n%s", d.view())
	}
}

// vim counts H and L in from the edge of the screen; M has no edge to count
// from and takes none.
func TestCountedViewportMotionsCountInFromTheEdge(t *testing.T) {
	d := listWith(t, 30)
	d.keys("G")
	visible := d.visibleKeys()

	cases := []struct {
		name string
		keys []string
		want string
	}{
		{"3H", []string{"3", "H"}, visible[2]},
		{"3L", []string{"3", "L"}, visible[len(visible)-3]},
		{"H is the first visible row", []string{"H"}, visible[0]},
		{"L is the last visible row", []string{"L"}, visible[len(visible)-1]},
		{"a count past the top stops at the top", []string{"H"}, visible[0]},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			d := listWith(t, 30)
			d.keys("G")
			d.keys(c.keys...)
			if got := d.selected(); got != c.want {
				t.Errorf("after %v the selection is %s, want %s", c.keys, got, c.want)
			}
		})
	}
}

// Widths computed from the rows on screen re-flow the whole table when the
// user only moved the cursor, which is exactly the display corruption the
// resize requirement is about.
func TestScrollingDoesNotReflowTheColumns(t *testing.T) {
	issues := sampleIssues(40)
	// One row far down the list is much wider than the rest, so a layout
	// measured from the visible rows cannot help but change when it scrolls
	// into view.
	issues[35].Summary = strings.Repeat("a very long summary indeed ", 4)
	d := newDriver(t, &fakeClient{issues: issues}, testConfig(t, nil))

	before := d.lines()[1]
	d.keys("G")
	if after := d.lines()[1]; after != before {
		t.Errorf("scrolling re-laid out the header:\n before %q\n after  %q", before, after)
	}
}

// One failed page must not become a request per keypress: the selection stays
// near the end, so every further motion would ask again.
func TestAFailedPageIsNotRetriedByEveryMotion(t *testing.T) {
	client := &fakeClient{issues: sampleIssues(120)}
	d := newDriver(t, client, testConfig(t, nil))

	client.searchErr = errors.New("the site is unreachable")
	d.keys("G")
	if n := len(client.requests()); n != 2 {
		t.Fatalf("%d searches were made, want 2", n)
	}

	d.keys("k")
	d.keys("j")
	d.keys("j")
	if n := len(client.requests()); n != 2 {
		t.Errorf("%d searches were made after the page failed, want no further attempts", n)
	}
	if !strings.Contains(d.view(), "the site is unreachable") {
		t.Errorf("the failure is not in the status line:\n%s", d.view())
	}

	// R is the way back: it must be willing to ask again.
	client.searchErr = nil
	d.keys("R")
	if n := len(client.requests()); n != 3 {
		t.Errorf("reloading made %d searches in total, want a fresh attempt", n)
	}
}

// Leftover width used to go entirely to the summary, so a list of short
// summaries showed one column trailing half a screen of blank before the rest.
// Slack is shared out instead, and no column runs away from the others.
func TestSpareWidthIsSharedBetweenColumns(t *testing.T) {
	issues := sampleIssues(1)
	issues[0].Summary = "Test"
	d := newDriver(t, &fakeClient{issues: issues}, testConfig(t, nil))
	d.send(tea.WindowSizeMsg{Width: 160, Height: 22})

	head := d.lines()[1]
	summary := strings.Index(head, "Summary")
	status := strings.Index(head, "Status")
	if summary < 0 || status < 0 {
		t.Fatalf("the header is missing columns: %q", head)
	}
	if gap := status - summary; gap > 40 {
		t.Errorf("the summary column is %d wide, want it in line with the others:\n%q", gap, head)
	}
}

// One issue with a paragraph for a summary must not stretch its column across
// the pane: the cap is a share of the terminal, so the other columns keep
// their place whatever a single row happens to contain.
func TestALongSummaryDoesNotEatTheRow(t *testing.T) {
	issues := sampleIssues(2)
	issues[0].Summary = strings.Repeat("a very lengthy text description ", 12)
	issues[1].Summary = "Test"
	d := newDriver(t, &fakeClient{issues: issues}, testConfig(t, nil))
	d.send(tea.WindowSizeMsg{Width: 160, Height: 22})

	head := d.lines()[1]
	summary, status := strings.Index(head, "Summary"), strings.Index(head, "Status")
	if summary < 0 || status < 0 {
		t.Fatalf("the header is missing columns: %q", head)
	}
	if width := status - summary; width > 160/4 {
		t.Errorf("the summary column is %d wide, want no more than an equal share:\n%q", width, head)
	}
}
