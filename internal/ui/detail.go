package ui

import (
	"context"
	"errors"
	"fmt"
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
	"labels", "created", "updated", "description", "attachment", "issuelinks",
	"subtasks",
}

// detailTab is one page of the detail pane. The tabs are the jiratui set, less
// Related, which has no counterpart in what jt fetches. Opening the pane starts
// every request the tabs need, so switching tabs does not start another one.
type detailTab int

const (
	tabInfo detailTab = iota
	tabDetails
	tabComments
	tabAttachments
	tabLinks
	tabSubtasks
	tabCount
)

var detailTabNames = [tabCount]string{
	tabInfo:        "Info",
	tabDetails:     "Details",
	tabComments:    "Comments",
	tabAttachments: "Attachments",
	tabLinks:       "Links",
	tabSubtasks:    "Subtasks",
}

// tabBarRows is what the tab strip costs the body: the labels and the rule
// under them.
const tabBarRows = 2

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

	request uint64
	cancel  context.CancelFunc
	// tab is the page on screen; tops remembers where each page was left, so
	// returning to a tab returns to the place in it, not to its top.
	tab   detailTab
	tops  [tabCount]int
	top   int
	width int
	rows  int
	lines []string
	frame int
}

func (d *detailPane) fetch(client jira.Client, key string) []tea.Cmd {
	d.cancelFetch()
	d.key = key
	d.open, d.loading, d.err, d.issue, d.top, d.frame = true, true, nil, nil, 0, 0
	d.commentsLoading, d.comments, d.commentsErr = true, nil, nil
	d.tops = [tabCount]int{}
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
	var lines []string
	switch d.tab {
	case tabDetails:
		lines = d.renderDetails()
	case tabComments:
		lines = d.renderComments()
	case tabAttachments:
		lines = d.renderAttachments()
	case tabLinks:
		lines = d.renderLinks()
	case tabSubtasks:
		lines = d.renderSubtasks()
	default:
		lines = d.renderInfo()
	}
	for n := range lines {
		lines[n] = fitWidth(lines[n], d.width)
	}
	d.lines = lines
	d.clamp()
}

// renderInfo is the landing tab: the item's summary and its description, which
// is what the eye wants first and what the other tabs are read around.
func (d *detailPane) renderInfo() []string {
	lines := wrapDetailLine(summaryStyle.Render(d.issue.Summary), d.width)
	lines = append(lines, "", sectionHeader("Description", d.width), "")
	return append(lines, d.renderDescription()...)
}

func (d *detailPane) renderDescription() []string {
	i := d.issue
	switch {
	case i.Description.IsEmpty():
		return []string{"No description provided."}
	}
	rendered, err := adf.Render(i.Description, adf.Options{Width: d.width})
	switch {
	case err != nil:
		return []string{"Description could not be rendered: " + err.Error()}
	case len(rendered) == 0:
		return []string{"No description provided."}
	default:
		return rendered
	}
}

func (d *detailPane) renderDetails() []string {
	i := d.issue
	fields := [][2]string{
		{"Key", i.Key},
		{"Status", valueOr(i.Status.Name, "None")},
		{"Assignee", userName(i.Assignee, "Unassigned")},
		{"Reporter", userName(i.Reporter, "None")},
		{"Priority", priorityName(i.Priority)},
		{"Type", valueOr(i.Type, "None")},
		{"Labels", labels(i.Labels)},
		{"Created", ticketTimestamp(i.Created)},
		{"Updated", ticketTimestamp(i.Updated)},
	}
	var lines []string
	for _, field := range fields {
		lines = append(lines, wrapDetailLine(fieldLabelStyle.Render(field[0]+":")+" "+field[1], d.width)...)
	}
	return lines
}

func (d *detailPane) renderComments() []string {
	switch {
	case d.commentsLoading:
		return []string{spinnerFrames[d.frame] + " Loading comments…"}
	case d.commentsErr != nil:
		return []string{"Comments could not be loaded."}
	case len(d.comments) == 0:
		return []string{"No comments."}
	}
	var lines []string
	for index, comment := range d.comments {
		if index > 0 {
			lines = append(lines, "")
		}
		lines = append(lines, renderComment(comment, d.width)...)
	}
	return lines
}

func (d *detailPane) renderAttachments() []string {
	if len(d.issue.Attachments) == 0 {
		return []string{"No attachments."}
	}
	var lines []string
	for index, attachment := range d.issue.Attachments {
		if index > 0 {
			lines = append(lines, "")
		}
		lines = append(lines, wrapDetailLine(itemKeyStyle.Render(attachment.Filename), d.width)...)
		meta := attachmentSize(attachment.Size) + " · " + valueOr(attachment.MimeType, "unknown type") +
			" · " + userName(attachment.Author, "Unknown author") + " · " + attachmentTimestamp(attachment.Created)
		lines = append(lines, wrapDetailLine(noteStyle.Render(meta), d.width)...)
	}
	return lines
}

func (d *detailPane) renderLinks() []string {
	if len(d.issue.Links) == 0 {
		return []string{"No links."}
	}
	var lines []string
	for index, link := range d.issue.Links {
		if index > 0 {
			lines = append(lines, "")
		}
		lines = append(lines, wrapDetailLine(noteStyle.Render(link.Relation), d.width)...)
		lines = append(lines, relatedItem(link.Key, link.Summary, link.Status, link.Type, d.width)...)
	}
	return lines
}

func (d *detailPane) renderSubtasks() []string {
	if len(d.issue.Subtasks) == 0 {
		return []string{"No subtasks."}
	}
	var lines []string
	for index, subtask := range d.issue.Subtasks {
		if index > 0 {
			lines = append(lines, "")
		}
		lines = append(lines, relatedItem(subtask.Key, subtask.Summary, subtask.Status, subtask.Type, d.width)...)
	}
	return lines
}

// relatedItem is how another work item is written wherever one is referred to:
// its key, its summary, and the state and kind it is in, coloured the way the
// list colours the same values.
func relatedItem(key, summary string, status jira.Status, kind string, width int) []string {
	lines := wrapDetailLine(keyStyle.Render(key)+" "+summary, width)
	meta := stateStyle.Render(valueOr(status.Name, "None")) + " · " + kindStyle.Render(valueOr(kind, "None"))
	return append(lines, wrapDetailLine(meta, width)...)
}

func attachmentSize(size int64) string {
	units := []struct {
		limit int64
		name  string
	}{{1 << 30, "GB"}, {1 << 20, "MB"}, {1 << 10, "kB"}}
	for _, unit := range units {
		if size >= unit.limit {
			return fmt.Sprintf("%.1f %s", float64(size)/float64(unit.limit), unit.name)
		}
	}
	return fmt.Sprintf("%d B", size)
}

func renderComment(comment jira.Comment, width int) []string {
	lines := wrapDetailLine(commentAuthorStyle.Render(userName(comment.Author, "Unknown author"))+" · "+ticketTimestamp(comment.Created), width)
	if comment.Updated.Sub(comment.Created) >= time.Second {
		lines = append(lines, wrapDetailLine("Edited: "+ticketTimestamp(comment.Updated), width)...)
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

// setTab moves to a page of the pane, parking the current page's scroll
// position so that coming back lands where the eye left off.
func (d *detailPane) setTab(tab detailTab) {
	if tab < 0 || tab >= tabCount || tab == d.tab {
		return
	}
	d.tops[d.tab] = d.top
	d.tab = tab
	d.top = d.tops[tab]
	d.render()
}

// cycleTab moves through the tab strip and wraps at either end.
func (d *detailPane) cycleTab(delta int) {
	d.setTab((d.tab + detailTab(delta) + tabCount) % tabCount)
}

func (d *detailPane) move(action config.Action, count int) {
	n := max(count, 1)
	half := max(d.bodyRows()/2, 1)
	switch action {
	case config.ActionDown:
		d.top += n
	case config.ActionUp:
		d.top -= n
	case config.ActionTop:
		d.top = max(count-1, 0)
	case config.ActionBottom:
		d.top = len(d.lines) - d.bodyRows()
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
		d.top += max(d.bodyRows()-n, 0)
	}
	d.clamp()
}

// bodyRows is what is left for content once the tab strip has taken its two
// rows.
func (d *detailPane) bodyRows() int { return max(d.rows-tabBarRows, 0) }

func (d *detailPane) clamp() {
	d.top = min(max(d.top, 0), max(len(d.lines)-d.bodyRows(), 0))
}

func (d *detailPane) view() []string {
	var lines []string
	switch {
	case d.loading:
		lines = []string{spinnerFrames[d.frame] + " Loading…"}
	case d.err != nil:
		lines = []string{"Detail could not be loaded.", d.err.Error()}
	case len(d.lines) > 0:
		end := min(d.top+d.bodyRows(), len(d.lines))
		lines = append(lines, d.lines[d.top:end]...)
	default:
		lines = []string{"No detail loaded."}
	}
	if d.issue == nil {
		return lines
	}
	return append(d.tabBar(), lines...)
}

// tabBar draws the strip jiratui puts above a work item: the tab names in a
// row, the active one reversed out of the accent colour, over a rule that runs
// the width of the pane and brightens under the active tab. It is what says
// which page of the item is on screen without spending a line on a title.
//
// A pane too narrow for every name shows a window of them around the active
// tab, marked with chevrons at whichever end is cut, because a strip of
// ellipses says nothing about where the user is.
func (d *detailPane) tabBar() []string {
	const gap = 2
	first, last := d.visibleTabs(gap)

	var labels, rule strings.Builder
	write := func(text string, active bool) {
		if active {
			labels.WriteString(activeTabStyle.Render(text))
			rule.WriteString(activeTabRuleStyle.Render(strings.Repeat("─", ansi.StringWidth(text))))
			return
		}
		labels.WriteString(inactiveTabStyle.Render(text))
		rule.WriteString(inactiveTabRuleStyle.Render(strings.Repeat("─", ansi.StringWidth(text))))
	}
	if first > 0 {
		write("‹ ", false)
	}
	for tab := first; tab <= last; tab++ {
		if tab > first {
			write(strings.Repeat(" ", gap), false)
		}
		write(detailTabNames[tab], tab == d.tab)
	}
	if last < tabCount-1 {
		write(" ›", false)
	}
	if pad := d.width - ansi.StringWidth(ansi.Strip(rule.String())); pad > 0 {
		rule.WriteString(inactiveTabRuleStyle.Render(strings.Repeat("─", pad)))
	}
	return []string{fitWidth(labels.String(), d.width), fitWidth(rule.String(), d.width)}
}

// visibleTabs is the run of tabs that fits the pane, grown outwards from the
// active one so that it is always on screen and its neighbours are shown in
// preference to distant tabs.
func (d *detailPane) visibleTabs(gap int) (detailTab, detailTab) {
	first, last := d.tab, d.tab
	width := ansi.StringWidth(detailTabNames[d.tab]) + 4 // room for both chevrons
	for {
		grew := false
		if last < tabCount-1 && width+gap+ansi.StringWidth(detailTabNames[last+1]) <= d.width {
			last++
			width += gap + ansi.StringWidth(detailTabNames[last])
			grew = true
		}
		if first > 0 && width+gap+ansi.StringWidth(detailTabNames[first-1]) <= d.width {
			first--
			width += gap + ansi.StringWidth(detailTabNames[first])
			grew = true
		}
		if !grew {
			return first, last
		}
	}
}

func (d *detailPane) close() {
	d.cancelFetch()
	request, tab := d.request+1, d.tab
	*d = detailPane{request: request, tab: tab}
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

func attachmentTimestamp(t time.Time) string {
	return formatTimestamp(t, "2006-01-02 15:04 MST")
}

func ticketTimestamp(t time.Time) string {
	return formatTimestamp(t, "Jan 2, 2006 15:04 MST")
}

func formatTimestamp(t time.Time, layout string) string {
	if t.IsZero() {
		return "Unknown"
	}
	return t.Format(layout)
}

// sectionHeader marks a section of the detail pane the way jiratui marks one:
// an accented title over a rule that runs the width of the pane.
func sectionHeader(title string, width int) string {
	if width <= 0 {
		return ""
	}
	return sectionStyle.Render(title)
}
