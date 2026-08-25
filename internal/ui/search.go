package ui

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/nikhil-sharma-b/jira-tui/internal/jira"
)

// In-pane search is the half of the vim split that never leaves the machine:
// / narrows what is already loaded, :jql goes and asks for something else.
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
	return m.moveToMatch(1, 1, true)
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
