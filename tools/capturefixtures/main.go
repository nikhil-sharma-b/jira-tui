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
	"fmt"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"time"

	"github.com/nikhil-sharma-b/jira-tui/internal/config"
)

func main() {
	if len(os.Args) < 2 || len(os.Args) > 3 {
		fmt.Fprintln(os.Stderr, "usage: capturefixtures DIR [ISSUE-KEY]")
		os.Exit(2)
	}
	path, err := config.DefaultPath()
	check(err)
	cfg, err := config.Load(path)
	check(err)
	token, err := cfg.ResolveToken()
	check(err)

	cases := []struct {
		name, path, email, token string
	}{
		{"myself.200", "/rest/api/3/myself", cfg.Site.Email, token},
		{"unauthorized.401", "/rest/api/3/myself", cfg.Site.Email, "not-a-real-token"},
		{"notfound.404", "/rest/api/3/issue/ZZZZ-99999", cfg.Site.Email, token},
	}
	if len(os.Args) == 3 {
		path := "/rest/api/3/issue/" + url.PathEscape(os.Args[2]) + "?fields=description,comment,attachment"
		cases = append(cases, struct {
			name, path, email, token string
		}{"adf-issue.200", path, cfg.Site.Email, token})
	}

	for _, c := range cases {
		req, err := http.NewRequest("GET", cfg.SiteURL()+c.path, nil)
		check(err)
		req.SetBasicAuth(c.email, c.token)
		req.Header.Set("Accept", "application/json")
		resp, err := (&http.Client{Timeout: 20 * time.Second}).Do(req)
		check(err)
		dump, err := httputil.DumpResponse(resp, true)
		check(err)
		resp.Body.Close()
		check(os.WriteFile(os.Args[1]+"/"+c.name+".http", dump, 0o644))
		fmt.Println(c.name, resp.Status)
	}
}

func check(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, "capture:", err)
		os.Exit(1)
	}
}
