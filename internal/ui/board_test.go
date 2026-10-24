package ui_test

import (
	"errors"
	"slices"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/nikhil-sharma-b/jira-tui/internal/config"
	"github.com/nikhil-sharma-b/jira-tui/internal/jira"
	"github.com/nikhil-sharma-b/jira-tui/internal/ui"
)

// boardIssues spreads n work items over three statuses, in the query's order
// rather than the workflow's, so that the order the lanes come out in is
// observably the board's own. With six: To Do holds ENG-3 and ENG-6, In
// Progress ENG-1 and ENG-4, Done ENG-2 and ENG-5.
func boardIssues(n int) []jira.Issue {
	statuses := []jira.Status{
		{ID: "3", Name: "In Progress", Category: "indeterminate"},
		{ID: "2", Name: "Done", Category: "done"},
		{ID: "1", Name: "To Do", Category: "new"},
	}
	issues := sampleIssues(n)
	for i := range issues {
		issues[i].Status = statuses[i%len(statuses)]
	}
	return issues
}

// rankField is the id the test site gives its Rank field.
const rankField = "customfield_10019"

// ranked gives work items Jira ranks, in the order they are listed: the first
// is ranked highest.
func ranked(issues []jira.Issue, keys ...string) {
	for r, key := range keys {
		for i := range issues {
			if issues[i].Key == key {
				if issues[i].Raw == nil {
					issues[i].Raw = map[string]any{}
				}
				issues[i].Raw[rankField] = "0|i0000" + string(rune('a'+r)) + ":"
			}
		}
	}
}

// engBoard is a kanban board for ENG whose columns are not the order the
// status categories would put them in, with In Review folded into In
// Progress and an empty column at the end.
func engBoard() jira.Board {
	return jira.Board{ID: 92, Name: "ENG board", Type: "kanban", Columns: []jira.BoardColumn{
		{Name: "Done", Statuses: []string{"2"}},
		{Name: "To Do", Statuses: []string{"1"}},
		{Name: "In Progress", Statuses: []string{"3", "4"}},
		{Name: "Archive"},
	}}
}

func boardClient(issues []jira.Issue) *fakeClient {
	return &fakeClient{
		issues: issues,
		boards: map[string][]jira.Board{"ENG": {
			{ID: 84, Name: "ENG sprints", Type: "scrum"},
			{ID: 92, Name: "ENG board", Type: "kanban"},
		}},
		boardFor: map[int]jira.Board{92: engBoard(), 7: {ID: 7, Name: "Ops", Type: "kanban", Columns: []jira.BoardColumn{
			{Name: "Doing", Statuses: []string{"3"}},
			{Name: "Done", Statuses: []string{"2"}},
		}}},
	}
}

func boardConfig(t *testing.T, columns []string) *config.Config {
	t.Helper()
	cfg := testConfig(t, columns)
	cfg.View = config.ViewBoard
	return cfg
}

func boardWith(t *testing.T, n int) (*driver, *fakeClient) {
	t.Helper()
	client := &fakeClient{issues: boardIssues(n)}
	return newDriver(t, client, boardConfig(t, nil)), client
}

// laneHeader is the row the lanes are named on, just inside the frame.
func (d *driver) laneHeader() string { return d.lines()[1] }

// assertInOrder fails unless every want appears in s, left to right.
func assertInOrder(t *testing.T, s string, want ...string) {
	t.Helper()
	at := -1
	for _, w := range want {
		i := strings.Index(s, w)
		if i < 0 {
			t.Fatalf("%q is missing from %q", w, s)
		}
		if i < at {
			t.Fatalf("%q is out of order in %q, want %q", w, s, want)
		}
		at = i
	}
}

func TestBoardDrawsALanePerStatusInWorkflowOrder(t *testing.T) {
	d, _ := boardWith(t, 6)
	if !strings.Contains(d.lines()[0], "Board") {
		t.Errorf("the pane is not titled Board:\n%s", d.view())
	}
	assertInOrder(t, d.laneHeader(), "To Do 2", "In Progress 2", "Done 2")
}

func TestBoardSelectsTheListsSelection(t *testing.T) {
	d, _ := boardWith(t, 6)
	if got := d.selected(); got != "ENG-1" {
		t.Errorf("selected %q, want the first row of the result set, ENG-1", got)
	}
	if got := d.statusLine(); !strings.Contains(got, "In Progress 1/2") {
		t.Errorf("status line %q does not say where on the board the selection is", got)
	}
}

func TestUpAndDownStayInTheLane(t *testing.T) {
	d, _ := boardWith(t, 6)
	for _, step := range []struct{ key, want string }{
		{"j", "ENG-4"},
		{"j", "ENG-4"},
		{"k", "ENG-1"},
		{"G", "ENG-4"},
	} {
		d.keys(strings.Split(step.key, "")...)
		if got := d.selected(); got != step.want {
			t.Fatalf("after %s selected %q, want %q", step.key, got, step.want)
		}
	}
	d.keys("g", "g")
	if got := d.selected(); got != "ENG-1" {
		t.Errorf("after gg selected %q, want ENG-1", got)
	}
}

func TestLaneMotionsMoveAcrossAtTheSameHeight(t *testing.T) {
	d, _ := boardWith(t, 6)
	for _, step := range []struct {
		keys []string
		want string
	}{
		{[]string{"l"}, "ENG-2"},
		{[]string{"l"}, "ENG-2"},
		{[]string{"h", "h"}, "ENG-3"},
		{[]string{"h"}, "ENG-3"},
		{[]string{"j", "l"}, "ENG-4"},
		{[]string{"k", "h"}, "ENG-3"},
		{[]string{"2", "l"}, "ENG-2"},
	} {
		d.keys(step.keys...)
		if got := d.selected(); got != step.want {
			t.Fatalf("after %s selected %q, want %q", strings.Join(step.keys, ""), got, step.want)
		}
	}
}

func TestLaneMotionsDoNothingInTheList(t *testing.T) {
	d := listWith(t, 3)
	d.keys("j", "l", "h")
	if got := d.selected(); got != "ENG-2" {
		t.Errorf("selected %q after h and l in the list, want ENG-2 still", got)
	}
}

func TestEnterOpensTheSelectedCard(t *testing.T) {
	d, client := boardWith(t, 6)
	d.keys("l", "enter")
	if !slices.Contains(client.issueRequests(), "ENG-2") {
		t.Fatalf("Enter on ENG-2 fetched %v", client.issueRequests())
	}
	if !d.split() || !strings.Contains(d.lines()[0], "ENG-2") {
		t.Errorf("ENG-2 did not open beside the board:\n%s", d.view())
	}
}

func TestALiveReadMovesTheCardToItsLane(t *testing.T) {
	client := &fakeClient{issues: boardIssues(6)}
	done := client.issues[0]
	done.Status = jira.Status{Name: "Done", Category: "done"}
	client.issueFor = map[string]jira.Issue{"ENG-1": done}
	d := newDriver(t, client, boardConfig(t, nil))

	d.keys("enter")
	d.keys("q")
	assertInOrder(t, d.laneHeader(), "To Do 2", "In Progress 1", "Done 3")
}

func TestToggleViewSwitchesBetweenListAndBoard(t *testing.T) {
	d := newDriver(t, &fakeClient{issues: boardIssues(6)}, testConfig(t, nil))
	if !strings.Contains(d.lines()[0], "Work Items") {
		t.Fatalf("the list is not the default view:\n%s", d.view())
	}
	d.keys("g", "v")
	if !strings.Contains(d.lines()[0], "Board") || !strings.Contains(d.laneHeader(), "To Do 2") {
		t.Fatalf("gv did not switch to the board:\n%s", d.view())
	}
	// The selection belongs to the result set, not the view, so it survives
	// the switch both ways.
	d.keys("l")
	d.keys("g", "v")
	if !strings.Contains(d.lines()[0], "Work Items") {
		t.Fatalf("gv did not switch back to the list:\n%s", d.view())
	}
	if got := d.selected(); got != "ENG-2" {
		t.Errorf("the list selected %q after the board selected ENG-2", got)
	}
}

func TestToggleViewRevealsAndFocusesTheList(t *testing.T) {
	d, _ := boardWith(t, 6)
	d.keys("enter", "ctrl+w", "o")
	if d.split() {
		t.Fatal("the detail pane did not zoom")
	}
	d.keys("g", "v")
	if !strings.Contains(d.lines()[0], "Work Items") {
		t.Errorf("gv from the detail pane did not bring the list up:\n%s", d.view())
	}
}

func TestViewCommand(t *testing.T) {
	d := newDriver(t, &fakeClient{issues: boardIssues(6)}, testConfig(t, nil))
	d.command(":view board")
	if !strings.Contains(d.lines()[0], "Board") {
		t.Fatalf(":view board did not show the board:\n%s", d.view())
	}
	d.command(":view list")
	if !strings.Contains(d.lines()[0], "Work Items") {
		t.Fatalf(":view list did not show the list:\n%s", d.view())
	}
	d.command(":view")
	if !strings.Contains(d.lines()[0], "Board") {
		t.Fatalf("a bare :view did not switch views:\n%s", d.view())
	}
	d.command(":view kanban")
	if got := d.statusLine(); !strings.Contains(got, `no view named "kanban"`) {
		t.Errorf("status line %q does not reject an unknown view", got)
	}
	if !strings.Contains(d.lines()[0], "Board") {
		t.Errorf("an unknown view changed the view:\n%s", d.view())
	}
}

func TestViewCommandCompletesTheViews(t *testing.T) {
	d := listWith(t, 1)
	d.keys(":")
	d.typeText("view b")
	d.keys("tab")
	if got := d.statusLine(); !strings.Contains(got, ":view board") {
		t.Errorf("Tab completed to %q, want :view board", got)
	}
}

func TestTheBoardAsksForStatusWhenNoColumnShowsIt(t *testing.T) {
	client := &fakeClient{issues: boardIssues(6)}
	d := newDriver(t, client, boardConfig(t, []string{"key", "summary"}))
	reqs := client.requests()
	if len(reqs) == 0 || strings.Join(reqs[0].Fields, ",") != "summary,status,"+rankField {
		t.Fatalf("the board searched with %v, want summary, status and the rank", reqs)
	}
	assertInOrder(t, d.laneHeader(), "To Do 2", "In Progress 2", "Done 2")
}

func TestSwitchingToTheBoardFetchesWhatItNeeds(t *testing.T) {
	client := &fakeClient{issues: boardIssues(6)}
	d := newDriver(t, client, testConfig(t, []string{"key", "summary"}))
	before := len(client.requests())
	d.keys("g", "v")
	reqs := client.requests()
	if len(reqs) != before+1 || !slices.Contains(reqs[len(reqs)-1].Fields, "status") {
		t.Fatalf("switching to the board searched %v, want one search that asks for the status", reqs[before:])
	}
	assertInOrder(t, d.laneHeader(), "To Do 2", "In Progress 2", "Done 2")
}

func TestTheListDoesNotAskForTheRank(t *testing.T) {
	client := &fakeClient{issues: boardIssues(6)}
	newDriver(t, client, testConfig(t, nil))
	if reqs := client.requests(); slices.Contains(reqs[0].Fields, rankField) {
		t.Errorf("the list searched with %v, want only its columns' fields", reqs[0].Fields)
	}
}

func TestTheBoardLoadsPastTheFirstPage(t *testing.T) {
	// A lane is only true when every work item in that status is in it, so
	// the board pages on its own where the list waits to be scrolled.
	client := &fakeClient{issues: boardIssues(120)}
	newDriver(t, client, boardConfig(t, nil))
	if got := len(client.requests()); got != 3 {
		t.Errorf("the board made %d searches for 120 work items, want all 3 pages", got)
	}

	client = &fakeClient{issues: boardIssues(120)}
	newDriver(t, client, testConfig(t, nil))
	if got := len(client.requests()); got != 1 {
		t.Errorf("the list made %d searches before being scrolled, want 1", got)
	}
}

func TestSearchWalksTheBoardInReadingOrder(t *testing.T) {
	d, _ := boardWith(t, 6)
	d.keys("/")
	d.typeText("number")
	d.keys("enter")
	// The list's order would be ENG-1, ENG-2, ENG-3; the board reads down
	// a lane before moving on to the next, and wraps round to the first.
	for _, want := range []string{"ENG-1", "ENG-4", "ENG-2", "ENG-5", "ENG-3"} {
		if got := d.selected(); got != want {
			t.Fatalf("search selected %q, want %q", got, want)
		}
		d.keys("n")
	}
	d.keys("N", "N")
	if got := d.selected(); got != "ENG-5" {
		t.Errorf("N selected %q, want ENG-5", got)
	}
}

func TestALongLaneScrollsToKeepTheSelectionInView(t *testing.T) {
	d := newDriver(t, &fakeClient{issues: sampleIssues(30)}, boardConfig(t, nil))
	d.keys("G")
	if got := d.selected(); got != "ENG-30" {
		t.Fatalf("G selected %q, want ENG-30", got)
	}
	keys := d.visibleKeys()
	if slices.Contains(keys, "ENG-1") || !slices.Contains(keys, "ENG-30") {
		t.Errorf("the lane did not scroll to its end, showing %v", keys)
	}
	if !strings.Contains(d.laneHeader(), "↑") {
		t.Errorf("lane header %q does not say there are cards above", d.laneHeader())
	}
	d.keys("g", "g")
	if keys := d.visibleKeys(); !slices.Contains(keys, "ENG-1") {
		t.Errorf("gg did not scroll back to the top, showing %v", keys)
	}
}

func TestLanesThatDoNotFitScrollSideways(t *testing.T) {
	d, _ := boardWith(t, 6)
	d.send(tea.WindowSizeMsg{Width: 60, Height: 22})
	header := d.laneHeader()
	if !strings.Contains(header, "To Do") || !strings.Contains(header, "In Progress") ||
		strings.Contains(header, "Done") || !strings.Contains(header, "›") {
		t.Fatalf("at 60 columns the header is %q, want To Do and In Progress with more to the right", header)
	}
	d.keys("l")
	header = d.laneHeader()
	if !strings.Contains(header, "Done") || strings.Contains(header, "To Do") || !strings.Contains(header, "‹") {
		t.Errorf("moving to Done left the header at %q, want it scrolled into view", header)
	}
}

func TestCardsShowOnlyTheConfiguredColumns(t *testing.T) {
	d := newDriver(t, &fakeClient{issues: boardIssues(3)}, boardConfig(t, nil))
	if !strings.Contains(d.view(), "Ada Lovelace · High") {
		t.Errorf("a card does not show the other columns:\n%s", d.view())
	}

	d = newDriver(t, &fakeClient{issues: boardIssues(3)}, boardConfig(t, []string{"key", "summary"}))
	if strings.Contains(d.view(), "Ada Lovelace") {
		t.Errorf("a card shows an assignee no column asked for:\n%s", d.view())
	}
	if !strings.Contains(d.view(), "Work item number 1") {
		t.Errorf("a card does not show its summary:\n%s", d.view())
	}
}

func TestALongSummaryIsCutAtTwoLines(t *testing.T) {
	issues := boardIssues(1)
	issues[0].Summary = strings.Repeat("word ", 40) + "TAIL"
	d := newDriver(t, &fakeClient{issues: issues}, boardConfig(t, nil))
	view := d.view()
	if strings.Contains(view, "TAIL") || !strings.Contains(view, "…") {
		t.Errorf("a long summary was not cut short with an ellipsis:\n%s", view)
	}
}

func TestCardsAreInJirasRankOrder(t *testing.T) {
	issues := boardIssues(9)
	// In Progress holds ENG-1, ENG-4 and ENG-7, in that order by the query.
	ranked(issues, "ENG-7", "ENG-1", "ENG-4")
	d := newDriver(t, &fakeClient{issues: issues}, boardConfig(t, nil))
	assertInOrder(t, d.view(), "ENG-7", "ENG-1", "ENG-4")
	d.keys("g", "g")
	if got := d.selected(); got != "ENG-7" {
		t.Errorf("gg selected %q, want the highest ranked card, ENG-7", got)
	}
}

func TestUnrankedCardsGoAfterRankedOnes(t *testing.T) {
	issues := boardIssues(9)
	ranked(issues, "ENG-7")
	d := newDriver(t, &fakeClient{issues: issues}, boardConfig(t, nil))
	assertInOrder(t, d.view(), "ENG-7", "ENG-1", "ENG-4")
}

func TestTheBoardFollowsTheProjectsJiraBoard(t *testing.T) {
	issues := boardIssues(6)
	issues[3].Status = jira.Status{ID: "4", Name: "In Review", Category: "indeterminate"}
	client := boardClient(issues)
	d := newDriver(t, client, boardConfig(t, nil))

	// The kanban board, not the scrum one listed before it.
	if projects, ids := client.boardRequests(); !slices.Equal(projects, []string{"ENG"}) || !slices.Equal(ids, []int{92}) {
		t.Fatalf("looked up boards for %v and read %v, want ENG's kanban board, 92", projects, ids)
	}
	if !strings.Contains(d.lines()[0], "ENG board") {
		t.Errorf("the pane is not named for the board it follows:\n%s", d.view())
	}
	// The board's own order, In Review in the column it is mapped to, and the
	// empty column drawn.
	d.send(tea.WindowSizeMsg{Width: 130, Height: 22})
	assertInOrder(t, d.laneHeader(), "Done 2", "To Do 2", "In Progress 2", "Archive 0")
	if strings.Contains(d.laneHeader(), "In Review") {
		t.Errorf("In Review has a lane of its own: %q", d.laneHeader())
	}
}

func TestLaneMotionsPassOverEmptyColumns(t *testing.T) {
	client := boardClient(boardIssues(6))
	board := engBoard()
	board.Columns = []jira.BoardColumn{
		{Name: "To Do", Statuses: []string{"1"}},
		{Name: "Blocked"},
		{Name: "In Progress", Statuses: []string{"3"}},
		{Name: "Done", Statuses: []string{"2"}},
	}
	client.boardFor[92] = board
	d := newDriver(t, client, boardConfig(t, nil))
	d.send(tea.WindowSizeMsg{Width: 130, Height: 22})
	assertInOrder(t, d.laneHeader(), "To Do 2", "Blocked 0", "In Progress 2", "Done 2")

	d.keys("h")
	if got := d.selected(); got != "ENG-3" {
		t.Fatalf("h selected %q, want ENG-3 in To Do", got)
	}
	d.keys("l")
	if got := d.selected(); got != "ENG-1" {
		t.Errorf("l from To Do selected %q, want ENG-1 in In Progress, past the empty column", got)
	}
}

func TestStatusesTheBoardDoesNotMapGetLanesOfTheirOwn(t *testing.T) {
	issues := boardIssues(6)
	issues[5].Status = jira.Status{ID: "9", Name: "Parked", Category: "new"}
	d := newDriver(t, boardClient(issues), boardConfig(t, nil))
	d.send(tea.WindowSizeMsg{Width: 160, Height: 22})
	assertInOrder(t, d.laneHeader(), "Done 2", "To Do 1", "In Progress 2", "Archive 0", "Parked 1")
}

func TestAConfiguredBoardIsFollowed(t *testing.T) {
	client := boardClient(boardIssues(6))
	cfg := boardConfig(t, nil)
	cfg.Board = 7
	d := newDriver(t, client, cfg)
	if projects, ids := client.boardRequests(); len(projects) != 0 || !slices.Equal(ids, []int{7}) {
		t.Fatalf("looked up boards for %v and read %v, want board 7 read and nothing looked up", projects, ids)
	}
	assertInOrder(t, d.laneHeader(), "Doing 2", "Done 2", "To Do 2")
}

func TestResultsFromSeveralProjectsAreGroupedByStatus(t *testing.T) {
	issues := boardIssues(6)
	issues[1].Key = "OPS-2"
	client := boardClient(issues)
	d := newDriver(t, client, boardConfig(t, nil))
	if projects, ids := client.boardRequests(); len(projects)+len(ids) != 0 {
		t.Errorf("looked up boards for %v and read %v, want nothing: no one board shows two projects", projects, ids)
	}
	assertInOrder(t, d.laneHeader(), "To Do 2", "In Progress 2", "Done 2")
}

func TestAFailedBoardLookupIsReportedAndTheLanesStayUp(t *testing.T) {
	client := boardClient(boardIssues(6))
	client.boardsErr = errors.New("boards: 403 Forbidden")
	d := newDriver(t, client, boardConfig(t, nil))
	if got := d.statusLine(); !strings.Contains(got, "403") {
		t.Errorf("status line %q does not report the failed lookup", got)
	}
	assertInOrder(t, d.laneHeader(), "To Do 2", "In Progress 2", "Done 2")
}

func TestTheListDoesNotLookUpBoards(t *testing.T) {
	client := boardClient(boardIssues(6))
	newDriver(t, client, testConfig(t, nil))
	if projects, ids := client.boardRequests(); len(projects)+len(ids) != 0 {
		t.Errorf("the list looked up boards for %v and read %v", projects, ids)
	}
}

func TestReloadRereadsTheBoard(t *testing.T) {
	client := boardClient(boardIssues(6))
	d := newDriver(t, client, boardConfig(t, nil))
	d.keys("R")
	if _, ids := client.boardRequests(); !slices.Equal(ids, []int{92, 92}) {
		t.Errorf("R read boards %v, want board 92 read again", ids)
	}
}

func TestTheBoardsLayoutIsCachedAcrossSessions(t *testing.T) {
	c := openCache(t)
	cfg := boardConfig(t, nil)
	first := newPausedDriver(t, ui.Options{Client: boardClient(boardIssues(6)), Config: cfg, Cache: c})
	first.flush()

	client := boardClient(boardIssues(6))
	d := newPausedDriver(t, ui.Options{Client: client, Config: cfg, Cache: c})
	d.flush()
	if projects, ids := client.boardRequests(); len(projects)+len(ids) != 0 {
		t.Errorf("a second session looked up boards for %v and read %v, want the cached layout", projects, ids)
	}
	if !strings.Contains(d.lines()[0], "ENG board") {
		t.Errorf("the second session does not follow the cached board:\n%s", d.view())
	}
}
