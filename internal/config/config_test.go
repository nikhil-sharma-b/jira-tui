package config_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nikhil-sharma-b/jira-tui/internal/config"
)

// write puts contents at a temporary config path with the given mode and
// returns the path.
func write(t *testing.T, contents string, mode os.FileMode) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte(contents), mode); err != nil {
		t.Fatal(err)
	}
	// WriteFile is subject to umask; force the mode we asked for.
	if err := os.Chmod(path, mode); err != nil {
		t.Fatal(err)
	}
	return path
}

// minimal is the smallest config that loads. Top-level keys come before the
// first table header, since in TOML everything after [site] belongs to it.
const minimal = `
[site]
url = "https://example.atlassian.net"
email = "someone@example.com"
`

// topLevel returns minimal with extra top-level keys placed ahead of [site].
func topLevel(extra string) string { return extra + minimal }

// inSite returns minimal with extra keys appended inside the [site] table.
func inSite(extra string) string { return minimal + extra }

func TestLoadMinimalConfigGetsDefaults(t *testing.T) {
	cfg, err := config.Load(write(t, minimal, 0o600))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got, want := cfg.Site.URL, "https://example.atlassian.net"; got != want {
		t.Errorf("site.url = %q, want %q", got, want)
	}
	if got, want := time.Duration(cfg.Timeouts.Request), 15*time.Second; got != want {
		t.Errorf("timeouts.request = %v, want %v", got, want)
	}
	if got, want := cfg.Leader, " "; got != want {
		t.Errorf("leader = %q, want %q", got, want)
	}
	if got, want := cfg.Images, "off"; got != want {
		t.Errorf("images = %q, want %q", got, want)
	}
	if len(cfg.Columns) == 0 {
		t.Error("columns = empty, want the default column set")
	}
	if cfg.DefaultQuery == "" {
		t.Error("default_query = empty, want the built-in default")
	}
}

func TestLoadOverridesDefaults(t *testing.T) {
	cfg, err := config.Load(write(t, topLevel(`
leader = ","
images = "sixel"
columns = ["key", "summary"]
default_query = "project = PROJ"

`)+`
[queries]
mine = "assignee = currentUser()"

[timeouts]
request = "3s"

[keys]
down = "e"
help = ""
`, 0o600))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got, want := time.Duration(cfg.Timeouts.Request), 3*time.Second; got != want {
		t.Errorf("timeouts.request = %v, want %v", got, want)
	}
	if got, want := cfg.Leader, ","; got != want {
		t.Errorf("leader = %q, want %q", got, want)
	}
	if got, want := strings.Join(cfg.Columns, ","), "key,summary"; got != want {
		t.Errorf("columns = %q, want %q", got, want)
	}
	if got, want := cfg.SavedQueries["mine"], "assignee = currentUser()"; got != want {
		t.Errorf("queries.mine = %q, want %q", got, want)
	}

	km := config.DefaultKeymap().Merge(cfg.Keys)
	if got, want := km[config.ActionDown], "e"; got != want {
		t.Errorf("down = %q, want %q", got, want)
	}
	if _, ok := km[config.ActionHelp]; ok {
		t.Error("help is still bound, want it unbound by an empty binding")
	}
	if got, want := km[config.ActionUp], "k"; got != want {
		t.Errorf("up = %q, want the default %q", got, want)
	}
	if got, want := config.DefaultKeymap()[config.ActionDown], "j"; got != want {
		t.Errorf("Merge mutated DefaultKeymap: down = %q, want %q", got, want)
	}
}

func TestLoadErrorsNameTheOffendingKey(t *testing.T) {
	tests := []struct {
		name     string
		contents string
		wantKey  string
	}{
		{
			name:     "missing site url",
			contents: "[site]\nemail = \"a@example.com\"\n",
			wantKey:  "site.url",
		},
		{
			name:     "site url is not a url",
			contents: "[site]\nurl = \"example.atlassian.net\"\nemail = \"a@example.com\"\n",
			wantKey:  "site.url",
		},
		{
			name:     "missing email",
			contents: "[site]\nurl = \"https://example.atlassian.net\"\n",
			wantKey:  "site.email",
		},
		{
			name:     "unknown key",
			contents: topLevel("colours = [\"key\"]\n"),
			wantKey:  "colours",
		},
		{
			name:     "unknown action",
			contents: minimal + "\n[keys]\nfly = \"f\"\n",
			wantKey:  "keys.fly",
		},
		{
			name:     "bad duration",
			contents: minimal + "\n[timeouts]\nrequest = \"soon\"\n",
			wantKey:  "timeouts.request",
		},
		{
			name:     "wrong type",
			contents: "[site]\nurl = 42\nemail = \"a@example.com\"\n",
			wantKey:  "site.url",
		},
		{
			name:     "unknown image mode",
			contents: topLevel("images = \"kitty\"\n"),
			wantKey:  "images",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := config.Load(write(t, tt.contents, 0o600))
			if err == nil {
				t.Fatal("Load succeeded, want an error")
			}
			var cerr *config.Error
			if !errors.As(err, &cerr) {
				t.Fatalf("error is %T (%v), want *config.Error", err, err)
			}
			if cerr.Key != tt.wantKey {
				t.Errorf("Key = %q, want %q (message: %v)", cerr.Key, tt.wantKey, err)
			}
		})
	}
}

func TestLoadMissingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "absent.toml")
	_, err := config.Load(path)
	if err == nil {
		t.Fatal("Load succeeded, want an error")
	}
	if !strings.Contains(err.Error(), path) {
		t.Errorf("error %q does not name the path it looked for", err)
	}
}

func TestLoadMalformedTOMLReportsLine(t *testing.T) {
	_, err := config.Load(write(t, "[site\nurl = \n", 0o600))
	if err == nil {
		t.Fatal("Load succeeded, want an error")
	}
	if !strings.Contains(err.Error(), "line") {
		t.Errorf("error %q does not say which line is malformed", err)
	}
}

func TestResolveTokenPrecedence(t *testing.T) {
	script := filepath.Join(t.TempDir(), "token.sh")
	if err := os.WriteFile(script, []byte("#!/bin/sh\necho from-command\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	all := inSite("token_command = \"" + script + "\"\ntoken = \"from-plaintext\"\n")

	tests := []struct {
		name     string
		contents string
		env      string
		want     string
	}{
		{"env beats everything", all, "from-env", "from-env"},
		{"command beats plaintext", all, "", "from-command"},
		{
			name:     "plaintext when nothing else",
			contents: inSite("token = \"from-plaintext\"\n"),
			want:     "from-plaintext",
		},
		{
			name:     "env alone with no token in config",
			contents: minimal,
			env:      "from-env",
			want:     "from-env",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("JIRA_API_TOKEN", tt.env)
			cfg, err := config.Load(write(t, tt.contents, 0o600))
			if err != nil {
				t.Fatalf("Load: %v", err)
			}
			got, err := cfg.ResolveToken()
			if err != nil {
				t.Fatalf("ResolveToken: %v", err)
			}
			if got != tt.want {
				t.Errorf("ResolveToken = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestResolveTokenRunsCommandOnce(t *testing.T) {
	t.Setenv("JIRA_API_TOKEN", "")
	dir := t.TempDir()
	counter := filepath.Join(dir, "runs")
	script := filepath.Join(dir, "token.sh")
	body := "#!/bin/sh\necho run >> " + counter + "\necho from-command\n"
	if err := os.WriteFile(script, []byte(body), 0o700); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(write(t, inSite("token_command = \""+script+"\"\n"), 0o600))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	for i := 0; i < 3; i++ {
		got, err := cfg.ResolveToken()
		if err != nil {
			t.Fatalf("ResolveToken call %d: %v", i, err)
		}
		if got != "from-command" {
			t.Fatalf("ResolveToken call %d = %q, want %q", i, got, "from-command")
		}
	}
	runs, err := os.ReadFile(counter)
	if err != nil {
		t.Fatalf("token command never ran: %v", err)
	}
	if n := strings.Count(string(runs), "run"); n != 1 {
		t.Errorf("token command ran %d times, want 1", n)
	}
}

func TestResolveTokenCommandFailureIsReported(t *testing.T) {
	t.Setenv("JIRA_API_TOKEN", "")
	cfg, err := config.Load(write(t, inSite("token_command = \"exit 3\"\n"), 0o600))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if _, err := cfg.ResolveToken(); err == nil {
		t.Fatal("ResolveToken succeeded, want an error")
	} else if !strings.Contains(err.Error(), "token_command") {
		t.Errorf("error %q does not name token_command", err)
	}
}

func TestResolveTokenRefusesWorldReadablePlaintext(t *testing.T) {
	t.Setenv("JIRA_API_TOKEN", "")
	path := write(t, inSite("token = \"from-plaintext\"\n"), 0o644)
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	tok, err := cfg.ResolveToken()
	if err == nil {
		t.Fatal("ResolveToken succeeded, want a refusal")
	}
	if tok != "" {
		t.Error("ResolveToken returned a token alongside its refusal")
	}
	if !strings.Contains(err.Error(), "chmod 600") {
		t.Errorf("error %q does not say how to tighten the permissions", err)
	}
}

func TestResolveTokenIgnoresFilePermissionsWhenSecretIsNotOnDisk(t *testing.T) {
	t.Setenv("JIRA_API_TOKEN", "from-env")
	cfg, err := config.Load(write(t, minimal, 0o644))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	got, err := cfg.ResolveToken()
	if err != nil {
		t.Fatalf("ResolveToken: %v", err)
	}
	if got != "from-env" {
		t.Errorf("ResolveToken = %q, want %q", got, "from-env")
	}
}

func TestResolveTokenWithNoSourceExplainsAllThree(t *testing.T) {
	t.Setenv("JIRA_API_TOKEN", "")
	cfg, err := config.Load(write(t, minimal, 0o600))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	_, err = cfg.ResolveToken()
	if err == nil {
		t.Fatal("ResolveToken succeeded, want an error")
	}
	for _, want := range []string{"JIRA_API_TOKEN", "token_command", "site.token"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not mention %s", err, want)
		}
	}
}

func TestDefaultPathIsUnderXDGConfig(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "/xdg")
	got, err := config.DefaultPath()
	if err != nil {
		t.Fatalf("DefaultPath: %v", err)
	}
	if want := "/xdg/jt/config.toml"; got != want {
		t.Errorf("DefaultPath = %q, want %q", got, want)
	}
}
