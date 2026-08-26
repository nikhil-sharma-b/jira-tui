// The which-key menu: while a prefix is in flight, the keys that can follow it
// are listed with what they do. It is the same idea as the pending indicator in
// the status line, carried one step further -- the indicator says a sequence is
// unfinished, this says how to finish it -- and like the help overlay it is
// rendered from the compiled bindings, so a rebound key documents itself.
//
// It floats: the menu is composited over the finished frame rather than added
// to it, because a menu that appears for the length of one keypress must not
// move the rows underneath it. What the user was reading stays where it was,
// and the keys they are half-way through typing are the only thing that moved.

package ui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/x/ansi"
	"github.com/nikhil-sharma-b/jira-tui/internal/config"
)

// whichKeyRows bounds how much of the screen the menu may take. Like the
// picker, it is a strip over the bottom of the panes rather than a modal: the
// item being worked on has to stay readable while the sequence is finished.
const whichKeyRows = 10

// whichKeyGap separates columns of the menu.
const whichKeyGap = "   "

// whichKeyFloat composites the menu over body, the finished pane lines,
// returning them unchanged when no sequence is in flight. The float sits at
// the bottom right, above the status line: out of the way of the list's
// left-hand columns, and next to the pending indicator it belongs to.
func (m *Model) whichKeyFloat(body []string) []string {
	box := m.whichKeyBox()
	if len(box) == 0 || len(body) == 0 {
		return body
	}
	boxWidth := ansi.StringWidth(box[0])
	left := max(m.width-boxWidth-whichKeyMargin, 0)
	// One row clear of the bottom of the panes, so the float never sits on a
	// frame's own edge.
	top := max(len(body)-len(box)-1, 0)

	out := append([]string(nil), body...)
	for i, line := range box {
		row := top + i
		if row >= len(out) {
			break
		}
		out[row] = overlay(out[row], line, left, m.width)
	}
	return out
}

// overlay writes float onto line starting at column left, keeping what the
// line had on either side of it. Both are already-styled strings, so the parts
// are cut by display column rather than by byte.
func overlay(line, float string, left, width int) string {
	line = pad(fitWidth(line, width), width)
	head := ansi.Truncate(line, left, "")
	if pad := left - ansi.StringWidth(head); pad > 0 {
		head += strings.Repeat(" ", pad)
	}
	tail := ansi.TruncateLeft(line, left+ansi.StringWidth(float), "")
	return head + float + tail
}

// whichKeyBox is the menu drawn in its own frame, empty when nothing is
// pending -- which is what keeps it off the screen in the ordinary case.
func (m *Model) whichKeyBox() []string {
	width := m.width - 2*whichKeyMargin
	if width <= frameWidth {
		return nil
	}
	pending := m.dispatch.PendingTokens()
	if len(pending) == 0 {
		return nil
	}
	next := m.bindings.Continuations(pending)
	if len(next) == 0 {
		return nil
	}
	rows := whichKeyGrid(whichKeyEntries(next), inner(width)-2*whichKeyPad)
	if len(rows) == 0 {
		return nil
	}
	// The frame is only as wide as the widest row, so a menu of four keys is a
	// small box in the corner rather than a band across the screen.
	content := 0
	for i, r := range rows {
		rows[i] = strings.Repeat(" ", whichKeyPad) + r
	}
	for _, r := range rows {
		if n := ansi.StringWidth(r) + whichKeyPad; n > content {
			content = n
		}
	}
	title := config.DisplayKeys(pending)
	if n := ansi.StringWidth(title) + 4; n > content+frameWidth {
		content = n - frameWidth
	}
	return boxed(rows, content+frameWidth, len(rows)+frameWidth, title, "", true)
}

// whichKeyMargin keeps the float clear of the screen edge, so its frame reads
// as a thing on top of the panes rather than as part of one.
const whichKeyMargin = 2

// whichKeyPad is the breathing room between the float's frame and its keys.
const whichKeyPad = 1

// whichKeyEntry is one line of the menu: the key and what it leads to.
type whichKeyEntry struct {
	key  string
	what string
	// group says what is a set of further keys rather than an action, which
	// is drawn the way vim's which-key draws it: named with a leading +.
	group bool
}

func whichKeyEntries(next []config.Continuation) []whichKeyEntry {
	out := make([]whichKeyEntry, 0, len(next))
	for _, c := range next {
		if c.Group {
			out = append(out, whichKeyEntry{key: c.Key, what: "+more keys", group: true})
			continue
		}
		out = append(out, whichKeyEntry{key: c.Key, what: actionLabel(c.Action)})
	}
	return out
}

// whichKeyGrid lays the entries out in as many columns as the width allows,
// filling column by column so the keys read down the screen in order. The
// menu is capped at whichKeyRows rows and anything past the last column it can
// fit is dropped -- a menu that pushes the panes off the screen is worse than
// one that is incomplete, and the help overlay lists everything.
func whichKeyGrid(entries []whichKeyEntry, width int) []string {
	if len(entries) == 0 {
		return nil
	}
	cells := make([]string, len(entries))
	keyWidth := 0
	for _, e := range entries {
		if n := ansi.StringWidth(e.key); n > keyWidth {
			keyWidth = n
		}
	}
	colWidth := 0
	for i, e := range entries {
		what := e.what
		style := plainStyle
		if e.group {
			style = noteStyle
		}
		cells[i] = fmt.Sprintf("%s %s %s",
			pendingStyle.Render(pad(e.key, keyWidth)), noteStyle.Render("→"), style.Render(what))
		if n := keyWidth + 3 + ansi.StringWidth(what); n > colWidth {
			colWidth = n
		}
	}

	// Tall and narrow, like vim's own which-key: the keys read down one column
	// and a second is opened only when the first is full, so the float stays a
	// small box in the corner instead of a band across the screen.
	rows := min(len(entries), whichKeyRows)
	cols := (len(entries) + rows - 1) / rows
	if perRow := max((width+len(whichKeyGap))/(colWidth+len(whichKeyGap)), 1); cols > perRow {
		cols = perRow
	}
	rows = (len(entries) + cols - 1) / cols

	lines := make([]string, 0, rows)
	for r := range rows {
		var b strings.Builder
		x := 0
		for c := range cols {
			i := c*rows + r
			if i >= len(entries) {
				break
			}
			gap := ""
			if x > 0 {
				gap = whichKeyGap
			}
			if x+len(gap)+colWidth > width {
				break
			}
			b.WriteString(gap)
			b.WriteString(cells[i])
			b.WriteString(strings.Repeat(" ", colWidth-ansi.StringWidth(cells[i])))
			x += len(gap) + colWidth
		}
		lines = append(lines, fitWidth(strings.TrimRight(b.String(), " "), width))
	}
	return lines
}

func pad(s string, width int) string {
	if n := width - ansi.StringWidth(s); n > 0 {
		return s + strings.Repeat(" ", n)
	}
	return s
}
