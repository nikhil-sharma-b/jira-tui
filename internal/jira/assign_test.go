package jira_test

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/nikhil-sharma-b/jira-tui/internal/jira"
)

func TestSearchUsersAsksForTheAssignableOnesOnThisItem(t *testing.T) {
	srv := newFixtureServer(t, map[string]string{
		"/rest/api/3/user/assignable/search": "assignableusers.200",
	})
	c := newClient(t, srv.URL)

	users, err := c.SearchUsers(context.Background(), "ENG-1", "ad")
	if err != nil {
		t.Fatalf("SearchUsers: %v", err)
	}
	if len(users) != 2 {
		t.Fatalf("got %d users, want 2: %#v", len(users), users)
	}
	if users[0].AccountID != "5b10a2844c20165700ede21g" || users[0].DisplayName != "Ada Lovelace" {
		t.Errorf("first user = %+v, want Ada Lovelace with her account id", users[0])
	}
	// A site that hides email addresses still returns the account, and the
	// account id is the only identifier an assignment needs.
	if users[1].Email != "" || users[1].AccountID == "" {
		t.Errorf("second user = %+v, want an account with no email disclosed", users[1])
	}

	if len(srv.Requests) != 1 {
		t.Fatalf("sent %d requests, want 1", len(srv.Requests))
	}
	q := srv.Requests[0].URL.Query()
	// Assignability is a property of one work item: asking the site who may be
	// assigned to it is what keeps a picker from offering an impossible choice.
	if got := q.Get("issueKey"); got != "ENG-1" {
		t.Errorf("issueKey = %q, want ENG-1", got)
	}
	if got := q.Get("query"); got != "ad" {
		t.Errorf("query = %q, want ad", got)
	}
}

// An empty search is the picker opening: it asks for who is assignable at all
// rather than sending an empty query the site would read as a filter.
func TestSearchUsersOmitsAnEmptyQuery(t *testing.T) {
	srv := newFixtureServer(t, map[string]string{
		"/rest/api/3/user/assignable/search": "assignableusers.200",
	})
	c := newClient(t, srv.URL)

	if _, err := c.SearchUsers(context.Background(), "ENG-1", ""); err != nil {
		t.Fatalf("SearchUsers: %v", err)
	}
	if _, ok := srv.Requests[0].URL.Query()["query"]; ok {
		t.Errorf("the request sent a query parameter: %s", srv.Requests[0].URL.RawQuery)
	}
}

func TestAssignPutsTheAccountIDToTheWriteAPI(t *testing.T) {
	srv := newScriptedServer(t, step{status: http.StatusNoContent})
	c := newClient(t, srv.URL)

	if err := c.Assign(context.Background(), "ENG-1", "5b10a2844c20165700ede21g"); err != nil {
		t.Fatalf("Assign: %v", err)
	}

	got := srv.received()
	if len(got) != 1 {
		t.Fatalf("sent %d requests, want 1", len(got))
	}
	if got[0].method != http.MethodPut {
		t.Errorf("method = %s, want PUT", got[0].method)
	}
	if want := "/rest/api/2/issue/ENG-1/assignee"; got[0].path != want {
		t.Errorf("path = %s, want %s", got[0].path, want)
	}
	var payload struct {
		AccountID *string `json:"accountId"`
	}
	if err := json.Unmarshal([]byte(got[0].body), &payload); err != nil {
		t.Fatalf("decoding sent body %q: %v", got[0].body, err)
	}
	if payload.AccountID == nil || *payload.AccountID != "5b10a2844c20165700ede21g" {
		t.Errorf("sent accountId %v, want the account id", payload.AccountID)
	}
}

// Unassigning is a null account id, not an absent field and not the "-1" that
// means "whoever the project defaults to".
func TestAssignSendsNullToUnassign(t *testing.T) {
	srv := newScriptedServer(t, step{status: http.StatusNoContent})
	c := newClient(t, srv.URL)

	if err := c.Assign(context.Background(), "ENG-1", ""); err != nil {
		t.Fatalf("Assign: %v", err)
	}

	got := srv.received()
	if len(got) != 1 {
		t.Fatalf("sent %d requests, want 1", len(got))
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(got[0].body), &payload); err != nil {
		t.Fatalf("decoding sent body %q: %v", got[0].body, err)
	}
	value, present := payload["accountId"]
	if !present || value != nil {
		t.Errorf("sent body %q, want an explicit null accountId", got[0].body)
	}
}

func TestAssignIsNeverRetried(t *testing.T) {
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

			if err := c.Assign(context.Background(), "ENG-1", "acct"); err == nil {
				t.Fatal("Assign succeeded, want the scripted failure")
			}
			if got := srv.received(); len(got) != 1 {
				t.Fatalf("sent %d requests, want exactly 1", len(got))
			}
		})
	}
}

func TestAssignRejectionKeepsItsStatus(t *testing.T) {
	srv := newScriptedServer(t, step{status: http.StatusForbidden})
	c := newClient(t, srv.URL)

	err := c.Assign(context.Background(), "ENG-1", "acct")
	if !jira.HasStatus(err, http.StatusForbidden) {
		t.Errorf("error = %v, want a 403", err)
	}
}
