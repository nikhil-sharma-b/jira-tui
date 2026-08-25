# jt — a fast, vim-native Jira TUI

## Problem Statement

I work on one Jira story at a time, inside a dedicated tmux session per story, and I
often have several such sessions open at once. The existing option, `jiratui`, fails me
in three ways:

1. **It's slow and heavy.** Python + Textual means a slow cold start and a large
   resident footprint. Running six of them, one per tmux session, is wasteful and
   sluggish.
2. **Its keybindings aren't vim.** They're Textual defaults with `j`/`k` bolted on. The
   specific daily friction is focus: moving between fields and panes requires
   `Tab`/`Shift-Tab` cycling. There is no way to jump directly to the pane I want, and
   no reliable way to *leave* a field once focus is trapped in it.
3. **It isn't built for many concurrent sessions.** Nothing coordinates cache, rate
   limits, or credentials across processes.

Separately, most of my *writes* to Jira originate from an AI agent I use to discuss and
align on a story, not from typing into a TUI. Any tool I use has to stay correct when
something else changed the issue thirty seconds ago.

## Solution

`jt` — a single static Go binary that opens instantly, holds a small resident
footprint, and is driven entirely by modal vim keybindings with **direct pane
addressing instead of focus cycling**.

It speaks to Jira Cloud over REST directly rather than shelling out to `acli` per
action, so interactions are one keep-alive HTTP round trip instead of a process spawn
plus a fresh TLS handshake. `acli` remains available as an explicit escape hatch for
anything `jt` doesn't cover, and as the stable CLI surface my AI agent uses.

Running `jt` in a story's tmux session opens pinned to that story — from an argument,
an environment variable, or inferred from the git branch name. Running bare `jt` opens
my open work. Every pane is reachable by a single chord from anywhere; `Esc` always
returns to normal mode; a text field is only ever entered deliberately.

## User Stories

### Launching and session binding

1. As a developer, I want `jt PROJ-123` to open pinned to that work item, so that a
   story-focused tmux session lands exactly where I'm working.
2. As a developer, I want bare `jt` in a git worktree on branch `feature/PROJ-123-foo`
   to infer and open `PROJ-123`, so that I don't retype the key I'm already standing in.
3. As a developer, I want `$JT_ISSUE` to pin a session, so that my tmux session
   definition can set it once.
4. As a developer, I want an explicit argument to beat the environment variable, which
   beats the branch inference, so that overriding is predictable.
5. As a developer, I want bare `jt` with no pin to open my unresolved assigned work
   ordered by recency, so that the default is useful without configuration.
6. As a developer, I want to override that default query in config, so that I can open
   on a saved filter or a team query instead.
7. As a developer, I want the binary to start in well under a second, so that opening a
   session never feels like a context switch.
8. As a developer, I want to run many sessions concurrently without them interfering,
   so that one tmux session per story is viable.

### Navigation and focus

9. As a vim user, I want `j`/`k` with counts (`5j`) to move in any list, so that motion
   is muscle memory.
10. As a vim user, I want `gg`/`G`/`Ctrl-d`/`Ctrl-u`/`H`/`M`/`L`, so that large lists
    navigate like a buffer.
11. As a vim user, I want `Ctrl-w h` and `Ctrl-w l` to move between panes spatially, so
    that pane movement matches vim windows.
12. As a vim user, I want `Ctrl-w o` to zoom the current pane, so that I can read a long
    description at full width.
13. As a vim user, I want `gl`, `gd`, `gc`, `ga` to jump directly to the list, detail,
    comments and attachments, so that I never cycle focus with Tab to reach a
    known destination.
14. As a vim user, I want `Esc` to return to normal mode from anywhere, with no widget
    ever swallowing it, so that I am never stuck inside a field.
15. As a vim user, I want text input to require an explicit action key to enter, so
    that arriving at a field does not capture my keystrokes.
16. As a vim user, I want `Enter` to open an item and `Ctrl-o`/`Ctrl-i` to move back and
    forward through where I've been, so that drilling into linked items is reversible.
17. As a vim user, I want `/` to search within the loaded pane with `n`/`N`, so that
    in-buffer search means what it means in vim.
18. As a vim user, I want `q` to close the current pane and `:q`/`:qa` to quit, so that
    both behave as they do in vim.
19. As a developer, I want `?` to show a help overlay of the current keymap, so that I
    can learn my own bindings.
20. As a developer, I want every binding declared in config and merged over the
    defaults, so that I can rebind without maintaining the full map.
21. As a developer, I want to unbind a default explicitly, so that I can free a key.

### Reading work items

22. As a developer, I want a list pane of work items with key, summary, status,
    assignee, priority and last-updated, so that I can scan a result set.
23. As a developer, I want to configure which columns appear, so that the list matches
    what I care about.
24. As a developer, I want only the displayed fields fetched from the API, so that
    lists load fast rather than dragging every field down the wire.
25. As a developer, I want the detail pane to render the description with real
    formatting — headings, lists, code blocks, quotes, links, emphasis — so that a
    ticket is readable.
26. As a developer, I want tables in descriptions rendered as tables, so that the half
    of real tickets containing them are legible.
27. As a developer, I want `@`-mentions and status lozenges rendered inline, so that
    tickets don't read as a field of placeholders.
28. As a developer, I want unrecognized rich-text constructs to fall back to their
    plain text rather than an error marker, so that I never silently lose content.
29. As a developer, I want to read comments in the detail view in chronological order
    with author and timestamp, so that I can follow a discussion.
30. As a developer, I want the pinned work item to open full-width with the list
    hidden, so that a story session is dedicated to its story.
31. As a developer, I want `gl` to reveal the list from a pinned view, so that I can
    still browse without relaunching.

### Searching

32. As a developer, I want `:jql <query>` to run an arbitrary JQL query, so that the
    full power of Jira search is one keystroke away.
33. As a developer, I want named saved queries expanded as `:jql @mine`, so that my
    common searches are short.
34. As a developer, I want `:jql` in my command history, so that I can recall and edit
    a previous query.
35. As a developer, I want tab completion on commandline commands, so that I don't
    memorize exact spellings.
36. As a developer, I want search results to appear from cache immediately and refresh
    silently underneath, so that re-running a familiar query feels instant.

### Writing

37. As a developer, I want `<leader>c` to open my `$EDITOR` on a scratch file and post
    its contents as a comment on save-and-quit, so that I compose in the editor I
    actually know.
38. As a developer, I want an empty or unchanged buffer to abort the comment, so that
    quitting the editor is a safe cancel.
39. As a developer, I want `<leader>e` to edit the description the same way, so that
    editing prose uses one consistent mechanism.
40. As a developer, I want `<leader>t` to open a picker of the transitions actually
    available on this item right now, filterable as I type, so that I don't guess
    status names.
41. As a developer, I want `:transition <name>` with completion as an alternative, so
    that a known transition is one line.
42. As a developer, I want to bind my most-used transitions to direct keys in config,
    so that my common status change is a single chord.
43. As a developer, I want a transition that fails because a screen requires fields to
    tell me exactly which fields are missing, so that I know why and what to do.
44. As a developer, I want `<leader>a` to open an assignee picker with search, so that
    reassignment doesn't need an account ID.
45. As a developer, I want a write to never be retried automatically, so that I never
    get a duplicate comment or a double transition.

### Attachments and images

46. As a developer, I want attachments listed with filename and size, so that I know
    what's on the ticket.
47. As a developer, I want `Enter` on an attachment to download it with my credentials
    and open it in my system viewer, so that a screenshot opens in something that can
    actually display it.
48. As a developer, I want inline images in a description shown as a labelled
    placeholder I can select and open, so that the layout stays readable and the image
    is still reachable.
49. As a developer, I want images pasted inline via Atlassian's media service to fail
    with a clear explanation rather than a mysterious error, so that I know it's a
    known limitation and not a bug.

### Freshness, caching, offline

50. As a developer whose AI agent writes to Jira out of band, I want the work item I'm
    looking at to always be fetched live, so that I never act on stale data.
51. As a developer, I want expensive metadata — fields, projects, users — cached for a
    day, so that opening the app doesn't re-fetch everything that rarely changes.
52. As a developer, I want `:cache clear` to drop the cache, so that I have an escape
    hatch when metadata has genuinely changed.
53. As a developer, I want cached lists to remain browsable when the network is down,
    with a clear offline indicator, so that a dropped connection doesn't blank my screen.
54. As a developer, I want `R` to force a reload of the current view, so that I can
    refresh on demand.
55. As a developer running several sessions, I want a shared cache that tolerates
    concurrent readers and writers, so that sessions neither corrupt each other's data
    nor block.

### Reliability

56. As a developer, I want rate-limited or failed reads retried with backoff that
    honours the server's stated delay, so that transient trouble is invisible.
57. As a developer, I want a bounded number of concurrent in-flight requests per
    session, so that my own behaviour is predictable across many sessions.
58. As a developer, I want errors surfaced in a status line rather than a modal, so
    that a failure doesn't interrupt what I'm doing.

### Escape hatches and integration

59. As a developer, I want `:!<cmd>` to run a shell command and show its output, so
    that anything unimplemented is still reachable.
60. As a developer, I want `%` to expand to the current work item key in shell
    commands, so that `:!acli jira workitem create --parent %` just works.
61. As a developer, I want `:acli <args>` as a shorthand, so that the documented CLI is
    always at hand.
62. As a developer, I want `<leader>y` and `<leader>Y` to yank the key and the URL, so
    that I can paste a reference into my agent conversation or a PR.
63. As a developer, I want `<leader>o` to open the item in a browser, so that I can
    reach the parts of Jira a TUI won't cover.
64. As a developer, I want the Jira client usable without the TUI, so that a headless
    subcommand set can be added later without restructuring.

### Configuration and credentials

65. As a developer, I want a single TOML config file in the XDG config directory, so
    that there's one place to look.
66. As a developer, I want my API token supplied by a command like `pass show
    jira/token`, so that my dotfiles can be public.
67. As a developer, I want an environment variable to override that, so that scripting
    and CI are straightforward.
68. As a developer, I want a plain token in config as a last resort, so that getting
    started requires no secret manager.
69. As a developer, I want the token command run once per process and held only in
    memory, so that it isn't written to disk and doesn't slow every request.

## Implementation Decisions

### Language and runtime

- **Go, with bubbletea/lipgloss for the TUI.** Chosen over Python/Textual (what
  `jiratui` uses) for startup time, resident memory, and single-binary distribution —
  all three matter because the target workflow is many concurrent tmux sessions.
  Chosen over Rust/ratatui for velocity.
- Single static binary named **`jt`** — deliberately two characters, since it's typed
  constantly. Module named for the repo, `jira-tui`.
- Config at `~/.config/jt/config.toml`; cache at `~/.cache/jt/cache.db`.

### Talking to Jira: REST, not `acli`

The original framing was "a wrapper for the Atlassian CLI." **That was reversed during
design**, deliberately:

- Every `acli` invocation costs a process spawn plus a fresh TLS handshake before any
  useful work — a floor of roughly 100–300 ms, with no connection reuse. A persistent
  REST client with keep-alive amortizes the handshake across the whole session.
- `acli` surfaces errors as stderr text. It hides HTTP status codes, error bodies, and
  crucially the `Retry-After` header — precisely what's needed to behave well when
  several sessions share one rate-limit budget.
- Its JSON output shape is undocumented and free to change between releases.
- `acli`'s one genuine advantage was owning the credential lifecycle. That advantage
  was already given up when we decided not to parse its private credential store, so
  nothing remained to justify the cost.

`acli` therefore survives as (a) an explicit escape hatch invoked from the commandline,
and (b) the stable CLI contract the user's AI agent already uses. `jt` never depends on
it at runtime.

### API version, split by direction

- **Read via REST v3**, whose rich-text fields are Atlassian Document Format — a JSON
  tree, which is straightforward to walk and render into styled terminal output with no
  parser.
- **Write via REST v2**, still fully supported, whose rich-text fields are plain wiki
  markup strings. A comment becomes "post the user's text as a string."
- This avoids both hard pieces: no markup parser for reading, no ADF builder for
  writing. Resource paths are identical between versions; the seam is a version
  parameter on the client.
- **Jira Cloud only.** Server/Data Center differs in auth and text format and is out of
  scope.

### Modules

- **`internal/jira`** — the Jira Cloud client. Owns transport, auth, retry/backoff,
  concurrency limiting, and typed request/response models. **Contains no TUI types
  whatsoever.** This is the primary test seam and the thing a future headless mode
  reuses unchanged.
- **`internal/adf`** — pure rendering of an ADF document tree into styled lines. No I/O,
  no dependence on the client. Rendering coverage: paragraph, text with marks
  (strong/emphasis/code/strikethrough/link), heading, bullet and ordered lists, code
  block, blockquote, rule, hard break, panel, table, mention, and status. Media,
  expand, and inline cards degrade to one-line placeholders. **Unknown node types
  recurse into their children and emit extracted plain text — never an error marker,
  never dropped content.**
- **`internal/cache`** — SQLite in WAL mode, so N concurrent reader processes and a
  writer coexist without a daemon or a lock dance.
- **`internal/config`** — TOML load, defaults, keymap merge, credential resolution.
- **`internal/ui`** — bubbletea models, panes, keymap dispatch, pickers.
- **`cmd/jt`** — argument parsing, pin resolution, wiring.

### Focus model — the core UX decision

Focus is **directly addressed, never cycled.** This is the direct answer to the primary
complaint about `jiratui`.

- Spatial movement: `Ctrl-w h` / `Ctrl-w l`, plus `Ctrl-w o` to zoom.
- Semantic jumps: `gl` list, `gd` detail, `gc` comments, `ga` attachments — valid from
  anywhere in normal mode.
- **No widget ever consumes `Esc`.** It unconditionally returns to normal mode.
- **Text input is entered only by an explicit action key**, never by arriving at a
  field. This is what makes the modal model hold together.
- Single-letter keys are reserved for **navigation only**. Every action that mutates
  anything, opens an editor, or leaves the app is behind the leader key. Rationale: on
  a list where a count or motion may be half-typed, a bare `c` or `e` triggering an
  editor is an accident waiting to happen.
- Leader is `<space>`.

### Keymap (defaults; all rebindable, merged over these)

```
NORMAL — global
  :          commandline         /        search in pane (n / N)
  Esc        normal mode, from anywhere, always
  q          close pane           :q :qa   quit
  R          reload view          ?        help overlay

MOTION (any list)
  j k        down / up            {count}j accepted (e.g. 5j)
  gg G       top / bottom         Ctrl-d Ctrl-u   half page
  H M L      viewport top / middle / bottom

PANES
  Ctrl-w h / l   move left / right     Ctrl-w o   zoom current
  gl  list       gd  detail       gc  comments    ga  attachments

ACTIONS (focused work item)
  Enter          open detail       Ctrl-o / Ctrl-i   jump back / forward
  <leader>c      comment via $EDITOR
  <leader>e      edit description via $EDITOR
  <leader>t      transition picker
  <leader>a      assign picker
  <leader>y      yank key          <leader>Y   yank URL
  <leader>o      open in browser
  <space>        leader

COMMANDLINE
  :jql <query>       :jql @saved
  :transition <name> :assign <user>
  :cache clear       :!<cmd>       (% expands to current work item key)
```

### Layout

- **Two panes: list on the left, detail on the right.** Comments and attachments are
  sections *inside* detail, reached by `gc` / `ga`, not sibling panes. Three panes is
  unreadable in an 80-column tmux pane.
- When the session is pinned to a work item, detail opens full-width with the list
  hidden; `gl` reveals it.

### Session-to-story binding

Pin resolution precedence: **command-line argument → `$JT_ISSUE` → git branch
inference** from the branch name in the working directory. Bare `jt` with no pin runs
the configured default query, itself defaulting to unresolved items assigned to the
current user, most recently updated first.

### Caching and freshness

The user's AI agent writes to Jira outside this tool, so cache invalidation is a real
hazard. It is **designed away rather than solved**:

- **The focused work item is never cached.** It is one HTTP call on an already-open
  connection; always fetch live.
- **Available transitions are never cached** — they depend on the item's current status
  and the caller's permissions, not just project and type, so a project-level map would
  be quietly wrong. Live-fetch; the POST is authoritative regardless.
- Cached, tiered by volatility: **search results ~60 s**; **field, project, user and
  workflow metadata ~24 h**. Metadata refetching is what makes a TUI feel slow on open,
  and it changes on the order of monthly.
- `:cache clear` is the manual override.
- Storage is SQLite in WAL mode. **No daemon and no cross-process coordination**; each
  session is an independent process. A broker owning all Jira I/O was considered and
  rejected as premature — the right response to actually hitting rate limits, not a
  thing to build first.

### Loading behaviour

Every Jira call is an asynchronous command in the bubbletea message loop; the UI never
blocks.

- **Cached data: stale-while-revalidate.** Render from cache immediately, replace
  silently when the response lands, with a subtle refreshing indicator.
- **Uncached data (the focused item): a spinner in the affected pane only**, with the
  rest of the UI still interactive.

This is what will make `jt` feel faster than the alternative regardless of the
underlying latency.

### Failure behaviour

- **Reads**: retried with exponential backoff on 429 and 5xx, honouring `Retry-After`.
- **Writes**: never retried automatically. A duplicated comment or a double transition
  is a worse outcome than an error message.
- Errors surface in a status line, not a modal.
- Offline: cached data stays browsable, with a persistent offline marker in the status
  line.
- **Concurrency: a per-process semaphore of ~4 in-flight requests.** Enough for a TUI,
  and it makes each session's load predictable. A cross-process shared limiter was
  rejected — a token bucket in SQLite is a lot of machinery for a problem a handful of
  interactive sessions won't reach.

### Credentials

Resolution precedence: **`JIRA_API_TOKEN` environment variable → `token_command` in
config (e.g. `pass show jira/token`) → plaintext token in config**. The config file
holds the site URL and account email. `token_command` is executed **once at process
start and the result held in memory only** — never written to disk, never re-run per
request. Config file expected at mode 0600 when it holds a plaintext token.

**Single site, flat config.** Named profiles were considered and deferred: the schema
change is trivial to make later, and adding it now taxes every code path with a profile
lookup for a capability not yet needed.

### Editor handoff

Comment and description authoring **suspends the TUI and hands off to `$EDITOR`** on a
temporary file; writing and quitting posts the contents, and an unchanged or empty
buffer aborts. No in-TUI multi-line text widget will be built — it is days of work for a
worse result than the editor the user already lives in, in a workflow that is already
inside tmux.

### Images

**No inline image rendering in v1.** Attachments are listed as focusable items showing
filename and size; `Enter` downloads with credentials to a temporary file and hands it
to the system opener. Rationale: the target environment is always tmux, where the kitty
graphics protocol works only through DCS passthrough and does not survive pane redraw
or scroll, because tmux does not track those images as cell content; sixel is native in
tmux 3.4+ but unsupported by one of the terminals in use. And a Jira screenshot at
terminal-cell resolution is unreadable anyway — the real want is almost always a real
viewer.

The attachment list, authenticated download, and system-opener handoff are the parts
with actual value and will be built properly. The renderer will be structured so sixel
can drop in later behind a config setting.

Inline images embedded through Atlassian's Media Services API (as opposed to ordinary
attachments) require a separate token exchange and will report a clear, specific
limitation rather than an opaque failure.

### Build and distribution

Plain `Makefile`, `make install` to `~/.local/bin`, built with symbols stripped, plus a
`jt version` subcommand. Release tooling is ceremony until there is a second user.

## Testing Decisions

**What makes a good test here:** it exercises externally observable behaviour through a
module's public interface, and it would still pass after the internals were rewritten.
Tests that assert on struct internals, unexported helpers, or the exact sequence of
private calls are not wanted. For the TUI, the observable behaviour is *rendered output
and state transitions in response to key messages* — not which model field changed.

**Seams.** Three, only one of which is an injection point:

1. **The `internal/jira` client interface** is *the* seam. Everything above it — UI,
   pin resolution, pickers, commandline — is tested by substituting it. This is the
   highest available seam and is deliberately the same seam that makes a future
   headless mode possible, so it earns its keep twice.
2. **HTTP transport sits beneath the client**, exercised via recorded fixtures. This
   means `internal/jira` itself is tested against genuine recorded payloads rather than
   against a hand-written fake that only encodes what we already believe.
3. **The ADF renderer is a pure function** and is tested directly on captured
   documents. It is not an injection point.

**Per module:**

- **`internal/jira`** — recorded HTTP fixtures captured once against a real Jira Cloud
  instance and committed as test data, with keys, names, emails and tokens scrubbed.
  Covers: search with field selection, work item fetch, transition list and apply,
  comment create, assign, attachment metadata. Also covers **error paths from real
  response bodies** — 401, 403, 404, a 400 from a transition screen requiring fields,
  and a 429 with `Retry-After` — since these are exactly what hand-written fakes get
  wrong. Backoff and the concurrency semaphore are tested with an injected clock rather
  than by sleeping.
- **`internal/adf`** — golden-file tests over captured real documents. Real tickets are
  the only credible source here; nobody hand-writes a convincing ADF table or a
  correctly nested mixed list. Explicit cases for the unknown-node fallback (content is
  preserved as plain text) and for media placeholders.
- **`internal/config`** — keymap merge over defaults, explicit unbinding, credential
  resolution precedence across all three sources, and the token command executing
  exactly once.
- **`internal/cache`** — TTL tiering, and a concurrency test with multiple processes or
  connections reading and writing simultaneously under WAL, since surviving concurrent
  sessions is a stated requirement rather than an assumption.
- **`internal/ui`** — driven by a hand-written fake client implementing the seam from
  (1). Tests feed key messages and assert on rendered output: counted motion, `Esc`
  returning to normal from every mode, direct pane jumps, that no widget swallows
  `Esc`, jumplist behaviour, and that stale-while-revalidate shows cached content
  before the refreshed content arrives.
- **`cmd/jt`** — pin resolution precedence, including branch-name inference against a
  temporary git repository.

**Prior art:** none — this is a greenfield repository. The conventions above are
therefore the prior art for everything that follows, and later work should extend them
rather than introduce a second style.

## Out of Scope

- **Creating work items.** Field screens vary per project and issue type and require
  fetching create metadata and rendering a dynamic form — effectively a second product.
  Delegated to `acli` via the escape hatch.
- **Board and sprint views.** Deferred to a later version. Noted deliberately: the
  primary workflow is pinned to a single story in a tmux session, which barely touches
  a board; browsing happens through JQL.
- **Inline image rendering.** Deferred, with the seam left in place for sixel.
- **A headless subcommand set** (`jt issue view --json` and friends) for the AI agent to
  call. Deferred, but the client module is deliberately structured so that adding it is
  a small amount of work rather than a refactor.
- **A background daemon or broker** owning Jira I/O for all sessions, and any
  cross-process rate limiter. The correct response to actually observing rate limiting,
  not a thing to build up front.
- **Cross-session cache-invalidation push** (a socket poke from an external writer).
  Made unnecessary for v1 by never caching the focused work item.
- **Jira Server / Data Center.**
- **Multiple sites or accounts.**
- **Transition screens requiring fields.** These will report which fields are missing
  and stop; prompting for and submitting them is out of scope.
- **Release engineering** — packaging, signing, distribution.

## Further Notes

- The reversal from "wrapper for the Atlassian CLI" to "REST client that keeps the CLI
  as an escape hatch" is the single most consequential decision here, and it was made
  on the merits during design rather than assumed. If credentials must live *only* in
  `acli`'s store, that decision has to be revisited, because it was justified by the
  fact that we manage the token ourselves.
- Two decisions do most of the work of making this feel fast, and neither is about raw
  request latency: **never caching the focused item** (which deletes the invalidation
  problem instead of solving it) and **stale-while-revalidate on everything cached**
  (which hides the latency that remains). Keep both intact under future changes.
- The rule that **single letters navigate and the leader key acts** is what keeps the
  modal model coherent as features are added. New actions go behind the leader or on the
  commandline. Resist adding a bare-letter action.
- The environment this was designed against: tmux 3.7c on Linux, with ghostty, kitty,
  foot and alacritty all installed — hence the terminal-graphics conclusions above,
  which are specific to that mix and worth re-checking if it changes.

## Status

**Not published to an issue tracker.** No issue-tracker configuration or triage-label
vocabulary was available in the session that produced this spec; run
`/setup-matt-pocock-skills` to wire that up, after which this document should be filed
and labelled `ready-for-agent`.
