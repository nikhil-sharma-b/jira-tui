package ui_test

import (
	"strings"
	"testing"
)

// search types a pattern at / and submits it.
func search(d *driver, pattern string) {
	d.t.Helper()
	d.keys("/")
	d.typeText(pattern)
	d.keys("enter")
}

func TestSlashMovesToTheFirstMatch(t *testing.T) {
	d := listWith(t, 5)

	search(d, "number 3")

	if got := d.selected(); got != "ENG-3" {
		t.Errorf("the selection is on %q, want ENG-3", got)
	}
}

// Highlighting is styling, and styling is what a test cannot read: the
// renderer strips it away from a terminal it cannot see. What is assertable
// here is that drawing a match leaves the row saying exactly what it said --
// the styling itself is TestHighlight, in package ui.
func TestHighlightingLeavesTheRowsReadingTheSame(t *testing.T) {
	d := listWith(t, 5)
	before := d.view()

	search(d, "Lovelace")

	for _, key := range []string{"ENG-1", "ENG-2", "ENG-5"} {
		if !strings.Contains(d.view(), key) {
			t.Errorf("%s is no longer on screen once its row matches:\n%s", key, d.view())
		}
	}
	if len(strings.Split(d.view(), "\n")) != len(strings.Split(before, "\n")) {
		t.Errorf("the search changed the shape of the screen:\n%s", d.view())
	}
}

func TestNAndNCycleThroughMatchesAndWrap(t *testing.T) {
	d := listWith(t, 5)

	// Every row matches, so the cycle is the whole list and the wrap is
	// observable at both ends.
	search(d, "Work item")
	if got := d.selected(); got != "ENG-1" {
		t.Fatalf("the search landed on %q, want ENG-1", got)
	}

	d.keys("n")
	if got := d.selected(); got != "ENG-2" {
		t.Errorf("n moved to %q, want ENG-2", got)
	}
	d.keys("N")
	if got := d.selected(); got != "ENG-1" {
		t.Errorf("N moved to %q, want ENG-1", got)
	}
	d.keys("N")
	if got := d.selected(); got != "ENG-5" {
		t.Errorf("N at the first match moved to %q, want it to wrap to ENG-5", got)
	}
	d.keys("n")
	if got := d.selected(); got != "ENG-1" {
		t.Errorf("n at the last match moved to %q, want it to wrap to ENG-1", got)
	}
}

// In-pane search is over what is loaded. Going to Jira for it would make the
// one instant thing in the UI as slow as everything else.
func TestInPaneSearchIssuesNoNetworkRequest(t *testing.T) {
	client := &fakeClient{issues: sampleIssues(5)}
	d := newDriver(t, client, testConfig(t, nil))
	before := len(client.requests())

	search(d, "Lovelace")
	d.keys("n", "n", "N")

	if n := len(client.requests()); n != before {
		t.Errorf("in-pane search made %d searches, want none", n-before)
	}
}

func TestSearchWithNoMatchReportsAndLeavesTheSelection(t *testing.T) {
	d := listWith(t, 5)
	d.keys("j", "j")
	before := d.selected()

	search(d, "nothing here matches this")

	if got := d.selected(); got != before {
		t.Errorf("a search with no match moved the selection from %q to %q", before, got)
	}
	if !strings.Contains(d.view(), "no match") {
		t.Errorf("a search with no match was not reported:\n%s", d.view())
	}
}

func TestEscCancelsTheSearchPrompt(t *testing.T) {
	d := listWith(t, 5)

	d.keys("/")
	d.typeText("number 3")
	d.keys("esc")

	if strings.Contains(d.view(), "/number 3") {
		t.Errorf("the search prompt is still up after Esc:\n%s", d.view())
	}
	if got := d.selected(); got != "ENG-1" {
		t.Errorf("a cancelled search moved the selection to %q", got)
	}
}

// n with no pattern has nothing to move to, and says so rather than moving.
func TestNextMatchWithoutAPatternIsReported(t *testing.T) {
	d := listWith(t, 5)

	d.keys("n")

	if got := d.selected(); got != "ENG-1" {
		t.Errorf("n without a pattern moved the selection to %q", got)
	}
	if !strings.Contains(d.view(), "no search pattern") {
		t.Errorf("n without a pattern was not reported:\n%s", d.view())
	}
}

// A count on n means the same thing it means on a motion.
func TestACountRepeatsTheJumpToTheNextMatch(t *testing.T) {
	d := listWith(t, 5)

	search(d, "Work item")
	d.keys("3", "n")

	if got := d.selected(); got != "ENG-4" {
		t.Errorf("3n moved to %q, want ENG-4", got)
	}
}
