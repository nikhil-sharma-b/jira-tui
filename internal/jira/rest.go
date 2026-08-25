package jira

import (
	"bytes"
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

	// MaxRetries bounds the repeats of a retryable read, not its attempts: two
	// means up to three requests. Zero takes DefaultMaxRetries, so a caller
	// that fills in only a site and a credential still gets sane behaviour; a
	// negative value turns retrying off.
	MaxRetries int

	// After is the sleep seam for backoff, defaulting to time.After. Injecting
	// it lets the suite verify timing by observing the delay a retry asks for,
	// which a clock that only reports the time cannot do -- a backoff wait has
	// to be made to return early, not merely to be measured.
	After func(time.Duration) <-chan time.Time

	HTTPClient *http.Client
}

const (
	DefaultMaxConcurrent = 4
	DefaultMaxRetries    = 3

	// baseBackoff is the first retry delay; each further attempt doubles it,
	// so the default ceiling of three retries spans 500ms, 1s and 2s. That is
	// short enough that a transient 502 stays invisible inside one keystroke's
	// worth of patience, which is the point of retrying at all.
	baseBackoff = 500 * time.Millisecond
	// maxBackoff caps the computed delay. A server's own Retry-After is
	// honoured as stated and is not capped: it is the only party that knows
	// when the rate-limit window reopens.
	maxBackoff = 8 * time.Second

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
	if cfg.MaxConcurrent <= 0 {
		cfg.MaxConcurrent = DefaultMaxConcurrent
	}
	if cfg.MaxRetries < 0 {
		cfg.MaxRetries = 0
	} else if cfg.MaxRetries == 0 {
		cfg.MaxRetries = DefaultMaxRetries
	}
	if cfg.After == nil {
		cfg.After = time.After
	}
	client := cfg.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: defaultRequestTimeout}
	}
	return &REST{
		cfg:  cfg,
		http: client,
		sem:  make(chan struct{}, cfg.MaxConcurrent),
	}, nil
}

// request is one call to Jira, described completely enough that the retry loop
// can repeat it. op names the operation for error messages. apiBase selects the
// REST version: apiRead for v3, whose rich text is ADF, apiWrite for v2, whose
// rich text is wiki markup.
type request struct {
	op      string
	method  string
	apiBase string
	path    string
	query   url.Values
	// body is marshalled as JSON when non-nil.
	body any
	// out receives the decoded response body when non-nil.
	out any
	// retry allows the exponential backoff loop. Reads set it; writes never
	// do, whatever the status code -- a duplicated comment or a double
	// transition is a worse outcome than an error message.
	retry bool
}

// get performs an authenticated GET and decodes a JSON body into out. Reads
// are retryable.
func (c *REST) get(ctx context.Context, op, apiBase, path string, query url.Values, out any) error {
	return c.do(ctx, request{op: op, method: http.MethodGet, apiBase: apiBase, path: path, query: query, out: out, retry: true})
}

// post performs an authenticated POST of a JSON body. Writes are not retried.
func (c *REST) post(ctx context.Context, op, apiBase, path string, body, out any) error {
	return c.do(ctx, request{op: op, method: http.MethodPost, apiBase: apiBase, path: path, body: body, out: out})
}

// do is the only path to the network in this package: the concurrency cap, the
// retry loop, the status code, the error body and Retry-After all meet here.
// Keeping that in one place is the whole reason this package speaks HTTP
// instead of shelling out to a CLI, which surfaces failures only as stderr.
func (c *REST) do(ctx context.Context, r request) error {
	// Encoded once here rather than inside attempt: a retried request needs a
	// fresh reader over the same bytes, not a fresh marshal of them.
	var body []byte
	if r.body != nil {
		encoded, err := json.Marshal(r.body)
		if err != nil {
			return fmt.Errorf("%s: encoding request: %w", r.op, err)
		}
		body = encoded
	}

	// The slot is held for the whole call, backoff included. Releasing it
	// while waiting would let queued requests rush a server that has just
	// asked us to slow down, which is the opposite of what backoff is for.
	if err := c.acquire(ctx, r.op); err != nil {
		return err
	}
	defer func() { <-c.sem }()

	for attempt := 0; ; attempt++ {
		err := c.attempt(ctx, r, body)
		if err == nil {
			return nil
		}
		if !r.retry || attempt >= c.cfg.MaxRetries {
			return err
		}
		delay, ok := backoffFor(err, attempt)
		if !ok {
			return err
		}
		if err := c.wait(ctx, r.op, delay); err != nil {
			return err
		}
	}
}

// acquire takes one of the MaxConcurrent slots, waiting when they are all
// taken. Requests beyond the cap queue rather than fail; only the caller
// changing its mind ends the wait early.
func (c *REST) acquire(ctx context.Context, op string) error {
	select {
	case c.sem <- struct{}{}:
		return nil
	case <-ctx.Done():
		return fmt.Errorf("%s: %w", op, ctx.Err())
	}
}

// wait sleeps for a backoff delay, abandoning it promptly if the caller
// cancels. The sleep goes through cfg.After so the suite never really waits.
func (c *REST) wait(ctx context.Context, op string, d time.Duration) error {
	select {
	case <-c.cfg.After(d):
		return nil
	case <-ctx.Done():
		return fmt.Errorf("%s: %w", op, ctx.Err())
	}
}

// backoffFor reports how long to wait before repeating a failed read, and
// whether repeating it is worth doing at all.
//
// An OfflineError is deliberately not retried: the UI's answer to an
// unreachable site is to keep cached data browsable behind an offline marker,
// and it should show that at once rather than after several silent waits.
func backoffFor(err error, attempt int) (time.Duration, bool) {
	var e *Error
	if !errors.As(err, &e) || !e.Retryable() {
		return 0, false
	}
	if e.RetryAfter > 0 {
		// The server knows when its rate-limit window reopens; a computed
		// guess can only be wrong in one of two unhelpful directions.
		return e.RetryAfter, true
	}
	d := baseBackoff << attempt
	if d > maxBackoff || d <= 0 {
		d = maxBackoff
	}
	// No jitter: several sessions do share one rate-limit budget, but they
	// also start at unrelated moments, and Retry-After already staggers the
	// case that actually collides. Randomness here would buy little and cost
	// the deterministic backoff this package is tested on.
	return d, true
}

// attempt performs one HTTP round trip. body is the already-encoded request
// body, nil for a request that has none.
func (c *REST) attempt(ctx context.Context, r request, body []byte) error {
	endpoint := c.cfg.SiteURL + r.apiBase + r.path
	if len(r.query) > 0 {
		endpoint += "?" + r.query.Encode()
	}

	// A fresh reader per attempt, so a retried request never sends a drained
	// one. Only reads retry today, but a half-sent body is not a bug worth
	// leaving armed.
	var payload io.Reader
	if body != nil {
		payload = bytes.NewReader(body)
	}

	req, err := http.NewRequestWithContext(ctx, r.method, endpoint, payload)
	if err != nil {
		return fmt.Errorf("%s: %w", r.op, err)
	}
	req.SetBasicAuth(c.cfg.Email, c.cfg.Token)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.http.Do(req)
	if err != nil {
		// A cancelled context is the caller changing its mind, not the site
		// being unreachable; only the latter is an OfflineError.
		if ctxErr := ctx.Err(); ctxErr != nil {
			return fmt.Errorf("%s: %w", r.op, ctxErr)
		}
		return &OfflineError{Err: fmt.Errorf("%s: %w", r.op, err)}
	}
	defer resp.Body.Close()

	got, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		return &OfflineError{Err: fmt.Errorf("%s: reading response: %w", r.op, err)}
	}
	if resp.StatusCode >= 300 {
		return newHTTPError(r.op, resp, got)
	}
	if r.out == nil {
		return nil
	}
	if err := json.Unmarshal(got, r.out); err != nil {
		return fmt.Errorf("%s: decoding response: %w", r.op, err)
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
