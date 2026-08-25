package jira_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/nikhil-sharma-b/jira-tui/internal/jira"
)

// newClient is the default client for tests that are not about transport
// tuning. Backoff waits expire at once: no test in this package is about how
// long a retry really takes -- the ones concerned with timing inspect the delay
// a retry asks for -- so the suite must never actually wait.
func newClient(t *testing.T, siteURL string) *jira.REST {
	t.Helper()
	return newTunedClient(t, siteURL, jira.Config{After: expireImmediately})
}

func TestNewRESTRejectsIncompleteConfig(t *testing.T) {
	tests := []struct {
		name string
		cfg  jira.Config
		want string
	}{
		{"no site", jira.Config{Email: "a@example.com", Token: "t"}, "site URL"},
		{"site is not a url", jira.Config{SiteURL: "example.atlassian.net", Email: "a@example.com", Token: "t"}, "site URL"},
		{"no email", jira.Config{SiteURL: "https://example.atlassian.net", Token: "t"}, "email"},
		{"no token", jira.Config{SiteURL: "https://example.atlassian.net", Email: "a@example.com"}, "token"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := jira.NewREST(tt.cfg)
			if err == nil {
				t.Fatal("NewREST succeeded, want an error")
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Errorf("error %q does not mention %q", err, tt.want)
			}
		})
	}
}

func TestMyselfReturnsTheAuthenticatedAccount(t *testing.T) {
	srv := newFixtureServer(t, map[string]string{"/rest/api/3/myself": "myself.200"})
	c := newClient(t, srv.URL)

	got, err := c.Myself(context.Background())
	if err != nil {
		t.Fatalf("Myself: %v", err)
	}
	if want := "Example User"; got.DisplayName != want {
		t.Errorf("DisplayName = %q, want %q", got.DisplayName, want)
	}
	if got.AccountID == "" {
		t.Error("AccountID is empty")
	}
	if !got.Active {
		t.Error("Active = false, want true")
	}

	if len(srv.Requests) != 1 {
		t.Fatalf("sent %d requests, want 1", len(srv.Requests))
	}
	req := srv.Requests[0]
	user, pass, ok := req.BasicAuth()
	if !ok {
		t.Fatal("request carried no basic auth")
	}
	if user != "someone@example.com" || pass != "a-token" {
		t.Errorf("basic auth = %q/%q, want the configured email and token", user, pass)
	}
	if got, want := req.Header.Get("Accept"), "application/json"; got != want {
		t.Errorf("Accept = %q, want %q", got, want)
	}
}

func TestMyselfUsesReadAPIVersion(t *testing.T) {
	// Reads go to v3 so rich text arrives as ADF. A route registered only at
	// v3 fails the test if the client asks for v2.
	srv := newFixtureServer(t, map[string]string{"/rest/api/3/myself": "myself.200"})
	if _, err := newClient(t, srv.URL).Myself(context.Background()); err != nil {
		t.Fatalf("Myself: %v", err)
	}
}

func TestIssueRequestsTheNamedFieldsFromV3(t *testing.T) {
	srv := newFixtureServer(t, map[string]string{"/rest/api/3/issue/ENG-1": "notfound.404"})
	_, _ = newClient(t, srv.URL).Issue(context.Background(), "ENG-1", []string{"summary", "description"})

	if len(srv.Requests) != 1 {
		t.Fatalf("sent %d requests, want 1", len(srv.Requests))
	}
	req := srv.Requests[0]
	if got := req.URL.Query().Get("fields"); got != "summary,description" {
		t.Errorf("fields = %q, want summary,description", got)
	}
}

func TestCommentsDecodesOnePageFromV3(t *testing.T) {
	srv := newFixtureServer(t, map[string]string{"/rest/api/3/issue/ENG-1/comment": "comments.200"})

	comments, err := newClient(t, srv.URL).Comments(context.Background(), "ENG-1")
	if err != nil {
		t.Fatalf("Comments: %v", err)
	}
	if len(comments) != 1 {
		t.Fatalf("Comments returned %d comments, want 1", len(comments))
	}
	got := comments[0]
	if got.ID != "10000" || got.Author == nil || got.Author.DisplayName != "Mia Krystof" || got.Author.Active {
		t.Errorf("comment identity = %#v, want inactive Mia Krystof comment 10000", got)
	}
	if want := time.Date(2026, 8, 20, 9, 30, 0, 0, time.UTC); !got.Created.Equal(want) {
		t.Errorf("Created = %v, want %v", got.Created, want)
	}
	if want := time.Date(2026, 8, 21, 10, 45, 0, 0, time.UTC); !got.Updated.Equal(want) {
		t.Errorf("Updated = %v, want %v", got.Updated, want)
	}
	if !strings.Contains(string(got.Body), "The relay has been replaced.") {
		t.Errorf("Body = %s, want the ADF document", got.Body)
	}
	if got := srv.Requests[0].URL.Query().Get("orderBy"); got != "created" {
		t.Errorf("orderBy = %q, want created", got)
	}
}

func TestCommentsFetchesEveryPageAndReturnsOldestFirst(t *testing.T) {
	created := []string{
		"2026-08-23T09:30:00.000+0000",
		"2026-08-20T09:30:00.000+0000",
		"2026-08-21T09:30:00.000+0000",
	}
	var starts []int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start, _ := strconv.Atoi(r.URL.Query().Get("startAt"))
		starts = append(starts, start)
		if start >= len(created) {
			t.Fatalf("client requested startAt=%d past the result set", start)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"startAt":    start,
			"maxResults": 1,
			"total":      len(created),
			"comments": []map[string]any{{
				"id":      strconv.Itoa(start + 1),
				"author":  nil,
				"created": created[start],
				"updated": created[start],
				"body":    map[string]any{"type": "doc", "version": 1},
			}},
		})
	}))
	t.Cleanup(srv.Close)

	comments, err := newClient(t, srv.URL).Comments(context.Background(), "ENG-1")
	if err != nil {
		t.Fatalf("Comments: %v", err)
	}
	if len(comments) != 3 {
		t.Fatalf("Comments returned %d comments, want 3", len(comments))
	}
	if got, want := starts, []int{0, 1, 2}; !equalInts(got, want) {
		t.Errorf("startAt requests = %v, want %v", got, want)
	}
	if got := []string{comments[0].ID, comments[1].ID, comments[2].ID}; strings.Join(got, ",") != "2,3,1" {
		t.Errorf("comment order = %v, want [2 3 1]", got)
	}
	if comments[0].Author != nil {
		t.Errorf("missing author decoded as %#v, want nil", comments[0].Author)
	}
}

func equalInts(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestErrorsCarryStatusAndJiraMessages(t *testing.T) {
	tests := []struct {
		name        string
		fixture     string
		wantStatus  int
		wantMessage string
		wantRetry   time.Duration
	}{
		{
			name:        "rejected credential",
			fixture:     "unauthorized.401",
			wantStatus:  http.StatusUnauthorized,
			wantMessage: "Client must be authenticated",
		},
		{
			name:        "no permission",
			fixture:     "forbidden.403",
			wantStatus:  http.StatusForbidden,
			wantMessage: "do not have permission",
		},
		{
			name:        "missing item",
			fixture:     "notfound.404",
			wantStatus:  http.StatusNotFound,
			wantMessage: "Issue does not exist",
		},
		{
			name:        "rate limited",
			fixture:     "ratelimited.429",
			wantStatus:  http.StatusTooManyRequests,
			wantMessage: "Rate limit exceeded",
			wantRetry:   7 * time.Second,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := newFixtureServer(t, map[string]string{"/rest/api/3/myself": tt.fixture})
			_, err := newClient(t, srv.URL).Myself(context.Background())
			if err == nil {
				t.Fatal("Myself succeeded, want an error")
			}
			var jerr *jira.Error
			if !errors.As(err, &jerr) {
				t.Fatalf("error is %T (%v), want *jira.Error", err, err)
			}
			if jerr.StatusCode != tt.wantStatus {
				t.Errorf("StatusCode = %d, want %d", jerr.StatusCode, tt.wantStatus)
			}
			if !strings.Contains(err.Error(), tt.wantMessage) {
				t.Errorf("error %q does not carry Jira's message %q", err, tt.wantMessage)
			}
			if jerr.RetryAfter != tt.wantRetry {
				t.Errorf("RetryAfter = %v, want %v", jerr.RetryAfter, tt.wantRetry)
			}
			if !jira.HasStatus(err, tt.wantStatus) {
				t.Errorf("HasStatus(%d) = false, want true", tt.wantStatus)
			}
			if jira.HasStatus(err, http.StatusTeapot) {
				t.Error("HasStatus reports a status the server never sent")
			}
			if jira.IsOffline(err) {
				t.Error("IsOffline = true, want false: the site answered")
			}
		})
	}
}

func TestUnreachableSiteIsOfflineNotRejection(t *testing.T) {
	// A server that is closed before use is the cheapest reliable stand-in for
	// a host that cannot be reached.
	srv := httptest.NewServer(http.NotFoundHandler())
	url := srv.URL
	srv.Close()

	_, err := newClient(t, url).Myself(context.Background())
	if err == nil {
		t.Fatal("Myself succeeded, want an error")
	}
	if !jira.IsOffline(err) {
		t.Errorf("IsOffline = false for %v, want true", err)
	}
	if jira.HasStatus(err, http.StatusUnauthorized) {
		t.Error("an unreachable host reported a 401: the credential was never judged")
	}
	var jerr *jira.Error
	if errors.As(err, &jerr) {
		t.Error("unreachable host produced an HTTP error, want an OfflineError")
	}
}

func TestCancelledContextIsNotReportedAsOffline(t *testing.T) {
	srv := newFixtureServer(t, map[string]string{"/rest/api/3/myself": "myself.200"})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := newClient(t, srv.URL).Myself(ctx)
	if err == nil {
		t.Fatal("Myself succeeded, want an error")
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("error %v does not unwrap to context.Canceled", err)
	}
	if jira.IsOffline(err) {
		t.Error("IsOffline = true, want false: the caller cancelled, the site did not fail")
	}
}

func TestBrowseURL(t *testing.T) {
	tests := []struct{ site, want string }{
		{"https://example.atlassian.net", "https://example.atlassian.net/browse/PROJ-1"},
		{"https://example.atlassian.net/", "https://example.atlassian.net/browse/PROJ-1"},
	}
	for _, tt := range tests {
		c := newClient(t, tt.site)
		if got := c.BrowseURL("PROJ-1"); got != tt.want {
			t.Errorf("BrowseURL(%q) = %q, want %q", tt.site, got, tt.want)
		}
	}
}
