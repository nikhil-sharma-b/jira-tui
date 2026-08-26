// Package config loads jt's TOML configuration, resolves credentials, and
// merges user keybindings over the defaults.
package config

import (
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/BurntSushi/toml"
)

// Error reports a configuration problem against the key that caused it. The
// point is that a user reads the key they must fix, not a parser's idea of
// where a byte offset landed.
type Error struct {
	Path string
	Key  string
	Msg  string
}

func (e *Error) Error() string {
	if e.Key == "" {
		return fmt.Sprintf("config %s: %s", e.Path, e.Msg)
	}
	return fmt.Sprintf("config %s: %s: %s", e.Path, e.Key, e.Msg)
}

// Config is the whole of ~/.config/jt/config.toml.
type Config struct {
	Site Site `toml:"site"`

	// DefaultQuery runs when no work item is pinned. Defaults to the current
	// user's unresolved items, most recently updated first.
	DefaultQuery string `toml:"default_query"`

	// SavedQueries back the :jql @name expansion.
	SavedQueries map[string]string `toml:"queries"`

	// Columns names the list columns. Only these fields are requested from the
	// API: fetch-what-you-display is the single biggest lever on list latency,
	// and it means adding a column here automatically fetches it.
	Columns []string `toml:"columns"`

	// Keys merges over the built-in keymap rather than replacing it, so a user
	// rebinding one key does not inherit maintenance of all of them. A binding
	// set to the empty string unbinds that action.
	Keys map[string]string `toml:"keys"`

	// Transitions binds a named workflow transition to a key directly, so the
	// status change done every day is one chord rather than a picker. It is a
	// table of its own rather than more entries in Keys, so that Keys can keep
	// refusing an action it does not know: a misspelled action is a typo, while
	// a transition name is only checkable against the site, live, at the
	// keypress.
	Transitions map[string]string `toml:"transitions"`

	// Leader prefixes every action binding. Single letters stay reserved for
	// navigation.
	Leader string `toml:"leader"`

	// Editor overrides $EDITOR for comment and description authoring.
	Editor string `toml:"editor"`

	// Images selects image handling. "off" lists attachments as openable
	// placeholders; "sixel" is reserved for a later version.
	Images string `toml:"images"`

	Timeouts Timeouts `toml:"timeouts"`

	// path and mode record where this config was read from, so that the
	// plaintext-token permission check can name the file and inspect it
	// without reading it a second time. Zero when the config was not loaded
	// from disk.
	path string
	mode fs.FileMode

	// bindings is the compiled keymap, built during validation so that a
	// collision is reported at load rather than at the keypress.
	bindings *Bindings

	// tokenOnce guards TokenCommand: it runs at most once per loaded config --
	// and jt loads one per process -- with its output living only in token.
	tokenOnce sync.Once
	token     string
	tokenErr  error
}

type Site struct {
	// URL is the Jira Cloud base, e.g. https://acme.atlassian.net
	URL   string `toml:"url"`
	Email string `toml:"email"`

	// TokenCommand is run once at process start and its output held in memory
	// only, so dotfiles can stay public. e.g. "pass show jira/token"
	TokenCommand string `toml:"token_command"`

	// Token is a plaintext fallback. Requires the config file be mode 0600.
	Token string `toml:"token"`
}

type Timeouts struct {
	Request Duration `toml:"request"`
}

// Duration is a time.Duration written the way a human writes one ("15s").
// TOML has no duration type and time.Duration cannot decode itself, so the
// wrapper exists purely to give the decoder somewhere to hang ParseDuration.
type Duration time.Duration

func (d Duration) String() string { return time.Duration(d).String() }

func (d *Duration) UnmarshalText(b []byte) error {
	parsed, err := time.ParseDuration(string(b))
	if err != nil {
		return fmt.Errorf("%q is not a duration such as \"15s\"", b)
	}
	*d = Duration(parsed)
	return nil
}

// DefaultRequestTimeout bounds a single HTTP request. Long enough for a slow
// JQL search, short enough that a wedged connection does not hang the UI.
const DefaultRequestTimeout = 15 * time.Second

// DefaultQuery is what jt shows when nothing is pinned.
const DefaultQuery = "assignee = currentUser() AND resolution = Unresolved ORDER BY updated DESC"

// Defaults returns the built-in configuration, including the full default
// keymap. User config is merged over this.
func Defaults() *Config {
	return &Config{
		DefaultQuery: DefaultQuery,
		SavedQueries: map[string]string{},
		Columns:      []string{"key", "summary", "status", "assignee", "priority", "updated"},
		Keys:         map[string]string{},
		Leader:       " ",
		Images:       "off",
		Timeouts:     Timeouts{Request: Duration(DefaultRequestTimeout)},
	}
}

// Load reads the config file, applies defaults, and validates. It does not
// resolve the token; call ResolveToken separately so credential errors are
// distinguishable from config errors.
func Load(path string) (*Config, error) {
	f, err := os.Open(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, &Error{Path: path, Msg: "no config file; create it with site.url and site.email"}
		}
		return nil, &Error{Path: path, Msg: err.Error()}
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		return nil, &Error{Path: path, Msg: err.Error()}
	}

	// Decoding over the defaults means an absent key keeps its default and a
	// config naming only site.url and site.email is complete.
	cfg := Defaults()
	md, err := toml.NewDecoder(f).Decode(cfg)
	if err != nil {
		return nil, decodeError(path, err)
	}
	if undecoded := md.Undecoded(); len(undecoded) > 0 {
		return nil, &Error{Path: path, Key: undecoded[0].String(), Msg: "unknown key"}
	}

	cfg.path = path
	cfg.mode = info.Mode()

	if err := cfg.validate(); err != nil {
		var cerr *Error
		if errors.As(err, &cerr) {
			cerr.Path = path
		}
		return nil, err
	}
	return cfg, nil
}

// decodeError turns a TOML failure into a message naming the key at fault
// where the decoder knows it, rather than a parse dump. BurntSushi records the
// last key it parsed on every parse error, which is exactly the key a
// type mismatch or a rejected value belongs to.
func decodeError(path string, err error) error {
	var cerr *Error
	if errors.As(err, &cerr) {
		cerr.Path = path
		return cerr
	}
	if key := keyFromDecodeError(err); key != "" {
		return &Error{Path: path, Key: key, Msg: typeMismatchDetail(err)}
	}
	var terr toml.ParseError
	if errors.As(err, &terr) {
		msg := terr.Message
		if terr.Position.Line > 0 && terr.LastKey == "" {
			msg = fmt.Sprintf("line %d: %s", terr.Position.Line, msg)
		}
		return &Error{Path: path, Key: terr.LastKey, Msg: msg}
	}
	return &Error{Path: path, Msg: err.Error()}
}

// keyFromDecodeError digs the key out of BurntSushi's type-mismatch text,
// which is of the form: toml: line 3 (last key "site.url"): <detail>. A type
// mismatch is not a toml.ParseError, so the key is only available as text.
func keyFromDecodeError(err error) string {
	return between(err.Error(), `last key "`, `"`)
}

// typeMismatchDetail is the part after the position prefix, so the message
// reads as an explanation of the key rather than repeating it.
func typeMismatchDetail(err error) string {
	if detail := after(err.Error(), "): "); detail != "" {
		return detail
	}
	return strings.TrimSpace(err.Error())
}

func between(s, open, close string) string {
	rest := after(s, open)
	if rest == "" {
		return ""
	}
	i := strings.Index(rest, close)
	if i < 0 {
		return ""
	}
	return rest[:i]
}

func after(s, marker string) string {
	i := strings.Index(s, marker)
	if i < 0 {
		return ""
	}
	return s[i+len(marker):]
}

func (c *Config) validate() error {
	if c.Site.URL == "" {
		return &Error{Key: "site.url", Msg: "required, e.g. https://acme.atlassian.net"}
	}
	u, err := url.Parse(c.Site.URL)
	if err != nil {
		return &Error{Key: "site.url", Msg: fmt.Sprintf("%q is not a URL", c.Site.URL)}
	}
	if u.Scheme != "https" && u.Scheme != "http" {
		return &Error{Key: "site.url", Msg: fmt.Sprintf("%q must be an http or https URL, e.g. https://acme.atlassian.net", c.Site.URL)}
	}
	if u.Host == "" {
		return &Error{Key: "site.url", Msg: fmt.Sprintf("%q has no host, e.g. https://acme.atlassian.net", c.Site.URL)}
	}
	if c.Site.Email == "" {
		return &Error{Key: "site.email", Msg: "required: the account the API token belongs to"}
	}
	if c.Timeouts.Request <= 0 {
		return &Error{Key: "timeouts.request", Msg: "must be greater than zero"}
	}
	switch c.Images {
	case "off", "sixel":
	default:
		return &Error{Key: "images", Msg: fmt.Sprintf("%q is not one of \"off\", \"sixel\"", c.Images)}
	}
	for name := range c.Keys {
		if !IsAction(name) {
			return &Error{Key: "keys." + name, Msg: "unknown action"}
		}
	}
	for name, binding := range c.Transitions {
		// An unbound transition is not an unbinding: nothing was bound to it in
		// the first place, so an empty value is a line the user meant to finish.
		if strings.TrimSpace(binding) == "" {
			return &Error{Key: "transitions." + name, Msg: "must name a key, e.g. \"<leader>td\""}
		}
		if strings.TrimSpace(name) == "" {
			return &Error{Key: "transitions", Msg: "must name a transition, e.g. \"In Progress\""}
		}
	}
	// Compiling here means a binding that could never fire is a startup
	// error naming both actions, rather than a key that silently does
	// nothing until someone notices.
	if _, err := c.Bindings(); err != nil {
		return err
	}
	if len(c.Columns) == 0 {
		return &Error{Key: "columns", Msg: "must name at least one column"}
	}
	return nil
}

// Keymap is the effective keymap: user bindings merged over the defaults, plus
// the direct transition bindings. They compile together because a key can only
// mean one thing, and a transition bound over an action is a collision the user
// wants told about at load.
func (c *Config) Keymap() Keymap {
	km := DefaultKeymap().Merge(c.Keys)
	for name, binding := range c.Transitions {
		km[TransitionAction(name)] = binding
	}
	return km
}

// Bindings is the compiled effective keymap, which dispatch reads. A config
// that was built rather than loaded compiles on first use.
func (c *Config) Bindings() (*Bindings, error) {
	if c.bindings == nil {
		b, err := Compile(c.Keymap(), c.Leader)
		if err != nil {
			return nil, err
		}
		c.bindings = b
	}
	return c.bindings, nil
}

// SiteURL is the site base with any trailing slash removed, so paths can be
// concatenated onto it.
func (c *Config) SiteURL() string { return strings.TrimRight(c.Site.URL, "/") }

// DefaultPath is the config location under the user's XDG config directory.
func DefaultPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("locating config directory: %w", err)
	}
	return filepath.Join(dir, "jt", "config.toml"), nil
}

// ResolveToken returns the API token, in precedence order: the
// JIRA_API_TOKEN environment variable, then TokenCommand, then the plaintext
// Token. It runs TokenCommand at most once; the caller holds the result in
// memory for the session and never writes it to disk.
func (c *Config) ResolveToken() (string, error) {
	if tok := strings.TrimSpace(os.Getenv("JIRA_API_TOKEN")); tok != "" {
		return tok, nil
	}
	if c.Site.TokenCommand != "" {
		c.tokenOnce.Do(func() { c.token, c.tokenErr = runTokenCommand(c.Site.TokenCommand) })
		return c.token, c.tokenErr
	}
	if c.Site.Token != "" {
		if err := c.checkTokenFilePermissions(); err != nil {
			return "", err
		}
		return strings.TrimSpace(c.Site.Token), nil
	}
	return "", &Error{
		Path: c.path,
		Msg:  "no API token: set $JIRA_API_TOKEN, or site.token_command, or site.token",
	}
}

// checkTokenFilePermissions refuses a plaintext token in a file any other
// account can read. Only the plaintext path is gated: an environment variable
// or a token command never puts the secret on disk in the first place.
func (c *Config) checkTokenFilePermissions() error {
	if c.path == "" {
		return nil
	}
	if c.mode.Perm()&0o077 == 0 {
		return nil
	}
	return &Error{
		Path: c.path,
		Key:  "site.token",
		Msg: fmt.Sprintf("plaintext token in a file readable by others (mode %04o); run: chmod 600 %s",
			c.mode.Perm(), c.path),
	}
}

// runTokenCommand executes the command through a shell so that pipes and
// quoting in the configured string behave the way the user wrote them.
func runTokenCommand(command string) (string, error) {
	cmd := exec.Command("sh", "-c", command)
	cmd.Stderr = os.Stderr
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("site.token_command %q: %w", command, err)
	}
	tok := strings.TrimSpace(string(out))
	if tok == "" {
		return "", fmt.Errorf("site.token_command %q produced no output", command)
	}
	return tok, nil
}
