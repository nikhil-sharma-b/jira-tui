# ADF fixture capture

Golden inputs belong here only after scrubbing. Capture a real issue into a
private temporary directory first:

```sh
fixture_dir=$(mktemp -d)
go run ./tools/capturefixtures "$fixture_dir" ISSUE-123
```

The issue must contain a table, a mixed nested list, a code block, a panel,
mentions, and status lozenges. `adf-issue.200.http` contains the issue response.
It is unsafe to commit until every account ID, display name, email, avatar URL,
site hostname, issue key, and project-specific value has been replaced with an
obvious `example` value. Drop HTTP headers except `Content-Type` and
`Retry-After`. Review the staged diff for tokens and personal data before
committing it.

Extract description and comment documents from the scrubbed response as JSON.
Golden output stays ANSI-free by rendering with `Options.Plain`.

`captured.adf.json` came from this process. All prose, links, media IDs,
filenames, collection names, and editor-local IDs were replaced or removed.
The two golden files exercise the same captured tree at 80 and 32 columns.
