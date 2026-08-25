package ui

import (
	"io"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"
)

// Highlighting is the one thing in the list that a rendered-View test cannot
// see: off a terminal the renderer strips styling, so what reaches View() is
// the same text either way. It is asserted here instead, against styles built
// on a renderer that has been told there is a terminal.
//
// This is the one test file in package ui rather than ui_test, because the
// thing it has to reach -- highlight -- is unexported, and exporting it to be
// tested would be a wider seam than the one behaviour needs.

// styledText is the characters that carry styling, which is how a test reads a
// highlight back out of a rendered line. The renderer styles each grapheme
// separately, so what matters is which characters are inside styling and not
// how many runs it took to put them there.
func styledText(rendered string) string {
	var b strings.Builder
	for _, part := range strings.Split(rendered, "\x1b[0m") {
		if !strings.Contains(part, "\x1b[") {
			continue
		}
		b.WriteString(part[strings.LastIndex(part, "m")+1:])
	}
	return b.String()
}

func highlightStyles() (base, match lipgloss.Style) {
	r := lipgloss.NewRenderer(io.Discard)
	r.SetColorProfile(termenv.ANSI)
	return r.NewStyle(), r.NewStyle().Underline(true)
}

func TestHighlightMarksEveryOccurrenceWithoutChangingTheText(t *testing.T) {
	base, match := highlightStyles()

	got := highlight("Ada Lovelace and Ada again", "ada", base, match)

	if plain := ansi.Strip(got); plain != "Ada Lovelace and Ada again" {
		t.Errorf("highlighting changed the text to %q", plain)
	}
	// Both occurrences, and nothing between or around them.
	if styled := styledText(got); styled != "AdaAda" {
		t.Errorf("the styled characters are %q, want both occurrences of Ada", styled)
	}
}

// The pattern is matched the way a user types it, which is not the way the
// row happens to be capitalised.
func TestHighlightIsCaseInsensitive(t *testing.T) {
	base, match := highlightStyles()

	got := highlight("IN PROGRESS", "progress", base, match)

	if styled := styledText(got); styled != "PROGRESS" {
		t.Errorf("the styled characters are %q, want the match differing only in case", styled)
	}
	if plain := ansi.Strip(got); plain != "IN PROGRESS" {
		t.Errorf("highlighting changed the text to %q", plain)
	}
}

// Lowercasing can move the bytes under a non-ASCII rune, and an index into the
// lowered string is then not an index into the original. Falling back to an
// exact match loses a case-insensitive hit; slicing at the wrong offset loses
// the row.
func TestHighlightDoesNotCorruptTextThatLowercasesToADifferentLength(t *testing.T) {
	base, match := highlightStyles()

	const text = "İstanbul incident"
	got := highlight(text, "incident", base, match)

	if plain := ansi.Strip(got); plain != text {
		t.Errorf("highlighting corrupted the text:\ngot  %q\nwant %q", plain, text)
	}
}

func TestHighlightLeavesANonMatchingLineToTheBaseStyle(t *testing.T) {
	base, match := highlightStyles()

	got := highlight("nothing here", "absent", base, match)

	if styled := styledText(got); styled != "" {
		t.Errorf("a line with no match had %q styled as one", styled)
	}
}
