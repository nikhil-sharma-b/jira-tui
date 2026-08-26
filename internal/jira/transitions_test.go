package jira_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/nikhil-sharma-b/jira-tui/internal/jira"
)

func TestTransitionsListsWhatIsAvailableNow(t *testing.T) {
	srv := newFixtureServer(t, map[string]string{
		"/rest/api/3/issue/ENG-1/transitions": "transitions.200",
	})
	c := newClient(t, srv.URL)

	got, err := c.Transitions(context.Background(), "ENG-1")
	if err != nil {
		t.Fatalf("Transitions: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d transitions, want 2: %#v", len(got), got)
	}
	if got[0].ID != "11" || got[0].Name != "To Do" {
		t.Errorf("first transition = %+v, want id 11 named To Do", got[0])
	}
	// The destination status is what a picker shows beside the transition
	// name: two transitions can be named alike and differ only in where they
	// land.
	if got[0].To.ID != "10000" || got[0].To.Name != "To Do" || got[0].To.Category != "new" {
		t.Errorf("first destination = %+v, want status 10000 To Do in category new", got[0].To)
	}
	if got[0].HasScreen {
		t.Error("first transition reports a screen, want none")
	}
	if !got[1].HasScreen {
		t.Error("second transition reports no screen, want one")
	}
	if got[1].To.Category != "done" {
		t.Errorf("second destination category = %q, want done", got[1].To.Category)
	}
}

func TestTransitionPostsTheIDToTheWriteAPI(t *testing.T) {
	srv := newScriptedServer(t, step{status: http.StatusNoContent})
	c := newClient(t, srv.URL)

	if err := c.Transition(context.Background(), "ENG-1", "31"); err != nil {
		t.Fatalf("Transition: %v", err)
	}

	got := srv.received()
	if len(got) != 1 {
		t.Fatalf("sent %d requests, want 1", len(got))
	}
	if got[0].method != http.MethodPost {
		t.Errorf("method = %s, want POST", got[0].method)
	}
	// Writes go to v2, whose rich-text fields are plain strings; only reads
	// need v3's ADF.
	if want := "/rest/api/2/issue/ENG-1/transitions"; got[0].path != want {
		t.Errorf("path = %s, want %s", got[0].path, want)
	}
	var payload struct {
		Transition struct {
			ID string `json:"id"`
		} `json:"transition"`
	}
	if err := json.Unmarshal([]byte(got[0].body), &payload); err != nil {
		t.Fatalf("decoding sent body %q: %v", got[0].body, err)
	}
	if payload.Transition.ID != "31" {
		t.Errorf("sent transition id %q, want 31", payload.Transition.ID)
	}
}

func TestTransitionIsNeverRetried(t *testing.T) {
	tests := []struct {
		name string
		step step
	}{
		{"rate limited", step{status: http.StatusTooManyRequests, retryAfter: "1"}},
		{"server failure", step{status: http.StatusBadGateway}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := newScriptedServer(t, tt.step)
			c := newClient(t, srv.URL)

			if err := c.Transition(context.Background(), "ENG-1", "31"); err == nil {
				t.Fatal("Transition succeeded, want the scripted failure")
			}
			// A double transition is a worse outcome than an error message, so
			// a retryable status buys the write nothing.
			if got := srv.received(); len(got) != 1 {
				t.Fatalf("sent %d requests, want exactly 1", len(got))
			}
		})
	}
}

func TestTransitionScreenRejectionNamesItsFields(t *testing.T) {
	srv := newScriptedServer(t, step{fixture: "transitionfields.400"})
	c := newClient(t, srv.URL)

	err := c.Transition(context.Background(), "ENG-1", "31")
	var required *jira.FieldsRequiredError
	if !errors.As(err, &required) {
		t.Fatalf("Transition error = %v (%T), want a FieldsRequiredError", err, err)
	}
	if required.TransitionID != "31" {
		t.Errorf("TransitionID = %q, want 31", required.TransitionID)
	}
	if got, want := required.Fields["resolution"], "Resolution is required."; got != want {
		t.Errorf("resolution message = %q, want %q", got, want)
	}
	if _, ok := required.Fields["customfield_10042"]; !ok {
		t.Errorf("fields = %v, want the custom field preserved", required.Fields)
	}
	// The message is read in a status line, so it must name the fields in a
	// stable order rather than in map order.
	message := required.Error()
	if !strings.Contains(message, "customfield_10042") || !strings.Contains(message, "resolution") {
		t.Errorf("message %q does not name both fields", message)
	}
	if strings.Index(message, "customfield_10042") > strings.Index(message, "resolution") {
		t.Errorf("message %q does not sort its fields", message)
	}
}

func TestTransitionRejectionWithoutFieldsStaysAnHTTPError(t *testing.T) {
	srv := newScriptedServer(t, step{status: http.StatusForbidden})
	c := newClient(t, srv.URL)

	err := c.Transition(context.Background(), "ENG-1", "31")
	var required *jira.FieldsRequiredError
	if errors.As(err, &required) {
		t.Fatalf("a permission rejection decoded as %v, want an HTTP error", required)
	}
	if !jira.HasStatus(err, http.StatusForbidden) {
		t.Errorf("error = %v, want a 403", err)
	}
}
