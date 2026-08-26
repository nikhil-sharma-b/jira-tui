// Panel frames: the rounded box a pane is drawn in, with its name written into
// the top edge and the focused pane's frame brightened. The frame is drawn
// around finished lines rather than by lipgloss's own border, because a pane's
// content is already fitted to an exact width and re-wrapping it inside a
// bordered block would undo that.

package ui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// frameWidth is what a frame costs its content on each axis: one column of
// border on the left and right, one row on the top and bottom.
const frameWidth = 2

// inner is the content size left inside a frame of the given outer size.
func inner(size int) int { return max(size-frameWidth, 0) }

// boxed draws lines inside a rounded frame of exactly width columns and height
// rows, padding or trimming the content to fit. title is written into the top
// edge and hint, the key that moves focus here, into its right end; focused decides whether the frame is drawn in the accent colour or in
// the quieter one, which is the only thing on screen saying where a keypress
// will land when both panes are up.
func boxed(lines []string, width, height int, title, hint string, focused bool) []string {
	if width < frameWidth+1 || height < frameWidth {
		return lines
	}
	content, border := inner(width), borderStyle
	if !focused {
		border = lipgloss.NewStyle().Foreground(noteColor)
	}

	out := make([]string, 0, height)
	out = append(out, border.Render("╭")+topEdge(title, hint, content, focused, border)+border.Render("╮"))
	for i := range height - frameWidth {
		line := ""
		if i < len(lines) {
			line = fitWidth(lines[i], content)
		}
		if pad := content - ansi.StringWidth(line); pad > 0 {
			line += strings.Repeat(" ", pad)
		}
		out = append(out, border.Render("│")+line+border.Render("│"))
	}
	return append(out, border.Render("╰")+border.Render(strings.Repeat("─", content))+border.Render("╯"))
}

// topEdge is the frame's top rule with the pane's name set into it, one space
// clear of the left corner, and the key that focuses the pane one space clear
// of the right. Either is dropped rather than truncated when the pane is too
// narrow for it: half a word in a border reads as corruption. The name wins
// the last columns, since a pane with no name cannot be told apart at all.
func topEdge(title, hint string, width int, focused bool, border lipgloss.Style) string {
	name := titleStyle
	if !focused {
		name = lipgloss.NewStyle().Foreground(noteColor)
	}
	if title == "" || ansi.StringWidth(title)+4 > width {
		return border.Render(strings.Repeat("─", width))
	}
	used := ansi.StringWidth(title) + 3
	rule := width - used
	if hint != "" && ansi.StringWidth(hint)+3 <= rule {
		rule -= ansi.StringWidth(hint) + 2
		return border.Render("─ ") + name.Render(title) +
			border.Render(" "+strings.Repeat("─", rule)+" ") + hintStyle.Render(hint) + border.Render(" ")
	}
	return border.Render("─ ") + name.Render(title) + border.Render(" "+strings.Repeat("─", rule))
}
