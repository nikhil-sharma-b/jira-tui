package jira

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
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

// defaultRequestTimeout bounds one request when the caller supplies no
// HTTPClient of its own. Callers that have read a config set the timeout
// there; this is only a floor so a bare client cannot hang forever.
const defaultRequestTimeout = 15 * time.Second

// NewREST builds a client. It performs no I/O.
func NewREST(cfg Config) (*REST, error) {
	if cfg.SiteURL == "" {
		return nil, errors.New("jira: no site URL")
	}
	u, err := url.Parse(cfg.SiteURL)
	if err != nil || u.Host == "" || (u.Scheme != "https" && u.Scheme != "http") {
		return nil, fmt.Errorf("jira: %q is not a site URL such as https://acme.atlassian.net", cfg.SiteURL)
	}
	if cfg.Email == "" {
		return nil, errors.New("jira: no account email")
	}
	if cfg.Token == "" {
		return nil, errors.New("jira: no API token")
	}

	cfg.SiteURL = strings.TrimRight(cfg.SiteURL, "/")
	client := cfg.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: defaultRequestTimeout}
	}
	return &REST{cfg: cfg, http: client}, nil
}

// get performs an authenticated GET and decodes a JSON body into out. It is
// the only path to the network in this package: keeping the status code, the
// error body and Retry-After in one place is the whole reason this package
// speaks HTTP instead of shelling out to a CLI.
//
// op names the operation for error messages. apiBase selects the REST version:
// apiRead for v3, whose rich text is ADF, apiWrite for v2, whose rich text is
// wiki markup.
func (c *REST) get(ctx context.Context, op, apiBase, path string, query url.Values, out any) error {
	endpoint := c.cfg.SiteURL + apiBase + path
	if len(query) > 0 {
		endpoint += "?" + query.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return fmt.Errorf("%s: %w", op, err)
	}
	req.SetBasicAuth(c.cfg.Email, c.cfg.Token)
	req.Header.Set("Accept", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		// A cancelled context is the caller changing its mind, not the site
		// being unreachable; only the latter is an OfflineError.
		if ctxErr := ctx.Err(); ctxErr != nil {
			return fmt.Errorf("%s: %w", op, ctxErr)
		}
		return &OfflineError{Err: fmt.Errorf("%s: %w", op, err)}
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		return &OfflineError{Err: fmt.Errorf("%s: reading response: %w", op, err)}
	}
	if resp.StatusCode >= 300 {
		return newHTTPError(op, resp, body)
	}
	if out == nil {
		return nil
	}
	if err := json.Unmarshal(body, out); err != nil {
		return fmt.Errorf("%s: decoding response: %w", op, err)
	}
	return nil
}

// maxResponseBytes caps a single response so a misrouted request cannot make
// the process read an unbounded body into memory.
const maxResponseBytes = 32 << 20

// errorBody is Jira's error envelope. Both shapes appear in the wild: an
// errorMessages array for whole-request failures, and an errors object keyed
// by field for validation failures.
type errorBody struct {
	Messages []string          `json:"errorMessages"`
	Fields   map[string]string `json:"errors"`
}

// newHTTPError preserves the status code and whatever Jira said, so callers
// can tell a rejected credential from a missing item from a rate limit.
func newHTTPError(op string, resp *http.Response, body []byte) *Error {
	e := &Error{Op: op, StatusCode: resp.StatusCode}

	var parsed errorBody
	if err := json.Unmarshal(body, &parsed); err == nil {
		e.Messages = parsed.Messages
		e.Fields = parsed.Fields
	}
	if len(e.Messages) == 0 && len(e.Fields) == 0 {
		// Not every failure is the JSON envelope: a 401 from Jira Cloud is a
		// bare sentence of plain text. Repeating what the server said beats
		// inventing a message, so long as it is short enough to be a sentence
		// and not a page of HTML.
		if text := strings.TrimSpace(string(body)); text != "" && len(text) <= 512 && !strings.HasPrefix(text, "<") {
			e.Messages = []string{text}
		} else if text := http.StatusText(resp.StatusCode); text != "" {
			e.Messages = []string{text}
		}
	}
	e.RetryAfter = retryAfter(resp)
	return e
}

// retryAfter reads the server's stated delay. Only the delta-seconds form is
// honoured: Jira Cloud sends that, and guessing at clock skew for the HTTP-date
// form would make backoff less predictable, not more.
func retryAfter(resp *http.Response) time.Duration {
	v := resp.Header.Get("Retry-After")
	if v == "" {
		return 0
	}
	secs, err := strconv.Atoi(strings.TrimSpace(v))
	if err != nil || secs < 0 {
		return 0
	}
	return time.Duration(secs) * time.Second
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
	var u User
	if err := c.get(ctx, "myself", apiRead, "/myself", nil, &u); err != nil {
		return nil, err
	}
	return &u, nil
}

func (c *REST) BrowseURL(key string) string {
	return c.cfg.SiteURL + "/browse/" + key
}

// HasStatus reports that err is an HTTP failure with the given status. It
// exists so callers can tell the failure modes apart -- a rejected credential
// (401), an authenticated account without permission (403) and a site that
// answered but has no such resource (404) each want a different sentence.
func HasStatus(err error, status int) bool {
	var e *Error
	if !errors.As(err, &e) {
		return false
	}
	return e.StatusCode == status
}

// IsOffline reports that Jira could not be reached at all.
func IsOffline(err error) bool {
	var e *OfflineError
	return errors.As(err, &e)
}

var _ Client = (*REST)(nil)
