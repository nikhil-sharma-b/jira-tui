package ui_test

import (
	"regexp"
	"strconv"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/nikhil-sharma-b/jira-tui/internal/config"
	"github.com/nikhil-sharma-b/jira-tui/internal/ui"
)

// sixelTerminal draws sixel with a palette of colors.
func sixelTerminal(colors int) ui.Terminal {
	return ui.Terminal{Sixel: true, SixelColors: colors}
}

// drawnSixel is a sixel found in a frame: the screen cell it is drawn from,
// 1-based as the cursor is positioned, its size in pixels, and how many
// colours it defines.
type drawnSixel struct {
	row, col      int
	width, height int
	colors        int
}

var sixelInFrame = regexp.MustCompile(`\x1b7\x1b\[(\d+);(\d+)H\x1bP[0-9;]*q"1;1;(\d+);(\d+)([^\x1b]*)\x1b\\\x1b8`)

// sixels finds every sixel the frame draws, each drawn from a saved cursor
// that is restored after, so the rest of the frame lands where it would have.
func sixels(t *testing.T, frame string) []drawnSixel {
	t.Helper()
	var out []drawnSixel
	for _, m := range sixelInFrame.FindAllStringSubmatch(frame, -1) {
		n := func(i int) int { v, _ := strconv.Atoi(m[i]); return v }
		out = append(out, drawnSixel{
			row: n(1), col: n(2), width: n(3), height: n(4),
			colors: len(regexp.MustCompile(`#\d+;2;`).FindAllString(m[5], -1)),
		})
	}
	if strings.Count(frame, "\x1bP") != len(out) {
		t.Fatalf("the frame holds %d DCS sequences but %d well-formed sixels", strings.Count(frame, "\x1bP"), len(out))
	}
	return out
}

// onlySixel is the one sixel the preview draws.
func onlySixel(t *testing.T, h *attachmentHarness) drawnSixel {
	t.Helper()
	found := sixels(t, h.model.View())
	if len(found) != 1 {
		t.Fatalf("the preview draws %d sixels, want 1:\n%s", len(found), h.view())
	}
	return found[0]
}

func TestInASixelTerminalThePreviewDrawsASixelOverBlankCells(t *testing.T) {
	h, log := kittyHarness(t, config.ImagesAuto, sixelTerminal(256))
	onAttachment(h, 1)
	h.keys("p")
	h.flush()

	s := onlySixel(t, h)
	view := h.view()
	if strings.Contains(view, "▀") {
		t.Errorf("a sixel terminal was drawn half-blocks:\n%s", view)
	}
	if len(log.seqs) != 0 {
		t.Errorf("sent %d kitty commands to a sixel terminal", len(log.seqs))
	}
	// The harness's cells are 10x20 pixels, so the overlay's 98x19 cells
	// are 980x380 px, which the 40x20 image fills to its height at 2:1.
	if s.height < 380-6 || s.height > 380 || s.height*2 > s.width+12 || s.height*2 < s.width-12 {
		t.Errorf("sixel is %dx%d px, want the overlay's height at the image's 2:1", s.width, s.height)
	}
	// Every cell the image covers is blank in the frame, so no text is
	// drawn over it, and the frame stays the size of the screen.
	lines := h.lines()
	cols, rows := (s.width+9)/10, (s.height+19)/20
	for r := s.row - 1; r < s.row-1+rows; r++ {
		covered := ansi.Cut(lines[r], s.col-1, s.col-1+cols)
		if strings.TrimSpace(covered) != "" || ansi.StringWidth(covered) != cols {
			t.Errorf("line %d under the image is %q, want %d blank cells", r, covered, cols)
		}
	}
	for i, line := range lines {
		if w := ansi.StringWidth(line); w > 100 {
			t.Errorf("line %d is %d cells wide, wider than the 100-column terminal", i, w)
		}
	}
	if len(lines) != 22 {
		t.Errorf("the preview is %d lines tall, want 22", len(lines))
	}
	for _, want := range []string{"diagram.png", "40×20", "close"} {
		if !strings.Contains(view, want) {
			t.Errorf("the preview does not show %q:\n%s", want, view)
		}
	}
}

func TestTheSixelIsQuantisedToTheTerminalsPalette(t *testing.T) {
	h, _ := kittyHarness(t, config.ImagesSixel, sixelTerminal(16))
	onAttachment(h, 1)
	h.keys("p")
	h.flush()

	if s := onlySixel(t, h); s.colors == 0 || s.colors > 16 {
		t.Errorf("the sixel defines %d colours, want at most the terminal's 16", s.colors)
	}
}

func TestAutoPrefersKittyToSixel(t *testing.T) {
	term := sixelTerminal(256)
	term.Kitty = true
	h, log := kittyHarness(t, config.ImagesAuto, term)
	onAttachment(h, 1)
	h.keys("p")
	h.flush()

	if len(log.seqs) == 0 {
		t.Error("a terminal with both drew no kitty graphics")
	}
	if found := sixels(t, h.model.View()); len(found) != 0 {
		t.Errorf("a terminal with both was drawn a sixel too")
	}
}

func TestAutoDrawsSixelWhenTmuxBlocksKitty(t *testing.T) {
	term := sixelTerminal(256)
	term.Kitty, term.Tmux, term.TmuxSixel = true, true, true
	h, log := kittyHarness(t, config.ImagesAuto, term)
	onAttachment(h, 1)
	h.keys("p")
	h.flush()

	onlySixel(t, h)
	if len(log.seqs) != 0 || strings.Contains(h.view(), "passthrough") {
		t.Errorf("sent %d kitty commands, or noted a fallback:\n%s", len(log.seqs), h.view())
	}
}

func TestResizingASixelPreviewEncodesItAgain(t *testing.T) {
	h, _ := kittyHarness(t, config.ImagesSixel, sixelTerminal(256))
	onAttachment(h, 1)
	h.keys("p")
	h.flush()
	wide := onlySixel(t, h)

	h.send(tea.WindowSizeMsg{Width: 60, Height: 15})
	// Until the new size is encoded, nothing is drawn where the old one
	// no longer fits.
	if found := sixels(t, h.model.View()); len(found) != 0 {
		t.Errorf("drew a %dx%d sixel sized for the old screen", found[0].width, found[0].height)
	}
	h.flush()

	narrow := onlySixel(t, h)
	if narrow.width >= wide.width || narrow.width > 58*10 {
		t.Errorf("the sixel is %d px wide after the resize, was %d", narrow.width, wide.width)
	}
	lines := h.lines()
	if len(lines) != 15 {
		t.Errorf("the preview is %d lines tall after the resize, want 15", len(lines))
	}
	for i, line := range lines {
		if w := ansi.StringWidth(line); w > 60 {
			t.Errorf("line %d is %d cells wide, wider than the 60-column terminal", i, w)
		}
	}
}

func TestClosingASixelPreviewRepaintsTheScreen(t *testing.T) {
	for _, key := range []string{"esc", "q"} {
		t.Run(key, func(t *testing.T) {
			h, _ := kittyHarness(t, config.ImagesAuto, sixelTerminal(256))
			onAttachment(h, 1)
			before := h.model.View()
			h.keys("p")
			h.flush()

			h.send(keyMsg(key))

			// The pane underneath is repainted whole: a line of it that
			// happens to match the overlay's would otherwise be skipped,
			// leaving the image showing through.
			cleared := false
			for _, cmd := range h.pending {
				cleared = cleared || cmd() == tea.ClearScreen()
			}
			if !cleared {
				t.Error("closing the preview did not clear the screen")
			}
			h.flush()
			if after := h.model.View(); after != before {
				t.Errorf("closing the preview changed the screen\nbefore:\n%s\nafter:\n%s", ansi.Strip(before), ansi.Strip(after))
			}
		})
	}
}

func TestExplicitSixelWithoutSupportSaysWhy(t *testing.T) {
	tests := []struct {
		name string
		term ui.Terminal
		want string
	}{
		{"terminal", ui.Terminal{}, "does not report sixel"},
		{"tmux built without it", ui.Terminal{Tmux: true}, "tmux was built without sixel"},
		{"tmux not told the terminal has it", ui.Terminal{Tmux: true, TmuxSixel: true}, "terminal-features"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h, _ := kittyHarness(t, config.ImagesSixel, tt.term)
			onAttachment(h, 1)
			h.keys("p")
			h.flush()

			if status := h.statusLine(); !strings.Contains(status, tt.want) {
				t.Errorf("status line = %q, want %q", status, tt.want)
			}
			if got := h.client.downloadRequests(); len(got) != 0 {
				t.Errorf("downloads = %q, want none", got)
			}
		})
	}
}

func TestAutoWithoutSixelDrawsHalfBlocksQuietly(t *testing.T) {
	h, _ := kittyHarness(t, config.ImagesAuto, ui.Terminal{Tmux: true, TmuxSixel: true})
	onAttachment(h, 1)
	h.keys("p")
	h.flush()

	view := h.view()
	if !strings.Contains(view, "▀") || strings.Contains(view, "sixel") {
		t.Errorf("want plain half-blocks with no note:\n%s", view)
	}
}

func TestResizesDuringAnEncodingEncodeOnlyTheLatestSize(t *testing.T) {
	h, _ := kittyHarness(t, config.ImagesSixel, sixelTerminal(256))
	onAttachment(h, 1)
	h.keys("p")
	h.flush()

	h.send(tea.WindowSizeMsg{Width: 60, Height: 15})
	h.send(tea.WindowSizeMsg{Width: 70, Height: 18})
	h.send(tea.WindowSizeMsg{Width: 80, Height: 20})
	if len(h.pending) != 1 {
		t.Errorf("%d encodings started by three resizes in a row, want 1 at a time", len(h.pending))
	}
	h.flush()

	s := onlySixel(t, h)
	// The 80x20 screen leaves the overlay 17 rows of 20 px, which the image
	// fills to its height in whole bands; the 70x18 one left it 15.
	if s.height != 336 {
		t.Errorf("drew a %dx%d sixel, want one encoded for the 80-column screen", s.width, s.height)
	}
}

func TestExplicitSixelWhenTmuxWillNotSaySaysSo(t *testing.T) {
	h, _ := kittyHarness(t, config.ImagesSixel, ui.Terminal{Tmux: true, Passthrough: true, TmuxSilent: true})
	onAttachment(h, 1)
	h.keys("p")

	if status := h.statusLine(); !strings.Contains(status, "tmux would not say") {
		t.Errorf("status line = %q, want tmux's silence named", status)
	}
}
