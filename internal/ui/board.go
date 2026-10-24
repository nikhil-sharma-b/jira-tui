// The board: the list's result set drawn as kanban lanes, one per status, side
// by side. It is a second way of drawing the same rows rather than a second
// result set -- the query, the paging and the selection all belong to the list
// -- so everything that acts on "the selected work item" acts on the selected
// card without having to know which view is up.

package ui

import (
	"fmt"
	"slices"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/nikhil-sharma-b/jira-tui/internal/config"
	"github.com/nikhil-sharma-b/jira-tui/internal/jira"
)

// laneMinWidth is the narrowest a lane is drawn. Below it a summary wraps a
// word to a line, so lanes that do not fit across the pane scroll sideways
// rather than squeeze.
const laneMinWidth = 26

// laneGap is the rule drawn between two lanes.
const laneGap = 1

// laneHeaderRows is a lane's name and the rule under it.
const laneHeaderRows = 2

// cardGap is the blank row between two cards.
const cardGap = 1

// summaryLines is the most of its summary a card shows. Two lines is most
// summaries whole, and a cap keeps one long summary from taking a lane over.
const summaryLines = 2

// boardEagerRows is how many work items the board loads without being asked.
// A lane only tells the truth when every item in that status is in it, so the
// board keeps paging until the result set ends or reaches this; past it, pages
// are fetched as the selection nears the end of a lane, as the list does.
const boardEagerRows = 250

// noStatus names the lane of work items whose status is not known, which is
// what rows fetched for a list without a status column are while the board's
// own search is on its way.
const noStatus = "No status"

// board is where the board is scrolled to. Lanes scroll independently, since
// one long lane must not carry the short ones beside it off screen, and they
// are remembered by status name: an index would move to another lane the
// moment a status gained or lost its last work item.
type board struct {
	tops map[string]int
	// left is the first lane drawn when they do not all fit across the pane.
	left int
}

func (b *board) setTop(l lane, top int) {
	if b.tops == nil {
		b.tops = map[string]int{}
	}
	b.tops[l.name] = top
}

// lane is one status and the work items in it, as indexes into the list, in
// the query's own order.
type lane struct {
	name  string
	rank  int
	cards []int
}

// lanes groups the result set by status. Lanes run in the order work moves
// through them -- to do, in progress, done -- and within that in the order the
// query first met them, which is the closest thing to the workflow's own order
// that a search reports.
func lanes(issues []*jira.Issue) []lane {
	var out []lane
	at := map[string]int{}
	for i, issue := range issues {
		name := issue.Status.Name
		if name == "" {
			name = noStatus
		}
		j, ok := at[name]
		if !ok {
			j = len(out)
			at[name] = j
			out = append(out, lane{name: name, rank: statusRank(issue.Status.Category)})
		}
		out[j].cards = append(out[j].cards, i)
	}
	slices.SortStableFunc(out, func(a, b lane) int { return a.rank - b.rank })
	return out
}

// statusRank orders status categories left to right. A status with no
// category goes last rather than being guessed into one.
func statusRank(category string) int {
	switch category {
	case "new":
		return 0
	case "indeterminate":
		return 1
	case "done":
		return 2
	}
	return 3
}

// locate finds the lane holding a work item and where in it the item is, -1
// for both when no lane does.
func locate(ls []lane, index int) (int, int) {
	for li, l := range ls {
		if pi := slices.Index(l.cards, index); pi >= 0 {
			return li, pi
		}
	}
	return -1, -1
}

// boardOrder is the result set's indexes in the order the board reads: lane by
// lane, each from the top.
func boardOrder(ls []lane) []int {
	var out []int
	for _, l := range ls {
		out = append(out, l.cards...)
	}
	return out
}

// follow is the first of n lanes drawn when shown of them fit, moved from
// first only as far as it takes to bring sel into view, as the list's viewport
// follows its selection.
func follow(first, sel, shown, n int) int {
	if sel >= 0 {
		if sel < first {
			first = sel
		}
		if sel >= first+shown {
			first = sel - shown + 1
		}
	}
	return min(max(first, 0), max(n-shown, 0))
}

// scrollTo is the first card a lane draws, given the cards' heights, the rows
// they have, where the lane was last scrolled to, and the selected card's
// place in it -- -1 when the selection is in another lane. Like follow it
// moves only as far as it must, and it never scrolls past the point where the
// last card sits at the bottom, which would leave the lane half empty.
func scrollTo(heights []int, top, sel, rows int) int {
	if len(heights) == 0 {
		return 0
	}
	last := len(heights) - 1
	for last > 0 && fits(heights[last-1:], rows) {
		last--
	}
	top = min(max(top, 0), last)
	if sel < 0 {
		return top
	}
	if sel < top {
		return sel
	}
	for top < sel && !fits(heights[top:sel+1], rows) {
		top++
	}
	return top
}

// fits reports whether cards of these heights, and the gaps between them, fit
// in rows.
func fits(heights []int, rows int) bool {
	total := cardGap * (len(heights) - 1)
	for _, h := range heights {
		total += h
	}
	return total <= rows
}

// shownFrom is how many cards are drawn from top: as many as fit, and always
// one, since a card too tall for the lane is better cut off than missing.
func shownFrom(heights []int, top, rows int) int {
	if top >= len(heights) {
		return 0
	}
	k := 1
	for top+k < len(heights) && fits(heights[top:top+k+1], rows) {
		k++
	}
	return k
}

// boardFit is how many of n lanes fit across width and how wide each is. Every
// lane is as wide as the others, so that a card wraps the same in whichever
// lane it is in; the few columns left over are left blank.
func boardFit(width, n int) (shown, laneWidth int) {
	shown = min(max((width+laneGap)/(laneMinWidth+laneGap), 1), n)
	if shown <= 0 {
		return 0, 0
	}
	return shown, max((width-laneGap*(shown-1))/shown, 1)
}

// laneRows is what a lane in a pane rows tall has left for its cards.
func laneRows(rows int) int { return max(rows-laneHeaderRows, 1) }

// metaColumns are the columns a card shows on its last line: everything but
// the key, which heads the card, the summary, which is its body, and the
// status, which is the lane it sits in.
func metaColumns(cols []Column) []Column {
	var out []Column
	for _, c := range cols {
		if c.Field == "" || c.Field == "summary" || c.Field == "status" {
			continue
		}
		out = append(out, c)
	}
	return out
}

func hasSummary(cols []Column) bool {
	return slices.ContainsFunc(cols, func(c Column) bool { return c.Field == "summary" })
}

// cardHeights is how many rows each card in a lane takes at a width: its key,
// then as much of the summary as it shows, then the rest of the columns when
// there are any. A card shows only what the columns fetched, since a field
// that was never asked for would read as empty rather than as unknown.
func (m *Model) cardHeights(l lane, width int) []int {
	summary, meta := hasSummary(m.columns), len(metaColumns(m.columns)) > 0
	heights := make([]int, len(l.cards))
	for i, index := range l.cards {
		h := 1
		if summary {
			h += len(summaryRows(m.list.issues[index].Summary, width-gutterWidth))
		}
		if meta {
			h++
		}
		heights[i] = h
	}
	return heights
}

// laneTop is the first card a lane draws at a width and a height, sel the
// selected card's place in it or -1 when the selection is elsewhere.
func (m *Model) laneTop(l lane, width, rows, sel int) int {
	return scrollTo(m.cardHeights(l, width), m.board.tops[l.name], sel, rows)
}

// boardSize is the content size of the work items pane, by the same
// arithmetic View uses for the frame it draws the pane in.
func (m *Model) boardSize() (width, rows int) {
	listVisible, detailVisible := m.visiblePanes()
	width = m.width
	if listVisible && detailVisible {
		width, _ = m.paneWidths()
	}
	return inner(width), inner(max(m.height-len(m.footer()), 0))
}

// moveOnBoard applies a motion to the selected card. Up and down stay in its
// lane, and a lane motion keeps the selection the same number of cards down
// the screen, which is where the eye already is.
func (m *Model) moveOnBoard(action config.Action, count int) {
	ls := lanes(m.list.issues)
	li, pi := locate(ls, m.list.cursor)
	if li < 0 {
		return
	}
	width, rows := m.boardSize()
	shown, laneWidth := boardFit(width, len(ls))
	rows = laneRows(rows)
	n := max(count, 1)
	// What is on screen is where a motion starts from, so the remembered
	// scroll is first brought round to it.
	heights := m.cardHeights(ls[li], laneWidth)
	top := scrollTo(heights, m.board.tops[ls[li].name], pi, rows)
	visible := shownFrom(heights, top, rows)

	switch action {
	case config.ActionDown:
		pi += n
	case config.ActionUp:
		pi -= n
	case config.ActionTop:
		pi = max(count-1, 0)
	case config.ActionBottom:
		pi = len(ls[li].cards) - 1
		if count > 0 {
			pi = count - 1
		}
	case config.ActionHalfPageDown:
		pi += n * max(visible/2, 1)
	case config.ActionHalfPageUp:
		pi -= n * max(visible/2, 1)
	case config.ActionViewportTop:
		pi = top + n - 1
	case config.ActionViewportMid:
		pi = top + visible/2
	case config.ActionViewportBot:
		pi = top + visible - n
	case config.ActionLaneLeft, config.ActionLaneRight:
		if action == config.ActionLaneLeft {
			n = -n
		}
		row := pi - top
		li = min(max(li+n, 0), len(ls)-1)
		pi = m.laneTop(ls[li], laneWidth, rows, -1) + row
	default:
		return
	}
	pi = min(max(pi, 0), len(ls[li].cards)-1)
	m.list.selectRow(ls[li].cards[pi])
	m.board.setTop(ls[li], m.laneTop(ls[li], laneWidth, rows, pi))
	m.board.left = follow(m.board.left, li, shown, len(ls))
}

// boardWantsMore is wantsMore for the board: everything up to boardEagerRows,
// and past that the next page once the selection nears the end of its lane.
func (m *Model) boardWantsMore() bool {
	if len(m.list.issues) < boardEagerRows {
		return true
	}
	ls := lanes(m.list.issues)
	li, pi := locate(ls, m.list.cursor)
	return li >= 0 && pi >= len(ls[li].cards)-pageLookahead
}

// matchOnBoard is the work item a search lands on when the board is up: the
// matches are walked in the order the board reads, so n goes down a lane and
// on to the next one rather than jumping about in the query's order.
func (m *Model) matchOnBoard(matches []int, direction, count int, inclusive bool) int {
	order := boardOrder(lanes(m.list.issues))
	at := make([]int, len(m.list.issues))
	for p, i := range order {
		at[i] = p
	}
	positions := make([]int, len(matches))
	for k, i := range matches {
		positions[k] = at[i]
	}
	slices.Sort(positions)
	return order[matchAt(positions, at[m.list.cursor], direction, count, inclusive)]
}

// boardPosition is where the selection is for the status line: its lane, and
// how far down the lane it is.
func (m *Model) boardPosition() string {
	ls := lanes(m.list.issues)
	li, pi := locate(ls, m.list.cursor)
	if li < 0 {
		return ""
	}
	return fmt.Sprintf("%s %d/%d", ls[li].name, pi+1, len(ls[li].cards))
}

// boardPane draws the lanes that fit, each scrolled to its own place, with the
// selected card's lane brought into view.
func (m *Model) boardPane(width, rows int) []string {
	if len(m.list.issues) == 0 || rows <= 0 || width <= 0 {
		return m.emptyPane(width, rows)
	}
	ls := lanes(m.list.issues)
	shown, laneWidth := boardFit(width, len(ls))
	sel, pos := locate(ls, m.list.cursor)
	left := follow(m.board.left, sel, shown, len(ls))

	drawn := make([][]string, shown)
	for i := range shown {
		li := left + i
		p := -1
		if li == sel {
			p = pos
		}
		more := [2]bool{i == 0 && left > 0, i == shown-1 && left+shown < len(ls)}
		drawn[i] = m.laneLines(ls[li], laneWidth, rows, p, more)
	}

	rule, edge := noteStyle.Render("│"), noteStyle.Render("┼")
	// The columns the lanes leave over still carry the rule under the
	// headers, so it runs to the frame rather than stopping short of it.
	tail := width - shown*laneWidth - laneGap*(shown-1)
	tailStyle := noteStyle
	if sel == left+shown-1 {
		tailStyle = accentStyle
	}
	lines := make([]string, rows)
	for r := range rows {
		var b strings.Builder
		for i, l := range drawn {
			if i > 0 {
				if r == 1 {
					b.WriteString(edge)
				} else {
					b.WriteString(rule)
				}
			}
			b.WriteString(l[r])
		}
		if r == 1 && tail > 0 {
			b.WriteString(tailStyle.Render(strings.Repeat("─", tail)))
		}
		lines[r] = b.String()
	}
	return lines
}

// laneLines draws one lane exactly width columns wide and rows tall: its name
// and count over a rule, then the cards that fit from where it is scrolled to.
// sel is the selected card's place in the lane, -1 when it is elsewhere, and
// more says whether lanes are hidden off the pane's left and right edges.
func (m *Model) laneLines(l lane, width, rows, sel int, more [2]bool) []string {
	heights := m.cardHeights(l, width)
	top := scrollTo(heights, m.board.tops[l.name], sel, laneRows(rows))
	end := top + shownFrom(heights, top, laneRows(rows))

	name := stateStyle.Bold(true).Render(l.name) + " " + noteStyle.Render(fmt.Sprint(len(l.cards)))
	if top > 0 {
		name += noteStyle.Render(" ↑")
	}
	if end < len(l.cards) {
		name += noteStyle.Render(" ↓")
	}
	gutter := strings.Repeat(" ", gutterWidth)
	if more[0] {
		gutter = noteStyle.Render("‹ ")
	}
	if more[1] {
		name += noteStyle.Render(" ›")
	}
	ruleStyle := noteStyle
	if sel >= 0 {
		ruleStyle = accentStyle
	}

	blank := strings.Repeat(" ", width)
	lines := []string{pad(fitWidth(gutter+name, width), width), ruleStyle.Render(strings.Repeat("─", width))}
	now := m.now()
	for p := top; p < end; p++ {
		if p > top {
			lines = append(lines, blank)
		}
		lines = append(lines, m.cardLines(m.list.issues[l.cards[p]], width, p == sel, now)...)
	}
	for len(lines) < rows {
		lines = append(lines, blank)
	}
	return lines[:rows]
}

// cardLines draws one card exactly width columns wide. The selected card is a
// solid band, as the selected row of the list is, with the marker in its
// gutter; the others are drawn in their columns' own colours.
func (m *Model) cardLines(issue *jira.Issue, width int, selected bool, now time.Time) []string {
	plain := []string{issue.Key}
	styled := []string{keyStyle.Render(issue.Key)}
	if hasSummary(m.columns) {
		for _, line := range summaryRows(issue.Summary, width-gutterWidth) {
			plain, styled = append(plain, line), append(styled, line)
		}
	}
	if meta := metaColumns(m.columns); len(meta) > 0 {
		var p, s []string
		for _, c := range meta {
			if v := c.Render(issue, now); v != "" {
				p, s = append(p, v), append(s, c.style.Render(v))
			}
		}
		plain = append(plain, strings.Join(p, " · "))
		styled = append(styled, strings.Join(s, noteStyle.Render(" · ")))
	}

	out := make([]string, len(plain))
	for i := range plain {
		gutter := strings.Repeat(" ", gutterWidth)
		if !selected {
			out[i] = pad(fitWidth(gutter+styled[i], width), width)
			continue
		}
		if i == 0 {
			gutter = SelectedMarker
		}
		line := pad(fitWidth(gutter+plain[i], width), width)
		out[i] = highlight(line, m.search.shown(), selectedStyle, selectedMatchStyle)
	}
	return out
}

// summaryRows wraps a summary into at most summaryLines rows, the last ending
// in an ellipsis when there was more to say. An empty summary takes no rows.
func summaryRows(summary string, width int) []string {
	if strings.TrimSpace(summary) == "" || width <= 0 {
		return nil
	}
	wrapped := wrapDetailLine(summary, width)
	if len(wrapped) <= summaryLines {
		return wrapped
	}
	out := wrapped[:summaryLines]
	out[summaryLines-1] = ansi.Truncate(out[summaryLines-1]+" "+wrapped[summaryLines], width, "…")
	return out
}

// toggleView is gv: the other view, with the work items pane revealed and
// focused, since switching how a pane is drawn is asking to look at it.
func (m *Model) toggleView() tea.Cmd {
	view := config.ViewBoard
	if m.view == config.ViewBoard {
		view = config.ViewList
	}
	m.goList()
	return m.setView(view)
}

// setView draws the work items pane the given way. The board needs every
// row's status whether or not a column shows it, so a switch that changes what
// a search asks for runs the query again -- through the cache, like any other
// view, and leaving the rows up until the answer lands.
func (m *Model) setView(view string) tea.Cmd {
	if view == m.view {
		return nil
	}
	before := m.searchFields()
	m.view = view
	if m.columns == nil {
		// Nothing has been asked for yet, and the first search reads the view
		// when it is built.
		return nil
	}
	if !slices.Equal(before, m.searchFields()) {
		return m.runQuery(m.query)
	}
	return m.maybePage()
}
