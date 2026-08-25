package jira_test

import (
	"context"
	"strings"
	"testing"

	"github.com/nikhil-sharma-b/jira-tui/internal/jira"
)

// fieldByID finds one field in the metadata, so an assertion names the field
// it is about rather than an index into a recorded payload.
func fieldByID(fields []jira.Field, id string) (jira.Field, bool) {
	for _, f := range fields {
		if f.ID == id {
			return f, true
		}
	}
	return jira.Field{}, false
}

func TestFieldsDecodesSystemAndCustomMetadata(t *testing.T) {
	srv := newFixtureServer(t, map[string]string{"/rest/api/3/field": "fields.200"})
	fields, err := newClient(t, srv.URL).Fields(context.Background())
	if err != nil {
		t.Fatalf("fields: %v", err)
	}
	if len(fields) == 0 {
		t.Fatal("no fields were decoded")
	}

	summary, ok := fieldByID(fields, "summary")
	if !ok {
		t.Fatal("the summary field is missing from the decoded metadata")
	}
	if summary.Name != "Summary" {
		t.Errorf("summary is named %q, want %q", summary.Name, "Summary")
	}
	if summary.Custom {
		t.Error("summary was decoded as a custom field")
	}
	if summary.Schema != "string" {
		t.Errorf("summary has schema %q, want %q", summary.Schema, "string")
	}
}

// A configured column names a custom field by the name a user sees; only the
// id it decodes to can be sent to the search endpoint.
func TestFieldsCarriesCustomFieldNamesAndClauses(t *testing.T) {
	srv := newFixtureServer(t, map[string]string{"/rest/api/3/field": "fields.200"})
	fields, err := newClient(t, srv.URL).Fields(context.Background())
	if err != nil {
		t.Fatalf("fields: %v", err)
	}

	var custom []jira.Field
	for _, f := range fields {
		if f.Custom {
			custom = append(custom, f)
		}
	}
	if len(custom) == 0 {
		t.Fatal("no custom fields were decoded")
	}
	for _, f := range custom {
		if f.Name == "" {
			t.Errorf("custom field %s decoded with no name", f.ID)
		}
		if len(f.Clauses) == 0 {
			t.Errorf("custom field %s decoded with no clause names", f.ID)
		}
	}
}

const searchPath = "/rest/api/3/search/jql"

func TestSearchDecodesARecordedResult(t *testing.T) {
	srv := newFixtureServer(t, map[string]string{searchPath: "search.200"})
	got, err := newClient(t, srv.URL).Search(context.Background(), jira.SearchOptions{
		JQL:    "reporter = currentUser() ORDER BY created DESC",
		Fields: []string{"summary", "status", "assignee", "priority", "updated"},
	})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(got.Issues) != 1 {
		t.Fatalf("decoded %d issues, want 1", len(got.Issues))
	}

	issue := got.Issues[0]
	if issue.Key != "KAN-1" {
		t.Errorf("key is %q, want %q", issue.Key, "KAN-1")
	}
	if issue.Summary != "Test" {
		t.Errorf("summary is %q, want %q", issue.Summary, "Test")
	}
	if issue.Status.Name != "In Progress" {
		t.Errorf("status is %q, want %q", issue.Status.Name, "In Progress")
	}
	if issue.Status.Category != "indeterminate" {
		t.Errorf("status category is %q, want %q", issue.Status.Category, "indeterminate")
	}
	if issue.Priority == nil || issue.Priority.Name != "Medium" {
		t.Errorf("priority is %+v, want Medium", issue.Priority)
	}
	if issue.Assignee != nil {
		t.Errorf("assignee is %+v, want nil for an unassigned item", issue.Assignee)
	}
	if issue.Updated.IsZero() {
		t.Error("updated did not decode")
	}
}

// Jira writes offsets without the colon RFC 3339 requires, so the timestamps
// in a genuine payload do not decode as a plain time.Time. The recorded
// fixture is the only reason this is known rather than discovered in the wild.
func TestSearchDecodesJiraTimestamps(t *testing.T) {
	srv := newFixtureServer(t, map[string]string{searchPath: "search.200"})
	got, err := newClient(t, srv.URL).Search(context.Background(), jira.SearchOptions{JQL: "x"})
	if err != nil {
		t.Fatalf("search: %v", err)
	}

	updated := got.Issues[0].Updated.UTC().Format("2006-01-02T15:04:05")
	if updated != "2026-08-25T10:45:32" {
		t.Errorf("updated decoded as %s, want the +0530 timestamp in UTC", updated)
	}
}

// Fetching every field is the biggest avoidable cost on a list load, so the
// request has to carry the field selection it was given.
func TestSearchRequestsOnlyTheGivenFields(t *testing.T) {
	srv := newFixtureServer(t, map[string]string{searchPath: "search.200"})
	_, err := newClient(t, srv.URL).Search(context.Background(), jira.SearchOptions{
		JQL:        "reporter = currentUser()",
		Fields:     []string{"summary", "customfield_10016"},
		MaxResults: 50,
	})
	if err != nil {
		t.Fatalf("search: %v", err)
	}

	q := srv.Requests[0].URL.Query()
	if got := q.Get("fields"); got != "summary,customfield_10016" {
		t.Errorf("the request asked for fields %q, want %q", got, "summary,customfield_10016")
	}
	if got := q.Get("jql"); got != "reporter = currentUser()" {
		t.Errorf("the request ran %q", got)
	}
	if got := q.Get("maxResults"); got != "50" {
		t.Errorf("the request asked for maxResults %q, want 50", got)
	}
	if _, ok := q["nextPageToken"]; ok {
		t.Error("a first page carried a continuation token")
	}
}

func TestSearchEmptyResultIsNotAnError(t *testing.T) {
	srv := newFixtureServer(t, map[string]string{searchPath: "searchempty.200"})
	got, err := newClient(t, srv.URL).Search(context.Background(), jira.SearchOptions{JQL: "x"})
	if err != nil {
		t.Fatalf("an empty result set failed: %v", err)
	}
	if len(got.Issues) != 0 {
		t.Errorf("decoded %d issues, want none", len(got.Issues))
	}
	if !got.IsLast {
		t.Error("an empty result set did not report itself as the last page")
	}
}

// The endpoint refuses a query with no search restriction, and its wording is
// the whole of what the status line can tell the user.
func TestSearchSurfacesTheServersRejection(t *testing.T) {
	srv := newFixtureServer(t, map[string]string{searchPath: "searchunbounded.400"})
	_, err := newClient(t, srv.URL).Search(context.Background(), jira.SearchOptions{JQL: "ORDER BY created DESC"})
	if err == nil {
		t.Fatal("a rejected query returned no error")
	}
	if !jira.HasStatus(err, 400) {
		t.Errorf("error %v is not a 400", err)
	}
	if !strings.Contains(err.Error(), "Unbounded JQL queries are not allowed here") {
		t.Errorf("error %q does not repeat what the server said", err)
	}
}

func TestSearchPagesByToken(t *testing.T) {
	srv := newFixtureServer(t, map[string]string{searchPath: "searchpage1.200"})
	client := newClient(t, srv.URL)

	first, err := client.Search(context.Background(), jira.SearchOptions{JQL: "x", MaxResults: 2})
	if err != nil {
		t.Fatalf("first page: %v", err)
	}
	if first.IsLast {
		t.Error("a page with a continuation token reported itself as the last")
	}
	if first.NextPageToken == "" {
		t.Fatal("the continuation token did not decode")
	}

	// The second page is routed to its own fixture, so what comes back is a
	// genuinely different payload rather than the same one replayed.
	srv2 := newFixtureServer(t, map[string]string{searchPath: "searchpage2.200"})
	second, err := newClient(t, srv2.URL).Search(context.Background(), jira.SearchOptions{
		JQL: "x", MaxResults: 2, PageToken: first.NextPageToken,
	})
	if err != nil {
		t.Fatalf("second page: %v", err)
	}
	if got := srv2.Requests[0].URL.Query().Get("nextPageToken"); got != first.NextPageToken {
		t.Errorf("the continuation request carried token %q, want %q", got, first.NextPageToken)
	}
	if !second.IsLast {
		t.Error("the final page did not report itself as the last")
	}
	if len(second.Issues) != 1 || second.Issues[0].Key != "KAN-3" {
		t.Errorf("the second page decoded %d issues, first %q", len(second.Issues), second.Issues[0].Key)
	}
}

// A custom field has no typed home on Issue, so it has to survive decoding in
// Raw or a configured column of one renders blank.
func TestSearchKeepsCustomFieldValues(t *testing.T) {
	srv := newFixtureServer(t, map[string]string{searchPath: "searchpage1.200"})
	got, err := newClient(t, srv.URL).Search(context.Background(), jira.SearchOptions{JQL: "x"})
	if err != nil {
		t.Fatalf("search: %v", err)
	}

	raw := got.Issues[0].Raw
	if raw["customfield_10016"] != 5.0 {
		t.Errorf("customfield_10016 is %#v, want 5", raw["customfield_10016"])
	}
	team, _ := raw["customfield_10001"].(map[string]any)
	if team["name"] != "Platform" {
		t.Errorf("customfield_10001 is %#v, want an object naming Platform", raw["customfield_10001"])
	}
	// A field with a typed home is not also copied into Raw: one of the two is
	// always the stale one.
	if _, ok := raw["summary"]; ok {
		t.Error("summary was kept in Raw as well as decoded")
	}
}

func TestSearchDecodesAnAssignee(t *testing.T) {
	srv := newFixtureServer(t, map[string]string{searchPath: "searchpage1.200"})
	got, err := newClient(t, srv.URL).Search(context.Background(), jira.SearchOptions{JQL: "x"})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if got.Issues[0].Assignee == nil {
		t.Fatal("an assigned item decoded with no assignee")
	}
	if name := got.Issues[0].Assignee.DisplayName; name != "Ada Lovelace" {
		t.Errorf("assignee is %q, want %q", name, "Ada Lovelace")
	}
	if got.Issues[1].Assignee != nil {
		t.Error("an unassigned item decoded with an assignee")
	}
}
