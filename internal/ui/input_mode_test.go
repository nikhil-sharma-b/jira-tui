package ui_test

import (
	"strings"
	"testing"

	"github.com/nikhil-sharma-b/jira-tui/internal/jira"
	"github.com/nikhil-sharma-b/jira-tui/internal/ui"
)

// Ticket 15 is an invariant over the whole UI rather than over one widget:
// reaching a pane may move focus or scroll it, but it must leave the next key
// with the normal-mode binding table. Seeing ? open help proves that the key
// was handled as an action instead of disappearing into text input.
func TestArrivingAtAPaneNeverStartsTextInput(t *testing.T) {
	issue := detailedIssue()
	issue.Description = longDescription()
	client := func() *fakeClient {
		return &fakeClient{
			issues: []jira.Issue{issue},
			comments: map[string][]jira.Comment{
				issue.Key: {{Author: &jira.User{DisplayName: "Ada Lovelace"}, Body: longCommentBody()}},
			},
		}
	}

	tests := []struct {
		name   string
		arrive func(*driver)
	}{
		{"startup list", func(*driver) {}},
		{"detail opened with Enter", func(d *driver) { d.keys("enter") }},
		{"list addressed with gl", func(d *driver) { d.keys("enter", "g", "l") }},
		{"detail addressed with gd", func(d *driver) { d.keys("enter", "g", "l", "g", "d") }},
		{"comments addressed with gc", func(d *driver) { d.keys("enter", "g", "c") }},
		{"list reached with Ctrl-w h", func(d *driver) { d.keys("enter", "ctrl+w", "h") }},
		{"detail reached with Ctrl-w l", func(d *driver) { d.keys("enter", "ctrl+w", "h", "ctrl+w", "l") }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := newDriver(t, client(), testConfig(t, nil))
			tt.arrive(d)

			d.keys("?")

			if !strings.Contains(d.view(), "MOTION") {
				t.Fatalf("? was captured as text after arriving at %s:\n%s", tt.name, d.view())
			}
		})
	}
}

// The four widgets that accept terminal text are reached through actions the
// user deliberately invokes. Merely rendering their surrounding pane is
// covered above; here the action itself must put an input cursor on screen.
func TestExplicitActionsEnterEveryTextInput(t *testing.T) {
	client := assignClient()
	client.transitions = map[string][]jira.Transition{"ENG-1": availableTransitions()}

	tests := []struct {
		name string
		keys []string
		want string
	}{
		{"commandline", []string{":"}, ":"},
		{"pane search", []string{"/"}, "/"},
		{"transition filter", []string{"space", "t"}, "Transition ENG-1"},
		{"assignee filter", []string{"space", "a"}, "Assign ENG-1"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := newPausedDriver(t, ui.Options{Client: client, Config: testConfig(t, nil)})
			d.flush()

			d.keys(tt.keys...)

			if !strings.Contains(d.view(), tt.want) {
				t.Fatalf("%v did not enter %s input:\n%s", tt.keys, tt.name, d.view())
			}
		})
	}
}
