package ui_test

import (
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/nikhil-sharma-b/jira-tui/internal/config"
	"github.com/nikhil-sharma-b/jira-tui/internal/jira"
	"github.com/nikhil-sharma-b/jira-tui/internal/ui"
)

// availableTransitions is the moves a work item offers in these tests: two
// that differ only in their destination, so filtering and selection have
// something to tell apart.
func availableTransitions() []jira.Transition {
	return []jira.Transition{
		{ID: "11", Name: "Back to To Do", To: jira.Status{ID: "10000", Name: "To Do", Category: "new"}},
		{ID: "21", Name: "Start Progress", To: jira.Status{ID: "10001", Name: "In Progress", Category: "indeterminate"}},
		{ID: "31", Name: "Done", To: jira.Status{ID: "10002", Name: "Done", Category: "done"}, HasScreen: true},
	}
}

// boundConfig compiles the bindings with direct transition bindings in place,
// since the compiled keymap is what dispatch reads.
func boundConfig(t *testing.T, transitions map[string]string) *config.Config {
	t.Helper()
	cfg := config.Defaults()
	cfg.Site.URL = "https://example.atlassian.net"
	cfg.Site.Email = "user@example.com"
	cfg.Transitions = transitions
	if _, err := cfg.Bindings(); err != nil {
		t.Fatalf("compiling bindings: %v", err)
	}
	return cfg
}

func transitionClient(t *testing.T) *fakeClient {
	t.Helper()
	return &fakeClient{
		issues:      sampleIssues(2),
		transitions: map[string][]jira.Transition{"ENG-1": availableTransitions()},
	}
}

func TestTransitionPickerFetchesLiveAndFiltersAsYouType(t *testing.T) {
	client := transitionClient(t)
	d := newDriver(t, client, testConfig(t, nil))

	d.keys("space", "t")

	view := d.view()
	for _, want := range []string{"Start Progress", "Back to To Do", "Done"} {
		if !strings.Contains(view, want) {
			t.Errorf("the picker does not offer %q:\n%s", want, view)
		}
	}
	// The list underneath stays readable: the picker is a strip at the bottom,
	// not a modal over the work being done.
	if !strings.Contains(view, "ENG-1") {
		t.Errorf("the list is no longer visible behind the picker:\n%s", view)
	}
	if got := client.transitionsRequests(); len(got) != 1 || got[0] != "ENG-1" {
		t.Fatalf("transition fetches = %v, want one for ENG-1", got)
	}

	d.typeText("prog")
	view = d.view()
	if !strings.Contains(view, "Start Progress") {
		t.Errorf("filtering hid the matching transition:\n%s", view)
	}
	if strings.Contains(view, "Back to To Do") {
		t.Errorf("filtering kept a transition that does not match:\n%s", view)
	}

	d.keys("enter")
	if len(client.TransitionCalls) != 1 || client.TransitionCalls[0] != (struct{ Key, ID string }{"ENG-1", "21"}) {
		t.Fatalf("transition writes = %#v, want the selected id applied to ENG-1", client.TransitionCalls)
	}
}

func TestTransitionPickerRefetchesEveryTimeItOpens(t *testing.T) {
	client := transitionClient(t)
	d := newDriver(t, client, testConfig(t, nil))

	d.keys("space", "t")
	d.keys("esc")
	d.keys("space", "t")

	// Availability depends on the current status and on permissions, so a
	// remembered list would be quietly wrong exactly when it mattered.
	if got := client.transitionsRequests(); len(got) != 2 {
		t.Errorf("transition fetches = %v, want one per opening", got)
	}
}

func TestTransitionPickerShowsLoadingWithoutBlockingTheUI(t *testing.T) {
	client := transitionClient(t)
	client.transitionsBlock = true
	d := newPausedDriver(t, ui.Options{Client: client, Config: testConfig(t, nil)})
	d.flush()
	d.send(keyMsg("space"))
	d.send(keyMsg("t"))

	if view := d.view(); !strings.Contains(view, "Loading transitions") {
		t.Errorf("the picker does not say it is loading:\n%s", view)
	}
	if view := d.view(); !strings.Contains(view, "ENG-1") {
		t.Errorf("the rest of the UI is not still drawn:\n%s", view)
	}
}

func TestEscapeClosesThePickerWithoutTransitioning(t *testing.T) {
	client := transitionClient(t)
	d := newDriver(t, client, testConfig(t, nil))

	d.keys("space", "t")
	d.keys("esc")

	if view := d.view(); strings.Contains(view, "Start Progress") {
		t.Errorf("the picker is still up after Esc:\n%s", view)
	}
	if len(client.TransitionCalls) != 0 {
		t.Errorf("transition writes = %#v, want none", client.TransitionCalls)
	}
	// Normal mode is back: a motion moves the list rather than filtering.
	d.keys("j")
	if got := d.selected(); got != "ENG-2" {
		t.Errorf("selection = %q after Esc and j, want ENG-2", got)
	}
}

func TestAnItemWithNoTransitionsReportsRatherThanOpeningAnEmptyPicker(t *testing.T) {
	client := &fakeClient{issues: sampleIssues(2), transitions: map[string][]jira.Transition{}}
	d := newDriver(t, client, testConfig(t, nil))

	d.keys("space", "t")

	view := d.view()
	if !strings.Contains(view, "no available transitions") {
		t.Errorf("the status line does not report the empty result:\n%s", view)
	}
	d.keys("j")
	if got := d.selected(); got != "ENG-2" {
		t.Errorf("selection = %q, want ENG-2: the picker mode was never left", got)
	}
}

func TestFailedTransitionFetchReportsAndReturnsToNormalMode(t *testing.T) {
	client := transitionClient(t)
	client.transitionsErr = errors.New("the site said no")
	d := newDriver(t, client, testConfig(t, nil))

	d.keys("space", "t")

	if view := d.view(); !strings.Contains(view, "the site said no") {
		t.Errorf("the failure is not on screen:\n%s", view)
	}
	d.keys("j")
	if got := d.selected(); got != "ENG-2" {
		t.Errorf("selection = %q, want ENG-2: the failed fetch left the UI in picker mode", got)
	}
}

func TestSuccessfulTransitionRefetchesTheItemLive(t *testing.T) {
	client := transitionClient(t)
	d := newDriver(t, client, testConfig(t, nil))
	d.keys("enter") // open detail on ENG-1
	issuesBefore := len(client.issueRequests())

	d.keys("space", "t")
	d.typeText("Done")
	d.keys("enter")

	if len(client.TransitionCalls) != 1 || client.TransitionCalls[0].ID != "31" {
		t.Fatalf("transition writes = %#v, want the Done id", client.TransitionCalls)
	}
	// The new status has to be visible in both panes, which is the live read
	// the detail pane already owns.
	if got := client.issueRequests(); len(got) != issuesBefore+1 {
		t.Errorf("issue fetches = %v, want one more after the transition", got)
	}
	if got := client.commentRequests(); len(got) == 0 || got[len(got)-1] != "ENG-1" {
		t.Errorf("comment fetches = %v, want the transitioned item refetched", got)
	}
}

func TestTransitionFailuresAreDistinctAndNeverRetried(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want []string
	}{
		{
			"required fields",
			&jira.FieldsRequiredError{TransitionID: "31", Fields: map[string]string{
				"resolution": "Resolution is required.", "customfield_10042": "Root cause is required.",
			}},
			// Sorted, so the same rejection reads the same way every time, and
			// pointing at the escape hatch, since collecting fields is out of
			// scope rather than unimplemented by accident.
			[]string{"customfield_10042, resolution", "acli"},
		},
		{
			"permission",
			&jira.Error{StatusCode: http.StatusForbidden, Op: "transition", Messages: []string{"nope"}},
			[]string{"not permitted"},
		},
		{
			"anything else",
			errors.New("the site fell over"),
			[]string{"the site fell over"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := transitionClient(t)
			client.transitionErr = tt.err
			d := newDriver(t, client, testConfig(t, nil))

			d.keys("space", "t")
			d.typeText("Done")
			d.keys("enter")

			view := d.view()
			for _, want := range tt.want {
				if !strings.Contains(view, want) {
					t.Errorf("the status line does not say %q:\n%s", want, view)
				}
			}
			if len(client.TransitionCalls) != 1 {
				t.Errorf("transition writes = %#v, want exactly one: a write is never retried", client.TransitionCalls)
			}
			// The list is still readable underneath the message.
			if !strings.Contains(view, "ENG-1") {
				t.Errorf("the list was cleared by a failed write:\n%s", view)
			}
		})
	}
}

func TestTransitionCommandAppliesByName(t *testing.T) {
	client := transitionClient(t)
	d := newDriver(t, client, testConfig(t, nil))

	d.command(":transition start progress")

	// The name is matched without regard to case: what a user types is not
	// what Jira capitalised.
	if len(client.TransitionCalls) != 1 || client.TransitionCalls[0].ID != "21" {
		t.Fatalf("transition writes = %#v, want the Start Progress id", client.TransitionCalls)
	}
	if got := client.transitionsRequests(); len(got) != 1 {
		t.Errorf("transition fetches = %v, want the name resolved live", got)
	}
}

func TestUnavailableTransitionNameListsWhatIsAvailable(t *testing.T) {
	client := transitionClient(t)
	d := newDriver(t, client, testConfig(t, nil))

	d.command(":transition Ship It")

	view := d.view()
	if !strings.Contains(view, "Ship It") {
		t.Errorf("the message does not repeat the name asked for:\n%s", view)
	}
	for _, want := range []string{"Back to To Do", "Done", "Start Progress"} {
		if !strings.Contains(view, want) {
			t.Errorf("the message does not list %q as available:\n%s", want, view)
		}
	}
	if len(client.TransitionCalls) != 0 {
		t.Errorf("transition writes = %#v, want none", client.TransitionCalls)
	}
}

func TestAmbiguousTransitionNameIsRefusedRatherThanGuessed(t *testing.T) {
	client := transitionClient(t)
	client.transitions["ENG-1"] = append(client.transitions["ENG-1"],
		jira.Transition{ID: "41", Name: "Done", To: jira.Status{Name: "Closed"}})
	d := newDriver(t, client, testConfig(t, nil))

	d.command(":transition done")

	if len(client.TransitionCalls) != 0 {
		t.Fatalf("transition writes = %#v, want none: the name is ambiguous", client.TransitionCalls)
	}
	if view := d.view(); !strings.Contains(view, "more than one") {
		t.Errorf("the ambiguity is not reported:\n%s", view)
	}
}

func TestExactCaseWinsOverAnInexactMatch(t *testing.T) {
	client := transitionClient(t)
	client.transitions["ENG-1"] = append(client.transitions["ENG-1"],
		jira.Transition{ID: "41", Name: "done", To: jira.Status{Name: "Closed"}})
	d := newDriver(t, client, testConfig(t, nil))

	d.command(":transition Done")

	if len(client.TransitionCalls) != 1 || client.TransitionCalls[0].ID != "31" {
		t.Errorf("transition writes = %#v, want the exactly spelled Done", client.TransitionCalls)
	}
}

func TestTransitionCommandCompletesOverLiveNames(t *testing.T) {
	client := transitionClient(t)
	d := newDriver(t, client, testConfig(t, nil))

	d.keys(":")
	d.typeText("transition ")
	d.keys("tab")

	view := d.view()
	for _, want := range []string{"Back to To Do", "Done", "Start Progress"} {
		if !strings.Contains(view, want) {
			t.Errorf("completion does not offer %q:\n%s", want, view)
		}
	}
	if got := client.transitionsRequests(); len(got) != 1 {
		t.Errorf("transition fetches = %v, want one live fetch for completion", got)
	}

	// A completed name is the whole line, and submitting it applies that
	// transition against a fresh fetch rather than against what completion saw.
	d.typeText("Start")
	d.keys("tab")
	d.keys("enter")
	if len(client.TransitionCalls) != 1 || client.TransitionCalls[0].ID != "21" {
		t.Errorf("transition writes = %#v, want Start Progress applied", client.TransitionCalls)
	}
	if got := client.transitionsRequests(); len(got) != 3 {
		t.Errorf("transition fetches = %v, want one per completion and one to resolve the name", got)
	}
}

func TestDirectlyBoundTransitionAppliesWithoutThePicker(t *testing.T) {
	client := transitionClient(t)
	d := newDriver(t, client, boundConfig(t, map[string]string{
		"Start Progress": "<leader>ms", "Ship It": "<leader>mp",
	}))

	d.keys("space", "m", "s")

	if view := d.view(); strings.Contains(view, "Back to To Do") {
		t.Errorf("a direct binding opened the picker:\n%s", view)
	}
	if len(client.TransitionCalls) != 1 || client.TransitionCalls[0].ID != "21" {
		t.Fatalf("transition writes = %#v, want Start Progress applied", client.TransitionCalls)
	}

	// A bound name the item cannot move to is reported the same way the
	// commandline reports one, since the binding resolves live too.
	d.keys("space", "m", "p")
	if view := d.view(); !strings.Contains(view, "Ship It") {
		t.Errorf("an unavailable bound transition is not reported:\n%s", view)
	}
	if len(client.TransitionCalls) != 1 {
		t.Errorf("transition writes = %#v, want no second write", client.TransitionCalls)
	}
}

func TestDirectTransitionBindingsAreDocumentedInHelp(t *testing.T) {
	client := transitionClient(t)
	d := newDriver(t, client, boundConfig(t, map[string]string{"Start Progress": "<leader>ms"}))

	d.keys("?")

	if view := d.view(); !strings.Contains(view, "Start Progress") {
		t.Errorf("the help overlay does not document the direct binding:\n%s", view)
	}
}

func TestTransitionTargetsTheFocusedItemInEveryView(t *testing.T) {
	tests := []struct {
		name string
		open func(*driver)
		want string
	}{
		{"list", func(d *driver) { d.keys("j") }, "ENG-2"},
		{"split detail", func(d *driver) { d.keys("j"); d.keys("enter") }, "ENG-2"},
		{"zoomed detail", func(d *driver) { d.keys("j"); d.keys("enter"); d.keys("ctrl+w", "o") }, "ENG-2"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := &fakeClient{issues: sampleIssues(2), transitions: map[string][]jira.Transition{
				"ENG-1": availableTransitions(), "ENG-2": availableTransitions(),
			}}
			d := newDriver(t, client, testConfig(t, nil))
			tt.open(d)

			d.keys("space", "t")
			d.typeText("Done")
			d.keys("enter")

			if len(client.TransitionCalls) != 1 || client.TransitionCalls[0].Key != tt.want {
				t.Errorf("transition writes = %#v, want one against %s", client.TransitionCalls, tt.want)
			}
		})
	}
}

func TestPinnedSessionTransitionsThePinnedItem(t *testing.T) {
	client := &fakeClient{issues: sampleIssues(2), transitions: map[string][]jira.Transition{
		"ENG-2": availableTransitions(),
	}}
	d := newPausedDriver(t, ui.Options{Client: client, Config: testConfig(t, nil), Pin: "ENG-2"})
	d.flush()

	d.keys("space", "t")
	d.typeText("Done")
	d.keys("enter")

	if len(client.TransitionCalls) != 1 || client.TransitionCalls[0].Key != "ENG-2" {
		t.Errorf("transition writes = %#v, want one against the pinned ENG-2", client.TransitionCalls)
	}
}

func TestNavigatingAwayDiscardsAnInFlightTransitionFetch(t *testing.T) {
	client := &fakeClient{issues: sampleIssues(2), transitions: map[string][]jira.Transition{
		"ENG-1": availableTransitions(),
	}}
	d := newPausedDriver(t, ui.Options{Client: client, Config: testConfig(t, nil)})
	d.flush()
	d.send(keyMsg("space"))
	d.send(keyMsg("t"))
	d.send(keyMsg("esc"))
	d.flush()

	if view := d.view(); strings.Contains(view, "Start Progress") {
		t.Errorf("a fetch that landed after Esc reopened the picker:\n%s", view)
	}
	if len(client.TransitionCalls) != 0 {
		t.Errorf("transition writes = %#v, want none", client.TransitionCalls)
	}
}

func TestTransitionCommandNeedsAName(t *testing.T) {
	client := transitionClient(t)
	d := newDriver(t, client, testConfig(t, nil))

	d.command(":transition")

	if view := d.view(); !strings.Contains(view, "transition") {
		t.Errorf("an argumentless :transition says nothing:\n%s", view)
	}
	if len(client.TransitionCalls) != 0 {
		t.Errorf("transition writes = %#v, want none", client.TransitionCalls)
	}
}

// TestTransitionActionIsNotAKeymapAction guards the config schema: a direct
// transition is bound in its own table, so the keys table stays a closed set.
func TestTransitionActionIsNotAKeymapAction(t *testing.T) {
	if config.IsAction(string(config.TransitionAction("Done"))) {
		t.Error("a transition action is accepted in the keys table")
	}
}

func TestTheNewStatusIsVisibleInBothPanes(t *testing.T) {
	client := transitionClient(t)
	// The refetched item is what Jira says now, which is the point of never
	// caching the focused work item.
	moved := sampleIssues(2)[0]
	moved.Status = jira.Status{Name: "Done", Category: "done"}
	client.issueFor = map[string]jira.Issue{"ENG-1": moved}
	d := newDriver(t, client, testConfig(t, nil))

	d.keys("space", "t")
	d.typeText("Done")
	d.keys("enter")

	row := transitionedRow(t, d, "ENG-1")
	if strings.Contains(row, "In Progress") {
		t.Errorf("the ENG-1 row still shows the status the search cached: %q", row)
	}
	if !strings.Contains(row, "Done") {
		t.Errorf("the ENG-1 row does not show the new status: %q", row)
	}
}

// transitionedRow is the drawn list row for one work item, which is where "the
// new status is visible in both panes" is observable for the list pane.
func transitionedRow(t *testing.T, d *driver, key string) string {
	t.Helper()
	for _, line := range d.lines() {
		if strings.Contains(line, key) {
			return line
		}
	}
	t.Fatalf("%s is not on screen:\n%s", key, d.view())
	return ""
}

func TestTransitioningFromTheListDoesNotOpenDetailOrMoveFocus(t *testing.T) {
	client := transitionClient(t)
	d := newDriver(t, client, testConfig(t, nil))

	d.keys("space", "t")
	d.typeText("Done")
	d.keys("enter")

	// A write is not navigation: focus is addressed directly, never moved as a
	// side effect. The list is still the focused pane, so j still moves it.
	d.keys("j")
	if got := d.selected(); got != "ENG-2" {
		t.Errorf("selection = %q after transitioning and pressing j, want ENG-2", got)
	}
	if got := client.commentRequests(); len(got) != 0 {
		t.Errorf("comment fetches = %v, want none: no detail pane was open", got)
	}
}

func TestEnterBeforeTheTransitionsArriveChangesNothing(t *testing.T) {
	client := transitionClient(t)
	client.transitionsBlock = true
	d := newPausedDriver(t, ui.Options{Client: client, Config: testConfig(t, nil)})
	d.flush()
	d.send(keyMsg("space"))
	d.send(keyMsg("t"))
	d.send(keyMsg("enter"))

	// Esc is the only thing that closes a picker without applying anything;
	// Enter with nothing selected must not become a second, silent cancel.
	if view := d.view(); !strings.Contains(view, "Loading transitions") {
		t.Errorf("Enter closed the picker while it was still loading:\n%s", view)
	}
	if len(client.TransitionCalls) != 0 {
		t.Errorf("transition writes = %#v, want none", client.TransitionCalls)
	}
}

func TestEnterWithNothingMatchingChangesNothing(t *testing.T) {
	client := transitionClient(t)
	d := newDriver(t, client, testConfig(t, nil))

	d.keys("space", "t")
	d.typeText("nothing named this")
	d.keys("enter")

	if view := d.view(); !strings.Contains(view, "Nothing matches") {
		t.Errorf("Enter closed the picker when nothing matched the filter:\n%s", view)
	}
	if len(client.TransitionCalls) != 0 {
		t.Errorf("transition writes = %#v, want none", client.TransitionCalls)
	}
}
