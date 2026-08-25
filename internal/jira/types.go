package jira

import "time"

// User is a Jira account. AccountID is the only stable identifier; display
// names are not unique and email is often hidden by privacy settings.
type User struct {
	AccountID   string `json:"accountId"`
	DisplayName string `json:"displayName"`
	Email       string `json:"emailAddress,omitempty"`
	Active      bool   `json:"active"`
}

type Status struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Category string `json:"-"` // statusCategory.key: new | indeterminate | done
}

type Priority struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type Attachment struct {
	ID       string    `json:"id"`
	Filename string    `json:"filename"`
	MimeType string    `json:"mimeType"`
	Size     int64     `json:"size"`
	Created  time.Time `json:"created"`
	Author   *User     `json:"author,omitempty"`
}

type Comment struct {
	ID      string    `json:"id"`
	Author  *User     `json:"author"`
	Created time.Time `json:"created"`
	Updated time.Time `json:"updated"`
	// Body is an ADF document when read via v3. Rendering belongs to internal/adf.
	Body RawDocument `json:"body"`
}

// Transition is a workflow move available on one work item at one moment.
// Availability depends on the item's current status and the caller's
// permissions, so these are always fetched live and never cached.
type Transition struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	To   Status `json:"-"`
	// HasScreen reports that the transition presents a screen. Required fields
	// on that screen are out of scope for v1 and surface as a FieldsRequired
	// error when the transition is applied.
	HasScreen bool `json:"hasScreen"`
}

// Issue is a work item. Fields not requested via SearchOptions.Fields are
// zero, not absent -- callers must ask for what they intend to display.
type Issue struct {
	Key         string
	ID          string
	Summary     string
	Status      Status
	Assignee    *User
	Reporter    *User
	Priority    *Priority
	Type        string
	Project     string
	Labels      []string
	Created     time.Time
	Updated     time.Time
	Description RawDocument
	Attachments []Attachment
	Comments    []Comment
	// Raw holds fields we have no typed home for, keyed by field ID, so that
	// configurable columns can display custom fields without a code change.
	Raw map[string]any
}

// RawDocument is an undecoded ADF document. It stays raw at this layer so the
// client has no opinion on rendering; internal/adf owns the tree.
type RawDocument []byte

func (d RawDocument) IsEmpty() bool { return len(d) == 0 || string(d) == "null" }

type SearchOptions struct {
	JQL string
	// Fields selects which fields the server returns. Empty means the
	// configured column set; requesting everything is the single biggest
	// avoidable cost on a list fetch.
	Fields []string

	// PageToken continues a previous page. Empty asks for the first one.
	//
	// Paging is by opaque token rather than by offset because the endpoint
	// that paged by offset, /rest/api/3/search, now answers 410 Gone. A token
	// is also the more honest model for a live result set: an offset silently
	// skips or repeats rows when items change rank between pages.
	PageToken string

	MaxResults int
}

type SearchResult struct {
	Issues []Issue
	// NextPageToken continues after this page, and is empty when there is no
	// page after it.
	NextPageToken string
	// IsLast reports that no further pages exist. The endpoint sends it
	// alongside the token, and it is the field to trust: a total is not
	// returned at all, so counting rows cannot answer the same question.
	IsLast bool
}
