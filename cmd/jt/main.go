// Command jt is a fast, vim-native TUI for Jira Cloud.
//
// It speaks REST directly rather than shelling out per action: a persistent
// connection amortizes the TLS handshake across the session, and HTTP status
// codes, error bodies and Retry-After stay visible. The Atlassian CLI remains
// available as an explicit escape hatch from the commandline.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	"github.com/nikhil-sharma-b/jira-tui/internal/config"
	"github.com/nikhil-sharma-b/jira-tui/internal/jira"
)

// version is set at build time via -ldflags.
var version = "dev"

const usage = `jt -- a vim-native TUI for Jira Cloud

usage:
  jt [ISSUE-KEY]        open the TUI, pinned to a work item
  jt auth check         verify config, credential and connectivity
  jt version            print the version

flags:
  --config PATH         config file to read instead of the XDG default
`

func main() {
	if err := run(os.Args[1:], os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, "jt:", err)
		os.Exit(1)
	}
}

func run(args []string, stdout, stderr io.Writer) error {
	return runWith(args, stdout, stderr, connect)
}

// runWith is run with its one outward dependency -- how to reach Jira -- passed in,
// so the command is exercised end to end in tests without a network.
func runWith(args []string, stdout, stderr io.Writer, dial connector) error {
	fs := flag.NewFlagSet("jt", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() { fmt.Fprint(stderr, usage) }
	configPath := fs.String("config", "", "config file to read instead of the XDG default")

	words, err := parse(fs, args)
	if err != nil {
		return errors.New("run 'jt --help' for usage")
	}

	switch {
	case len(words) == 0:
		return errors.New("the TUI is not built yet; try 'jt auth check'")
	case words[0] == "version":
		fmt.Fprintln(stdout, "jt", version)
		return nil
	case words[0] == "auth":
		if len(words) < 2 || words[1] != "check" {
			return errors.New("usage: jt auth check")
		}
		return authCheck(context.Background(), stdout, *configPath, dial)
	default:
		return fmt.Errorf("unknown command %q; run 'jt --help' for usage", words[0])
	}
}

// parse splits args into flags and command words, accepting flags before or
// after the subcommand. Go's flag package stops at the first bare word, which
// would make "jt auth check --config X" silently ignore the override.
func parse(fs *flag.FlagSet, args []string) ([]string, error) {
	var words []string
	for {
		if err := fs.Parse(args); err != nil {
			return nil, err
		}
		args = fs.Args()
		if len(args) == 0 {
			return words, nil
		}
		words = append(words, args[0])
		args = args[1:]
	}
}

// connector turns a validated config and a resolved token into a client. It
// is a parameter so authCheck is testable without a network.
type connector func(cfg *config.Config, token string) (jira.Client, error)

// connect is the real connector: one REST client, whose request timeout comes
// from config.
func connect(cfg *config.Config, token string) (jira.Client, error) {
	return jira.NewREST(jira.Config{
		SiteURL:    cfg.SiteURL(),
		Email:      cfg.Site.Email,
		Token:      token,
		HTTPClient: &http.Client{Timeout: time.Duration(cfg.Timeouts.Request)},
	})
}

// authCheck reports on the three things that must be true before anything else
// works, and keeps their failures distinguishable: the config is readable and
// well formed, a credential resolves and is accepted, and the site answers.
func authCheck(ctx context.Context, stdout io.Writer, configPath string, dial connector) error {
	if configPath == "" {
		p, err := config.DefaultPath()
		if err != nil {
			return err
		}
		configPath = p
	}

	cfg, err := config.Load(configPath)
	if err != nil {
		return err
	}
	token, err := cfg.ResolveToken()
	if err != nil {
		return err
	}

	client, err := dial(cfg, token)
	if err != nil {
		return err
	}
	return reportIdentity(ctx, stdout, client, cfg.SiteURL())
}

// reportIdentity prints who the token authenticates as, or explains which of
// the three failure modes happened. The client is a jira.Client so this is
// testable without a network.
func reportIdentity(ctx context.Context, stdout io.Writer, client jira.Client, site string) error {
	me, err := client.Myself(ctx)
	if err != nil {
		switch {
		case jira.IsOffline(err):
			return fmt.Errorf("could not reach %s: %w", site, errors.Unwrap(err))
		case jira.HasStatus(err, http.StatusUnauthorized):
			return fmt.Errorf("%s rejected the credential: %w\n"+
				"     check site.email and the API token; a token is created at "+
				"https://id.atlassian.com/manage-profile/security/api-tokens", site, err)
		case jira.HasStatus(err, http.StatusForbidden):
			return fmt.Errorf("%s accepted the credential but refused the request: %w\n"+
				"     the account is authenticated but lacks permission, or the site "+
				"requires a different login method", site, err)
		case jira.HasStatus(err, http.StatusNotFound):
			// The credential was never judged: a well-formed URL for a host
			// that is not this Jira site answers, but not with an account.
			return fmt.Errorf("%s answered but is not a Jira site: %w\n"+
				"     check site.url", site, err)
		default:
			return err
		}
	}

	fmt.Fprintf(stdout, "authenticated as %s", me.DisplayName)
	if me.Email != "" {
		fmt.Fprintf(stdout, " <%s>", me.Email)
	}
	fmt.Fprintf(stdout, "\nsite %s\n", site)
	return nil
}
