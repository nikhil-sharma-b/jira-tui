package ui_test

import (
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/nikhil-sharma-b/jira-tui/internal/jira"
)

func detailedIssue() jira.Issue {
	return jira.Issue{
		Key:      "ENG-1",
		Summary:  "Repair the flux capacitor",
		Status:   jira.Status{Name: "In Progress"},
		Assignee: &jira.User{DisplayName: "Ada Lovelace"},
		Reporter: &jira.User{DisplayName: "Grace Hopper"},
		Priority: &jira.Priority{Name: "Highest"},
		Type:     "Bug",
		Labels:   []string{"time-travel", "urgent"},
		Created:  time.Date(2026, 8, 20, 9, 30, 0, 0, time.UTC),
		Updated:  time.Date(2026, 8, 25, 11, 45, 0, 0, time.UTC),
		Description: jira.RawDocument(`{"type":"doc","version":1,"content":[` +
			`{"type":"heading","attrs":{"level":2},"content":[{"type":"text","text":"Diagnosis"}]},` +
			`{"type":"bulletList","content":[{"type":"listItem","content":[{"type":"paragraph","content":[{"type":"text","text":"Replace the relay"}]}]}]}` +
			`]}`),
	}
}

func TestOpeningTheSameIssueTwiceFetchesItTwice(t *testing.T) {
	issue := detailedIssue()
	client := &fakeClient{issues: []jira.Issue{issue}}
	d := newDriver(t, client, testConfig(t, nil))

	d.keys("enter")
	d.keys("q")
	d.keys("enter")

	if got := client.issueRequests(); len(got) != 2 || got[0] != "ENG-1" || got[1] != "ENG-1" {
		t.Errorf("detail requests = %v, want two live requests for ENG-1", got)
	}
	if got := client.commentRequests(); len(got) != 2 || got[0] != "ENG-1" || got[1] != "ENG-1" {
		t.Errorf("comment requests = %v, want two live requests for ENG-1", got)
	}
}

func TestCommentsRenderAfterDescriptionWithMetadataAndADF(t *testing.T) {
	issue := detailedIssue()
	created := time.Date(2026, 8, 20, 10, 0, 0, 0, time.UTC)
	client := &fakeClient{
		issues: []jira.Issue{issue},
		comments: map[string][]jira.Comment{"ENG-1": {
			{
				ID: "1", Author: &jira.User{DisplayName: "Ada Lovelace", Active: false},
				Created: created, Updated: created.Add(2 * time.Hour),
				Body: jira.RawDocument(`{"type":"doc","version":1,"content":[{"type":"bulletList","content":[{"type":"listItem","content":[{"type":"paragraph","content":[{"type":"text","text":"Replace the relay"}]}]}]}]}`),
			},
			{
				ID: "2", Author: nil,
				Created: created.Add(time.Hour), Updated: created.Add(time.Hour),
				Body: jira.RawDocument(`{"type":"doc","version":1,"content":[{"type":"paragraph","content":[{"type":"text","text":"Second comment"}]}]}`),
			},
		}},
	}
	d := newDriver(t, client, testConfig(t, nil))
	d.send(tea.WindowSizeMsg{Width: 100, Height: 40})
	d.keys("enter", "]", "]")

	view := d.view()
	for _, want := range []string{
		"Comments", "Ada Lovelace", "2026-08-20 10:00 UTC", "• Replace the relay",
		"Edited: 2026-08-20 12:00 UTC", "Unknown author", "Second comment",
	} {
		if !strings.Contains(view, want) {
			t.Errorf("comments do not show %q:\n%s", want, view)
		}
	}
	if strings.Contains(view, "Description") {
		t.Errorf("the Comments tab is showing the description too:\n%s", view)
	}
	if first, second := strings.Index(view, "Replace the relay"), strings.Index(view, "Second comment"); first < 0 || second <= first {
		t.Errorf("comments are not oldest first:\n%s", view)
	}
}

func TestNoCommentsIsExplicit(t *testing.T) {
	issue := detailedIssue()
	d := newDriver(t, &fakeClient{issues: []jira.Issue{issue}}, testConfig(t, nil))
	d.keys("enter", "]", "]")

	if !strings.Contains(d.view(), "No comments.") {
		t.Errorf("empty comments have no explanation:\n%s", d.view())
	}
}

func TestCommentFailureKeepsTheLoadedIssueReadable(t *testing.T) {
	issue := detailedIssue()
	client := &fakeClient{
		issues:        []jira.Issue{issue},
		commentErrFor: map[string]error{"ENG-1": errors.New("comments endpoint failed")},
	}
	d := newDriver(t, client, testConfig(t, nil))
	d.keys("enter")
	info := d.view()
	for _, want := range []string{"Repair the flux capacitor", "Diagnosis"} {
		if !strings.Contains(info, want) {
			t.Errorf("comment failure lost the loaded issue: no %q:\n%s", want, info)
		}
	}

	d.keys("]", "]")
	view := d.view()
	for _, want := range []string{"Comments could not be loaded.", "comments endpoint failed"} {
		if !strings.Contains(view, want) {
			t.Errorf("comment failure view does not show %q:\n%s", want, view)
		}
	}
	lines := d.lines()
	if !strings.Contains(lines[len(lines)-1], "comments endpoint failed") {
		t.Errorf("the comment failure is not in the status line:\n%s", view)
	}

	d.keys("q")
	client.mu.Lock()
	delete(client.commentErrFor, "ENG-1")
	client.mu.Unlock()
	d.keys("enter")
	if strings.Contains(d.view(), "comments endpoint failed") {
		t.Errorf("a successful reopen left the old comment failure in the status line:\n%s", d.view())
	}
}

func TestLongCommentsScrollInTheDetailViewport(t *testing.T) {
	issue := detailedIssue()
	comments := make([]jira.Comment, 24)
	for i := range comments {
		comments[i] = jira.Comment{
			ID:      string(rune('A' + i)),
			Author:  &jira.User{DisplayName: "Commenter"},
			Created: time.Date(2026, 8, 20, 10, i, 0, 0, time.UTC),
			Updated: time.Date(2026, 8, 20, 10, i, 0, 0, time.UTC),
			Body:    jira.RawDocument(`{"type":"doc","version":1,"content":[{"type":"paragraph","content":[{"type":"text","text":"Comment body ` + string(rune('A'+i)) + `"}]}]}`),
		}
	}
	d := newDriver(t, &fakeClient{issues: []jira.Issue{issue}, comments: map[string][]jira.Comment{"ENG-1": comments}}, testConfig(t, nil))
	d.send(tea.WindowSizeMsg{Width: 80, Height: 12})
	d.keys("enter", "]", "]")

	if strings.Contains(d.view(), "Comment body X") {
		t.Fatalf("the Comments tab started at the end:\n%s", d.view())
	}
	d.keys("G")
	if !strings.Contains(d.view(), "Comment body X") {
		t.Errorf("G did not reach the end of the comments:\n%s", d.view())
	}
}

func TestCommentLayoutRewrapsAfterPaneAndTerminalResizes(t *testing.T) {
	issue := detailedIssue()
	comment := jira.Comment{
		Author:  &jira.User{DisplayName: "Ada Lovelace"},
		Created: time.Date(2026, 8, 20, 10, 0, 0, 0, time.UTC),
		Updated: time.Date(2026, 8, 20, 10, 0, 0, 0, time.UTC),
		Body:    jira.RawDocument(`{"type":"doc","version":1,"content":[{"type":"paragraph","content":[{"type":"text","text":"A comment with enough words to wrap differently in split and zoomed detail layouts."}]}]}`),
	}
	d := newDriver(t, &fakeClient{issues: []jira.Issue{issue}, comments: map[string][]jira.Comment{"ENG-1": {comment}}}, testConfig(t, nil))
	d.keys("enter")

	for _, keys := range [][]string{{"]", "]"}, {"ctrl+w", "o"}, {"ctrl+w", "o"}} {
		d.keys(keys...)
		if !strings.Contains(d.view(), "Comments") || !strings.Contains(d.view(), "A comment with enough words") {
			t.Errorf("a layout change lost the comments section:\n%s", d.view())
		}
	}
	for _, width := range []int{60, 100, 42} {
		d.send(tea.WindowSizeMsg{Width: width, Height: 22})
		for _, line := range d.lines() {
			if got := ansi.StringWidth(line); got > width {
				t.Errorf("at width %d a line is %d columns wide: %q", width, got, line)
			}
		}
	}
}

func TestMovingSelectionCancelsCommentRequestAtJira(t *testing.T) {
	started := make(chan string, 1)
	client := &fakeClient{issues: sampleIssues(2), commentBlock: true, commentStart: started}
	d := newDriver(t, client, testConfig(t, nil))
	d.send(keyMsg("enter"))
	d.step()

	commentCmd := d.pending[1]
	d.pending = append(d.pending[:1], d.pending[2:]...)
	result := make(chan tea.Msg, 1)
	go func() { result <- commentCmd() }()
	if key := <-started; key != "ENG-1" {
		t.Fatalf("Jira started fetching comments for %s, want ENG-1", key)
	}
	d.keys("g", "l", "j")

	select {
	case msg := <-result:
		d.deliver(msg)
	case <-time.After(time.Second):
		t.Fatal("moving the selection did not cancel comments at the Jira seam")
	}
	if strings.Contains(d.view(), "context canceled") {
		t.Errorf("the cancelled comment response replaced the selection-moved state:\n%s", d.view())
	}
}

func TestOldCommentResponseCannotReplaceReopenedDetail(t *testing.T) {
	started := make(chan string, 1)
	issue := detailedIssue()
	client := &fakeClient{issues: []jira.Issue{issue}, commentBlock: true, commentStart: started}
	d := newDriver(t, client, testConfig(t, nil))
	d.send(keyMsg("enter"))
	d.step()

	oldCommentCmd := d.pending[1]
	d.pending = append(d.pending[:1], d.pending[2:]...)
	oldResult := make(chan tea.Msg, 1)
	go func() { oldResult <- oldCommentCmd() }()
	<-started

	d.keys("q")
	client.mu.Lock()
	client.commentBlock = false
	client.commentStart = nil
	client.comments = map[string][]jira.Comment{"ENG-1": {{
		Author: &jira.User{DisplayName: "Fresh commenter"}, Created: time.Now(), Updated: time.Now(),
		Body: jira.RawDocument(`{"type":"doc","version":1,"content":[{"type":"paragraph","content":[{"type":"text","text":"Fresh comment"}]}]}`),
	}}}
	client.mu.Unlock()
	d.keys("enter", "]", "]")
	if !strings.Contains(d.view(), "Fresh comment") {
		t.Fatalf("the reopened pane did not render fresh comments:\n%s", d.view())
	}

	d.deliver(<-oldResult)
	if !strings.Contains(d.view(), "Fresh comment") || strings.Contains(d.view(), "context canceled") {
		t.Errorf("the old comment response replaced fresh comments:\n%s", d.view())
	}
}

func TestEmptyDescriptionIsExplicit(t *testing.T) {
	issue := detailedIssue()
	issue.Description = nil
	d := newDriver(t, &fakeClient{issues: []jira.Issue{issue}}, testConfig(t, nil))

	d.keys("enter")

	if !strings.Contains(d.view(), "No description provided.") {
		t.Errorf("empty description has no explanation:\n%s", d.view())
	}
}

func TestLongDetailScrollsWithVimMotionWithoutMovingTheList(t *testing.T) {
	issue := detailedIssue()
	var paragraphs []string
	for i := 1; i <= 26; i++ {
		paragraphs = append(paragraphs, `{"type":"paragraph","content":[{"type":"text","text":"Detail line `+string(rune('A'+i-1))+`"}]}`)
	}
	issue.Description = jira.RawDocument(`{"type":"doc","version":1,"content":[` + strings.Join(paragraphs, ",") + `]}`)
	d := newDriver(t, &fakeClient{issues: []jira.Issue{issue}}, testConfig(t, nil))
	d.send(tea.WindowSizeMsg{Width: 80, Height: 12})
	d.keys("enter")

	if strings.Contains(d.view(), "Detail line Z") {
		t.Fatalf("the long description did not start at the top:\n%s", d.view())
	}
	d.keys("G")

	if !strings.Contains(d.view(), "Detail line Z") {
		t.Errorf("G did not scroll to the end of the detail:\n%s", d.view())
	}
	if got := d.selected(); got != "ENG-1" {
		t.Errorf("detail scrolling moved the list selection to %s", got)
	}
}

func TestDetailFailuresAreExplicitAndKeepTheUIUsable(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want string
	}{
		{"missing", &jira.Error{StatusCode: http.StatusNotFound, Messages: []string{"Issue does not exist"}, Op: "issue"}, "does not exist"},
		{"forbidden", &jira.Error{StatusCode: http.StatusForbidden, Messages: []string{"Forbidden"}, Op: "issue"}, "not permitted"},
		{"other failure", errors.New("the connection broke"), "the connection broke"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			issue := detailedIssue()
			client := &fakeClient{issues: []jira.Issue{issue}, issueErrFor: map[string]error{"ENG-1": tt.err}}
			d := newDriver(t, client, testConfig(t, nil))

			d.keys("enter")

			if !strings.Contains(strings.ToLower(d.view()), strings.ToLower(tt.want)) {
				t.Errorf("detail failure does not distinguish %q:\n%s", tt.want, d.view())
			}
			d.keys("q")
			if strings.Contains(d.view(), "Detail could not be loaded") {
				t.Errorf("q did not close the failed detail pane:\n%s", d.view())
			}
		})
	}
}

func TestEnterOpensLiveDetailBesideTheList(t *testing.T) {
	issue := detailedIssue()
	client := &fakeClient{issues: []jira.Issue{issue}}
	d := newDriver(t, client, testConfig(t, nil))

	d.keys("enter")

	view := d.view()
	for _, want := range []string{"ENG-1", "Repair the flux capacitor", "Diagnosis", "• Replace the relay"} {
		if !strings.Contains(view, want) {
			t.Errorf("the Info tab does not show %q:\n%s", want, view)
		}
	}

	d.keys("]")
	view = d.view()
	for _, want := range []string{
		"ENG-1", "In Progress", "Ada Lovelace",
		"Grace Hopper", "Highest", "Bug", "time-travel, urgent",
		"2026-08-20 09:30 UTC", "2026-08-25 11:45 UTC",
	} {
		if !strings.Contains(view, want) {
			t.Errorf("detail does not show %q:\n%s", want, view)
		}
	}
	if got := client.issueRequests(); len(got) != 1 || got[0] != "ENG-1" {
		t.Errorf("detail requests = %v, want [ENG-1]", got)
	}
	for _, field := range []string{"summary", "status", "assignee", "reporter", "priority", "issuetype", "labels", "created", "updated", "description"} {
		if !contains(client.requestedIssueFields(), field) {
			t.Errorf("detail request omitted %q: %v", field, client.requestedIssueFields())
		}
	}
	if !d.split() {
		t.Errorf("list and detail have no visible split:\n%s", view)
	}
}

func TestDetailPaneRelaysOutOnResize(t *testing.T) {
	issue := detailedIssue()
	issue.Description = jira.RawDocument(`{"type":"doc","version":1,"content":[{"type":"paragraph","content":[{"type":"text","text":"A description with enough words to wrap when the detail pane becomes narrow."}]}]}`)
	d := newDriver(t, &fakeClient{issues: []jira.Issue{issue}}, testConfig(t, nil))
	d.keys("enter")

	for _, width := range []int{100, 60, 32, 120} {
		d.send(tea.WindowSizeMsg{Width: width, Height: 22})
		for _, line := range d.lines() {
			if got := ansi.StringWidth(line); got > width {
				t.Errorf("at width %d a line is %d columns wide: %q", width, got, line)
			}
		}
		if !strings.Contains(d.view(), "ENG-1") {
			t.Errorf("resize to %d lost the opened issue:\n%s", width, d.view())
		}
	}
}

func TestDetailLoadsInItsPaneWhileTheListRemainsNavigable(t *testing.T) {
	issues := sampleIssues(2)
	client := &fakeClient{issues: issues}
	d := newDriver(t, client, testConfig(t, nil))

	// Keep the detail command queued to observe the same state as a request on
	// the wire. The spinner belongs to the right pane, not the global footer.
	d.send(keyMsg("enter"))
	if got := strings.Count(strings.ToLower(d.view()), "loading"); got != 1 {
		t.Fatalf("loading appears %d times, want once in the detail pane:\n%s", got, d.view())
	}
	d.send(keyMsg("j"))

	if got := d.selected(); got != "ENG-2" {
		t.Errorf("selection = %s, want ENG-2 while detail is in flight", got)
	}
	if !strings.Contains(strings.ToLower(d.view()), "selection moved") {
		t.Errorf("cancelled detail fetch has no explicit state:\n%s", d.view())
	}
	d.flush()
	if strings.Contains(d.view(), "Key: ENG-1") {
		t.Errorf("the cancelled response rendered into the new selection:\n%s", d.view())
	}
}

func TestDetailLoadingIndicatorAnimates(t *testing.T) {
	client := &fakeClient{issues: sampleIssues(1)}
	d := newDriver(t, client, testConfig(t, nil))
	d.send(keyMsg("enter"))
	before := d.view()

	// Unwrap the batch, then run its timer while leaving the Jira request
	// queued. This observes animation during the in-flight state.
	d.step()
	tick := d.pending[2]
	d.pending = d.pending[:2]
	d.deliver(tick())

	if after := d.view(); after == before {
		t.Errorf("the detail spinner did not advance:\n%s", after)
	}
	d.flush()
}

func TestCommentLoadingIndicatorAnimatesWithoutHidingDetail(t *testing.T) {
	started := make(chan string, 1)
	client := &fakeClient{issues: []jira.Issue{detailedIssue()}, commentBlock: true, commentStart: started}
	d := newDriver(t, client, testConfig(t, nil))
	d.send(keyMsg("enter"))
	d.step()

	issueCmd, commentCmd, tick := d.pending[0], d.pending[1], d.pending[2]
	d.pending = nil
	d.deliver(issueCmd())
	result := make(chan tea.Msg, 1)
	go func() { result <- commentCmd() }()
	<-started
	if info := d.view(); !strings.Contains(info, "Repair the flux capacitor") {
		t.Fatalf("comment loading hid the loaded issue:\n%s", info)
	}
	d.keys("]", "]")
	before := d.view()
	if !strings.Contains(before, "Loading comments") {
		t.Fatalf("the Comments tab does not say it is loading:\n%s", before)
	}
	d.deliver(tick())
	if after := d.view(); after == before {
		t.Errorf("the comment loading spinner did not advance:\n%s", after)
	}

	d.keys("q")
	d.deliver(<-result)
}

func TestMovingTheSelectionCancelsARequestAlreadyAtJira(t *testing.T) {
	started := make(chan string, 1)
	client := &fakeClient{issues: sampleIssues(2), issueBlock: true, issueStart: started}
	d := newDriver(t, client, testConfig(t, nil))
	d.send(keyMsg("enter"))
	d.step()

	cmd := d.pending[0]
	d.pending = d.pending[1:]
	result := make(chan tea.Msg, 1)
	go func() { result <- cmd() }()
	if key := <-started; key != "ENG-1" {
		t.Fatalf("Jira started fetching %s, want ENG-1", key)
	}

	d.send(keyMsg("j"))
	select {
	case msg := <-result:
		d.deliver(msg)
	case <-time.After(time.Second):
		t.Fatal("moving the selection did not cancel the request at the Jira seam")
	}
	if got := d.selected(); got != "ENG-2" {
		t.Errorf("selection = %s, want ENG-2", got)
	}
}

func TestClosingAndReopeningCannotLetTheOldRequestReplaceTheNewDetail(t *testing.T) {
	started := make(chan string, 1)
	issue := detailedIssue()
	client := &fakeClient{issues: []jira.Issue{issue}, issueBlock: true, issueStart: started}
	d := newDriver(t, client, testConfig(t, nil))
	d.send(keyMsg("enter"))
	d.step()

	oldCmd := d.pending[0]
	d.pending = d.pending[1:]
	oldResult := make(chan tea.Msg, 1)
	go func() { oldResult <- oldCmd() }()
	<-started

	d.send(keyMsg("q"))
	client.mu.Lock()
	client.issueBlock = false
	client.issueStart = nil
	client.issues[0].Summary = "Fresh detail"
	client.mu.Unlock()
	d.send(keyMsg("enter"))
	d.stepUntil("Fresh detail")
	if !strings.Contains(d.view(), "Fresh detail") {
		t.Fatalf("the reopened pane did not render the new response:\n%s", d.view())
	}

	d.deliver(<-oldResult)
	if !strings.Contains(d.view(), "Fresh detail") {
		t.Errorf("the old cancelled response replaced the reopened detail:\n%s", d.view())
	}
}

// relatedIssue is a work item with something in every tab, so that a test can
// assert on which of them is on screen rather than on whether data arrived.
func relatedIssue() jira.Issue {
	issue := detailedIssue()
	issue.Attachments = []jira.Attachment{{
		ID: "1", Filename: "trace.log", MimeType: "text/plain", Size: 20480,
		Author:  &jira.User{DisplayName: "Ada Lovelace"},
		Created: time.Date(2026, 8, 20, 9, 30, 0, 0, time.UTC),
	}}
	issue.Links = []jira.IssueLink{{
		Relation: "is blocked by", Key: "ENG-9", Summary: "Source a replacement relay",
		Status: jira.Status{Name: "To Do"}, Type: "Bug",
	}}
	issue.Subtasks = []jira.Subtask{{
		Key: "ENG-4", Summary: "Order the capacitor", Status: jira.Status{Name: "Done"}, Type: "Sub-task",
	}}
	return issue
}

func TestBracketsCycleTabsAndEachTabShowsOnlyItsOwnContent(t *testing.T) {
	d := newDriver(t, &fakeClient{issues: []jira.Issue{relatedIssue()}}, testConfig(t, nil))
	d.send(tea.WindowSizeMsg{Width: 110, Height: 30})
	d.keys("enter")

	tests := []struct {
		name  string
		keys  []string
		want  []string
		avoid []string
	}{
		{name: "Info", want: []string{"Repair the flux capacitor", "Diagnosis"}, avoid: []string{"Reporter:", "trace.log"}},
		{name: "Details", keys: []string{"]"}, want: []string{"Key: ENG-1", "Reporter: Grace Hopper", "Labels: time-travel, urgent"}, avoid: []string{"Diagnosis"}},
		{name: "Comments", keys: []string{"]"}, want: []string{"No comments."}, avoid: []string{"Diagnosis", "trace.log"}},
		{name: "Attachments", keys: []string{"]"}, want: []string{"trace.log", "20.0 kB", "text/plain", "Ada Lovelace"}, avoid: []string{"Diagnosis"}},
		{name: "Links", keys: []string{"]"}, want: []string{"is blocked by", "ENG-9", "Source a replacement relay", "To Do"}, avoid: []string{"Order the capacitor"}},
		{name: "Subtasks", keys: []string{"]"}, want: []string{"ENG-4", "Order the capacitor", "Done"}, avoid: []string{"Source a replacement relay"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			d.keys(test.keys...)
			view := d.view()
			// The tab strip names every tab, so the assertions are made
			// against the pane's body rather than against the whole screen.
			body := strings.Join(d.lines()[3:], "\n")
			for _, want := range test.want {
				if !strings.Contains(body, want) {
					t.Errorf("the %s tab does not show %q:\n%s", test.name, want, view)
				}
			}
			for _, avoid := range test.avoid {
				if strings.Contains(body, avoid) {
					t.Errorf("the %s tab is also showing %q:\n%s", test.name, avoid, view)
				}
			}
			if !strings.Contains(view, test.name) {
				t.Errorf("the tab strip does not name %s:\n%s", test.name, view)
			}
		})
	}

	d.keys("]")
	if !strings.Contains(strings.Join(d.lines()[3:], "\n"), "Diagnosis") {
		t.Errorf("] did not wrap from Subtasks to Info:\n%s", d.view())
	}
	d.keys("[")
	if !strings.Contains(strings.Join(d.lines()[3:], "\n"), "Order the capacitor") {
		t.Errorf("[ did not wrap from Info to Subtasks:\n%s", d.view())
	}
}

func TestEmptyTabsSayWhatIsMissing(t *testing.T) {
	d := newDriver(t, &fakeClient{issues: []jira.Issue{detailedIssue()}}, testConfig(t, nil))
	d.keys("enter")

	for _, test := range []struct {
		keys []string
		want string
	}{
		{keys: []string{"]", "]", "]"}, want: "No attachments."},
		{keys: []string{"]"}, want: "No links."},
		{keys: []string{"]"}, want: "No subtasks."},
	} {
		d.keys(test.keys...)
		if !strings.Contains(d.view(), test.want) {
			t.Errorf("an empty tab does not say %q:\n%s", test.want, d.view())
		}
	}
}

func TestATabRemembersWhereItWasLeft(t *testing.T) {
	issue := detailedIssue()
	issue.Description = longDescription()
	d := newDriver(t, &fakeClient{issues: []jira.Issue{issue}}, testConfig(t, nil))
	d.send(tea.WindowSizeMsg{Width: 90, Height: 16})
	d.keys("enter", "G")
	end := d.view()

	d.keys("]", "[")
	if got := d.view(); got != end {
		t.Errorf("returning to the Info tab did not return to where it was left:\n%s\nwant:\n%s", got, end)
	}
}
