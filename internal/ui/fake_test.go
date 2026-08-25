package ui_test

import (
	"context"
	"fmt"
	"strconv"
	"sync"
	"time"

	"github.com/nikhil-sharma-b/jira-tui/internal/jira"
)

// fakeClient is the hand-written stand-in for the Jira client seam. The UI
// depends on nothing else from that package, so substituting this is the whole
// of what UI tests need to fake -- no HTTP, no transport, no fixtures.
type fakeClient struct {
	mu sync.Mutex

	fields    []jira.Field
	fieldsErr error

	// issues is the whole result set; Search pages through it so that
	// pagination is exercised against a client that really does return one
	// page at a time.
	issues    []jira.Issue
	searchErr error

	// block, when non-nil, holds every Search until it is closed. It is how a
	// test observes what happens while a request is in flight.
	block chan struct{}

	// Searches records every request the UI made, which is how field
	// selection and duplicate paging are asserted on.
	Searches []jira.SearchOptions
}

func (c *fakeClient) Fields(ctx context.Context) ([]jira.Field, error) {
	if c.fieldsErr != nil {
		return nil, c.fieldsErr
	}
	if c.fields != nil {
		return c.fields, nil
	}
	return siteFields(), nil
}

func (c *fakeClient) Search(ctx context.Context, opts jira.SearchOptions) (*jira.SearchResult, error) {
	c.mu.Lock()
	c.Searches = append(c.Searches, opts)
	block := c.block
	c.mu.Unlock()

	if block != nil {
		<-block
	}
	if c.searchErr != nil {
		return nil, c.searchErr
	}

	// The token is the offset spelled opaquely, which is enough to exercise
	// the caller: what matters is that it round-trips and that the caller
	// never invents one.
	start := 0
	if opts.PageToken != "" {
		n, err := strconv.Atoi(opts.PageToken)
		if err != nil {
			return nil, fmt.Errorf("the caller invented a page token: %q", opts.PageToken)
		}
		start = n
	}
	size := opts.MaxResults
	if size <= 0 {
		size = len(c.issues)
	}
	start = min(start, len(c.issues))
	end := min(start+size, len(c.issues))

	result := &jira.SearchResult{
		Issues: append([]jira.Issue(nil), c.issues[start:end]...),
		IsLast: end >= len(c.issues),
	}
	if !result.IsLast {
		result.NextPageToken = strconv.Itoa(end)
	}
	return result, nil
}

// requests reports how many searches have been made, safely against a search
// still parked on block.
func (c *fakeClient) requests() []jira.SearchOptions {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]jira.SearchOptions(nil), c.Searches...)
}

// The rest of the seam is unreached by the list pane. They fail loudly rather
// than returning a zero value, so a test that starts depending on one says so.
func (c *fakeClient) Issue(context.Context, string, []string) (*jira.Issue, error) {
	panic("ui list pane called Issue")
}
func (c *fakeClient) Comments(context.Context, string) ([]jira.Comment, error) {
	panic("ui list pane called Comments")
}
func (c *fakeClient) Transitions(context.Context, string) ([]jira.Transition, error) {
	panic("ui list pane called Transitions")
}
func (c *fakeClient) AddComment(context.Context, string, string) (*jira.Comment, error) {
	panic("ui list pane called AddComment")
}
func (c *fakeClient) SetDescription(context.Context, string, string) error {
	panic("ui list pane called SetDescription")
}
func (c *fakeClient) Transition(context.Context, string, string) error {
	panic("ui list pane called Transition")
}
func (c *fakeClient) Assign(context.Context, string, string) error {
	panic("ui list pane called Assign")
}
func (c *fakeClient) SearchUsers(context.Context, string, string) ([]jira.User, error) {
	panic("ui list pane called SearchUsers")
}
func (c *fakeClient) DownloadAttachment(context.Context, string, jira.Writer) (int64, error) {
	panic("ui list pane called DownloadAttachment")
}
func (c *fakeClient) Myself(context.Context) (*jira.User, error) {
	return &jira.User{AccountID: "acct-1", DisplayName: "Test User"}, nil
}
func (c *fakeClient) BrowseURL(key string) string {
	return "https://example.atlassian.net/browse/" + key
}

var _ jira.Client = (*fakeClient)(nil)

// sampleIssues builds n issues that are distinguishable on sight, so an
// assertion about which row is selected reads as a key rather than an index.
func sampleIssues(n int) []jira.Issue {
	base := time.Date(2026, 8, 25, 12, 0, 0, 0, time.UTC)
	issues := make([]jira.Issue, n)
	for i := range issues {
		issues[i] = jira.Issue{
			Key:      fmt.Sprintf("ENG-%d", i+1),
			Summary:  fmt.Sprintf("Work item number %d", i+1),
			Status:   jira.Status{Name: "In Progress"},
			Assignee: &jira.User{DisplayName: "Ada Lovelace"},
			Priority: &jira.Priority{Name: "High"},
			Updated:  base.Add(-time.Duration(i+1) * time.Hour),
		}
	}
	return issues
}
