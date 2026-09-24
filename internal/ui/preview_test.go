package ui_test

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"slices"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/nikhil-sharma-b/jira-tui/internal/config"
	"github.com/nikhil-sharma-b/jira-tui/internal/jira"
)

// pngOf is a real PNG, width x height, in a gradient so no two cells of a
// drawing of it are alike.
func pngOf(t *testing.T, width, height int) string {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	for y := range height {
		for x := range width {
			img.Set(x, y, color.RGBA{uint8(x * 255 / width), uint8(y * 255 / height), 128, 255})
		}
	}
	var b bytes.Buffer
	if err := png.Encode(&b, img); err != nil {
		t.Fatal(err)
	}
	return b.String()
}

// previewClient serves attachedIssue with diagram.png as a real 40x20 PNG.
func previewClient(t *testing.T) *fakeClient {
	return &fakeClient{
		issues:    []jira.Issue{attachedIssue()},
		downloads: map[string]string{"10": "log lines", "11": pngOf(t, 40, 20)},
	}
}

// onAttachment opens the item and selects the nth entry on its Attachments
// tab: 0 trace.log, 1 diagram.png, then the embedded diagram.png, remote.png
// and cat.png.
func onAttachment(h *attachmentHarness, n int) {
	h.keys("enter")
	h.flush()
	h.keys("g", "a")
	for range n {
		h.keys("j")
	}
}

// halfBlockWidth is how many cells wide the widest run of half-blocks on
// screen is, which is the width the image was drawn at.
func halfBlockWidth(view string) int {
	widest := 0
	for _, line := range strings.Split(view, "\n") {
		widest = max(widest, strings.Count(line, "▀"))
	}
	return widest
}

func TestPOnAnImageAttachmentOpensAFullscreenPreview(t *testing.T) {
	client := previewClient(t)
	h := newAttachmentHarness(t, client)
	onAttachment(h, 1)
	h.keys("p")
	h.flush()

	view := h.view()
	for _, want := range []string{"diagram.png", "40×20", "▀", "esc", "close"} {
		if !strings.Contains(view, want) {
			t.Errorf("the preview does not show %q:\n%s", want, view)
		}
	}
	for _, gone := range []string{"Work Items", "trace.log"} {
		if strings.Contains(view, gone) {
			t.Errorf("the preview is not fullscreen; %q shows through:\n%s", gone, view)
		}
	}
	if lines := h.lines(); len(lines) != 22 {
		t.Errorf("the preview is %d lines tall, want the terminal's 22", len(lines))
	}
	if got := client.downloadRequests(); !slices.Equal(got, []string{"11"}) {
		t.Errorf("downloads = %q, want the selected image", got)
	}
	if len(h.opened) != 0 {
		t.Errorf("a previewed image was handed to the system opener: %q", h.opened)
	}
	if files := h.files(); len(files) != 0 {
		t.Errorf("the preview left %q behind", files)
	}
}

func TestAnImageEmbeddedInTheDescriptionPreviewsTheSameWay(t *testing.T) {
	client := previewClient(t)
	h := newAttachmentHarness(t, client)
	onAttachment(h, 2)
	h.keys("p")
	h.flush()

	if view := h.view(); !strings.Contains(view, "▀") || !strings.Contains(view, "40×20") {
		t.Errorf("the embedded image was not previewed:\n%s", view)
	}
}

func TestEscAndQCloseThePreviewLeavingTheDetailPaneAsItWas(t *testing.T) {
	for _, key := range []string{"esc", "q"} {
		t.Run(key, func(t *testing.T) {
			h := newAttachmentHarness(t, previewClient(t))
			onAttachment(h, 1)
			before := h.model.View()

			h.keys("p")
			h.flush()
			h.keys(key)

			if after := h.model.View(); after != before {
				t.Errorf("closing the preview changed the screen\nbefore:\n%s\nafter:\n%s", ansi.Strip(before), ansi.Strip(after))
			}
		})
	}
}

func TestResizingThePreviewRedrawsTheImageAtTheNewSize(t *testing.T) {
	h := newAttachmentHarness(t, previewClient(t))
	onAttachment(h, 1)
	h.keys("p")
	h.flush()
	wide := halfBlockWidth(h.view())

	h.send(tea.WindowSizeMsg{Width: 60, Height: 15})

	lines := h.lines()
	if len(lines) != 15 {
		t.Errorf("the preview is %d lines tall after the resize, want 15", len(lines))
	}
	for i, line := range lines {
		if w := ansi.StringWidth(line); w > 60 {
			t.Errorf("line %d is %d cells wide, wider than the 60-column terminal", i, w)
		}
	}
	if narrow := halfBlockWidth(h.view()); narrow == 0 || narrow >= wide {
		t.Errorf("the image is %d cells wide after shrinking the terminal, was %d", narrow, wide)
	}
}

func TestPSaysWhyAnEntryCannotBePreviewed(t *testing.T) {
	tests := []struct {
		name  string
		entry int
		want  []string
	}{
		{"not an image", 0, []string{"trace.log", "cannot be previewed", "enter"}},
		{"held by Media Services", 3, []string{"Media Services"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := previewClient(t)
			h := newAttachmentHarness(t, client)
			onAttachment(h, tt.entry)
			h.keys("p")
			h.flush()

			status := h.statusLine()
			for _, want := range tt.want {
				if !strings.Contains(status, want) {
					t.Errorf("status line = %q, want it to say %q", status, want)
				}
			}
			if got := client.downloadRequests(); len(got) != 0 {
				t.Errorf("downloads = %q, want none", got)
			}
		})
	}
}

func TestPReportsAnImageThatWillNotDecode(t *testing.T) {
	client := previewClient(t)
	client.downloads["11"] = "not really a PNG"
	h := newAttachmentHarness(t, client)
	onAttachment(h, 1)
	h.keys("p")
	h.flush()

	if status := h.statusLine(); !strings.Contains(status, "preview diagram.png") || !strings.Contains(status, "not a PNG, JPEG or GIF") {
		t.Errorf("status line = %q, want the decode failure named", status)
	}
	if strings.Contains(h.view(), "▀") {
		t.Errorf("a preview opened for an image that did not decode:\n%s", h.view())
	}
	if files := h.files(); len(files) != 0 {
		t.Errorf("a failed preview left %q behind", files)
	}
}

func TestPIsOffWhenImagesAreOff(t *testing.T) {
	client := previewClient(t)
	cfg := testConfig(t, nil)
	cfg.Images = config.ImagesOff
	h := newAttachmentHarnessWith(t, client, cfg)
	onAttachment(h, 1)
	h.keys("p")
	h.flush()

	if status := h.statusLine(); !strings.Contains(status, "off") || !strings.Contains(status, "images") {
		t.Errorf("status line = %q, want it to say the images setting turned preview off", status)
	}
	if got := client.downloadRequests(); len(got) != 0 {
		t.Errorf("downloads = %q, want none", got)
	}
}

func TestPOutsideTheAttachmentsTabSaysWhereImagesAre(t *testing.T) {
	client := previewClient(t)
	h := newAttachmentHarness(t, client)
	h.keys("enter")
	h.flush()
	h.keys("g", "d", "p")

	if status := h.statusLine(); !strings.Contains(status, "Attachments") {
		t.Errorf("status line = %q, want it to point at the Attachments tab", status)
	}
	if got := client.downloadRequests(); len(got) != 0 {
		t.Errorf("downloads = %q, want none", got)
	}
}

func TestAPreviewDownloadShowsProgressAndEscCancelsIt(t *testing.T) {
	client := previewClient(t)
	client.downloadBlock = true
	h := newAttachmentHarness(t, client)
	onAttachment(h, 1)
	h.send(keyMsg("p"))
	collect := h.runQueued()

	deadline := time.Now().Add(2 * time.Second)
	for !strings.Contains(h.statusLine(), "downloading diagram.png") {
		if time.Now().After(deadline) {
			t.Fatalf("no progress on the status line: %q", h.statusLine())
		}
		time.Sleep(5 * time.Millisecond)
	}

	h.send(keyMsg("esc"))
	for _, msg := range collect() {
		h.deliver(msg)
	}
	h.flush()

	if status := h.statusLine(); !strings.Contains(status, "cancelled") {
		t.Errorf("status line = %q, want the download reported cancelled", status)
	}
	if strings.Contains(h.view(), "▀") {
		t.Errorf("a cancelled preview opened anyway:\n%s", h.view())
	}
	if files := h.files(); len(files) != 0 {
		t.Errorf("a cancelled preview left %q behind", files)
	}
}

// A line opened while the image downloads would be typed into blind once the
// overlay covers it, so the overlay puts it away.
func TestAPreviewThatLandsWhileALineIsOpenPutsTheLineAway(t *testing.T) {
	h := newAttachmentHarness(t, previewClient(t))
	onAttachment(h, 1)
	before := h.model.View()
	h.send(keyMsg("p"))
	h.send(keyMsg(":"))
	h.flush()

	if !strings.Contains(h.view(), "▀") {
		t.Fatalf("the preview did not open:\n%s", h.view())
	}
	h.keys("esc")
	if after := h.model.View(); after != before {
		t.Errorf("closing the preview left a line open\nbefore:\n%s\nafter:\n%s", ansi.Strip(before), ansi.Strip(after))
	}
	h.keys("j")
	if view := h.view(); !strings.Contains(view, "▸ diagram.png") || strings.Contains(view, "▸ trace.log") {
		t.Errorf("j after closing the preview did not move the selection:\n%s", view)
	}
}
