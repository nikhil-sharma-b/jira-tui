package ui

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/nikhil-sharma-b/jira-tui/internal/adf"
	"github.com/nikhil-sharma-b/jira-tui/internal/config"
	"github.com/nikhil-sharma-b/jira-tui/internal/jira"
)

var detailFields = []string{
	"summary", "status", "assignee", "reporter", "priority", "issuetype",
	"labels", "created", "updated", "description",
}

var spinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

type detailMsg struct {
	request uint64
	issue   *jira.Issue
	err     error
}

type detailTickMsg struct{ request uint64 }

// detailPane owns everything specific to one live work-item fetch and its
// viewport. The root model only opens, cancels, moves, resizes, and renders it.
type detailPane struct {
	open    bool
	loading bool
	issue   *jira.Issue
	err     error

	request uint64
	cancel  context.CancelFunc
	top     int
	width   int
	rows    int
	lines   []string
	frame   int
}

func (d *detailPane) fetch(client jira.Client, key string) tea.Cmd {
	d.cancelFetch()
	d.open, d.loading, d.err, d.issue, d.top, d.frame = true, true, nil, nil, 0, 0
	d.request++
	request := d.request
	ctx, cancel := context.WithCancel(context.Background())
	d.cancel = cancel
	fields := append([]string(nil), detailFields...)
	return func() tea.Msg {
		issue, err := client.Issue(ctx, key, fields)
		return detailMsg{request: request, issue: issue, err: err}
	}
}

func (d *detailPane) tick() tea.Cmd {
	request := d.request
	return tea.Tick(80*time.Millisecond, func(time.Time) tea.Msg {
		return detailTickMsg{request: request}
	})
}

func (d *detailPane) handleTick(msg detailTickMsg) tea.Cmd {
	if !d.loading || msg.request != d.request {
		return nil
	}
	d.frame = (d.frame + 1) % len(spinnerFrames)
	return d.tick()
}

func (d *detailPane) cancelFetch() {
	if d.cancel != nil {
		d.cancel()
		d.cancel = nil
	}
}

func (d *detailPane) selectionMoved() {
	if !d.loading {
		return
	}
	d.cancelFetch()
	d.request++
	d.loading = false
	d.err = errors.New("detail fetch cancelled because the selection moved")
	d.lines = nil
}

func (d *detailPane) handle(msg detailMsg) bool {
	if msg.request != d.request {
		return false
	}
	d.cancelFetch()
	d.loading = false
	if msg.err != nil {
		d.issue = nil
		d.err = detailError(msg.err)
		d.lines = nil
		return true
	}
	d.issue, d.err = msg.issue, nil
	d.render()
	return true
}

func detailError(err error) error {
	switch {
	case jira.HasStatus(err, http.StatusNotFound):
		return errors.New("work item does not exist")
	case jira.HasStatus(err, http.StatusForbidden):
		return errors.New("work item exists, but you are not permitted to view it")
	default:
		return err
	}
}

func (d *detailPane) resize(width, rows int) {
	d.width, d.rows = max(width, 0), max(rows, 0)
	d.render()
}

func (d *detailPane) render() {
	if d.issue == nil || d.width <= 0 {
		d.lines = nil
		d.clamp()
		return
	}
	i := d.issue
	values := []string{
		i.Summary,
		"Key: " + i.Key,
		"Status: " + valueOr(i.Status.Name, "None"),
		"Assignee: " + userName(i.Assignee, "Unassigned"),
		"Reporter: " + userName(i.Reporter, "None"),
		"Priority: " + priorityName(i.Priority),
		"Type: " + valueOr(i.Type, "None"),
		"Labels: " + labels(i.Labels),
		"Created: " + detailTime(i.Created),
		"Updated: " + detailTime(i.Updated),
	}
	var lines []string
	for _, value := range values {
		lines = append(lines, wrapDetailLine(value, d.width)...)
	}
	lines = append(lines, "", "Description")
	if i.Description.IsEmpty() {
		lines = append(lines, "No description provided.")
	} else if rendered, err := adf.Render(i.Description, adf.Options{Width: d.width}); err != nil {
		lines = append(lines, "Description could not be rendered: "+err.Error())
	} else if len(rendered) == 0 {
		lines = append(lines, "No description provided.")
	} else {
		lines = append(lines, rendered...)
	}
	for n := range lines {
		lines[n] = fitWidth(lines[n], d.width)
	}
	d.lines = lines
	d.clamp()
}

func (d *detailPane) move(action config.Action, count int) {
	n := max(count, 1)
	half := max(d.rows/2, 1)
	switch action {
	case config.ActionDown:
		d.top += n
	case config.ActionUp:
		d.top -= n
	case config.ActionTop:
		d.top = max(count-1, 0)
	case config.ActionBottom:
		d.top = len(d.lines) - d.rows
		if count > 0 {
			d.top = count - 1
		}
	case config.ActionHalfPageDown:
		d.top += n * half
	case config.ActionHalfPageUp:
		d.top -= n * half
	case config.ActionViewportTop:
		d.top += n - 1
	case config.ActionViewportMid:
		d.top += half
	case config.ActionViewportBot:
		d.top += max(d.rows-n, 0)
	}
	d.clamp()
}

func (d *detailPane) clamp() {
	d.top = min(max(d.top, 0), max(len(d.lines)-d.rows, 0))
}

func (d *detailPane) view() []string {
	var lines []string
	switch {
	case d.loading:
		lines = []string{spinnerFrames[d.frame] + " Loading…"}
	case d.err != nil:
		lines = []string{"Detail could not be loaded.", d.err.Error()}
	case len(d.lines) > 0:
		end := min(d.top+d.rows, len(d.lines))
		lines = append(lines, d.lines[d.top:end]...)
	default:
		lines = []string{"No detail loaded."}
	}
	return lines
}

func (d *detailPane) close() {
	d.cancelFetch()
	request := d.request + 1
	*d = detailPane{request: request}
}

func fitWidth(s string, width int) string {
	if width <= 0 {
		return ""
	}
	return ansi.Truncate(s, width, "…")
}

func wrapDetailLine(s string, width int) []string {
	if width <= 0 {
		return nil
	}
	return strings.Split(ansi.Hardwrap(ansi.Wordwrap(s, width, ""), width, false), "\n")
}

func valueOr(value, absent string) string {
	if strings.TrimSpace(value) == "" {
		return absent
	}
	return value
}

func priorityName(p *jira.Priority) string {
	if p == nil {
		return "None"
	}
	return valueOr(p.Name, "None")
}

func labels(values []string) string {
	if len(values) == 0 {
		return "None"
	}
	return strings.Join(values, ", ")
}

func detailTime(t time.Time) string {
	if t.IsZero() {
		return "Unknown"
	}
	return t.Format("2006-01-02 15:04 MST")
}
