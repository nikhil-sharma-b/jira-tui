package jira

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// searchEndpoint is /rest/api/3/search/jql. Its predecessor, /rest/api/3/search,
// now answers 410 Gone; this one pages by an opaque token and reports no total.
const searchEndpoint = "/search/jql"

// Search runs JQL. Only the fields the caller names are requested, which is
// what keeps a list load proportional to what is on screen rather than to how
// many fields the site happens to define.
func (c *REST) Search(ctx context.Context, opts SearchOptions) (*SearchResult, error) {
	q := url.Values{}
	q.Set("jql", opts.JQL)
	if len(opts.Fields) > 0 {
		q.Set("fields", strings.Join(opts.Fields, ","))
	}
	if opts.MaxResults > 0 {
		q.Set("maxResults", strconv.Itoa(opts.MaxResults))
	}
	if opts.PageToken != "" {
		q.Set("nextPageToken", opts.PageToken)
	}

	var wire struct {
		Issues        []issueWire `json:"issues"`
		NextPageToken string      `json:"nextPageToken"`
		IsLast        bool        `json:"isLast"`
	}
	if err := c.get(ctx, "search", apiRead, searchEndpoint, q, &wire); err != nil {
		return nil, err
	}

	result := &SearchResult{
		NextPageToken: wire.NextPageToken,
		// A page with nothing to follow it is the last one whatever the flag
		// says, so that a caller walking pages cannot be made to loop.
		IsLast: wire.IsLast || wire.NextPageToken == "",
		Issues: make([]Issue, 0, len(wire.Issues)),
	}
	for _, w := range wire.Issues {
		issue, err := w.issue()
		if err != nil {
			return nil, fmt.Errorf("search: %w", err)
		}
		result.Issues = append(result.Issues, issue)
	}
	return result, nil
}

// issueWire is an issue as it arrives: an envelope carrying a bag of fields
// whose contents depend entirely on what was asked for. The fields stay
// undecoded until we know which ones came back, because a field that was not
// requested is absent rather than null and must not overwrite anything.
type issueWire struct {
	ID     string                     `json:"id"`
	Key    string                     `json:"key"`
	Fields map[string]json.RawMessage `json:"fields"`
}

// issue converts the wire form. Fields with a typed home on Issue are decoded
// into it; everything else is kept in Raw so a configured column naming a
// custom field has something to render without this package knowing about it.
func (w issueWire) issue() (Issue, error) {
	issue := Issue{ID: w.ID, Key: w.Key}

	for id, raw := range w.Fields {
		typed, err := issue.setField(id, raw)
		if err != nil {
			return Issue{}, fmt.Errorf("field %s: %w", id, err)
		}
		if typed {
			continue
		}
		var value any
		if err := json.Unmarshal(raw, &value); err != nil {
			return Issue{}, fmt.Errorf("field %s: %w", id, err)
		}
		if issue.Raw == nil {
			issue.Raw = map[string]any{}
		}
		issue.Raw[id] = value
	}
	return issue, nil
}

// setField decodes one field into its typed home, reporting whether it had
// one. Only the fields a list or a detail pane displays are typed; the rest
// stay JSON, which is what makes a new column a config change rather than a
// code change.
func (i *Issue) setField(id string, raw json.RawMessage) (bool, error) {
	switch id {
	case "summary":
		return true, json.Unmarshal(raw, &i.Summary)
	case "status":
		var s statusWire
		if err := json.Unmarshal(raw, &s); err != nil {
			return true, err
		}
		i.Status = Status{ID: s.ID, Name: s.Name, Category: s.Category.Key}
		return true, nil
	case "assignee":
		return true, json.Unmarshal(raw, &i.Assignee)
	case "reporter":
		return true, json.Unmarshal(raw, &i.Reporter)
	case "priority":
		return true, json.Unmarshal(raw, &i.Priority)
	case "issuetype":
		return true, unmarshalName(raw, &i.Type)
	case "project":
		var p struct {
			Key string `json:"key"`
		}
		if err := json.Unmarshal(raw, &p); err != nil {
			return true, err
		}
		i.Project = p.Key
		return true, nil
	case "labels":
		return true, json.Unmarshal(raw, &i.Labels)
	case "created":
		return true, unmarshalTime(raw, &i.Created)
	case "updated":
		return true, unmarshalTime(raw, &i.Updated)
	case "description":
		// Kept as bytes: rendering ADF belongs to internal/adf, and this
		// package deliberately has no opinion on it.
		if !isJSONNull(raw) {
			i.Description = RawDocument(raw)
		}
		return true, nil
	}
	return false, nil
}

type statusWire struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Category struct {
		Key string `json:"key"`
	} `json:"statusCategory"`
}

// unmarshalName pulls the name out of one of Jira's {id, name} objects, which
// is all a list column ever shows of them.
func unmarshalName(raw json.RawMessage, dst *string) error {
	if isJSONNull(raw) {
		return nil
	}
	var v struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(raw, &v); err != nil {
		return err
	}
	*dst = v.Name
	return nil
}

// jiraTimeLayout is the timestamp format Jira Cloud actually sends. It is RFC
// 3339 except for the offset, which has no colon -- so time.Time's own
// unmarshaller rejects it, and every timestamp has to come through here.
const jiraTimeLayout = "2006-01-02T15:04:05.000-0700"

func unmarshalTime(raw json.RawMessage, dst *time.Time) error {
	if isJSONNull(raw) {
		return nil
	}
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return err
	}
	if s == "" {
		return nil
	}
	t, err := time.Parse(jiraTimeLayout, s)
	if err != nil {
		// Not every deployment agrees about the fractional seconds, and a
		// timestamp we cannot read is not a reason to lose the work item.
		if t, err = time.Parse(time.RFC3339, s); err != nil {
			return fmt.Errorf("%q is not a timestamp", s)
		}
	}
	*dst = t
	return nil
}

func isJSONNull(raw json.RawMessage) bool {
	return len(raw) == 0 || string(raw) == "null"
}
