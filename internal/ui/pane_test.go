package ui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

// A frame says both what the pane is and which key reaches it.
func TestFrameCarriesTitleAndHint(t *testing.T) {
	lines := boxed([]string{"body"}, 30, 4, "Work Items", "ctrl+w h", true)
	top := ansi.Strip(lines[0])
	if !strings.Contains(top, "Work Items") || !strings.Contains(top, "ctrl+w h") {
		t.Fatalf("top edge missing title or hint: %q", top)
	}
	for i, l := range lines {
		if w := ansi.StringWidth(l); w != 30 {
			t.Fatalf("line %d is %d columns wide, want 30: %q", i, w, ansi.Strip(l))
		}
	}
}

// A hint that does not fit is dropped, not truncated, and costs the frame
// nothing.
func TestFrameDropsHintThatDoesNotFit(t *testing.T) {
	lines := boxed([]string{"body"}, 18, 3, "Work Items", "ctrl+w h", true)
	top := ansi.Strip(lines[0])
	if strings.Contains(top, "ctrl") {
		t.Fatalf("hint drawn in a frame too narrow for it: %q", top)
	}
	if w := ansi.StringWidth(lines[0]); w != 18 {
		t.Fatalf("top edge is %d columns wide, want 18: %q", w, top)
	}
}
