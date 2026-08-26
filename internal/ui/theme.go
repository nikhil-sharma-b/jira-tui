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
)
