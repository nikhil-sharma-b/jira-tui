package ui_test

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/nikhil-sharma-b/jira-tui/internal/ui"
)

func whichKeyDriver(t *testing.T) *driver {
	t.Helper()
	d := newPausedDriver(t, ui.Options{
		Client: &fakeClient{issues: sampleIssues(2)},
		Config: testConfig(t, nil),
	})
	d.flush()
	d.send(tea.WindowSizeMsg{Width: 120, Height: 24})
	return d
}

// A prefix in flight lists what may follow it, described the way the help
// overlay describes it.
func TestWhichKeyListsContinuationsOfAPrefix(t *testing.T) {
	d := whichKeyDriver(t)
	d.keys("ctrl+w")

	view := d.view()
	for _, want := range []string{"pane left", "pane right", "zoom pane"} {
		if !strings.Contains(view, want) {
			t.Errorf("which-key menu is missing %q:\n%s", want, view)
		}
	}
}

// The leader is a prefix like any other, so it gets the same menu.
func TestWhichKeyListsLeaderContinuations(t *testing.T) {
	d := whichKeyDriver(t)
	d.keys(" ")

	if got := d.view(); !strings.Contains(got, "yank key") {
		t.Errorf("leader menu is missing the leader bindings:\n%s", got)
	}
}

// The menu is only up while a sequence is: it goes away when the sequence
// resolves, and when Esc abandons it.
func TestWhichKeyClosesWhenTheSequenceEnds(t *testing.T) {
	d := whichKeyDriver(t)

	d.keys("ctrl+w")
	d.keys("esc")
	if got := d.view(); strings.Contains(got, "zoom pane") {
		t.Errorf("which-key menu survived Esc:\n%s", got)
	}

	d.keys(" ")
	d.keys("y")
	if got := d.view(); strings.Contains(got, "yank URL") {
		t.Errorf("which-key menu survived the completed sequence:\n%s", got)
	}
}

// The menu floats: it is drawn over the panes, so opening it must not move a
// single row of what was already on screen.
func TestWhichKeyFloatsWithoutShiftingTheLayout(t *testing.T) {
	d := whichKeyDriver(t)
	before := strings.Split(d.view(), "\n")

	d.keys(" ")
	during := strings.Split(d.view(), "\n")

	if len(before) != len(during) {
		t.Fatalf("view is %d lines with the menu up, %d without", len(during), len(before))
	}
	// The rows the float does not cover are untouched, and the first one is
	// the list's own header.
	if before[1] != during[1] {
		t.Errorf("row above the float moved:\n%q\n%q", before[1], during[1])
	}
	if got := during[len(during)-2]; !strings.Contains(got, "╰") {
		t.Errorf("the pane's bottom frame was pushed off screen: %q", got)
	}
}
