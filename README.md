# jt

A fast, vim-native TUI for Jira Cloud.

Built for one workflow: a tmux session per story, several of them open at once.

- **Single static Go binary.** Fast start, small footprint, many sessions.
- **Direct pane addressing, never focus cycling.** `gl` / `gd` / `gc` / `ga` jump
  straight to a pane from anywhere. `Esc` always returns to normal mode; no widget
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

## Use

```sh
jt PROJ-123      # pinned to a work item
jt               # $JT_ISSUE, else inferred from git branch, else your open work
```

Press `?` for the keymap.
