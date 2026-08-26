# Recorded HTTP fixtures

Each `.http` file is a raw HTTP response: a status line, a few headers, a blank
line, and the body. `fixture_test.go` replays them through `httptest`, so
`internal/jira` is tested against payloads Jira actually sent rather than
against a hand-written fake that only encodes what we already believe.

`myself.200`, `unauthorized.401` and `notfound.404` were captured against a
real Jira Cloud site and then scrubbed: account IDs, display names, email
addresses, avatar URLs and the site host are replaced with `example` values,
and every header except `Content-Type` and `Retry-After` is dropped, since the
rest is CDN and tracing noise that would only make the fixtures churn.

`fields.200`, `search.200`, `searchempty.200` and `searchunbounded.400` were
captured the same way. `search.200` is a bounded `reporter = currentUser()`
query over only the fields a default column set displays, which is what the
list pane really asks for; `searchunbounded.400` is this endpoint refusing a
query with no search restriction, which is the rejection a user meets first.

Note that `search.200` records a timestamp whose offset has no colon. That is
what Jira Cloud sends, it is not RFC 3339, and it is why timestamps are parsed
explicitly rather than decoded straight into a `time.Time`.

`ratelimited.429` and `forbidden.403` are written to Atlassian's documented
error shape rather than captured: provoking a real rate limit against a live
site is antisocial, and the response body Jira returns for one is the same
`errorMessages` envelope as every other error.

`searchpage1.200` and `searchpage2.200` as committed are likewise constructed,
from the issue shape `search.200` recorded. A continuation token only appears
once a query matches more items than one page holds, and the site these were
captured against holds a single work item, so a genuine pair could not be
recorded. They exist to cover `nextPageToken` decoding and the custom-field
values that `Issue.Raw` has to preserve.

`tools/capturefixtures` does capture that pair under the same two names, one
work item per page, and will overwrite the constructed files with genuine ones
when run against a site holding more than one item. Prefer the real capture if
you have such a site.

`comments.200` is constructed from Atlassian's documented v3 comment response.
It covers the ADF body, Jira timestamp format, and an inactive author without
capturing discussion from a real work item.

`transitions.200` is written to Atlassian's documented v3 transitions response
rather than captured: what a real site offers depends on its workflow and on
the capturing account's permissions, so a recording would encode one site's
workflow as though it were the shape. It covers the destination status, its
category, and a transition that presents a screen.

`transitionfields.400` is the rejection a transition screen produces when it
demands fields jt does not collect: the per-field `errors` object, which is
what turns into a `FieldsRequiredError`. It is likewise written to the
documented shape, since provoking one needs a workflow configured for it.

`assignableusers.200` is Atlassian's documented v3 assignable-user response
rather than a capture: who may be assigned to a work item is a property of one
site's permission scheme, and recording it would also mean scrubbing every
colleague out of it again. It covers the account id, and an account whose email
the site's privacy settings withhold -- which must still be selectable, since
the account id is the only identifier an assignment needs.

To capture more, add a case to `tools/capturefixtures` and run it against your
own site:

    go run ./tools/capturefixtures internal/jira/testdata

Never commit a fixture without scrubbing it first.
