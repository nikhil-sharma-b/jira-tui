package ui_test

import (
	"strconv"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/nikhil-sharma-b/jira-tui/internal/config"
	"github.com/nikhil-sharma-b/jira-tui/internal/ui"
)

// graphicsLog records what the model sends the terminal outside the frame.
type graphicsLog struct{ seqs []string }

// commands is every graphics command sent, taken out of tmux's passthrough
// where it was wrapped, as its control keys.
func (g *graphicsLog) commands(t *testing.T) []map[string]string {
	t.Helper()
	var out []map[string]string
	for _, seq := range g.seqs {
		if inner, ok := strings.CutPrefix(seq, "\x1bPtmux;"); ok {
			seq = strings.ReplaceAll(strings.TrimSuffix(inner, "\x1b\\"), "\x1b\x1b", "\x1b")
		}
		body, ok := strings.CutPrefix(seq, "\x1b_G")
		if !ok {
			t.Fatalf("%q is not a graphics command", seq)
		}
		control, _, _ := strings.Cut(strings.TrimSuffix(body, "\x1b\\"), ";")
		keys := map[string]string{}
		for _, kv := range strings.Split(control, ",") {
			k, v, _ := strings.Cut(kv, "=")
			keys[k] = v
		}
		out = append(out, keys)
	}
	return out
}

// actions is the a= of every command, in order, with a run of transmitted
// chunks counted once.
func (g *graphicsLog) actions(t *testing.T) []string {
	var out []string
	for _, c := range g.commands(t) {
		if a, ok := c["a"]; ok {
			out = append(out, a)
		}
	}
	return out
}

// kittyHarness is the attachment harness in a terminal described by term,
// with the images setting at images.
func kittyHarness(t *testing.T, images string, term ui.Terminal) (*attachmentHarness, *graphicsLog) {
	t.Helper()
	log := &graphicsLog{}
	cfg := testConfig(t, nil)
	cfg.Images = images
	h := newAttachmentHarnessWith(t, previewClient(t), cfg, func(o *ui.Options) {
		o.Terminal = term
		o.WriteGraphics = func(seq string) { log.seqs = append(log.seqs, seq) }
		o.CellSize = func() (int, int) { return 10, 20 }
	})
	return h, log
}

// placeholderRows counts the lines showing placeholders, and the widest run.
func placeholderRows(view string) (rows, cols int) {
	for _, line := range strings.Split(view, "\n") {
		if n := strings.Count(line, "\U0010EEEE"); n > 0 {
			rows++
			cols = max(cols, n)
		}
	}
	return rows, cols
}

func TestInAKittyTerminalThePreviewSendsTheImageAndPlacesIt(t *testing.T) {
	h, log := kittyHarness(t, config.ImagesAuto, ui.Terminal{Kitty: true})
	onAttachment(h, 1)
	h.keys("p")
	h.flush()

	if got := log.actions(t); len(got) != 2 || got[0] != "t" || got[1] != "p" {
		t.Fatalf("graphics actions = %q, want a transmission then a placement", got)
	}
	cmds := log.commands(t)
	transmit, place := cmds[0], cmds[len(cmds)-1]
	if transmit["i"] != place["i"] || transmit["i"] == "" {
		t.Errorf("transmitted image %q but placed %q", transmit["i"], place["i"])
	}
	if place["U"] != "1" {
		t.Errorf("placement is not virtual (U=%q): tmux would lose it on the first redraw", place["U"])
	}
	if strings.HasPrefix(log.seqs[0], "\x1bPtmux;") {
		t.Errorf("outside tmux the command was wrapped for tmux: %q", log.seqs[0][:20])
	}

	view := h.view()
	if strings.Contains(view, "▀") {
		t.Errorf("a kitty terminal was drawn half-blocks:\n%s", view)
	}
	rows, cols := placeholderRows(view)
	if rows == 0 || place["r"] != strconv.Itoa(rows) || place["c"] != strconv.Itoa(cols) {
		t.Errorf("placed %sx%s cells, drew %dx%d placeholders", place["c"], place["r"], cols, rows)
	}
	for _, want := range []string{"diagram.png", "40×20", "close"} {
		if !strings.Contains(view, want) {
			t.Errorf("the preview does not show %q:\n%s", want, view)
		}
	}
	if lines := h.lines(); len(lines) != 22 {
		t.Errorf("the preview is %d lines tall, want 22", len(lines))
	}
}

func TestInTmuxTheImageGoesThroughPassthrough(t *testing.T) {
	h, log := kittyHarness(t, config.ImagesAuto, ui.Terminal{Kitty: true, Tmux: true, Passthrough: true})
	onAttachment(h, 1)
	h.keys("p")
	h.flush()

	if len(log.seqs) == 0 {
		t.Fatal("nothing was sent to the terminal")
	}
	for _, seq := range log.seqs {
		if !strings.HasPrefix(seq, "\x1bPtmux;") {
			t.Errorf("%q was not wrapped for tmux passthrough", seq[:min(len(seq), 20)])
		}
	}
	if rows, _ := placeholderRows(h.view()); rows == 0 {
		t.Errorf("no placeholders on screen:\n%s", h.view())
	}
}

func TestResizingAKittyPreviewPlacesTheImageAgain(t *testing.T) {
	h, log := kittyHarness(t, config.ImagesKitty, ui.Terminal{Kitty: true})
	onAttachment(h, 1)
	h.keys("p")
	h.flush()
	_, wide := placeholderRows(h.view())
	sent := len(log.seqs)

	h.send(tea.WindowSizeMsg{Width: 60, Height: 15})

	cmds := log.commands(t)[sent:]
	if len(cmds) == 0 || cmds[len(cmds)-1]["a"] != "p" {
		t.Fatalf("resizing sent %v, want a new placement", cmds)
	}
	rows, cols := placeholderRows(h.view())
	last := cmds[len(cmds)-1]
	if last["c"] != strconv.Itoa(cols) || last["r"] != strconv.Itoa(rows) || cols >= wide {
		t.Errorf("placed %sx%s after the resize, drew %dx%d, was %d wide", last["c"], last["r"], cols, rows, wide)
	}
	for i, line := range h.lines() {
		if w := ansi.StringWidth(line); w > 60 {
			t.Errorf("line %d is %d cells wide, wider than the 60-column terminal", i, w)
		}
	}
}

func TestClosingAKittyPreviewDeletesTheImage(t *testing.T) {
	for _, key := range []string{"esc", "q"} {
		t.Run(key, func(t *testing.T) {
			h, log := kittyHarness(t, config.ImagesAuto, ui.Terminal{Kitty: true})
			onAttachment(h, 1)
			before := h.model.View()
			h.keys("p")
			h.flush()
			id := log.commands(t)[0]["i"]

			h.keys(key)

			cmds := log.commands(t)
			last := cmds[len(cmds)-1]
			if last["a"] != "d" || last["d"] != "I" || last["i"] != id {
				t.Errorf("closing sent %v, want image %s deleted with its data", last, id)
			}
			if after := h.model.View(); after != before {
				t.Errorf("closing the preview changed the screen\nbefore:\n%s\nafter:\n%s", ansi.Strip(before), ansi.Strip(after))
			}
		})
	}
}

func TestEachPreviewSendsItsImageUnderANewID(t *testing.T) {
	h, log := kittyHarness(t, config.ImagesAuto, ui.Terminal{Kitty: true})
	onAttachment(h, 1)
	h.keys("p")
	h.flush()
	h.keys("esc", "p")
	h.flush()

	var ids []string
	for _, c := range log.commands(t) {
		if c["a"] == "t" {
			ids = append(ids, c["i"])
		}
	}
	if len(ids) != 2 || ids[0] == ids[1] {
		t.Errorf("transmitted under %q, want two different IDs", ids)
	}
}

func TestAutoWithoutTmuxPassthroughFallsBackToHalfBlocksAndSaysWhy(t *testing.T) {
	h, log := kittyHarness(t, config.ImagesAuto, ui.Terminal{Kitty: true, Tmux: true})
	onAttachment(h, 1)
	h.keys("p")
	h.flush()

	view := h.view()
	if !strings.Contains(view, "▀") {
		t.Errorf("the preview was not drawn in half-blocks:\n%s", view)
	}
	if !strings.Contains(view, "allow-passthrough") {
		t.Errorf("the preview does not say why it fell back:\n%s", view)
	}
	if len(log.seqs) != 0 {
		t.Errorf("sent %d graphics commands through a tmux that drops them", len(log.seqs))
	}
	if lines := h.lines(); len(lines) != 22 {
		t.Errorf("the preview is %d lines tall, want 22", len(lines))
	}
}

func TestExplicitKittyWithoutTmuxPassthroughIsAnError(t *testing.T) {
	h, log := kittyHarness(t, config.ImagesKitty, ui.Terminal{Kitty: true, Tmux: true})
	onAttachment(h, 1)
	h.keys("p")
	h.flush()

	if status := h.statusLine(); !strings.Contains(status, "allow-passthrough") {
		t.Errorf("status line = %q, want the missing passthrough named", status)
	}
	if strings.Contains(h.view(), "▀") {
		t.Errorf("an explicit kitty setting fell back to half-blocks:\n%s", h.view())
	}
	if got := h.client.downloadRequests(); len(got) != 0 {
		t.Errorf("downloads = %q, want none", got)
	}
	if len(log.seqs) != 0 {
		t.Errorf("sent %d graphics commands", len(log.seqs))
	}
}

func TestAutoInATerminalWithoutKittyGraphicsDrawsHalfBlocksQuietly(t *testing.T) {
	h, log := kittyHarness(t, config.ImagesAuto, ui.Terminal{})
	onAttachment(h, 1)
	h.keys("p")
	h.flush()

	view := h.view()
	if !strings.Contains(view, "▀") || strings.Contains(view, "passthrough") {
		t.Errorf("want plain half-blocks with no fallback note:\n%s", view)
	}
	if len(log.seqs) != 0 {
		t.Errorf("sent %d graphics commands to a terminal without graphics", len(log.seqs))
	}
}

func TestHalfblocksSettingIgnoresAKittyTerminal(t *testing.T) {
	h, log := kittyHarness(t, config.ImagesHalfblocks, ui.Terminal{Kitty: true})
	onAttachment(h, 1)
	h.keys("p")
	h.flush()

	if !strings.Contains(h.view(), "▀") || len(log.seqs) != 0 {
		t.Errorf("images = halfblocks did not draw half-blocks (%d graphics commands)", len(log.seqs))
	}
}
