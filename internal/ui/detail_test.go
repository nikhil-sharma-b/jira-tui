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
	for _, want := range []string{
		"ENG-1", "Repair the flux capacitor", "In Progress", "Ada Lovelace",
		"Grace Hopper", "Highest", "Bug", "time-travel, urgent",
		"2026-08-20 09:30 UTC", "2026-08-25 11:45 UTC", "## Diagnosis", "• Replace the relay",
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
	if !strings.Contains(view, "│") {
		t.Errorf("list and detail have no visible split:\n%s", view)
	}
}

func TestDetailPaneRelaysOutOnResize(t *testing.T) {
	issue := detailedIssue()
	issue.Description = jira.RawDocument(`{"type":"doc","version":1,"content":[{"type":"paragraph","content":[{"type":"text","text":"A description with enough words to wrap when the detail pane becomes narrow."}]}]}`)
	d := newDriver(t, &fakeClient{issues: []jira.Issue{issue}}, testConfig(t, nil))
	d.keys("enter")

	for _, width := range []int{100, 60, 32, 120} {
		d.send(tea.WindowSizeMsg{Width: width, Height: 20})
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
	tick := d.pending[1]
	d.pending = append(d.pending[:1], d.pending[2:]...)
	d.deliver(tick())

	if after := d.view(); after == before {
		t.Errorf("the detail spinner did not advance:\n%s", after)
	}
	d.flush()
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
