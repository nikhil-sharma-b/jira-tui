package ui_test

import (
	"strings"
	"testing"

	"github.com/nikhil-sharma-b/jira-tui/internal/jira"
)

func TestColonOpensTheCommandline(t *testing.T) {
	d := listWith(t, 3)

	d.keys(":")
	d.typeText("jql project = ENG")

	if !strings.Contains(d.view(), ":jql project = ENG") {
		t.Errorf("the commandline does not show what was typed:\n%s", d.view())
	}
}

// Esc means the same thing on the commandline as it does anywhere else, and
// what was typed is discarded rather than run.
func TestEscCancelsTheCommandline(t *testing.T) {
	client := &fakeClient{issues: sampleIssues(3)}
	d := newDriver(t, client, testConfig(t, nil))
	before := len(client.requests())

	d.keys(":")
	d.typeText("jql project = ENG")
	d.keys("esc")

	if strings.Contains(d.view(), ":jql") {
		t.Errorf("the commandline is still up after Esc:\n%s", d.view())
	}
	if n := len(client.requests()); n != before {
		t.Errorf("a cancelled commandline made %d searches, want %d", n, before)
	}
	// Normal mode is back: j moves rather than typing a j.
	d.keys("j")
	if got := d.selected(); got != "ENG-2" {
		t.Errorf("after Esc the selection is on %q, want ENG-2 -- keys are still text", got)
	}
}

func TestUnknownCommandIsReportedAndLeavesTheView(t *testing.T) {
	d := listWith(t, 3)

	d.command(":frobnicate")

	view := d.view()
	if !strings.Contains(view, "frobnicate") {
		t.Errorf("an unknown command was not reported:\n%s", view)
	}
	if !strings.Contains(view, "ENG-1") {
		t.Errorf("an unknown command cleared the view:\n%s", view)
	}
}

func TestQClosesTheViewAndQaQuits(t *testing.T) {
	for _, line := range []string{":q", ":qa"} {
		t.Run(line, func(t *testing.T) {
			d := listWith(t, 3)
			d.command(line)
			if !d.quit {
				t.Errorf("%s did not end the session", line)
			}
		})
	}
}

func TestJQLReplacesTheListContents(t *testing.T) {
	client := &fakeClient{issues: sampleIssues(3)}
	d := newDriver(t, client, testConfig(t, nil))

	d.command(":jql project = ENG AND status = Done")

	reqs := client.requests()
	if len(reqs) == 0 {
		t.Fatal("no search was made")
	}
	if got := reqs[len(reqs)-1].JQL; got != "project = ENG AND status = Done" {
		t.Errorf("the search ran %q, want the typed query", got)
	}
	if !strings.Contains(d.view(), "project = ENG AND status = Done") {
		t.Errorf("the status line does not show the query now on screen:\n%s", d.view())
	}
}

func TestSavedQueryNameExpandsToItsQuery(t *testing.T) {
	client := &fakeClient{issues: sampleIssues(3)}
	cfg := testConfig(t, nil)
	cfg.SavedQueries = map[string]string{"mine": "assignee = currentUser()"}
	d := newDriver(t, client, cfg)

	d.command(":jql @mine")

	reqs := client.requests()
	if got := reqs[len(reqs)-1].JQL; got != "assignee = currentUser()" {
		t.Errorf("@mine ran %q, want the saved query", got)
	}
}

// A mistyped saved name is not a query. Sending it would cost a round trip to
// be told what the config already knows.
func TestUnknownSavedNameNeverReachesJira(t *testing.T) {
	client := &fakeClient{issues: sampleIssues(3)}
	cfg := testConfig(t, nil)
	cfg.SavedQueries = map[string]string{"mine": "assignee = currentUser()"}
	d := newDriver(t, client, cfg)
	before := len(client.requests())

	d.command(":jql @myne")

	if n := len(client.requests()); n != before {
		t.Errorf("an unknown saved name made %d searches, want %d", n-before, 0)
	}
	if !strings.Contains(d.view(), "myne") {
		t.Errorf("an unknown saved name was not reported:\n%s", d.view())
	}
}

// The server knows why a query is invalid and we do not, so its own words go
// on screen -- over rows the user can still read.
func TestInvalidJQLShowsTheServersExplanationAndKeepsTheRows(t *testing.T) {
	bad := "project = AND ORDER BY"
	client := &fakeClient{
		issues: sampleIssues(3),
		searchErrFor: map[string]error{bad: &jira.Error{
			StatusCode: 400,
			Op:         "search",
			Messages:   []string{"Expecting a value or a function after '='"},
		}},
	}
	d := newDriver(t, client, testConfig(t, nil))

	d.command(":jql " + bad)

	view := d.view()
	if !strings.Contains(view, "Expecting a value or a function after '='") {
		t.Errorf("the server's explanation is not on screen:\n%s", view)
	}
	if !strings.Contains(view, "ENG-1") {
		t.Errorf("an invalid query cleared the rows that were already up:\n%s", view)
	}
}

func TestCommandHistoryIsNavigableWithUpAndDown(t *testing.T) {
	d := listWith(t, 3)

	d.command(":jql project = A")
	d.command(":jql project = B")

	d.keys(":", "up")
	if !strings.Contains(d.view(), ":jql project = B") {
		t.Errorf("up did not recall the most recent command:\n%s", d.view())
	}
	d.keys("up")
	if !strings.Contains(d.view(), ":jql project = A") {
		t.Errorf("a second up did not reach the older command:\n%s", d.view())
	}
	d.keys("down")
	if !strings.Contains(d.view(), ":jql project = B") {
		t.Errorf("down did not walk back towards the newest command:\n%s", d.view())
	}
}

// A recalled query is edited rather than retyped, which is the whole reason to
// keep a history at all.
func TestARecalledCommandCanBeEdited(t *testing.T) {
	client := &fakeClient{issues: sampleIssues(3)}
	d := newDriver(t, client, testConfig(t, nil))

	d.command(":jql project = A")
	d.keys(":", "up")
	d.keys("backspace")
	d.typeText("B")
	d.keys("enter")

	reqs := client.requests()
	if got := reqs[len(reqs)-1].JQL; got != "project = B" {
		t.Errorf("the edited query ran %q, want %q", got, "project = B")
	}
}

func TestCompletionSuggestsCommandNames(t *testing.T) {
	d := listWith(t, 3)

	d.keys(":")
	d.typeText("ca")
	d.keys("tab")

	if !strings.Contains(d.view(), ":cache") {
		t.Errorf("tab did not complete the command name:\n%s", d.view())
	}
}

// Several candidates cannot be chosen between, so they are shown rather than
// guessed at, and the line grows only by what they agree on.
func TestCompletionShowsTheCandidatesWhenSeveralMatch(t *testing.T) {
	d := listWith(t, 3)

	d.keys(":")
	d.typeText("q")
	d.keys("tab")

	view := d.view()
	if !strings.Contains(view, "qa") {
		t.Errorf("the candidates were not shown:\n%s", view)
	}
}

func TestCompletionSuggestsSavedQueryNames(t *testing.T) {
	cfg := testConfig(t, nil)
	cfg.SavedQueries = map[string]string{"mine": "assignee = currentUser()"}
	d := newDriver(t, &fakeClient{issues: sampleIssues(3)}, cfg)

	d.keys(":")
	d.typeText("jql @m")
	d.keys("tab")

	if !strings.Contains(d.view(), ":jql @mine") {
		t.Errorf("tab did not complete the saved query name:\n%s", d.view())
	}
}

// The rows left up by a rejected query are rows a user moves through, and
// motion near the end of a loaded set is what asks for another page. Without
// the failure being remembered, every j would ask Jira the question it has
// already refused to answer.
func TestARejectedQueryIsNotAskedAgainByEveryMotion(t *testing.T) {
	bad := "project = AND ORDER BY"
	client := &fakeClient{
		issues: sampleIssues(3),
		searchErrFor: map[string]error{bad: &jira.Error{
			StatusCode: 400,
			Op:         "search",
			Messages:   []string{"Expecting a value or a function after '='"},
		}},
	}
	d := newDriver(t, client, testConfig(t, nil))

	d.command(":jql " + bad)
	settled := len(client.requests())

	d.keys("j", "k", "j", "k")

	if n := len(client.requests()); n != settled {
		t.Errorf("moving over the rows re-ran the rejected query %d times", n-settled)
	}
}

func TestJQLWithoutAQueryIsReportedAndSendsNothing(t *testing.T) {
	client := &fakeClient{issues: sampleIssues(3)}
	d := newDriver(t, client, testConfig(t, nil))
	before := len(client.requests())

	d.command(":jql")

	if n := len(client.requests()); n != before {
		t.Errorf(":jql with no query made %d searches, want none", n-before)
	}
	if !strings.Contains(d.view(), "needs a query") {
		t.Errorf(":jql with no query was not reported:\n%s", d.view())
	}
}

// The @ is spelling, not something to be memorised before completion will
// help: a saved name is the only thing :jql can complete, so a bare partial is
// taken for the start of one.
func TestASavedNameCompletesBeforeTheAtIsTyped(t *testing.T) {
	cfg := testConfig(t, nil)
	cfg.SavedQueries = map[string]string{"mine": "assignee = currentUser()"}
	d := newDriver(t, &fakeClient{issues: sampleIssues(3)}, cfg)

	d.keys(":")
	d.typeText("jql mi")
	d.keys("tab")

	if !strings.Contains(d.view(), ":jql @mine") {
		t.Errorf("tab did not complete a saved name typed without its @:\n%s", d.view())
	}
}
