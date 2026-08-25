package ui

import (
	"fmt"
	"strings"

	"github.com/nikhil-sharma-b/jira-tui/internal/config"
)

// Help renders the keymap that is actually in effect. Every key it prints
// comes from the compiled bindings, so a rebound key is documented correctly
// by construction and an unbound one is not listed at all. What is
// hand-maintained is only the grouping and the wording -- and
// TestHelpCoversEveryDefaultAction fails if an action is missing from it.
type Help struct {
	bindings *config.Bindings
	visible  bool
}

// NewHelp builds the overlay over the effective bindings.
func NewHelp(b *config.Bindings) *Help { return &Help{bindings: b} }

// Visible reports whether the overlay is up.
func (h *Help) Visible() bool { return h.visible }

// Hide dismisses the overlay, which is what opening a prompt does: a line
// being typed under a full-screen overlay is a line typed blind.
func (h *Help) Hide() { h.visible = false }

// HandleAction gives the overlay first refusal on an action, reporting whether
// it consumed it. The overlay answers to exactly two: its own key toggles it,
// and Esc dismisses it. Everything else belongs to the pane underneath.
func (h *Help) HandleAction(a config.Action) bool {
	switch {
	case a == config.ActionHelp:
		h.visible = !h.visible
		return true
	case a == config.ActionNormalMode && h.visible:
		h.visible = false
		return true
	}
	return false
}

// entry is one documented action.
type entry struct {
	action config.Action
	// what the action does, in the words a user would use for it.
	what string
}

// category groups entries under a heading. Order is the order a reader wants:
// what is global, then how to move, then where to move to, then what can be
// done to the thing in front of them.
type category struct {
	name    string
	entries []entry
}

var helpCategories = []category{
	{"GLOBAL", []entry{
		{config.ActionCommandline, "commandline"},
		{config.ActionNormalMode, "normal mode, from anywhere"},
		{config.ActionSearchInPane, "search in pane"},
		{config.ActionSearchNext, "next match"},
		{config.ActionSearchPrev, "previous match"},
		{config.ActionClosePane, "close pane"},
		{config.ActionReload, "reload view"},
		{config.ActionHelp, "this help"},
	}},
	{"MOTION", []entry{
		{config.ActionDown, "down"},
		{config.ActionUp, "up"},
		{config.ActionTop, "top"},
		{config.ActionBottom, "bottom"},
		{config.ActionHalfPageDown, "half page down"},
		{config.ActionHalfPageUp, "half page up"},
		{config.ActionViewportTop, "viewport top"},
		{config.ActionViewportMid, "viewport middle"},
		{config.ActionViewportBot, "viewport bottom"},
	}},
	{"PANES", []entry{
		{config.ActionPaneLeft, "pane left"},
		{config.ActionPaneRight, "pane right"},
		{config.ActionPaneZoom, "zoom pane"},
		{config.ActionGoList, "go to list"},
		{config.ActionGoDetail, "go to detail"},
		{config.ActionGoComments, "go to comments"},
		{config.ActionGoAttachments, "go to attachments"},
	}},
	{"ACTIONS", []entry{
		{config.ActionOpen, "open"},
		{config.ActionJumpBack, "jump back"},
		{config.ActionJumpFwd, "jump forward"},
		{config.ActionComment, "comment via $EDITOR"},
		{config.ActionEditDesc, "edit description via $EDITOR"},
		{config.ActionTransition, "transition"},
		{config.ActionAssign, "assign"},
		{config.ActionYankKey, "yank key"},
		{config.ActionYankURL, "yank URL"},
		{config.ActionOpenBrowser, "open in browser"},
	}},
}

// helpIndent is the left margin of a binding line under its heading.
const helpIndent = "  "

// Lines renders the overlay, one line per binding under its category heading.
// A count prefix is documented once rather than on every motion.
func (h *Help) Lines() []string {
	width := h.keyColumnWidth()

	var lines []string
	for _, c := range helpCategories {
		rendered := h.categoryLines(c, width)
		if len(rendered) == 0 {
			continue
		}
		if len(lines) > 0 {
			lines = append(lines, "")
		}
		lines = append(lines, c.name)
		lines = append(lines, rendered...)
	}
	if len(lines) > 0 {
		lines = append(lines, "", helpIndent+"a count prefixes a motion, e.g. 5j")
	}
	return lines
}

func (h *Help) categoryLines(c category, width int) []string {
	var lines []string
	for _, e := range c.entries {
		keys, ok := h.bindings.Display(e.action)
		if !ok {
			// Unbound: not reachable, so not documented.
			continue
		}
		lines = append(lines, fmt.Sprintf("%s%-*s  %s", helpIndent, width, keys, e.what))
	}
	return lines
}

// keyColumnWidth aligns descriptions across every category, so the overlay
// reads as one table rather than four.
func (h *Help) keyColumnWidth() int {
	width := 0
	for _, c := range helpCategories {
		for _, e := range c.entries {
			keys, ok := h.bindings.Display(e.action)
			if !ok {
				continue
			}
			if n := len([]rune(keys)); n > width {
				width = n
			}
		}
	}
	return width
}

// String renders the overlay as a block of text.
func (h *Help) String() string { return strings.Join(h.Lines(), "\n") }
