package ui

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/nikhil-sharma-b/jira-tui/internal/jira"
)

// In-pane search is the half of the vim split that never leaves the machine:
// / searches the focused pane, what is already loaded; :jql and <leader>/ go
// and ask the site for something else.
// Keeping them apart is what makes / instant and what stops a typo in it from
// costing a round trip.

// paneSearch is the in-pane search state: a pattern and nothing else. Which
// rows match is recomputed rather than stored, because the loaded set moves
// underneath it -- a page lands, a revalidation replaces the rows -- and a
// remembered list of indexes would then point at rows that have shifted.
type paneSearch struct {
	pattern string
}

func (s paneSearch) active() bool { return s.pattern != "" }

// runSearch takes a pattern typed at / and moves to the first match at or
// after the selection, as vim does: the row the user is on can be the answer.
func (m *Model) runSearch(pattern string) tea.Cmd {
	if pattern == "" {
		// Submitting an empty line keeps the previous pattern, which is what
		// makes an accidental / harmless.
		return nil
	}
	m.search.pattern = pattern
	m.detail.hit = -1
	return m.moveToMatch(1, 1, true)
}

// searchesDetail reports whether / and n act on the detail pane: it has focus
// and has something on it to search. Otherwise they act on the list, as they
// did before there was a second pane.
func (m *Model) searchesDetail() bool {
	_, detailVisible := m.visiblePanes()
	return m.focus == PaneDetail && detailVisible && m.detail.open && !m.detail.loading
}

// runJiraSearch replaces the list with the work items whose text contains
// what was typed, anywhere on the site. The pattern is kept as the in-pane one
// too, so the rows that come back show why they matched and n walks them.
func (m *Model) runJiraSearch(text string) tea.Cmd {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}
	query := "text ~ " + jqlString(text) + " ORDER BY updated DESC"
	// Recorded as the :jql it stands for, so it can be recalled and refined.
	m.history.add("jql " + query)
	m.search.pattern = text
	m.goList()
	return m.runQuery(query)
}

// jqlString quotes s as a JQL string literal.
func jqlString(s string) string {
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(s) + `"`
}

// moveToMatch moves the selection count matches away in the given direction,
// wrapping at both ends. inclusive lets the row the selection is already on
// count, which is what distinguishes submitting a pattern from pressing n.
//
// It never asks for another page. Search is over what is loaded, by
// definition, and a search that stops to fetch is no longer the fast half of
// the split.
func (m *Model) moveToMatch(direction, count int, inclusive bool) tea.Cmd {
	if !m.search.active() {
		m.status = errors.New("there is no search pattern")
		return nil
	}
	if m.searchesDetail() {
		return m.moveToDetailMatch(direction, count, inclusive)
	}
	matches := m.matchingRows()
	if len(matches) == 0 {
		// The selection stays where it is. A search that found nothing has no
		// row to put the user on, and moving them anywhere would be a lie
		// about what was found.
		m.status = fmt.Errorf("no match for %q", m.search.pattern)
		return nil
	}
	m.status = nil
	m.list.selectRow(matchAt(matches, m.list.cursor, direction, count, inclusive))
	return nil
}

// moveToDetailMatch is moveToMatch over the lines of the detail tab on screen.
// With no hit to count from -- a fresh pattern, a tab change, a scroll away --
// it counts from the top of the viewport, inclusively, which is where the eye
// is.
func (m *Model) moveToDetailMatch(direction, count int, inclusive bool) tea.Cmd {
	d := &m.detail
	var matches []int
	for i, line := range d.lines {
		if lineMatches(line, m.search.pattern) {
			matches = append(matches, i)
		}
	}
	if len(matches) == 0 {
		m.status = fmt.Errorf("no match for %q", m.search.pattern)
		return nil
	}
	m.status = nil
	from := d.hit
	if from < 0 {
		from, inclusive = d.top, true
		if direction < 0 {
			// Backwards from the viewport means from its last line.
			from = min(d.top+d.bodyRows(), len(d.lines))
			inclusive = false
		}
	}
	d.hit = matchAt(matches, from, direction, count, inclusive)
	if d.hit < d.top || d.hit >= d.top+d.bodyRows() {
		d.top = d.hit
		d.clamp()
	}
	return nil
}

// lineMatches reports whether a drawn line contains the pattern, looking only
// at its text: the escape sequences styling it are not something anyone typed
// a pattern to find.
func lineMatches(line, pattern string) bool {
	return strings.Contains(strings.ToLower(ansi.Strip(line)), strings.ToLower(pattern))
}

// matchAt picks the match count steps from where the cursor sits. The
// arithmetic is modular in both directions, which is the wrap: there is no end
// of the list to fall off, only a way back round to the start.
func matchAt(matches []int, from, direction, count int, inclusive bool) int {
	n := len(matches)
	if direction >= 0 {
		start := from + 1
		if inclusive {
			start = from
		}
		// SearchInts lands on the first match at or after start, or on n when
		// there is none -- which the modulo turns into the first match.
		return matches[(sort.SearchInts(matches, start)+count-1)%n]
	}
	// One before the first match at or after the cursor is the last match
	// strictly before it, or -1, which the modulo turns into the last.
	i := sort.SearchInts(matches, from) - 1
	return matches[((i-count+1)%n+n)%n]
}

// matchingRows are the indexes of the loaded rows containing the pattern,
// ascending. The haystack is the columns' own values rather than the drawn
// line, so a match is not lost because the terminal happened to be narrow
// enough to truncate it away.
//
// Matching ignores case, unlike vim's default. A list is scanned rather than
// read, and someone typing a pattern into it is describing what they remember
// seeing, not how the site chose to capitalise it.
func (m *Model) matchingRows() []int {
	needle := strings.ToLower(m.search.pattern)
	now := m.now()
	var out []int
	for i, issue := range m.list.issues {
		if strings.Contains(strings.ToLower(m.rowText(issue, now)), needle) {
			out = append(out, i)
		}
	}
	return out
}

// rowText is everything one row says, untruncated and unpadded.
func (m *Model) rowText(issue *jira.Issue, now time.Time) string {
	cells := make([]string, 0, len(m.columns))
	for _, c := range m.columns {
		cells = append(cells, c.Render(issue, now))
	}
	return strings.Join(cells, " ")
}

// highlight styles every occurrence of the pattern in a drawn line, leaving
// base on the rest. The selected row passes its own base in, so a match on it
// is marked without losing the marking that says it is selected.
func highlight(text, pattern string, base, match lipgloss.Style) string {
	if pattern == "" {
		return base.Render(text)
	}
	haystack, needle := strings.ToLower(text), strings.ToLower(pattern)
	if len(haystack) != len(text) {
		// Lowercasing moved the bytes, so an index into the lowered string is
		// not an index into this one. Matching exactly is wrong in a way the
		// user can see and correct; slicing at the wrong offset is not.
		haystack, needle = text, pattern
	}
	var b strings.Builder
	for {
		i := strings.Index(haystack, needle)
		if i < 0 || needle == "" {
			break
		}
		b.WriteString(base.Render(text[:i]))
		b.WriteString(match.Render(text[i : i+len(needle)]))
		text, haystack = text[i+len(needle):], haystack[i+len(needle):]
	}
	b.WriteString(base.Render(text))
	return b.String()
}

var (
	// plainStyle is the absence of styling, so that an unselected row and a
	// selected one go through the same rendering path.
	plainStyle = lipgloss.NewStyle()
	// matchStyle marks a match. Underline rather than reverse, because the
	// selected row is already reverse and a match on it must still show.
	matchStyle         = lipgloss.NewStyle().Bold(true).Underline(true)
	selectedMatchStyle = selectedStyle.Bold(true).Underline(true)
)
