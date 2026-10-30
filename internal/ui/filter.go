package ui

import (
	"slices"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/nikhil-sharma-b/jira-tui/internal/jira"
)

// :filter narrows the work items pane to the rows that contain a pattern. It
// is the third kind of search: / moves between matches and leaves every row
// up, :jql asks the site for a different result set, and :filter hides the
// loaded rows that do not match. It matches the way / does, over the same
// text, so that what / would find is exactly what :filter keeps.
//
// The filter belongs to the model rather than to the list, because R replaces
// the list wholesale and a filter is part of what R is asked to reread.

// filterEagerRows is how many work items a filter loads without being asked.
// A filter over the first page alone would hide matches that are one page
// further on, so it pages on its own, as the board does, up to the same cap.
// Past it, narrowing the query with :jql is the way to see further.
const filterEagerRows = boardEagerRows

// runFilter is :filter <pattern>, and a bare :filter shows every row again.
func runFilter(m *Model, arg string) tea.Cmd {
	m.setFilter(arg)
	// A filter is asked for in order to look at the list, so a pinned session
	// or a zoomed detail brings it up.
	m.goList()
	return m.maybePage()
}

// setFilter applies a pattern, empty for none. The selection stays on the
// work item it was on when that item is still shown, and otherwise goes to
// the first one that is.
func (m *Model) setFilter(pattern string) {
	where := m.list.anchor()
	m.filter = pattern
	m.refilter()
	if !slices.ContainsFunc(m.list.issues, func(i *jira.Issue) bool { return i.Key == where.key }) {
		m.list.cursor, where = 0, anchor{}
	}
	m.list.restore(where)
}

// refilter derives the rows on screen from the rows loaded. Without a filter
// they are the same slice; with one, a new slice of the same work items, so a
// live update to a row shows in both.
func (m *Model) refilter() {
	if m.filter == "" {
		m.list.issues = m.list.all
		return
	}
	needle := strings.ToLower(m.filter)
	now := m.now()
	shown := make([]*jira.Issue, 0, len(m.list.all))
	for _, issue := range m.list.all {
		if m.rowContains(issue, needle, now) {
			shown = append(shown, issue)
		}
	}
	m.list.issues = shown
}
