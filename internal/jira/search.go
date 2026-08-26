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
	case "attachment":
		attachments, err := decodeAttachments(raw)
		if err != nil {
			return true, err
		}
		i.Attachments = attachments
		return true, nil
	case "issuelinks":
		links, err := decodeLinks(raw)
		if err != nil {
			return true, err
		}
		i.Links = links
		return true, nil
	case "subtasks":
		subtasks, err := decodeSubtasks(raw)
		if err != nil {
			return true, err
		}
		i.Subtasks = subtasks
		return true, nil
	case "description":
		if isJSONNull(raw) {
			i.Description = nil
		} else {
			i.Description = append(i.Description[:0], raw...)
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

// linkWire is one entry of issuelinks. Exactly one of inwardIssue and
// outwardIssue is present, and which one it is decides which of the link
// type's two names describes the relationship from here.
type linkWire struct {
	Type struct {
		Name    string `json:"name"`
		Inward  string `json:"inward"`
		Outward string `json:"outward"`
	} `json:"type"`
	InwardIssue  *linkedIssueWire `json:"inwardIssue"`
	OutwardIssue *linkedIssueWire `json:"outwardIssue"`
}

type linkedIssueWire struct {
	Key    string `json:"key"`
	Fields struct {
		Summary   string     `json:"summary"`
		Status    statusWire `json:"status"`
		IssueType struct {
			Name string `json:"name"`
		} `json:"issuetype"`
	} `json:"fields"`
}

func decodeLinks(raw json.RawMessage) ([]IssueLink, error) {
	if isJSONNull(raw) {
		return nil, nil
	}
	var wires []linkWire
	if err := json.Unmarshal(raw, &wires); err != nil {
		return nil, err
	}
	var links []IssueLink
	for _, wire := range wires {
		other, relation := wire.OutwardIssue, wire.Type.Outward
		if other == nil {
			other, relation = wire.InwardIssue, wire.Type.Inward
		}
		if other == nil {
			continue
		}
		if relation == "" {
			relation = wire.Type.Name
		}
		links = append(links, IssueLink{
			Relation: relation,
			Key:      other.Key,
			Summary:  other.Fields.Summary,
			Status:   Status{ID: other.Fields.Status.ID, Name: other.Fields.Status.Name, Category: other.Fields.Status.Category.Key},
			Type:     other.Fields.IssueType.Name,
		})
	}
	return links, nil
}

func decodeSubtasks(raw json.RawMessage) ([]Subtask, error) {
	if isJSONNull(raw) {
		return nil, nil
	}
	var wires []linkedIssueWire
	if err := json.Unmarshal(raw, &wires); err != nil {
		return nil, err
	}
	var subtasks []Subtask
	for _, wire := range wires {
		subtasks = append(subtasks, Subtask{
			Key:     wire.Key,
			Summary: wire.Fields.Summary,
			Status:  Status{ID: wire.Fields.Status.ID, Name: wire.Fields.Status.Name, Category: wire.Fields.Status.Category.Key},
			Type:    wire.Fields.IssueType.Name,
		})
	}
	return subtasks, nil
}

// attachmentWire is an attachment as it arrives. Only the timestamp needs
// help: Jira's offset has no colon, so time.Time's own unmarshaller rejects it.
type attachmentWire struct {
	ID       string          `json:"id"`
	Filename string          `json:"filename"`
	MimeType string          `json:"mimeType"`
	Size     int64           `json:"size"`
	Created  json.RawMessage `json:"created"`
	Author   *User           `json:"author"`
}

func decodeAttachments(raw json.RawMessage) ([]Attachment, error) {
	if isJSONNull(raw) {
		return nil, nil
	}
	var wires []attachmentWire
	if err := json.Unmarshal(raw, &wires); err != nil {
		return nil, err
	}
	var attachments []Attachment
	for _, wire := range wires {
		attachment := Attachment{
			ID: wire.ID, Filename: wire.Filename, MimeType: wire.MimeType,
			Size: wire.Size, Author: wire.Author,
		}
		if err := unmarshalTime(wire.Created, &attachment.Created); err != nil {
			return nil, err
		}
		attachments = append(attachments, attachment)
	}
	return attachments, nil
}
