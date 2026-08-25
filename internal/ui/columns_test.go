package ui_test

import (
	"strings"
	"testing"
	"time"

	"github.com/nikhil-sharma-b/jira-tui/internal/jira"
	"github.com/nikhil-sharma-b/jira-tui/internal/ui"
)

// siteFields is the field metadata a Jira site returns, trimmed to what the
// column tests need. Custom fields carry their user-facing name and the
// customfield_NNNNN id the API actually wants, which is the whole reason
// resolution exists.
func siteFields() []jira.Field {
	return []jira.Field{
		{ID: "summary", Name: "Summary"},
		{ID: "status", Name: "Status"},
		{ID: "assignee", Name: "Assignee"},
		{ID: "priority", Name: "Priority"},
		{ID: "updated", Name: "Updated"},
		{ID: "issuetype", Name: "Issue Type"},
		{ID: "labels", Name: "Labels"},
		{ID: "customfield_10016", Name: "Story Points", Custom: true},
		{ID: "customfield_10020", Name: "Sprint", Custom: true},
	}
}

func resolve(t *testing.T, names ...string) []ui.Column {
	t.Helper()
	cols, err := ui.ResolveColumns(names, siteFields())
	if err != nil {
		t.Fatalf("resolving columns %v: %v", names, err)
	}
	return cols
}

func TestColumnsResolveConfiguredNamesToFieldIDs(t *testing.T) {
	cols := resolve(t, "key", "summary", "status", "updated")

	got := ui.ColumnFields(cols)
	want := []string{"summary", "status", "updated"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("requested fields are %v, want %v", got, want)
	}
}

// The key is issue envelope data rather than a field, so asking for it must
// not add anything to the fields the search requests.
func TestKeyColumnRequestsNoField(t *testing.T) {
	if got := ui.ColumnFields(resolve(t, "key")); len(got) != 0 {
		t.Errorf("the key column requested fields %v, want none", got)
	}
}

func TestCustomFieldColumnResolvesToItsAPIID(t *testing.T) {
	cols := resolve(t, "Story Points")

	if got := ui.ColumnFields(cols); strings.Join(got, ",") != "customfield_10016" {
		t.Errorf("requested fields are %v, want customfield_10016", got)
	}
	if cols[0].Title != "Story Points" {
		t.Errorf("column title is %q, want %q", cols[0].Title, "Story Points")
	}
}

func TestColumnNamesResolveWhateverTheirCase(t *testing.T) {
	cols := resolve(t, "SUMMARY", "story points")

	if got := ui.ColumnFields(cols); strings.Join(got, ",") != "summary,customfield_10016" {
		t.Errorf("requested fields are %v, want summary,customfield_10016", got)
	}
}

// A misspelled column is a config mistake, and the user can only fix it if the
// message repeats the name they wrote.
func TestUnknownColumnIsReportedWithTheOffendingName(t *testing.T) {
	_, err := ui.ResolveColumns([]string{"summary", "Storey Points"}, siteFields())
	if err == nil {
		t.Fatal("an unknown column name was accepted")
	}
	if !strings.Contains(err.Error(), `"Storey Points"`) {
		t.Errorf("error %q does not name the offending column", err)
	}
}

func TestAmbiguousColumnNameIsReportedWithBothCandidates(t *testing.T) {
	fields := append(siteFields(), jira.Field{ID: "customfield_10099", Name: "Story Points", Custom: true})

	_, err := ui.ResolveColumns([]string{"Story Points"}, fields)
	if err == nil {
		t.Fatal("a name matching two custom fields was accepted")
	}
	for _, want := range []string{"customfield_10016", "customfield_10099"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not name candidate %s", err, want)
		}
	}
}

func TestColumnsRenderTypedAndCustomValues(t *testing.T) {
	now := time.Date(2026, 8, 25, 12, 0, 0, 0, time.UTC)
	issue := &jira.Issue{
		Key:      "ENG-42",
		Summary:  "Rate limiter drops the first request",
		Status:   jira.Status{Name: "In Progress"},
		Assignee: &jira.User{DisplayName: "Ada Lovelace"},
		Priority: &jira.Priority{Name: "High"},
		Updated:  now.Add(-3 * time.Hour),
		Raw:      map[string]any{"customfield_10016": 5.0},
	}

	cases := []struct{ column, want string }{
		{"key", "ENG-42"},
		{"summary", "Rate limiter drops the first request"},
		{"status", "In Progress"},
		{"assignee", "Ada Lovelace"},
		{"priority", "High"},
		{"updated", "3h"},
		{"Story Points", "5"},
	}
	for _, c := range cases {
		col := resolve(t, c.column)[0]
		if got := col.Render(issue, now); got != c.want {
			t.Errorf("column %q rendered %q, want %q", c.column, got, c.want)
		}
	}
}

// An unset field reads as a word rather than a blank cell, so a row of empty
// columns is not mistaken for a failed fetch.
func TestUnassignedRendersAsAWord(t *testing.T) {
	col := resolve(t, "assignee")[0]
	if got := col.Render(&jira.Issue{}, time.Now()); got != "Unassigned" {
		t.Errorf("an issue with no assignee rendered %q, want %q", got, "Unassigned")
	}
}

// Custom fields arrive as whatever JSON shape their type uses; a select list is
// an object with a value, so a column of one must not render Go's map syntax.
func TestCustomFieldObjectRendersItsValue(t *testing.T) {
	col := resolve(t, "Sprint")[0]
	issue := &jira.Issue{Raw: map[string]any{
		"customfield_10020": []any{map[string]any{"name": "Sprint 14"}},
	}}
	if got := col.Render(issue, time.Now()); got != "Sprint 14" {
		t.Errorf("a sprint field rendered %q, want %q", got, "Sprint 14")
	}
}
