package ui_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/nikhil-sharma-b/jira-tui/internal/jira"
)

// namedIssues are work items whose summaries share no words, so a filter
// keeps exactly the ones a test names.
func namedIssues() []jira.Issue {
	issues := sampleIssues(5)
	for i, summary := range []string{
		"Fix login redirect",
		"Add billing export",
		"Login page copy",
		"Remove legacy flags",
		"Billing retry backoff",
	} {
		issues[i].Summary = summary
	}
	return issues
}

func filterList(t *testing.T) (*driver, *fakeClient) {
	t.Helper()
	client := &fakeClient{issues: namedIssues()}
	return newDriver(t, client, testConfig(t, nil)), client
}

func TestFilterKeepsOnlyTheMatchingRows(t *testing.T) {
	d, _ := filterList(t)

	d.command(":filter login")

	if got := d.visibleKeys(); !slices.Equal(got, []string{"ENG-1", "ENG-3"}) {
		t.Errorf("the filtered list shows %v, want [ENG-1 ENG-3]:\n%s", got, d.view())
	}
}

// The filter matches the way / does: case is ignored, since someone typing
// it is describing what they remember seeing.
func TestFilterIgnoresCase(t *testing.T) {
	d, _ := filterList(t)

	d.command(":filter BILLING")

	if got := d.visibleKeys(); !slices.Equal(got, []string{"ENG-2", "ENG-5"}) {
		t.Errorf("the filtered list shows %v, want [ENG-2 ENG-5]", got)
	}
}

// The filter is over the rows already loaded, like /, so it is instant.
func TestFilterIssuesNoNetworkRequest(t *testing.T) {
	d, client := filterList(t)
	before := len(client.requests())

	d.command(":filter login")

	if n := len(client.requests()); n != before {
		t.Errorf("filtering a complete result set made %d searches, want none", n-before)
	}
}

func TestBareFilterShowsEveryRowAgain(t *testing.T) {
	d, _ := filterList(t)

	d.command(":filter login")
	d.command(":filter")

	if got := d.visibleKeys(); len(got) != 5 {
		t.Errorf("clearing the filter shows %v, want all five rows", got)
	}
}

func TestFilterKeepsTheSelectionWhenItStillMatches(t *testing.T) {
	d, _ := filterList(t)
	d.keys("j", "j") // ENG-3

	d.command(":filter login")

	if got := d.selected(); got != "ENG-3" {
		t.Errorf("the selection moved to %q, want it to stay on ENG-3", got)
	}
	d.command(":filter")
	if got := d.selected(); got != "ENG-3" {
		t.Errorf("clearing the filter moved the selection to %q, want ENG-3", got)
	}
}

func TestFilterSelectsTheFirstMatchWhenTheSelectionIsHidden(t *testing.T) {
	d, _ := filterList(t)
	d.keys("j", "j", "j") // ENG-4, which is not a billing item

	d.command(":filter billing")

	if got := d.selected(); got != "ENG-2" {
		t.Errorf("the selection is on %q, want the first match ENG-2", got)
	}
}

// Motions and actions work on the filtered rows, not on the rows behind them.
func TestEnterOpensTheSelectedFilteredRow(t *testing.T) {
	d, _ := filterList(t)

	d.command(":filter billing")
	d.keys("j", "enter")

	if !d.split() {
		t.Fatalf("Enter did not open the detail pane:\n%s", d.view())
	}
	if !strings.Contains(d.lines()[0], "ENG-5") {
		t.Errorf("Enter opened something other than ENG-5:\n%s", d.view())
	}
}

func TestFilterWithNoMatchSaysSo(t *testing.T) {
	d, _ := filterList(t)

	d.command(":filter nothing-like-this")

	view := d.view()
	if len(d.visibleKeys()) != 0 {
		t.Errorf("a filter that matches nothing still shows rows:\n%s", view)
	}
	if !strings.Contains(view, "nothing-like-this") {
		t.Errorf("the empty list does not name the filter that emptied it:\n%s", view)
	}
}

func TestTheStatusLineShowsTheFilter(t *testing.T) {
	d, _ := filterList(t)

	d.command(":filter login")

	line := d.statusLine()
	if !strings.Contains(line, "1/2") {
		t.Errorf("the status line %q does not count the filtered rows", line)
	}
	if !strings.Contains(line, `filter "login"`) {
		t.Errorf("the status line %q does not show the filter", line)
	}
}

// R rereads what is on screen, and the filter is part of what is on screen.
func TestReloadKeepsTheFilter(t *testing.T) {
	d, _ := filterList(t)

	d.command(":filter login")
	d.keys("R")
	d.flush()

	if got := d.visibleKeys(); !slices.Equal(got, []string{"ENG-1", "ENG-3"}) {
		t.Errorf("after R the list shows %v, want the filter kept", got)
	}
}

// A new query is a new list. A filter left over from the last one would hide
// rows for a reason the user has stopped thinking about.
func TestANewQueryClearsTheFilter(t *testing.T) {
	d, _ := filterList(t)

	d.command(":filter login")
	d.command(":jql project = ENG")

	if got := d.visibleKeys(); len(got) != 5 {
		t.Errorf("after :jql the list shows %v, want every row of the new query", got)
	}
	if strings.Contains(d.statusLine(), "filter") {
		t.Errorf("the status line %q still shows a filter", d.statusLine())
	}
}

// A filter over the first page alone would miss matches the user knows are
// there, so filtering loads the rest of the result set, up to a cap.
func TestFilterLoadsTheRestOfTheResultSet(t *testing.T) {
	issues := sampleIssues(120)
	issues[110].Summary = "Needle in the haystack"
	client := &fakeClient{issues: issues}
	d := newDriver(t, client, testConfig(t, nil))

	d.command(":filter needle")

	if got := d.visibleKeys(); !slices.Equal(got, []string{"ENG-111"}) {
		t.Errorf("the filter found %v, want ENG-111 from the third page", got)
	}
	if got := len(client.requests()); got != 3 {
		t.Errorf("filtering 120 work items made %d searches, want 3 pages", got)
	}
}

func TestFilterStopsLoadingAtTheCap(t *testing.T) {
	client := &fakeClient{issues: sampleIssues(400)}
	d := newDriver(t, client, testConfig(t, nil))

	d.command(":filter nothing-like-this")

	if got := len(client.requests()); got != 5 {
		t.Errorf("filtering 400 work items made %d searches, want 5 pages (250 rows)", got)
	}
	// Moving about a filtered list is not a sign of nearing its end.
	d.keys("G", "k", "j")
	if got := len(client.requests()); got != 5 {
		t.Errorf("motions over a filtered list made %d searches, want still 5", got)
	}
}

func TestFilterNarrowsTheBoard(t *testing.T) {
	issues := boardIssues(6)
	issues[0].Summary = "Fix login redirect"
	issues[4].Summary = "Login page copy"
	d := newDriver(t, &fakeClient{issues: issues}, boardConfig(t, nil))

	d.command(":filter login")

	view := d.view()
	for _, key := range []string{"ENG-1", "ENG-5"} {
		if !strings.Contains(view, key) {
			t.Errorf("the filtered board lost %s:\n%s", key, view)
		}
	}
	for _, key := range []string{"ENG-2", "ENG-3", "ENG-4", "ENG-6"} {
		if strings.Contains(view, key) {
			t.Errorf("the filtered board still shows %s:\n%s", key, view)
		}
	}
}

// From a pinned session the list is hidden; a filter is asked for in order to
// look at the list, so it brings the list up.
func TestFilterRevealsTheList(t *testing.T) {
	d, _ := filterList(t)
	d.keys("enter")
	d.keys("ctrl+w", "o") // zoom the detail

	d.command(":filter login")

	if got := d.visibleKeys(); !slices.Contains(got, "ENG-3") {
		t.Errorf("the filtered list is not on screen:\n%s", d.view())
	}
}
