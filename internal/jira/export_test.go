package jira

import "context"

// WriteForTest exercises the transport's write path from the external test
// package. No Client method issues a write yet -- those arrive with the
// tickets that need them -- but the rule that a write is never retried is a
// property of this package's transport, and it is tested here rather than left
// unverified until its first caller exists.
func (c *REST) WriteForTest(ctx context.Context, path string, body any) error {
	return c.post(ctx, "write", apiWrite, path, body, nil)
}
