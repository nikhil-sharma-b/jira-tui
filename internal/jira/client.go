// Package jira is the Jira Cloud REST client. It deliberately contains no TUI
// types: everything above it is tested by substituting Client, and a future
// headless mode reuses this package unchanged.
//
// Reads go to REST v3, whose rich-text fields are ADF (a JSON tree we can walk
// and render without a parser). Writes go to REST v2, whose rich-text fields
// are plain wiki-markup strings, so posting a comment is posting a string.
// Resource paths are identical between the two; only the version prefix
// differs. This avoids both writing a markup parser and building ADF.
package jira

import "context"

// Client is the seam. Everything above it -- UI, pickers, commandline, pin
// resolution -- depends on this interface and nothing else in this package.
type Client interface {
	// Search runs JQL. Results are cacheable (~60s); the caller decides.
	Search(ctx context.Context, opts SearchOptions) (*SearchResult, error)

	// Issue fetches one work item. Never cache the result: the user's agent
	// writes to Jira out of band, so the focused item is always fetched live.
	Issue(ctx context.Context, key string, fields []string) (*Issue, error)

	// Comments fetches a work item's comments in chronological order.
	Comments(ctx context.Context, key string) ([]Comment, error)

	// Transitions lists the moves available on this item right now. Always
	// live: availability depends on current status and caller permissions.
	Transitions(ctx context.Context, key string) ([]Transition, error)

	// AddComment posts body as wiki markup via v2. Never retried on failure --
	// a duplicated comment is worse than an error message.
	AddComment(ctx context.Context, key, body string) (*Comment, error)

	// SetDescription replaces the description with wiki markup via v2.
	SetDescription(ctx context.Context, key, body string) error

	// Transition applies a workflow move. Returns FieldsRequiredError when the
	// transition screen demands fields we do not collect.
	Transition(ctx context.Context, key, transitionID string) error

	// Assign sets the assignee. An empty accountID unassigns.
	Assign(ctx context.Context, key, accountID string) error

	// SearchUsers finds assignable users for the assignee picker.
	SearchUsers(ctx context.Context, key, query string) ([]User, error)

	// DownloadAttachment writes an attachment's bytes to dst using the
	// session's credentials, returning the number of bytes written.
	DownloadAttachment(ctx context.Context, attachmentID string, dst Writer) (int64, error)

	// Fields lists field metadata, used to resolve configured column names to
	// field IDs. Cacheable for ~24h.
	Fields(ctx context.Context) ([]Field, error)

	// Myself identifies the authenticated account, for currentUser() display
	// and for the default query.
	Myself(ctx context.Context) (*User, error)

	// BrowseURL builds the human-facing URL for a work item.
	BrowseURL(key string) string
}

// Writer is io.Writer, restated so callers need not import io alongside this
// package's own vocabulary.
type Writer interface {
	Write(p []byte) (int, error)
}

// Field is Jira field metadata. Name is what a user writes in config; ID is
// what the API wants.
type Field struct {
	ID      string   `json:"id"`
	Name    string   `json:"name"`
	Custom  bool     `json:"custom"`
	Schema  string   `json:"-"`
	Clauses []string `json:"clauseNames,omitempty"`
}

var _ = (*Field)(nil)
