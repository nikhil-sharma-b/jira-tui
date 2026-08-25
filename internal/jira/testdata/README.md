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

`ratelimited.429` and `forbidden.403` are written to Atlassian's documented
error shape rather than captured: provoking a real rate limit against a live
site is antisocial, and the response body Jira returns for one is the same
`errorMessages` envelope as every other error.

To capture more, add a case to `tools/capturefixtures` and run it against your
own site:

    go run ./tools/capturefixtures internal/jira/testdata

Never commit a fixture without scrubbing it first.
