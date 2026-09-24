package ui

import (
	"fmt"
	"os"
	"path"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/nikhil-sharma-b/jira-tui/internal/config"
	"github.com/nikhil-sharma-b/jira-tui/internal/imageview"
	"github.com/nikhil-sharma-b/jira-tui/internal/jira"
)

// imagePreview is the fullscreen overlay p opens. It is modal and fills the
// screen on purpose: an overlay has a fixed size, so it can simply be drawn
// again, where an image inline in the detail pane would have to survive
// scrolling and everything tmux does to a pane.
type imagePreview struct {
	name  string
	image *imageview.Image
	// lines is the image drawn for cols x rows, kept so that a redraw that is
	// not a resize does not scale it again.
	cols, rows int
	lines      []string
}

// previewable reports whether an attachment is an image p can draw. The MIME
// type is trusted when the server gave one; the extension decides otherwise.
func previewable(a *jira.Attachment) bool {
	switch strings.ToLower(a.MimeType) {
	case "image/png", "image/jpeg", "image/gif":
		return true
	case "", "application/octet-stream":
		switch strings.ToLower(path.Ext(a.Filename)) {
		case ".png", ".jpg", ".jpeg", ".gif":
			return true
		}
	}
	return false
}

// previewSelected is p. It downloads the selected image the way Enter does,
// with the same progress and the same Esc, and decodes it in place of handing
// it to the system opener.
func (m *Model) previewSelected() tea.Cmd {
	if m.cfg.Images == config.ImagesOff {
		m.status = fmt.Errorf("image preview is off; set images = %q in the config to turn it on", config.ImagesAuto)
		return nil
	}
	item, ok := m.detail.selectedItem()
	if !ok || m.focus != PaneDetail {
		where := "the Attachments tab"
		if keys, bound := m.bindings.Display(config.ActionGoAttachments); bound {
			where += " (" + keys + ")"
		}
		m.status = fmt.Errorf("select an image on %s to preview it", where)
		return nil
	}
	enter := m.keyFor(config.ActionOpen)
	switch {
	case item.err != nil:
		m.status = fmt.Errorf("preview %s: %w", item.name, item.err)
		return nil
	case item.url != "":
		m.status = fmt.Errorf("%s is linked from %s and cannot be previewed; %s opens it in the browser", item.name, hostOf(item.url), enter)
		return nil
	case !previewable(item.attachment):
		m.status = fmt.Errorf("%s cannot be previewed: only PNG, JPEG and GIF images can; %s opens it", item.name, enter)
		return nil
	case m.downloading():
		return nil
	}
	return m.startDownload(*item.attachment, thenPreview)
}

// keyFor spells the key bound to an action for a message, falling back to
// the action's name when it is unbound.
func (m *Model) keyFor(a config.Action) string {
	if keys, ok := m.bindings.Display(a); ok {
		return keys
	}
	return string(a)
}

// decodeImage reads the downloaded file. Decoding and the downscale it ends
// with run in the download's command, off the update loop, so a large
// screenshot never stalls the screen.
func decodeImage(path string) (*imageview.Image, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return imageview.Decode(f)
}

// handlePreviewAction is what an action does while the preview is up. Esc and
// q close it and nothing else -- Esc does not also hide the search marks, since
// the pane underneath is meant to come back exactly as it was left. Quitting
// still quits; everything else is swallowed, since it would act on a screen
// the user cannot see.
func (m *Model) handlePreviewAction(action config.Action) tea.Cmd {
	switch action {
	case config.ActionNormalMode, config.ActionClosePane:
		m.preview = nil
	case config.ActionQuit:
		return tea.Quit
	}
	return nil
}

// previewView draws the overlay: the image centred in a frame named for the
// file, its pixel size in the frame's corner, and the way out below.
func (m *Model) previewView() string {
	p := m.preview
	bodyRows := max(m.height-1, 0)
	cols, rows := inner(m.width), inner(bodyRows)
	if p.lines == nil || p.cols != cols || p.rows != rows {
		p.cols, p.rows = cols, rows
		p.lines = p.image.HalfBlocks(cols, rows, m.colorProfile)
		if p.lines == nil {
			p.lines = []string{}
		}
	}

	body := make([]string, (rows-len(p.lines))/2, rows)
	for _, line := range p.lines {
		body = append(body, strings.Repeat(" ", (cols-ansi.StringWidth(line))/2)+line)
	}
	size := fmt.Sprintf("%d×%d", p.image.Width, p.image.Height)
	lines := boxed(body, m.width, bodyRows, p.name, size, true)

	hint := m.keyFor(config.ActionNormalMode)
	if keys, ok := m.bindings.Display(config.ActionClosePane); ok {
		hint += "/" + keys
	}
	footer := m.withPending(statusStyle.Render(m.fit(p.name + " · " + size + " px · " + hint + " close")))
	return strings.Join(append(lines, footer), "\n")
}
