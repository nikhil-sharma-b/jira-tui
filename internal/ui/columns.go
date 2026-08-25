package ui

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/x/ansi"
	"github.com/nikhil-sharma-b/jira-tui/internal/jira"
)

// Column is one resolved list column: the header a user reads, the API field
// that backs it, and how a value of that field reads as a single line.
//
// Resolution happens once, against the site's own field metadata, because a
// configured column is a display name ("Story Points") and the search endpoint
// wants an id ("customfield_10016"). Doing it here is what lets the search ask
// for exactly the fields on screen -- adding a column to config adds a field to
// the request, and nothing else has to know.
type Column struct {
	// Title is the header text: the field's display name as the site spells
	// it, not as the config happened to capitalise it.
	Title string

	// Field is the API field id. Empty for envelope data such as the key,
	// which arrives with every issue and must never be asked for as a field.
	Field string

	// render turns one issue into this column's cell.
	render func(*jira.Issue, time.Time) string

	// min is the width below which the cell is no longer worth showing, and
	// natural caps how wide it grows on content alone.
	min, natural int

	// flex marks the column that absorbs leftover width. Exactly one column
	// should have it -- the summary, in every sensible configuration -- and a
	// layout with none simply leaves the remainder unused.
	flex bool
}

// Render produces this column's cell for one issue. now is passed rather than
// read so that relative timestamps are a function of their inputs.
func (c Column) Render(issue *jira.Issue, now time.Time) string {
	if issue == nil || c.render == nil {
		return ""
	}
	return c.render(issue, now)
}

// ColumnFields lists the distinct API fields the columns need, which is
// exactly what SearchOptions.Fields should carry. Envelope columns contribute
// nothing, so a list of keys and nothing else fetches no fields at all.
func ColumnFields(cols []Column) []string {
	var fields []string
	seen := map[string]bool{}
	for _, c := range cols {
		if c.Field == "" || seen[c.Field] {
			continue
		}
		seen[c.Field] = true
		fields = append(fields, c.Field)
	}
	return fields
}

// ColumnError is a configured column that could not be resolved. It carries
// the name as the user wrote it, since that is the string they have to go and
// find in their config file.
type ColumnError struct {
	Name string
	Msg  string
}

func (e *ColumnError) Error() string {
	return fmt.Sprintf("columns: %q %s", e.Name, e.Msg)
}

// ResolveColumns turns configured column names into columns, using the site's
// field metadata. Every name must resolve: a column that silently disappeared
// would leave the user staring at a list missing the one thing they configured
// it for.
func ResolveColumns(names []string, fields []jira.Field) ([]Column, error) {
	index := indexFields(fields)
	cols := make([]Column, 0, len(names))
	for _, name := range names {
		col, err := resolveColumn(name, index)
		if err != nil {
			return nil, err
		}
		cols = append(cols, col)
	}
	if len(cols) == 0 {
		return nil, &ColumnError{Name: "", Msg: "must name at least one column"}
	}
	return cols, nil
}

func resolveColumn(name string, index map[string][]jira.Field) (Column, error) {
	key := strings.ToLower(strings.TrimSpace(name))

	// The key is on the issue envelope rather than in its fields, so it is
	// resolved before the metadata is consulted. Sites do publish an
	// "issuekey" field, but naming it in a search request is how a list ends
	// up paying for data it already has.
	if key == "key" {
		return Column{Title: "Key", min: 6, natural: 14, render: renderKey}, nil
	}

	matches := index[key]
	switch len(matches) {
	case 0:
		return Column{}, &ColumnError{Name: name, Msg: "is not a field on this site"}
	case 1:
	default:
		// Jira permits two custom fields to share a display name, and there is
		// no way to guess which one was meant. Naming both ids lets the user
		// write the id instead, which always resolves.
		ids := make([]string, 0, len(matches))
		for _, f := range matches {
			ids = append(ids, f.ID)
		}
		sort.Strings(ids)
		return Column{}, &ColumnError{Name: name, Msg: "names more than one field (" +
			strings.Join(ids, ", ") + "); use the id instead"}
	}

	field := matches[0]
	col := Column{Title: field.Name, Field: field.ID}
	if col.Title == "" {
		col.Title = field.ID
	}
	applyShape(&col, field.ID)
	return col, nil
}

// indexFields maps every name a user could reasonably write -- display name,
// id, and JQL clause name -- onto the fields answering to it. A name matching
// several fields is kept as several, so ambiguity is reported rather than
// resolved by whichever happened to be first.
func indexFields(fields []jira.Field) map[string][]jira.Field {
	index := map[string][]jira.Field{}
	add := func(alias string, f jira.Field) {
		alias = strings.ToLower(strings.TrimSpace(alias))
		if alias == "" {
			return
		}
		for _, existing := range index[alias] {
			if existing.ID == f.ID {
				return
			}
		}
		index[alias] = append(index[alias], f)
	}
	for _, f := range fields {
		add(f.ID, f)
		add(f.Name, f)
		for _, clause := range f.Clauses {
			add(clause, f)
		}
	}
	return index
}

// applyShape gives a column its renderer and its width appetite. Fields with a
// typed home on Issue are rendered from it; everything else falls through to
// the raw JSON the client kept for exactly this purpose.
func applyShape(col *Column, id string) {
	switch id {
	case "summary":
		col.render = func(i *jira.Issue, _ time.Time) string { return i.Summary }
		col.min, col.natural, col.flex = 20, 0, true
	case "status":
		col.render = func(i *jira.Issue, _ time.Time) string { return i.Status.Name }
		col.min, col.natural = 6, 16
	case "assignee":
		col.render = func(i *jira.Issue, _ time.Time) string { return userName(i.Assignee, "Unassigned") }
		col.min, col.natural = 8, 18
	case "reporter":
		col.render = func(i *jira.Issue, _ time.Time) string { return userName(i.Reporter, "") }
		col.min, col.natural = 8, 18
	case "priority":
		col.render = func(i *jira.Issue, _ time.Time) string {
			if i.Priority == nil {
				return ""
			}
			return i.Priority.Name
		}
		col.min, col.natural = 4, 10
	case "issuetype":
		col.render = func(i *jira.Issue, _ time.Time) string { return i.Type }
		col.min, col.natural = 4, 12
	case "project":
		col.render = func(i *jira.Issue, _ time.Time) string { return i.Project }
		col.min, col.natural = 4, 14
	case "labels":
		col.render = func(i *jira.Issue, _ time.Time) string { return strings.Join(i.Labels, ", ") }
		col.min, col.natural = 6, 24
	case "updated":
		col.render = func(i *jira.Issue, now time.Time) string { return age(i.Updated, now) }
		col.min, col.natural = 3, 7
	case "created":
		col.render = func(i *jira.Issue, now time.Time) string { return age(i.Created, now) }
		col.min, col.natural = 3, 7
	default:
		col.render = func(i *jira.Issue, _ time.Time) string { return renderRaw(i.Raw[id]) }
		col.min, col.natural = 4, 20
	}
}

func renderKey(i *jira.Issue, _ time.Time) string { return i.Key }

func userName(u *jira.User, absent string) string {
	if u == nil || u.DisplayName == "" {
		return absent
	}
	return u.DisplayName
}

// renderRaw flattens a custom field's JSON into one line. Jira's field types
// share a handful of shapes -- a scalar, an object with a human-readable
// member, or an array of either -- so covering those covers most of them, and
// anything left over falls back to its own text rather than to a marker.
func renderRaw(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return t
	case bool:
		return strconv.FormatBool(t)
	case float64:
		// Custom numbers are story points and estimates far more often than
		// they are fractions, so an integral value loses its ".0".
		return strconv.FormatFloat(t, 'f', -1, 64)
	case []any:
		parts := make([]string, 0, len(t))
		for _, item := range t {
			if s := renderRaw(item); s != "" {
				parts = append(parts, s)
			}
		}
		return strings.Join(parts, ", ")
	case map[string]any:
		// In descending order of how much like a label the member reads.
		for _, k := range []string{"displayName", "name", "value", "text", "key", "id"} {
			if s, ok := t[k].(string); ok && s != "" {
				return s
			}
		}
		return ""
	default:
		return fmt.Sprint(t)
	}
}

// age renders a timestamp as how long ago it was, in one or two characters
// plus a unit. A list is scanned rather than read, and "3h" answers "is this
// still moving?" faster than a date does.
func age(t, now time.Time) string {
	if t.IsZero() {
		return ""
	}
	d := now.Sub(t)
	if d < 0 {
		d = 0
	}
	switch {
	case d < time.Minute:
		return "now"
	case d < time.Hour:
		return strconv.Itoa(int(d.Minutes())) + "m"
	case d < 24*time.Hour:
		return strconv.Itoa(int(d.Hours())) + "h"
	case d < 365*24*time.Hour:
		return strconv.Itoa(int(d.Hours()/24)) + "d"
	default:
		return strconv.Itoa(int(d.Hours()/24/365)) + "y"
	}
}

// columnGap separates columns. One space is enough to read a boundary and, at
// six columns in an 80-wide tmux pane, five spaces is already a column's worth
// of summary.
const columnGap = 1

// layout decides how wide each column is drawn at a given terminal width, and
// which columns are drawn at all. It returns one width per column; a zero
// width means the column did not fit and is omitted.
//
// The rule is: every column gets what its content needs up to its natural cap,
// the flexible column absorbs whatever is left, and when even the minimums do
// not fit, columns are dropped from the right. Dropping beats squeezing
// because a four-character summary is not a smaller version of a summary.
func layout(cols []Column, issues []*jira.Issue, now time.Time, width int) []int {
	widths := make([]int, len(cols))
	if len(cols) == 0 || width <= 0 {
		return widths
	}

	for i, c := range cols {
		w := ansi.StringWidth(c.Title)
		for _, issue := range issues {
			w = max(w, ansi.StringWidth(c.Render(issue, now)))
		}
		if c.natural > 0 {
			w = min(w, c.natural)
		}
		widths[i] = max(w, c.min)
	}

	// Columns beyond what the minimums can pay for are dropped from the right,
	// keeping at least one. Dropping beats squeezing: a four-character summary
	// is not a smaller summary, it is a column of ellipses.
	shown := len(cols)
	for shown > 1 && minimumWidth(cols[:shown]) > width {
		shown--
	}
	for i := shown; i < len(cols); i++ {
		widths[i] = 0
	}
	fitted := widths[:shown]
	visible := cols[:shown]

	for total(fitted) > width {
		i := widestShrinkable(visible, fitted)
		if i < 0 {
			break
		}
		fitted[i]--
	}

	if slack := width - total(fitted); slack > 0 {
		if i := flexIndex(visible); i >= 0 {
			fitted[i] += slack
		}
	}

	// A single column that still does not fit is truncated rather than
	// dropped: something is always better than an empty pane.
	if shown == 1 && fitted[0] > width {
		fitted[0] = width
	}
	return widths
}

func minimumWidth(cols []Column) int {
	w := columnGap * (len(cols) - 1)
	for _, c := range cols {
		w += c.min
	}
	return w
}

func total(widths []int) int {
	w := columnGap * (len(widths) - 1)
	for _, x := range widths {
		w += x
	}
	return w
}

// widestShrinkable picks the column to take a character from: the flexible one
// while it is above its minimum, then whichever fixed column is furthest above
// its own. Shrinking the widest keeps the loss spread rather than gutting one
// column to spare the rest.
func widestShrinkable(cols []Column, widths []int) int {
	if i := flexIndex(cols); i >= 0 && widths[i] > cols[i].min {
		return i
	}
	best, bestSlack := -1, 0
	for i, c := range cols {
		if slack := widths[i] - c.min; slack > bestSlack {
			best, bestSlack = i, slack
		}
	}
	return best
}

func flexIndex(cols []Column) int {
	for i, c := range cols {
		if c.flex {
			return i
		}
	}
	return -1
}

// cell pads or truncates a value to exactly w columns, so rows line up whatever
// the content and whatever the terminal's idea of a wide character is.
func cell(value string, w int) string {
	if w <= 0 {
		return ""
	}
	value = ansi.Truncate(value, w, "…")
	if pad := w - ansi.StringWidth(value); pad > 0 {
		value += strings.Repeat(" ", pad)
	}
	return value
}
