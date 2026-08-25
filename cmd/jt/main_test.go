package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nikhil-sharma-b/jira-tui/internal/cache"
	"github.com/nikhil-sharma-b/jira-tui/internal/config"
	"github.com/nikhil-sharma-b/jira-tui/internal/jira"
	"github.com/nikhil-sharma-b/jira-tui/internal/ui"
)

// fakeClient substitutes for the Jira client at its interface, which is the
// seam the whole application is tested through.
type fakeClient struct {
	jira.Client
	me  *jira.User
	err error
}

func (f fakeClient) Myself(context.Context) (*jira.User, error) { return f.me, f.err }

// dialing returns a session that hands back client and swallows the launch,
// so run() is driven end to end without a network and without a terminal.
func dialing(client jira.Client) session {
	return session{
		dial:   func(*config.Config, string) (jira.Client, error) { return client, nil },
		launch: func(ui.Options) error { return nil },
	}
}

// writeConfig puts a loadable config on disk and returns its path.
func writeConfig(t *testing.T, contents string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

const validConfig = `
[site]
url = "https://example.atlassian.net"
email = "someone@example.com"
token = "a-token"
`

func TestVersion(t *testing.T) {
	var out bytes.Buffer
	if err := run([]string{"version"}, &out, &out); err != nil {
		t.Fatalf("run version: %v", err)
	}
	if got := out.String(); !strings.Contains(got, version) {
		t.Errorf("run version printed %q, want the build version %q", got, version)
	}
}

func TestUnknownCommand(t *testing.T) {
	var out bytes.Buffer
	err := run([]string{"frobnicate"}, &out, &out)
	if err == nil {
		t.Fatal("run succeeded, want an error")
	}
	if !strings.Contains(err.Error(), "frobnicate") {
		t.Errorf("error %q does not name the unknown command", err)
	}
}

func TestAuthCheckReportsConfigErrors(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte("[site]\nemail = \"a@example.com\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	err := runWith([]string{"--config", path, "auth", "check"}, &out, &out, dialing(fakeClient{}))
	if err == nil {
		t.Fatal("auth check succeeded, want an error")
	}
	if !strings.Contains(err.Error(), "site.url") {
		t.Errorf("error %q does not name the offending key", err)
	}
}

// TestAuthCheckHonoursConfigFlagAnywhere guards the ordering trap: Go's flag
// package stops at the first bare word, so a --config after the subcommand
// would otherwise be dropped and the user's real config read instead.
func TestAuthCheckHonoursConfigFlagAnywhere(t *testing.T) {
	path := writeConfig(t, validConfig)
	client := fakeClient{me: &jira.User{DisplayName: "Example User", Active: true}}

	for _, args := range [][]string{
		{"--config", path, "auth", "check"},
		{"auth", "check", "--config", path},
		{"auth", "--config", path, "check"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			var out bytes.Buffer
			if err := runWith(args, &out, &out, dialing(client)); err != nil {
				t.Fatalf("run: %v", err)
			}
			if !strings.Contains(out.String(), "Example User") {
				t.Errorf("output %q does not report the authenticated account", out.String())
			}
		})
	}
}

func TestAuthCheckMissingConfigNamesThePath(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "absent.toml")
	var out bytes.Buffer
	err := runWith([]string{"auth", "check", "--config", missing}, &out, &out, dialing(fakeClient{}))
	if err == nil {
		t.Fatal("auth check succeeded, want an error")
	}
	if !strings.Contains(err.Error(), missing) {
		t.Errorf("error %q does not name the config it looked for", err)
	}
}

func TestAuthCheckPrintsAccountAndSite(t *testing.T) {
	var out bytes.Buffer
	client := fakeClient{me: &jira.User{
		DisplayName: "Example User",
		Email:       "someone@example.com",
		AccountID:   "0123456789abcdef01234567",
		Active:      true,
	}}
	err := runWith([]string{"auth", "check", "--config", writeConfig(t, validConfig)}, &out, &out, dialing(client))
	if err != nil {
		t.Fatalf("auth check: %v", err)
	}
	for _, want := range []string{"Example User", "someone@example.com", "https://example.atlassian.net"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("output %q does not contain %q", out.String(), want)
		}
	}
}

func TestAuthCheckDistinguishesFailures(t *testing.T) {
	tests := []struct {
		name string
		err  error
		// want are all the things the message must say.
		want []string
		// notWant guards the distinction: an offline report must not read as a
		// rejection, and a rejection must not read as unreachable.
		notWant string
	}{
		{
			name: "rejected credential",
			err:  &jira.Error{StatusCode: http.StatusUnauthorized, Op: "myself", Messages: []string{"Client must be authenticated to access this resource."}},
			// The status code and Jira's own words survive to the user.
			want:    []string{"rejected the credential", "401", "Client must be authenticated"},
			notWant: "could not reach",
		},
		{
			name:    "authenticated but not permitted",
			err:     &jira.Error{StatusCode: http.StatusForbidden, Op: "myself", Messages: []string{"You do not have permission."}},
			want:    []string{"refused the request", "403"},
			notWant: "rejected the credential",
		},
		{
			name:    "site url points somewhere that is not Jira",
			err:     &jira.Error{StatusCode: http.StatusNotFound, Op: "myself", Messages: []string{"Not Found"}},
			want:    []string{"not a Jira site", "site.url"},
			notWant: "rejected the credential",
		},
		{
			name:    "unreachable host",
			err:     &jira.OfflineError{Err: errors.New("dial tcp: no such host")},
			want:    []string{"could not reach"},
			notWant: "rejected",
		},
		{
			name:    "some other failure",
			err:     &jira.Error{StatusCode: http.StatusInternalServerError, Op: "myself", Messages: []string{"Internal server error"}},
			want:    []string{"500"},
			notWant: "rejected the credential",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var out bytes.Buffer
			err := runWith([]string{"auth", "check", "--config", writeConfig(t, validConfig)},
				&out, &out, dialing(fakeClient{err: tt.err}))
			if err == nil {
				t.Fatal("auth check succeeded, want an error")
			}
			for _, want := range tt.want {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("error %q does not contain %q", err, want)
				}
			}
			if strings.Contains(err.Error(), tt.notWant) {
				t.Errorf("error %q reads as the wrong failure mode (%q)", err, tt.notWant)
			}
			if out.Len() != 0 {
				t.Errorf("wrote %q to stdout on failure, want nothing", out.String())
			}
		})
	}
}

// Bare jt is the ordinary way in: no subcommand, no pin, the configured
// default query on screen.
func TestBareCommandLaunchesTheTUI(t *testing.T) {
	var launched *ui.Options
	s := dialing(fakeClient{})
	s.launch = func(opts ui.Options) error {
		launched = &opts
		return nil
	}

	var out bytes.Buffer
	if err := runWith([]string{"--config", writeConfig(t, validConfig)}, &out, &out, s); err != nil {
		t.Fatalf("bare jt: %v", err)
	}
	if launched == nil {
		t.Fatal("bare jt did not open the TUI")
	}
	if launched.Client == nil {
		t.Error("the TUI was opened without a Jira client")
	}
	if launched.Config == nil {
		t.Fatal("the TUI was opened without a config")
	}
	if launched.Config.Site.URL != "https://example.atlassian.net" {
		t.Errorf("the TUI got site %q, want the configured one", launched.Config.Site.URL)
	}
	if launched.Pin != "" {
		t.Errorf("bare jt pinned %q; nothing asked it to", launched.Pin)
	}
}

// A config that cannot be loaded must be reported as text, not behind an
// alternate screen that is torn down before it can be read.
func TestBareCommandReportsConfigProblemsWithoutOpeningTheTUI(t *testing.T) {
	opened := false
	s := dialing(fakeClient{})
	s.launch = func(ui.Options) error {
		opened = true
		return nil
	}

	var out bytes.Buffer
	err := runWith([]string{"--config", filepath.Join(t.TempDir(), "absent.toml")}, &out, &out, s)
	if err == nil {
		t.Fatal("a missing config did not stop bare jt")
	}
	if opened {
		t.Error("the TUI was opened despite the config failing to load")
	}
}

// The failure the TUI ended on is the process's failure: it is the only way a
// startup error reaches the user once the alternate screen is gone.
func TestBareCommandReportsWhatTheTUIFailedOn(t *testing.T) {
	s := dialing(fakeClient{})
	s.launch = func(ui.Options) error { return errors.New(`columns: "Storey Points" is not a field on this site`) }

	var out bytes.Buffer
	err := runWith([]string{"--config", writeConfig(t, validConfig)}, &out, &out, s)
	if err == nil || !strings.Contains(err.Error(), "Storey Points") {
		t.Errorf("bare jt returned %v, want the failure the TUI ended on", err)
	}
}

// The cache is opened before the UI is built, so a session that reopens a
// familiar query answers it from disk rather than from Jira.
func TestBareCommandOpensTheCacheForTheTUI(t *testing.T) {
	dir := t.TempDir()
	var launched *ui.Options
	s := dialing(fakeClient{})
	s.store = func(io.Writer) *cache.Cache {
		c, err := cache.Open(filepath.Join(dir, "cache.db"))
		if err != nil {
			t.Fatalf("opening the cache: %v", err)
		}
		return c
	}
	s.launch = func(opts ui.Options) error {
		launched = &opts
		return nil
	}

	var out bytes.Buffer
	if err := runWith([]string{"--config", writeConfig(t, validConfig)}, &out, &out, s); err != nil {
		t.Fatalf("bare jt: %v", err)
	}
	if launched == nil || launched.Cache == nil {
		t.Fatal("the TUI was opened without a cache")
	}
}

// A cache that cannot be opened is a slower session, not a failed one.
func TestBareCommandRunsWithoutACache(t *testing.T) {
	var launched *ui.Options
	s := dialing(fakeClient{})
	s.store = func(stderr io.Writer) *cache.Cache {
		fmt.Fprintln(stderr, "jt: running without a cache: nope")
		return nil
	}
	s.launch = func(opts ui.Options) error {
		launched = &opts
		return nil
	}

	var out bytes.Buffer
	if err := runWith([]string{"--config", writeConfig(t, validConfig)}, &out, &out, s); err != nil {
		t.Fatalf("bare jt without a cache: %v", err)
	}
	if launched == nil {
		t.Fatal("an uncacheable session did not open the TUI at all")
	}
	if !strings.Contains(out.String(), "without a cache") {
		t.Errorf("nothing said the session was running uncached:\n%s", out.String())
	}
}
