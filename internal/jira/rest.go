package jira

import (
	"context"
	"net/http"
	"time"
)

// Config configures a REST client. Credentials are resolved by
// internal/config before they arrive here.
type Config struct {
	// SiteURL is the Jira Cloud base, e.g. https://acme.atlassian.net
	SiteURL string
	Email   string
	Token   string

	// MaxConcurrent bounds in-flight requests per process. Several tmux
	// sessions share one rate-limit budget; a small per-process ceiling keeps
	// each session's load predictable without cross-process coordination.
	MaxConcurrent int

	// MaxRetries bounds backoff attempts on retryable reads.
	MaxRetries int

	// Now is injectable so backoff is tested with a fake clock rather than by
	// sleeping.
	Now func() time.Time

	HTTPClient *http.Client
}

const (
	DefaultMaxConcurrent = 4
	DefaultMaxRetries    = 3

	// apiRead is REST v3: rich text arrives as ADF, a JSON tree we render
	// without a parser.
	apiRead = "/rest/api/3"
	// apiWrite is REST v2: rich text is a plain wiki-markup string, so we
	// never build ADF.
	apiWrite = "/rest/api/2"
)

// REST is the Client implementation. One instance holds one keep-alive
// connection pool for the session, which is the whole latency argument against
// spawning a CLI per action.
type REST struct {
	cfg  Config
	http *http.Client
	sem  chan struct{}
}

// NewREST builds a client. It performs no I/O.
func NewREST(cfg Config) (*REST, error) {
	panic("not implemented")
}

func (c *REST) Search(ctx context.Context, opts SearchOptions) (*SearchResult, error) {
	panic("not implemented")
}

func (c *REST) Issue(ctx context.Context, key string, fields []string) (*Issue, error) {
	panic("not implemented")
}

func (c *REST) Comments(ctx context.Context, key string) ([]Comment, error) {
	panic("not implemented")
}

func (c *REST) Transitions(ctx context.Context, key string) ([]Transition, error) {
	panic("not implemented")
}

func (c *REST) AddComment(ctx context.Context, key, body string) (*Comment, error) {
	panic("not implemented")
}

func (c *REST) SetDescription(ctx context.Context, key, body string) error {
	panic("not implemented")
}

func (c *REST) Transition(ctx context.Context, key, transitionID string) error {
	panic("not implemented")
}

func (c *REST) Assign(ctx context.Context, key, accountID string) error {
	panic("not implemented")
}

func (c *REST) SearchUsers(ctx context.Context, key, query string) ([]User, error) {
	panic("not implemented")
}

func (c *REST) DownloadAttachment(ctx context.Context, attachmentID string, dst Writer) (int64, error) {
	panic("not implemented")
}

func (c *REST) Fields(ctx context.Context) ([]Field, error) {
	panic("not implemented")
}

func (c *REST) Myself(ctx context.Context) (*User, error) {
	panic("not implemented")
}

func (c *REST) BrowseURL(key string) string {
	panic("not implemented")
}

var _ Client = (*REST)(nil)
