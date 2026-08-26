package ui

import (
	"strconv"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/nikhil-sharma-b/jira-tui/internal/config"
	"github.com/nikhil-sharma-b/jira-tui/internal/jira"
)

// SelectedMarker is drawn in the gutter of the selected row. The selection is
// marked with a character as well as with styling because a row that is only
// reverse-video disappears under a terminal theme that has its own opinion
// about reverse-video, and because it gives tests something to read.
const SelectedMarker = "▸ "

// gutterWidth is the width of SelectedMarker, which is also the indent of
// every unselected row so that the columns line up.
const gutterWidth = 2

// pageSize is how many issues one search asks for. Fifty is several screens on
// any terminal, so the first page is nearly always the only one fetched, and
// it is small enough that the first rows appear quickly on a slow link.
const pageSize = 50

// pageLookahead is how close the selection may get to the end of what is
// loaded before the next page is requested. Paging on approach rather than on
// arrival is what makes a long list feel continuous.
const pageLookahead = 10

// list holds the result set and where the user is in it. It is deliberately
// not a bubbles component: the motions come from the dispatcher as named
// actions, and a component with its own keymap would be a second place for a
// key to be bound.
type list struct {
	issues []*jira.Issue
	cursor int
	// top is the index of the first drawn row. Selection and viewport are
	// tracked separately because H, M and L address the viewport and every
	// other motion addresses the result set.
	top int
	// rows is how many issues fit on screen, set from the window size.
	rows int
}

// refresh puts a live reading of one work item into the row that shows it, so
// a status the user just changed is not left saying what a cached search said
// a minute ago. Only the fields a live read asked for are replaced: the row may
// display columns the detail fetch never requested, and overwriting those with
// zero values would blank them.
func (l *list) refresh(issue *jira.Issue) {
	if issue == nil {
		return
	}
	for _, row := range l.issues {
		if row.Key != issue.Key {
			continue
		}
		row.Summary = issue.Summary
		row.Status = issue.Status
		row.Assignee = issue.Assignee
		row.Priority = issue.Priority
		row.Labels = issue.Labels
		row.Updated = issue.Updated
		return
	}
}

// visible returns the slice currently drawn.
func (l *list) visible() []*jira.Issue {
	if len(l.issues) == 0 || l.rows <= 0 {
		return nil
	}
	end := min(l.top+l.rows, len(l.issues))
	return l.issues[l.top:end]
}

// anchor is where the user was: the work item they were on and how far down
// the screen it sat. The key is the only durable name for a row -- an index
// means something different the moment the result set changes underneath --
// and the offset is what keeps a refresh from jumping the viewport.
type anchor struct {
	key    string
	offset int
}

// anchor records the current position, key empty when there is no selection.
func (l *list) anchor() anchor {
	a := anchor{offset: l.cursor - l.top}
	if l.cursor >= 0 && l.cursor < len(l.issues) {
		a.key = l.issues[l.cursor].Key
	}
	return a
}

// restore puts the selection back where it was after the result set was
// replaced. A key that is gone from the new result set leaves the selection at
// its old index, clamped, which is the nearest thing to "where the user was"
// that remains true.
func (l *list) restore(a anchor) {
	if a.key != "" {
		for i, issue := range l.issues {
			if issue.Key == a.key {
				l.cursor = i
				break
			}
		}
	}
	l.top = l.cursor - max(a.offset, 0)
	l.clamp()
}

// move applies a motion. count is the numeric prefix, zero when none was
// typed.
func (l *list) move(action config.Action, count int) {
	if len(l.issues) == 0 {
		return
	}
	n := max(count, 1)
	half := max(l.rows/2, 1)

	switch action {
	case config.ActionDown:
		l.cursor += n
	case config.ActionUp:
		l.cursor -= n
	case config.ActionTop:
		// A count turns gg into "go to this line", as in vim, which is the
		// only reading under which counting a jump to the top means anything.
		l.cursor = 0
		if count > 0 {
			l.cursor = count - 1
		}
	case config.ActionBottom:
		l.cursor = len(l.issues) - 1
		if count > 0 {
			l.cursor = count - 1
		}
	case config.ActionHalfPageDown:
		l.cursor += n * half
	case config.ActionHalfPageUp:
		l.cursor -= n * half
	// The viewport motions count lines in from an edge of the screen, as in
	// vim, so 3H is the third visible row and 3L the third from the bottom.
	case config.ActionViewportTop:
		l.cursor = l.top + n - 1
	case config.ActionViewportMid:
		l.cursor = l.top + len(l.visible())/2
	case config.ActionViewportBot:
		l.cursor = l.top + len(l.visible()) - n
	default:
		return
	}
	l.clamp()
}

// selectRow puts the selection on an index and brings it on screen. It is the
// list's own, so that anything with a row in mind -- a motion, a search --
// says where to go rather than how the viewport follows.
func (l *list) selectRow(i int) {
	l.cursor = i
	l.clamp()
}

// clamp keeps the selection inside the result set and the viewport around the
// selection. Both are done together because a motion is only correct once the
// row it selected is on screen.
func (l *list) clamp() {
	l.cursor = min(max(l.cursor, 0), max(len(l.issues)-1, 0))
	if l.rows <= 0 {
		l.top = 0
		return
	}
	if l.cursor < l.top {
		l.top = l.cursor
	}
	if l.cursor >= l.top+l.rows {
		l.top = l.cursor - l.rows + 1
	}
	l.top = min(max(l.top, 0), max(len(l.issues)-l.rows, 0))
}

// wantsMore reports that the selection has come close enough to the end of the
// loaded rows that the next page should be on its way.
func (l *list) wantsMore() bool {
	return l.cursor >= len(l.issues)-pageLookahead
}

// header renders the column titles at the given widths, behind a right-aligned
// "#" over the row numbers.
func header(cols []Column, widths []int, numWidth int) string {
	return headerStyle.Render(strings.Repeat(" ", gutterWidth) + numberCell("#", numWidth) +
		row(cols, widths, func(i int) string {
			return cols[i].Title
		}))
}

// numberWidth is how wide the row-number column has to be to hold the largest
// number that will be drawn in it, plus the gap after it. Zero rows means no
// column at all rather than an empty one.
func numberWidth(rows int) int {
	if rows <= 0 {
		return 0
	}
	return len(strconv.Itoa(rows)) + columnGap
}

// numberCell right-aligns a row number in a column of the given total width,
// the gap after it included. Right alignment is what keeps the digits of a
// three-figure list in one stack rather than a staircase.
func numberCell(text string, width int) string {
	if width <= 0 {
		return ""
	}
	if pad := width - columnGap - ansi.StringWidth(text); pad > 0 {
		text = strings.Repeat(" ", pad) + text
	}
	return ansi.Truncate(text, width-columnGap, "") + strings.Repeat(" ", columnGap)
}

// styledRow assembles one line with each cell in its column's own colour. The
// header and the selected row do not use it: both are one colour by design,
// and a selected row striped in six is a row that no longer reads as selected.
func styledRow(cols []Column, widths []int, at func(int) string) string {
	var b strings.Builder
	for i := range cols {
		if widths[i] <= 0 {
			continue
		}
		if b.Len() > 0 {
			b.WriteString(strings.Repeat(" ", columnGap))
		}
		b.WriteString(cols[i].style.Render(cell(at(i), widths[i])))
	}
	return b.String()
}

// row assembles one line from a per-column cell function, skipping the columns
// the layout dropped.
func row(cols []Column, widths []int, at func(int) string) string {
	var b strings.Builder
	for i := range cols {
		if widths[i] <= 0 {
			continue
		}
		if b.Len() > 0 {
			b.WriteString(strings.Repeat(" ", columnGap))
		}
		b.WriteString(cell(at(i), widths[i]))
	}
	return strings.TrimRight(b.String(), " ")
}

var (
	headerStyle = lipgloss.NewStyle().Bold(true).Foreground(accent)
	// selectedStyle paints the whole selected row, as the reference TUI does,
	// rather than reversing it: a solid band survives terminal themes that
	// have their own reading of reverse-video.
	selectedStyle = lipgloss.NewStyle().Foreground(selectionFG).Background(selectionBG)
	statusStyle   = lipgloss.NewStyle().Faint(true)
	errorStyle    = lipgloss.NewStyle().Bold(true)
)
