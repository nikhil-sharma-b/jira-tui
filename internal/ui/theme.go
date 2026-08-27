// The palette. Colours are adaptive so the same build reads on a light and a
// dark terminal, and they are named for what they mark rather than for what
// they are, so a column asking for "accent" does not have to know it is blue.

package ui

import "github.com/charmbracelet/lipgloss"

var (
	// accent marks structure: panel titles, borders, column headers.
	accent = lipgloss.AdaptiveColor{Light: "#1a6fd4", Dark: "#4aa3ff"}
	// keyColor marks an issue key, which is the row's identity.
	keyColor = lipgloss.AdaptiveColor{Light: "#7b3fb5", Dark: "#c58af9"}
	// stateColor marks where an item is: its status.
	stateColor = lipgloss.AdaptiveColor{Light: "#0f7b6c", Dark: "#5bd6c0"}
	// kindColor marks what an item is: its type.
	kindColor = lipgloss.AdaptiveColor{Light: "#b3306b", Dark: "#ff6b9d"}
	// noteColor marks the quieter cells -- timestamps, priorities, counts --
	// which are read only when the eye is already on the row.
	noteColor = lipgloss.AdaptiveColor{Light: "#5c6773", Dark: "#8a94a6"}
	// selectionBG is the band under the selected row, in the reference TUI's
	// solid blue rather than reverse-video.
	selectionBG = lipgloss.AdaptiveColor{Light: "#1a6fd4", Dark: "#1f6feb"}
	selectionFG = lipgloss.AdaptiveColor{Light: "#ffffff", Dark: "#ffffff"}
)

var (
	accentStyle = lipgloss.NewStyle().Foreground(accent)
	keyStyle    = lipgloss.NewStyle().Foreground(keyColor)
	stateStyle  = lipgloss.NewStyle().Foreground(stateColor)
	kindStyle   = lipgloss.NewStyle().Foreground(kindColor)
	noteStyle   = lipgloss.NewStyle().Foreground(noteColor)
	// borderStyle is the rounded frame a pane is drawn in, and titleStyle the
	// name written into its top edge.
	borderStyle = lipgloss.NewStyle().Foreground(accent)
	titleStyle  = lipgloss.NewStyle().Foreground(accent).Bold(true)
	// hintStyle is the key written into a frame's top-right edge. It is quiet
	// in both panes: it is a reminder, not part of what the pane says.
	hintStyle = lipgloss.NewStyle().Foreground(noteColor)
	// pendingStyle is the half-typed key sequence at the right of the status
	// line. It is bright: it says the next keypress will not mean what it
	// usually means.
	pendingStyle = lipgloss.NewStyle().Foreground(accent).Bold(true)
)

var (
	// summaryStyle is the work item's own title, at the top of the detail pane.
	summaryStyle = lipgloss.NewStyle().Bold(true)
	// sectionStyle names a block of the detail pane: Description, Comments.
	sectionStyle = lipgloss.NewStyle().Foreground(accent).Bold(true).Underline(true)
	// commentAuthorStyle marks who wrote a comment.
	commentAuthorStyle = lipgloss.NewStyle().Foreground(keyColor).Bold(true)
)

var (
	// fieldLabelStyle names a field in the Details tab; the value beside it is
	// left plain so the eye lands on the value rather than on the label.
	fieldLabelStyle = lipgloss.NewStyle().Foreground(noteColor)
	// itemKeyStyle marks the identity of a listed thing that is not a work
	// item -- an attachment's filename.
	itemKeyStyle = lipgloss.NewStyle().Foreground(keyColor)
	// The tab strip: the active tab is reversed out of the accent colour and
	// carries a bright rule; the rest are quiet, with a quiet rule.
	activeTabStyle       = lipgloss.NewStyle().Foreground(selectionFG).Background(selectionBG).Bold(true)
	inactiveTabStyle     = lipgloss.NewStyle().Foreground(noteColor)
	activeTabRuleStyle   = lipgloss.NewStyle().Foreground(accent)
	inactiveTabRuleStyle = lipgloss.NewStyle().Foreground(noteColor)
	// The quiet rule drawn between one comment and the next.
	commentSeparatorStyle = lipgloss.NewStyle().Foreground(noteColor)
)
