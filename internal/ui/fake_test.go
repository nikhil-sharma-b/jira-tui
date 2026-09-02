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
	// searchFor answers one JQL query with its own result set, which is how a
	// query that is not the list's own -- an epic's children -- is given
	// something other than the rows the list pages through.
	searchFor map[string][]jira.Issue
	// searchErrFor fails only the queries named in it, so one session can run
	// a query that works and then one the server rejects.
	searchErrFor map[string]error

	// Searches records every request the UI made, which is how field
	// selection and duplicate paging are asserted on.
	Searches []jira.SearchOptions
	// fieldCalls counts metadata fetches, which is how a cache hit on the
	// ~24h tier is observed: the call simply does not happen.
	fieldCalls int

	// IssueCalls records live detail fetches. issueErrFor and issueBlock let
	// detail-pane tests exercise failures and cancellation at the Jira seam.
	// issueFor answers a live detail read with something other than the row in
	// issues, which is how a test says "Jira has moved on since the search".
	issueFor    map[string]jira.Issue
	IssueCalls  []string
	IssueFields [][]string
	issueErrFor map[string]error
	issueBlock  bool
	issueStart  chan string

	comments      map[string][]jira.Comment
	commentErrFor map[string]error
	CommentCalls  []string
	commentBlock  bool
	commentStart  chan string

	AddCommentCalls []struct{ Key, Body string }
	addCommentErr   error
	Descriptions    []struct{ Key, Body string }
	descriptionErr  error

	// transitions is what each work item can move to right now. It is answered
	// live on every call, never from anything the UI kept, which is the
	// behaviour the picker is required to have.
	transitions      map[string][]jira.Transition
	transitionsErr   error
	TransitionsCalls []string
	transitionsBlock bool

	// TransitionCalls records the writes, so a test can assert both which
	// transition id was applied and that a failure was not retried.
	TransitionCalls []struct{ Key, ID string }
	transitionErr   error

	// users answers an assignable-user search per query, so a test can make the
	// server return something a local filter over the previous answer could not
	// have produced -- which is how "the search really goes to Jira" is
	// observed at all. A query with no entry falls back to the empty one.
	users       map[string][]jira.User
	usersErr    error
	SearchCalls []struct{ Key, Query string }
	myself      *jira.User
	myselfErr   error
	MyselfCalls int
	AssignCalls []struct{ Key, AccountID string }
	assignErr   error
}

func (c *fakeClient) Fields(ctx context.Context) ([]jira.Field, error) {
	c.mu.Lock()
	c.fieldCalls++
	c.mu.Unlock()

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
	c.mu.Unlock()

	if err := c.searchErrFor[opts.JQL]; err != nil {
		return nil, err
	}
	if c.searchErr != nil {
		return nil, c.searchErr
	}
	if issues, ok := c.searchFor[opts.JQL]; ok {
		return &jira.SearchResult{Issues: append([]jira.Issue(nil), issues...), IsLast: true}, nil
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

// fieldRequests reports how many times the field metadata was fetched.
func (c *fakeClient) fieldRequests() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.fieldCalls
}

// requests reports what searches have been made.
func (c *fakeClient) requests() []jira.SearchOptions {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]jira.SearchOptions(nil), c.Searches...)
}

// The rest of the seam is unreached by the list pane. They fail loudly rather
// than returning a zero value, so a test that starts depending on one says so.
func (c *fakeClient) Issue(ctx context.Context, key string, fields []string) (*jira.Issue, error) {
	c.mu.Lock()
	c.IssueCalls = append(c.IssueCalls, key)
	c.IssueFields = append(c.IssueFields, append([]string(nil), fields...))
	err := c.issueErrFor[key]
	block, started := c.issueBlock, c.issueStart
	var found *jira.Issue
	if issue, ok := c.issueFor[key]; ok {
		found = &issue
	}
	for i := range c.issues {
		if found != nil {
			break
		}
		if c.issues[i].Key == key {
			copy := c.issues[i]
			found = &copy
			break
		}
	}
	c.mu.Unlock()

	if started != nil {
		started <- key
	}
	if block {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	if err != nil {
		return nil, err
	}
	if found == nil {
		return nil, fmt.Errorf("unknown fake issue %s", key)
	}
	return found, nil
}

func (c *fakeClient) requestedIssueFields() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.IssueFields) == 0 {
		return nil
	}
	return append([]string(nil), c.IssueFields[len(c.IssueFields)-1]...)
}

func (c *fakeClient) issueRequests() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]string(nil), c.IssueCalls...)
}
func (c *fakeClient) Comments(ctx context.Context, key string) ([]jira.Comment, error) {
	c.mu.Lock()
	c.CommentCalls = append(c.CommentCalls, key)
	comments := append([]jira.Comment(nil), c.comments[key]...)
	err := c.commentErrFor[key]
	block, started := c.commentBlock, c.commentStart
	c.mu.Unlock()

	if started != nil {
		started <- key
	}
	if block {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	if err != nil {
		return nil, err
	}
	return comments, nil
}

func (c *fakeClient) commentRequests() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]string(nil), c.CommentCalls...)
}
func (c *fakeClient) Transitions(ctx context.Context, key string) ([]jira.Transition, error) {
	c.mu.Lock()
	c.TransitionsCalls = append(c.TransitionsCalls, key)
	available := append([]jira.Transition(nil), c.transitions[key]...)
	err, block := c.transitionsErr, c.transitionsBlock
	c.mu.Unlock()

	if block {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	if err != nil {
		return nil, err
	}
	return available, nil
}

func (c *fakeClient) transitionsRequests() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]string(nil), c.TransitionsCalls...)
}
func (c *fakeClient) AddComment(_ context.Context, key, body string) (*jira.Comment, error) {
	c.mu.Lock()
	c.AddCommentCalls = append(c.AddCommentCalls, struct{ Key, Body string }{key, body})
	err := c.addCommentErr
	c.mu.Unlock()
	if err != nil {
		return nil, err
	}
	return &jira.Comment{ID: "created"}, nil
}
func (c *fakeClient) SetDescription(_ context.Context, key, body string) error {
	c.mu.Lock()
	c.Descriptions = append(c.Descriptions, struct{ Key, Body string }{key, body})
	err := c.descriptionErr
	c.mu.Unlock()
	return err
}
func (c *fakeClient) Transition(_ context.Context, key, transitionID string) error {
	c.mu.Lock()
	c.TransitionCalls = append(c.TransitionCalls, struct{ Key, ID string }{key, transitionID})
	err := c.transitionErr
	c.mu.Unlock()
	return err
}

// Assign records the write and moves the fake's own copy of the work item, so
// that reading it back afterwards shows what was written rather than what the
// UI believes it wrote.
func (c *fakeClient) Assign(_ context.Context, key, accountID string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.AssignCalls = append(c.AssignCalls, struct{ Key, AccountID string }{key, accountID})
	if c.assignErr != nil {
		return c.assignErr
	}
	assignee := c.lookupUser(accountID)
	for i := range c.issues {
		if c.issues[i].Key == key {
			c.issues[i].Assignee = assignee
		}
	}
	if issue, ok := c.issueFor[key]; ok {
		issue.Assignee = assignee
		c.issueFor[key] = issue
	}
	return nil
}

// lookupUser finds the account an assignment names, nil for the empty account
// id that means unassigned. The caller holds the lock.
func (c *fakeClient) lookupUser(accountID string) *jira.User {
	if accountID == "" {
		return nil
	}
	if c.myself != nil && c.myself.AccountID == accountID {
		found := *c.myself
		return &found
	}
	for _, users := range c.users {
		for _, u := range users {
			if u.AccountID == accountID {
				found := u
				return &found
			}
		}
	}
	return &jira.User{AccountID: accountID, DisplayName: accountID}
}

func (c *fakeClient) SearchUsers(_ context.Context, key, query string) ([]jira.User, error) {
	c.mu.Lock()
	users, ok := c.users[query]
	if !ok {
		users = c.users[""]
	}
	users = append([]jira.User(nil), users...)
	err := c.usersErr
	c.SearchCalls = append(c.SearchCalls, struct{ Key, Query string }{key, query})
	c.mu.Unlock()

	if err != nil {
		return nil, err
	}
	return users, nil
}

// searchCalls reports the assignable-user searches that were made, which is
// how debouncing is observed: a request that a later keystroke made pointless
// simply never happens.
func (c *fakeClient) searchCalls() []struct{ Key, Query string } {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]struct{ Key, Query string }(nil), c.SearchCalls...)
}

func (c *fakeClient) assignCalls() []struct{ Key, AccountID string } {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]struct{ Key, AccountID string }(nil), c.AssignCalls...)
}

func (c *fakeClient) DownloadAttachment(context.Context, string, jira.Writer) (int64, error) {
	panic("ui list pane called DownloadAttachment")
}
func (c *fakeClient) Myself(context.Context) (*jira.User, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.MyselfCalls++
	if c.myselfErr != nil {
		return nil, c.myselfErr
	}
	if c.myself != nil {
		found := *c.myself
		return &found, nil
	}
	return &jira.User{AccountID: "acct-1", DisplayName: "Test User"}, nil
}

func (c *fakeClient) myselfCalls() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.MyselfCalls
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
