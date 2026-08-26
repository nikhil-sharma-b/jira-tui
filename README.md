# jt

A fast, vim-native TUI for Jira Cloud.

Built for one workflow: a tmux session per story, several of them open at once.

- **Single static Go binary.** Fast start, small footprint, many sessions.
- **Direct pane addressing.** `gl` / `gd` jump to the list and the detail pane,
  while `[` / `]` cycle the detail tabs. `Esc` always returns to normal mode; no widget
  swallows it.
- **Speaks REST directly**, not a CLI subprocess per action, so one keep-alive
  connection serves the session and HTTP status codes stay visible. `acli` remains
  available as an explicit escape hatch: `:!acli jira workitem create --parent %`.
- **Never caches the item you're looking at**, so an AI agent writing to Jira out of
  band can't leave you acting on stale data.

## Status

Scaffolded. See [docs/spec-jt-v1.md](docs/spec-jt-v1.md) for the full design and the
reasoning behind each decision.

## Build

```sh
make install    # -> ~/.local/bin/jt
```

## Configure

Copy `config.example.toml` to `~/.config/jt/config.toml` and fill in your site.
Create an API token at <https://id.atlassian.com/manage-profile/security/api-tokens>.

The token is resolved in this order: the `JIRA_API_TOKEN` environment variable,
then `site.token_command` (run once per process, held in memory only), then a
plaintext `site.token`. A plaintext token requires `chmod 600` on the config file.

```sh
jt auth check    # config, credential and connectivity, reported separately
```

## Use

```sh
jt PROJ-123      # pinned to a work item
jt               # $JT_ISSUE, else inferred from git branch, else your open work
```

Press `?` for the keymap.

Comments and description edits open in the configured external editor. Set
`editor = "nvim -f"` in config, or leave it unset to use `$EDITOR`, then `vi`.
The editor command may include quoted flags. `jt` runs it directly, not through
a shell.
