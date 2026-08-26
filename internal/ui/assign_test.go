package ui_test

import (
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/nikhil-sharma-b/jira-tui/internal/jira"
	"github.com/nikhil-sharma-b/jira-tui/internal/ui"
)

// assignableUsers is who this site will offer, per search term. "hopper"
// deliberately answers with a name the typed text does not appear in, so a
// picker that filtered the previous answer locally could not produce it.
func assignableUsers() map[string][]jira.User {
	return map[string][]jira.User{
		"": {
			{AccountID: "acct-2", DisplayName: "Ada Lovelace", Email: "ada@example.com", Active: true},
			{AccountID: "acct-3", DisplayName: "Grace Hopper", Active: true},
		},
		"hopper": {
			{AccountID: "acct-4", DisplayName: "Katherine Johnson", Active: true},
		},
		"ada": {
			{AccountID: "acct-2", DisplayName: "Ada Lovelace", Email: "ada@example.com", Active: true},
		},
	}
}

func assignClient() *fakeClient {
	return &fakeClient{
		issues: sampleIssues(2),
		users:  assignableUsers(),
		myself: &jira.User{AccountID: "acct-1", DisplayName: "Test User", Email: "me@example.com"},
	}
}

// assignDriver runs the model with a debounce short enough that a test never
// waits on the clock. What the debounce is being asked to prove is that a
// keystroke supersedes the request the one before it was about to make, and
// that is a matter of order rather than of duration.
func assignDriver(t *testing.T, client *fakeClient) *driver {
	t.Helper()
	return assignDriverWith(t, client, time.Millisecond)
}

func assignDriverWith(t *testing.T, client *fakeClient, debounce time.Duration) *driver {
	t.Helper()
	d := newPausedDriver(t, ui.Options{
		Client:         client,
		Config:         testConfig(t, nil),
		SearchDebounce: debounce,
	})
	d.flush()
	return d
}

// statusLine is the bottom line of the screen, which is where a failure is
// reported: the panes above it stay readable, so what a test asks about is the
// one line that changed.
func (d *driver) statusLine() string {
	lines := d.lines()
	return lines[len(lines)-1]
}

func TestAssignPickerOffersTheUsersAssignableToThisItem(t *testing.T) {
	client := assignClient()
	d := assignDriver(t, client)

	d.keys("space", "a")

	view := d.view()
	for _, want := range []string{"Assign ENG-1", "Ada Lovelace", "Grace Hopper"} {
		if !strings.Contains(view, want) {
			t.Errorf("the picker does not show %q:\n%s", want, view)
		}
	}
	// The list underneath stays readable: the picker is a strip at the bottom,
	// not a modal over the work being done.
	if !strings.Contains(view, "Work item number 1") {
		t.Errorf("the picker hid the list:\n%s", view)
	}
	calls := client.searchCalls()
	if len(calls) != 1 || calls[0].Key != "ENG-1" || calls[0].Query != "" {
		t.Errorf("searches = %+v, want one unfiltered search for ENG-1", calls)
	}
}

// A user whose site hides email addresses is an ordinary choice: the account
// id is the only identifier an assignment needs.
func TestAssignPickerOffersUsersWhoseEmailIsHidden(t *testing.T) {
	client := assignClient()
	d := assignDriver(t, client)

	d.keys("space", "a")
	// Ada is offered with her address, Grace without one; both are selectable.
	if view := d.view(); !strings.Contains(view, "ada@example.com") {
		t.Errorf("a disclosed email is not shown:\n%s", view)
	}
	d.keys("down", "down", "down", "enter")

	calls := client.assignCalls()
	if len(calls) != 1 || calls[0].AccountID != "acct-3" {
		t.Fatalf("assignments = %+v, want Grace's account id", calls)
	}
}

// Typing asks the server rather than narrowing what it last said, so a name
// that was never in the first answer is still reachable.
func TestAssignSearchQueriesTheServer(t *testing.T) {
	client := assignClient()
	d := assignDriver(t, client)

	d.keys("space", "a")
	d.typeText("hopper")

	calls := client.searchCalls()
	if len(calls) != 2 || calls[1].Query != "hopper" {
		t.Fatalf("searches = %+v, want a second one for %q", calls, "hopper")
	}
	if view := d.view(); !strings.Contains(view, "Katherine Johnson") {
		t.Errorf("the picker does not show what the server answered:\n%s", view)
	}
}

// A keystroke asks for nothing by itself: the request waits out the debounce,
// which is what gives the keystroke after it the chance to supersede it.
func TestATypedLetterDoesNotSearchUntilTheDebounceElapses(t *testing.T) {
	const debounce = 60 * time.Millisecond
	client := assignClient()
	d := assignDriverWith(t, client, debounce)

	d.keys("space", "a")
	if got := len(client.searchCalls()); got != 1 {
		t.Fatalf("searches before typing = %d, want the picker's own", got)
	}

	// Sent without running what it queued: the letter has been typed, and
	// nothing has been asked of the site yet.
	d.send(keyMsg("a"))
	if got := len(client.searchCalls()); got != 1 {
		t.Fatalf("searches = %d immediately after a keystroke, want no new one", got)
	}

	start := time.Now()
	d.flush()
	if elapsed := time.Since(start); elapsed < debounce {
		t.Errorf("the search went out after %v, want it to wait out the %v debounce", elapsed, debounce)
	}
	calls := client.searchCalls()
	if len(calls) != 2 || calls[1].Query != "a" {
		t.Errorf("searches = %+v, want one for the typed letter once the wait was over", calls)
	}
}

// Every keystroke would otherwise be a request. Only the last one survives.
func TestAssignSearchIsDebounced(t *testing.T) {
	client := assignClient()
	d := assignDriver(t, client)

	d.keys("space", "a")
	d.typeText("ada")

	calls := client.searchCalls()
	if len(calls) != 2 {
		t.Fatalf("searches = %+v, want the picker's own and one for the typed name", calls)
	}
	if calls[1].Query != "ada" {
		t.Errorf("the surviving search asked for %q, want ada", calls[1].Query)
	}
}

// The unfiltered picker opens on the two answers the user already knows: their
// own account, and no account at all.
func TestAssignPickerOffersSelfAndUnassign(t *testing.T) {
	client := assignClient()
	d := assignDriver(t, client)

	d.keys("space", "a")

	view := d.view()
	if !strings.Contains(view, "Unassigned") {
		t.Errorf("the picker offers no way to unassign:\n%s", view)
	}
	if !strings.Contains(view, "Test User") || !strings.Contains(view, "(me)") {
		t.Errorf("the picker does not offer the current user:\n%s", view)
	}
}

func TestAssignPickerUnassigns(t *testing.T) {
	client := assignClient()
	d := assignDriver(t, client)

	d.keys("space", "a")
	// Unassigned is the first row, where the cursor already is.
	d.keys("enter")

	calls := client.assignCalls()
	if len(calls) != 1 || calls[0].Key != "ENG-1" || calls[0].AccountID != "" {
		t.Fatalf("assignments = %+v, want ENG-1 unassigned", calls)
	}
	if view := d.view(); !strings.Contains(view, "Unassigned") {
		t.Errorf("the row does not read back as unassigned:\n%s", view)
	}
}

func TestAssignPickerSelfAssigns(t *testing.T) {
	client := assignClient()
	d := assignDriver(t, client)

	d.keys("space", "a")
	d.keys("down", "enter")

	calls := client.assignCalls()
	if len(calls) != 1 || calls[0].AccountID != "acct-1" {
		t.Fatalf("assignments = %+v, want the current user's account id", calls)
	}
}

func TestEscLeavesTheAssignPickerWithoutAssigning(t *testing.T) {
	client := assignClient()
	d := assignDriver(t, client)

	d.keys("space", "a")
	d.keys("esc")

	if view := d.view(); strings.Contains(view, "Assign ENG-1") {
		t.Errorf("the picker is still up after Esc:\n%s", view)
	}
	if calls := client.assignCalls(); len(calls) != 0 {
		t.Errorf("assignments = %+v, want none", calls)
	}
	// Normal mode is back: a bare motion moves the selection again.
	d.keys("j")
	if got := d.selected(); got != "ENG-2" {
		t.Errorf("selection = %q, want ENG-2", got)
	}
}

// The write is followed by a live read, because the row on screen came from a
// cached search and the assignment has just made it wrong.
func TestTheNewAssigneeIsVisibleInTheListRow(t *testing.T) {
	client := assignClient()
	d := assignDriver(t, client)

	d.keys("space", "a")
	d.keys("down", "down", "down", "enter") // Grace Hopper

	row := assignedRow(t, d, "ENG-1")
	if strings.Contains(row, "Ada Lovelace") {
		t.Errorf("the ENG-1 row still shows the assignee the search cached: %q", row)
	}
	if !strings.Contains(row, "Grace Hopper") {
		t.Errorf("the ENG-1 row does not show the new assignee: %q", row)
	}
}

// assignedRow is the drawn list row for one work item, which is where "the new
// assignee is visible in both panes" is observable for the list pane.
func assignedRow(t *testing.T, d *driver, key string) string {
	t.Helper()
	for _, line := range d.lines() {
		// The pane's frame carries the key in its title, which is not a row.
		if strings.HasPrefix(line, "╭") || strings.Contains(line, "╮") {
			continue
		}
		if strings.Contains(line, key) {
			return line
		}
	}
	t.Fatalf("%s is not on screen:\n%s", key, d.view())
	return ""
}

func TestAssigningRefetchesTheItemLiveForTheDetailPane(t *testing.T) {
	client := assignClient()
	d := assignDriver(t, client)
	d.keys("enter", "]") // open detail on ENG-1, on its fields
	issuesBefore := len(client.issueRequests())

	d.keys("space", "a")
	d.keys("down", "down", "down", "enter") // Grace Hopper

	if got := client.issueRequests(); len(got) != issuesBefore+1 {
		t.Errorf("issue fetches = %v, want one more after the assignment", got)
	}
	if view := d.view(); !strings.Contains(view, "Assignee: Grace Hopper") {
		t.Errorf("the detail pane does not show the new assignee:\n%s", view)
	}
	// The other half of "both panes": the row beside the detail came from a
	// cached search, and the live read is what corrects it. The screen is
	// widened first, because a 45-column list pane has no room to draw the
	// assignee column at all.
	d.send(tea.WindowSizeMsg{Width: 300, Height: 22})
	row := assignedRow(t, d, "ENG-1")
	if !strings.Contains(row, "Grace Hopper") {
		t.Errorf("the ENG-1 row still disagrees with the pane beside it: %q", row)
	}
}

func TestAssignFromTheCommandline(t *testing.T) {
	client := assignClient()
	d := assignDriver(t, client)

	d.command(":assign ada")

	if calls := client.searchCalls(); len(calls) != 1 || calls[0].Query != "ada" {
		t.Fatalf("searches = %+v, want one for the typed name", calls)
	}
	calls := client.assignCalls()
	if len(calls) != 1 || calls[0].AccountID != "acct-2" {
		t.Fatalf("assignments = %+v, want Ada's account id", calls)
	}
}

func TestAssignByNameTellsNoMatchFromSeveral(t *testing.T) {
	client := assignClient()
	client.users["o"] = []jira.User{
		{AccountID: "acct-3", DisplayName: "Grace Hopper"},
		{AccountID: "acct-4", DisplayName: "Katherine Johnson"},
	}
	client.users["nobody"] = nil
	d := assignDriver(t, client)

	d.command(":assign nobody")
	if got := d.view(); !strings.Contains(got, "no user") {
		t.Errorf("a name matching nobody reported %q, want it said so", d.statusLine())
	}
	if calls := client.assignCalls(); len(calls) != 0 {
		t.Fatalf("assignments = %+v, want none", calls)
	}

	d.command(":assign o")
	status := d.statusLine()
	if !strings.Contains(status, "Grace Hopper") || !strings.Contains(status, "Katherine Johnson") {
		t.Errorf("an ambiguous name reported %q, want both candidates named", status)
	}
	if calls := client.assignCalls(); len(calls) != 0 {
		t.Fatalf("assignments = %+v, want none", calls)
	}
}

// An exactly spelled name is not ambiguous, however many others the server
// offered alongside it.
func TestAssignByNamePrefersAnExactName(t *testing.T) {
	client := assignClient()
	client.users["Grace Hopper"] = []jira.User{
		{AccountID: "acct-3", DisplayName: "Grace Hopper"},
		{AccountID: "acct-9", DisplayName: "Grace Hopperton"},
	}
	d := assignDriver(t, client)

	d.command(":assign Grace Hopper")

	calls := client.assignCalls()
	if len(calls) != 1 || calls[0].AccountID != "acct-3" {
		t.Fatalf("assignments = %+v, want the exactly named account", calls)
	}
}

func TestRejectedAssignReportsTheReasonAndIsNotRetried(t *testing.T) {
	client := assignClient()
	client.assignErr = &jira.Error{Op: "assign", StatusCode: http.StatusForbidden, Messages: []string{"no soup for you"}}
	d := assignDriver(t, client)

	d.keys("space", "a")
	d.keys("down", "down", "enter")

	status := d.statusLine()
	if !strings.Contains(status, "not permitted") {
		t.Errorf("status = %q, want it to say the assignment was refused", status)
	}
	// What the site said is the only part that can say which permission, or
	// which account, it objected to.
	if !strings.Contains(status, "no soup for you") {
		t.Errorf("status = %q, want the site's own words kept", status)
	}
	if calls := client.assignCalls(); len(calls) != 1 {
		t.Errorf("assignments = %+v, want exactly one attempt", calls)
	}
}

func TestFailedUserSearchClosesThePickerAndSaysWhy(t *testing.T) {
	client := assignClient()
	client.usersErr = errors.New("the site said no")
	d := assignDriver(t, client)

	d.keys("space", "a")

	if view := d.view(); strings.Contains(view, "Assign ENG-1") {
		t.Errorf("the picker stayed up over a failed search:\n%s", view)
	}
	if got := d.statusLine(); !strings.Contains(got, "the site said no") {
		t.Errorf("status = %q, want the failure reported", got)
	}
	// The dispatcher is back in normal mode rather than stuck in the picker's.
	d.keys("j")
	if got := d.selected(); got != "ENG-2" {
		t.Errorf("selection = %q, want ENG-2", got)
	}
}

// Who the current user is does not change during a session, so it is asked for
// once and pinned to the top of every picker after that.
func TestTheCurrentUserIsFetchedOnce(t *testing.T) {
	client := assignClient()
	d := assignDriver(t, client)

	d.keys("space", "a")
	d.keys("esc")
	d.keys("space", "a")

	if got := client.myselfCalls(); got != 1 {
		t.Errorf("the current user was fetched %d times, want 1", got)
	}
}
