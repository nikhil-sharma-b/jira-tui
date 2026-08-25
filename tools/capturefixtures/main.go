// Command capturefixtures records raw HTTP responses from a real Jira Cloud
// site so internal/jira is tested against genuine payloads rather than a
// hand-written fake that only encodes what we already believe.
//
// It reads the developer's own ~/.config/jt/config.toml for credentials and
// writes one .http file per case:
//
//	go run ./tools/capturefixtures internal/jira/testdata
//
// Output is unscrubbed and contains real account details. Scrub it before
// committing -- see internal/jira/testdata/README.md.
package main

import (
	"fmt"
	"net/http"
	"net/http/httputil"
	"os"
	"time"

	"github.com/nikhil-sharma-b/jira-tui/internal/config"
)

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: capturefixtures DIR")
		os.Exit(2)
	}
	path, err := config.DefaultPath()
	check(err)
	cfg, err := config.Load(path)
	check(err)
	token, err := cfg.ResolveToken()
	check(err)

	for _, c := range []struct {
		name, path, email, token string
	}{
		{"myself.200", "/rest/api/3/myself", cfg.Site.Email, token},
		{"unauthorized.401", "/rest/api/3/myself", cfg.Site.Email, "not-a-real-token"},
		{"notfound.404", "/rest/api/3/issue/ZZZZ-99999", cfg.Site.Email, token},
		// Add cases here as later tickets need them; ticket 04 captures real
		// ADF documents this way.
	} {
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
