// Command capturefixtures records raw HTTP responses from a real Jira Cloud
// site so internal/jira is tested against genuine payloads rather than a
// hand-written fake that only encodes what we already believe.
//
// It reads the developer's own ~/.config/jt/config.toml for credentials and
// writes one .http file per case:
//
//	go run ./tools/capturefixtures internal/jira/testdata
//	go run ./tools/capturefixtures /path/to/private/output ISSUE-123
//
// Output is unscrubbed and contains real account details. Scrub it before
// committing -- see internal/jira/testdata/README.md.
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"time"

	"github.com/nikhil-sharma-b/jira-tui/internal/config"
)

// capture is one recorded case. The credential is per-case so that the
// rejected-token fixture is captured alongside the accepted ones.
type capture struct {
	name, path, email, token string
}

func main() {
	if len(os.Args) < 2 || len(os.Args) > 3 {
		fmt.Fprintln(os.Stderr, "usage: capturefixtures DIR [ISSUE-KEY]")
		os.Exit(2)
	}
	dir := os.Args[1]

	path, err := config.DefaultPath()
	check(err)
	cfg, err := config.Load(path)
	check(err)
	token, err := cfg.ResolveToken()
	check(err)
	site := cfg.SiteURL()
	email := cfg.Site.Email

	cases := []capture{
		{"myself.200", "/rest/api/3/myself", email, token},
		{"unauthorized.401", "/rest/api/3/myself", email, "not-a-real-token"},
		{"notfound.404", "/rest/api/3/issue/ZZZZ-99999", email, token},

		// Field metadata resolves configured column names to the ids the search
		// endpoint wants, so the list cannot be built without it.
		{"fields.200", "/rest/api/3/field", email, token},

		// A search that returns work items, over the fields a default column
		// set displays. Reported-by rather than assigned-to, because an account
		// always has reported something and may have nothing assigned.
		{"search.200", searchPath(reportedQuery, "", 3), email, token},

		// A search that matches nothing. The list pane's empty state has to be
		// distinguishable from a failure, and the payloads are what say so.
		{"searchempty.200", searchPath(emptyQuery, "", 3), email, token},

		// The rejection a user is most likely to meet: this endpoint refuses a
		// query with no search restriction, whatever its syntax.
		{"searchunbounded.400", searchPath("ORDER BY created DESC", "", 3), email, token},

		// One item per page, so that a site with only a handful of work items
		// still produces a genuine continuation token.
		{"searchpaged.200", searchPath(anyQuery, "", 1), email, token},
	}
	if len(os.Args) == 3 {
		p := "/rest/api/3/issue/" + url.PathEscape(os.Args[2]) + "?fields=description,comment,attachment"
		cases = append(cases, capture{"adf-issue.200", p, email, token})
	}

	for _, c := range cases {
		fetch(site, dir, c)
	}

	// The second page can only be asked for once the first has been read, so
	// it is a second pass rather than another entry in the table.
	if next := nextPageToken(dir + "/searchpaged.200.http"); next != "" {
		fetch(site, dir, capture{"searchpaged2.200", searchPath(anyQuery, next, 1), email, token})
	} else {
		fmt.Println("searchpaged2.200 skipped: the first page carried no nextPageToken")
	}
}

// The queries are written here rather than taken from config so that what a
// fixture recorded is legible from this file alone. All three are bounded:
// this endpoint rejects a query with no search restriction.
const (
	reportedQuery = `reporter = currentUser() ORDER BY created DESC`
	emptyQuery    = `reporter = currentUser() AND summary ~ "zznosuchsummaryzz" ORDER BY created DESC`
	anyQuery      = `created >= "2000-01-01" ORDER BY created DESC`
)

// searchPath builds a search request the way the list pane does: one page of
// only the fields a default column set displays. Capturing a full page with
// every field would produce a fixture nobody can scrub by hand.
//
// /rest/api/3/search was removed and answers 410; /search/jql replaced it and
// pages by an opaque token rather than by offset.
func searchPath(jql, pageToken string, maxResults int) string {
	q := url.Values{}
	q.Set("jql", jql)
	q.Set("fields", "summary,status,assignee,priority,updated")
	q.Set("maxResults", fmt.Sprint(maxResults))
	if pageToken != "" {
		q.Set("nextPageToken", pageToken)
	}
	return "/rest/api/3/search/jql?" + q.Encode()
}

// nextPageToken digs the continuation token out of an already-recorded
// response, so the follow-up request asks for the page that really comes next.
func nextPageToken(path string) string {
	raw, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	i := bytes.Index(raw, []byte("\r\n\r\n"))
	if i < 0 {
		if i = bytes.Index(raw, []byte("\n\n")); i < 0 {
			return ""
		}
	}
	var body struct {
		NextPageToken string `json:"nextPageToken"`
	}
	if err := json.Unmarshal(bytes.TrimSpace(raw[i:]), &body); err != nil {
		return ""
	}
	return body.NextPageToken
}

// fetch performs one case and writes the raw response.
func fetch(site, dir string, c capture) {
	req, err := http.NewRequest("GET", site+c.path, nil)
	check(err)
	req.SetBasicAuth(c.email, c.token)
	req.Header.Set("Accept", "application/json")

	resp, err := (&http.Client{Timeout: 20 * time.Second}).Do(req)
	check(err)
	dump, err := httputil.DumpResponse(resp, true)
	check(err)
	resp.Body.Close()

	check(os.WriteFile(dir+"/"+c.name+".http", dump, 0o644))
	fmt.Println(c.name, resp.Status)
}

func check(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, "capture:", err)
		os.Exit(1)
	}
}
