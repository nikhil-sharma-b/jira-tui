package ui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
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

// visible returns the slice currently drawn.
func (l *list) visible() []*jira.Issue {
	if len(l.issues) == 0 || l.rows <= 0 {
		return nil
	}
	end := min(l.top+l.rows, len(l.issues))
	return l.issues[l.top:end]
}

// move applies a motion. count is the numeric prefix, zero when none was
// typed. It returns whether the selection changed, which is what decides
// whether paging is worth reconsidering.
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
	case config.ActionViewportTop:
		l.cursor = l.top
	case config.ActionViewportMid:
		l.cursor = l.top + len(l.visible())/2
	case config.ActionViewportBot:
		l.cursor = l.top + len(l.visible()) - 1
	default:
		return
	}
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

// header renders the column titles at the given widths.
func header(cols []Column, widths []int) string {
	return headerStyle.Render(strings.Repeat(" ", gutterWidth) + row(cols, widths, func(i int) string {
		return cols[i].Title
	}))
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
	headerStyle   = lipgloss.NewStyle().Bold(true)
	selectedStyle = lipgloss.NewStyle().Reverse(true)
	statusStyle   = lipgloss.NewStyle().Faint(true)
	errorStyle    = lipgloss.NewStyle().Bold(true)
)
