# jt

A fast, vim-native TUI for Jira Cloud.

Built for one workflow: a tmux session per story, several of them open at once.

- **Single static Go binary.** Fast start, small footprint, many sessions.
- **Direct pane addressing.** `gl` / `gd` jump to the list and the detail pane,
  `ga` to the attachments, where `p` previews an image fullscreen in any terminal
  (at full resolution in kitty, ghostty and sixel terminals such as foot, tmux included),
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

`gv` switches the work items pane between the list and a board, with the
query's results as cards. When the results are all in one project, the board
follows that project's Jira board: its columns in its order, empty ones
included, each holding the statuses Jira maps to it. Set `board = <id>` in
config to follow a particular board, for instance when a project has several
or a query spans projects. Without a board to follow, there is a lane per
status, in workflow order (to do, in progress, done). Either way, cards are in
Jira's rank order, as on its own board.

`h` / `l` move between lanes and `j` / `k` within one; `Enter` and every leader
action work on the selected card as they do on a row. `:view board` and
`:view list` pick a view directly, and `view = "board"` in config starts on the
board. The board pages through the whole result set on its own, up to 250 work
items, so that a lane holds every item in its status.

`:filter <text>` narrows the work items pane, list or board, to the rows
containing that text, ignoring case. It matches what `/` would find, but hides
the rest rather than jumping between matches. A bare `:filter` shows every row
again. The filter loads the rest of the result set, up to 250 work items, so
it is not limited to the first page. `R` keeps it; a new query clears it.

In the Links, Subtasks, and Children tabs, use `j` / `k` to select a work item,
`gg` / `G` to select the first or last, and `Enter` to open it. Counts work as
they do for attachments. `<leader>y` copies the selected item's key and
`<leader>Y` copies its URL, and `<leader>o` opens it in your browser.
The default leader is Space. `Ctrl-o` returns to the
previously opened item.

The Comments tab uses the same selection keys. `Enter` opens the selected
comment in your browser, `<leader>y` copies its ID, and `<leader>Y` copies its
direct URL. `<leader>o` also opens the selected comment in your browser.

Comments and description edits open in the configured external editor. Set
`editor = "nvim -f"` in config, or leave it unset to use `$EDITOR`, then `vi`.
The editor command may include quoted flags. `jt` runs it directly, not through
a shell.
