// Package config loads jt's TOML configuration, resolves credentials, and
// merges user keybindings over the defaults.
package config

import "time"

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

	// Leader prefixes every action binding. Single letters stay reserved for
	// navigation.
	Leader string `toml:"leader"`

	// Editor overrides $EDITOR for comment and description authoring.
	Editor string `toml:"editor"`

	// Images selects image handling. "off" lists attachments as openable
	// placeholders; "sixel" is reserved for a later version.
	Images string `toml:"images"`

	Timeouts Timeouts `toml:"timeouts"`
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
	Request time.Duration `toml:"request"`
}

// Defaults returns the built-in configuration, including the full default
// keymap. User config is merged over this.
func Defaults() *Config {
	panic("not implemented")
}

// Load reads the config file, applies defaults, and validates. It does not
// resolve the token; call ResolveToken separately so credential errors are
// distinguishable from config errors.
func Load(path string) (*Config, error) {
	panic("not implemented")
}

// DefaultPath is the config location under the user's XDG config directory.
func DefaultPath() (string, error) {
	panic("not implemented")
}

// ResolveToken returns the API token, in precedence order: the
// JIRA_API_TOKEN environment variable, then TokenCommand, then the plaintext
// Token. It runs TokenCommand at most once; the caller holds the result in
// memory for the session and never writes it to disk.
func (c *Config) ResolveToken() (string, error) {
	panic("not implemented")
}
