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

type commentsMsg struct {
	request  uint64
	comments []jira.Comment
	err      error
}

type detailTickMsg struct{ request uint64 }

// detailPane owns everything specific to one live work-item fetch and its
// viewport. The root model only opens, cancels, moves, resizes, and renders it.
type detailPane struct {
	key             string
	open            bool
	loading         bool
	issue           *jira.Issue
	err             error
	commentsLoading bool
	comments        []jira.Comment
	commentsErr     error

	request        uint64
	cancel         context.CancelFunc
	top            int
	width          int
	rows           int
	lines          []string
	frame          int
	commentsOffset int
	jumpToComments bool
}

func (d *detailPane) fetch(client jira.Client, key string) []tea.Cmd {
	d.cancelFetch()
	d.key = key
	d.open, d.loading, d.err, d.issue, d.top, d.frame = true, true, nil, nil, 0, 0
	d.commentsLoading, d.comments, d.commentsErr = true, nil, nil
	d.commentsOffset, d.jumpToComments = 0, false
	d.request++
	request := d.request
	ctx, cancel := context.WithCancel(context.Background())
	d.cancel = cancel
	fields := append([]string(nil), detailFields...)
	return []tea.Cmd{
		func() tea.Msg {
			issue, err := client.Issue(ctx, key, fields)
			return detailMsg{request: request, issue: issue, err: err}
		},
		func() tea.Msg {
			comments, err := client.Comments(ctx, key)
			return commentsMsg{request: request, comments: comments, err: err}
		},
	}
}

func (d *detailPane) tick() tea.Cmd {
	request := d.request
	return tea.Tick(80*time.Millisecond, func(time.Time) tea.Msg {
		return detailTickMsg{request: request}
	})
}

func (d *detailPane) handleTick(msg detailTickMsg) tea.Cmd {
	if (!d.loading && !d.commentsLoading) || msg.request != d.request {
		return nil
	}
	d.frame = (d.frame + 1) % len(spinnerFrames)
	if d.issue != nil && d.commentsLoading {
		d.render()
	}
	return d.tick()
}

func (d *detailPane) cancelFetch() {
	if d.cancel != nil {
		d.cancel()
		d.cancel = nil
	}
}

func (d *detailPane) selectionMoved() {
	if !d.loading && !d.commentsLoading {
		return
	}
	d.cancelFetch()
	d.request++
	d.loading, d.commentsLoading = false, false
	d.err = errors.New("detail fetch cancelled because the selection moved")
	d.comments, d.commentsErr = nil, nil
	d.lines = nil
}

func (d *detailPane) handle(msg detailMsg) bool {
	if msg.request != d.request || !d.loading {
		return false
	}
	d.loading = false
	if msg.err != nil {
		d.cancelFetch()
		d.commentsLoading = false
		d.issue = nil
		d.err = detailError(msg.err)
		d.lines = nil
		return true
	}
	d.issue, d.err = msg.issue, nil
	d.finishFetch()
	d.render()
	return true
}

func (d *detailPane) handleComments(msg commentsMsg) bool {
	if msg.request != d.request || !d.commentsLoading {
		return false
	}
	d.commentsLoading = false
	d.commentsErr = msg.err
	d.comments = append(d.comments[:0], msg.comments...)
	d.finishFetch()
	d.render()
	return true
}

func (d *detailPane) finishFetch() {
	if !d.loading && !d.commentsLoading {
		d.cancelFetch()
	}
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
	lines = append(lines, "", "Comments")
	d.commentsOffset = len(lines) - 1
	switch {
	case d.commentsLoading:
		lines = append(lines, spinnerFrames[d.frame]+" Loading comments…")
	case d.commentsErr != nil:
		lines = append(lines, "Comments could not be loaded.")
	case len(d.comments) == 0:
		lines = append(lines, "No comments.")
	default:
		for index, comment := range d.comments {
			if index > 0 {
				lines = append(lines, "")
			}
			lines = append(lines, renderComment(comment, d.width)...)
		}
	}
	for n := range lines {
		lines[n] = fitWidth(lines[n], d.width)
	}
	d.lines = lines
	if d.jumpToComments {
		d.top = d.commentsOffset
		if !d.commentsLoading {
			d.jumpToComments = false
		}
	}
	d.clamp()
}

func renderComment(comment jira.Comment, width int) []string {
	lines := wrapDetailLine(userName(comment.Author, "Unknown author")+" · "+detailTime(comment.Created), width)
	if comment.Updated.Sub(comment.Created) >= time.Second {
		lines = append(lines, wrapDetailLine("Edited: "+detailTime(comment.Updated), width)...)
	}
	switch {
	case comment.Body.IsEmpty():
		lines = append(lines, "No comment body.")
	default:
		rendered, err := adf.Render(comment.Body, adf.Options{Width: width})
		if err != nil {
			lines = append(lines, "Comment could not be rendered: "+err.Error())
		} else if len(rendered) == 0 {
			lines = append(lines, "No comment body.")
		} else {
			lines = append(lines, rendered...)
		}
	}
	return lines
}

func (d *detailPane) goComments() {
	if d.issue == nil {
		d.jumpToComments = true
		return
	}
	d.top = d.commentsOffset
	d.clamp()
}

func (d *detailPane) move(action config.Action, count int) {
	d.jumpToComments = false
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
