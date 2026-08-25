// Width-aware layout: how many columns are drawn at a given terminal width,
// how wide each one is, and how a value is fitted into its cell. It is kept
// apart from column resolution because the two change for unrelated reasons --
// one answers to the site's field metadata, the other to the window size.

package ui

import (
	"strings"
	"time"

	"github.com/charmbracelet/x/ansi"
	"github.com/nikhil-sharma-b/jira-tui/internal/jira"
)

// columnGap separates columns. One space is enough to read a boundary and, at
// six columns in an 80-wide tmux pane, five spaces is already a column's worth
// of summary.
const columnGap = 1

// layout decides how wide each column is drawn at a given terminal width, and
// which columns are drawn at all. It returns one width per column; a zero
// width means the column did not fit and is omitted.
//
// The rule is: every column gets what its content needs up to its natural cap,
// the flexible column absorbs whatever is left, and when even the minimums do
// not fit, columns are dropped from the right.
func layout(cols []Column, issues []*jira.Issue, now time.Time, width int) []int {
	widths := make([]int, len(cols))
	if len(cols) == 0 || width <= 0 {
		return widths
	}

	for i, c := range cols {
		w := ansi.StringWidth(c.Title)
		for _, issue := range issues {
			w = max(w, ansi.StringWidth(c.Render(issue, now)))
		}
		if c.natural > 0 {
			w = min(w, c.natural)
		}
		widths[i] = max(w, c.min)
	}

	// Columns beyond what the minimums can pay for are dropped from the right,
	// keeping at least one. Dropping beats squeezing: a four-character summary
	// is not a smaller summary, it is a column of ellipses.
	shown := len(cols)
	for shown > 1 && minimumWidth(cols[:shown]) > width {
		shown--
	}
	for i := shown; i < len(cols); i++ {
		widths[i] = 0
	}
	fitted := widths[:shown]
	visible := cols[:shown]

	for totalWidth(fitted) > width {
		i := widestShrinkable(visible, fitted)
		if i < 0 {
			break
		}
		fitted[i]--
	}

	if slack := width - totalWidth(fitted); slack > 0 {
		if i := flexIndex(visible); i >= 0 {
			fitted[i] += slack
		}
	}

	// A single column that still does not fit is truncated rather than
	// dropped: something is always better than an empty pane.
	if shown == 1 && fitted[0] > width {
		fitted[0] = width
	}
	return widths
}

func minimumWidth(cols []Column) int {
	w := columnGap * (len(cols) - 1)
	for _, c := range cols {
		w += c.min
	}
	return w
}

func totalWidth(widths []int) int {
	w := columnGap * (len(widths) - 1)
	for _, x := range widths {
		w += x
	}
	return w
}

// widestShrinkable picks the column to take a character from: the flexible one
// while it is above its minimum, then whichever fixed column is furthest above
// its own. Shrinking the widest keeps the loss spread rather than gutting one
// column to spare the rest.
func widestShrinkable(cols []Column, widths []int) int {
	if i := flexIndex(cols); i >= 0 && widths[i] > cols[i].min {
		return i
	}
	best, bestSlack := -1, 0
	for i, c := range cols {
		if slack := widths[i] - c.min; slack > bestSlack {
			best, bestSlack = i, slack
		}
	}
	return best
}

func flexIndex(cols []Column) int {
	for i, c := range cols {
		if c.flex {
			return i
		}
	}
	return -1
}

// cell pads or truncates a value to exactly w columns, so rows line up whatever
// the content and whatever the terminal's idea of a wide character is.
func cell(value string, w int) string {
	if w <= 0 {
		return ""
	}
	value = ansi.Truncate(value, w, "…")
	if pad := w - ansi.StringWidth(value); pad > 0 {
		value += strings.Repeat(" ", pad)
	}
	return value
}
