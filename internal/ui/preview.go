package ui

import (
	"fmt"
	"math/rand/v2"
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
	// kitty is the image as the terminal holds it, nil when it is drawn in
	// half-blocks. placedCols x placedRows is the placement last sent, which
	// the placeholders drawn must match.
	kitty                  *imageview.Kitty
	placedCols, placedRows int
	// sixel is the image as sixel, nil when it is not drawn that way.
	sixel *sixelDrawing
}

// sixelDrawing is a preview's image as sixel: box is the size the overlay
// wants it at, and seq the image encoded at that size, empty until it is.
// One encoding runs at a time, busy while it does, so dragging a window's
// edge does not start one for every size it passes through.
type sixelDrawing struct {
	box  sixelBox
	seq  string
	busy bool
}

// sixelBox is the size a sixel is drawn at: its pixels, and the cells they
// cover.
type sixelBox struct {
	width, height int
	cols, rows    int
}

// sixelMsg is an encoding finished off the update loop, for the preview and
// the box it was started for.
type sixelMsg struct {
	preview *imagePreview
	box     sixelBox
	seq     string
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
	if m.renderer.err != nil {
		m.status = m.renderer.err
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

// decodeImage reads the downloaded file for the renderer that will draw it:
// a kitty terminal is sent a PNG, and a sixel one is encoded from a copy near
// the screen's resolution. Decoding and the downscale it ends with run in the
// download's command, off the update loop, so a large screenshot never stalls
// the screen.
func decodeImage(path string, kind rendererKind) (*imageview.Image, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	switch kind {
	case drawKitty:
		return imageview.DecodeForGraphics(f, graphicsSide)
	case drawSixel:
		return imageview.DecodeForSixel(f, graphicsSide)
	}
	return imageview.Decode(f)
}

// openPreview puts the overlay up on a decoded image. A kitty terminal is
// sent the image now, once; everything after is placing it. A sixel one is
// sent it in the frame, once it is encoded for the overlay's size.
func (m *Model) openPreview(name string, img *imageview.Image) tea.Cmd {
	m.preview = &imagePreview{name: name, image: img}
	switch {
	case m.renderer.kind == drawKitty && img.PNG != nil:
		k := imageview.Kitty{ID: newImageID(), Tmux: m.terminal.Tmux}
		for _, seq := range k.Transmit(img.PNG) {
			m.writeGraphics(seq)
		}
		m.preview.kitty = &k
	case m.renderer.kind == drawSixel:
		m.preview.sixel = &sixelDrawing{}
	}
	return m.placePreview()
}

// newImageID is a fresh ID for kitty to hold an image under. It is random
// rather than counted: every jt in every tmux pane shares the one outer
// terminal, and one preview closing must not delete another's image. Both
// bytes it uses are non-zero; see imageview.Kitty.
func newImageID() uint32 {
	r := rand.Uint32()
	return (1+(r>>8)%255)<<24 | (1 + r%255)
}

// placePreview fits the preview's image to the overlay as it now is, when
// that differs from what it was last fitted to: a kitty image is placed
// again, and a sixel encoded again, off the update loop.
func (m *Model) placePreview() tea.Cmd {
	p := m.preview
	switch {
	case p == nil:
		return nil
	case p.sixel != nil:
		return m.encodeSixel()
	case p.kitty == nil:
		return nil
	}
	cols, rows := m.previewBox()
	cols = min(cols, imageview.MaxPlaceholderCells)
	rows = min(rows, imageview.MaxPlaceholderCells)
	cellWidth, cellHeight := m.cellSize()
	cols, rows = imageview.FitCells(p.image.Width, p.image.Height, cols, rows, cellWidth, cellHeight)
	if cols == 0 || (cols == p.placedCols && rows == p.placedRows) {
		return nil
	}
	m.writeGraphics(p.kitty.Place(cols, rows))
	p.placedCols, p.placedRows = cols, rows
	// The placeholders drawn must name the cells just placed.
	p.lines = nil
	return nil
}

// assumedCellWidth x assumedCellHeight is the cell size in pixels a sixel is
// sized for when the terminal does not say: a common one for a 10-11pt font.
const assumedCellWidth, assumedCellHeight = 10, 20

// maxSixelColors bounds the palette a sixel is quantised to, whatever the
// terminal reports: past it, encoding takes longer without a screenshot
// looking any different.
const maxSixelColors = 1024

// encodeSixel starts the preview's image encoding as sixel for the overlay as
// it now is. What was encoded for another size is dropped at once, so the
// frame never draws an image where it no longer fits.
func (m *Model) encodeSixel() tea.Cmd {
	p := m.preview
	cols, rows := m.previewBox()
	cellWidth, cellHeight := m.cellSize()
	if cellWidth <= 0 || cellHeight <= 0 {
		cellWidth, cellHeight = assumedCellWidth, assumedCellHeight
	}
	var box sixelBox
	box.width, box.height = imageview.SixelFit(p.image.Width, p.image.Height, cols*cellWidth, rows*cellHeight)
	box.cols, box.rows = ceilDiv(box.width, cellWidth), ceilDiv(box.height, cellHeight)
	if box != p.sixel.box {
		p.sixel.box, p.sixel.seq, p.lines = box, "", nil
	}
	if p.sixel.busy || p.sixel.seq != "" || box.width == 0 {
		return nil
	}
	p.sixel.busy = true
	img, colors := p.image, m.terminal.SixelColors
	if colors <= 0 {
		// The terminal did not say. 256 is what sixel terminals commonly
		// have, and xterm's default.
		colors = 256
	}
	colors = min(colors, maxSixelColors)
	return func() tea.Msg {
		return sixelMsg{preview: p, box: box, seq: img.Sixel(box.width, box.height, colors)}
	}
}

// ceilDiv is a / b rounded up, how many cells a run of pixels touches.
func ceilDiv(a, b int) int { return (a + b - 1) / b }

// handleSixel takes a finished encoding. One for a preview since closed is
// dropped; one for a size since left behind starts the encoding for the size
// the overlay is now.
func (m *Model) handleSixel(msg sixelMsg) tea.Cmd {
	p := m.preview
	if p == nil || p != msg.preview {
		return nil
	}
	p.sixel.busy = false
	if msg.box != p.sixel.box {
		return m.encodeSixel()
	}
	p.sixel.seq = msg.seq
	return nil
}

// closePreview takes the overlay down, and a kitty image out of the terminal
// with it, data and all, so no image outlives its preview.
//
// A sixel is part of the screen, so the screen underneath is cleared and
// painted whole: bubbletea skips the lines of a frame that match the last
// one, and a line of the pane that matched one of the blank lines under the
// image would leave the image showing there.
func (m *Model) closePreview() tea.Cmd {
	p := m.preview
	m.preview = nil
	switch {
	case p == nil:
	case p.kitty != nil:
		m.writeGraphics(p.kitty.Delete())
	case p.sixel != nil:
		return tea.ClearScreen
	}
	return nil
}

// previewBox is the room inside the overlay's frame for the image, less the
// line saying why it is drawn in half-blocks when there is one.
func (m *Model) previewBox() (cols, rows int) {
	cols, rows = inner(m.width), inner(max(m.height-1, 0))
	if m.renderer.note != "" {
		rows = max(rows-1, 0)
	}
	return cols, rows
}

// handlePreviewAction is what an action does while the preview is up. Esc and
// q close it and nothing else -- Esc does not also hide the search marks, since
// the pane underneath is meant to come back exactly as it was left. Quitting
// still quits; everything else is swallowed, since it would act on a screen
// the user cannot see.
func (m *Model) handlePreviewAction(action config.Action) tea.Cmd {
	switch action {
	case config.ActionNormalMode, config.ActionClosePane:
		return m.closePreview()
	case config.ActionQuit:
		m.closePreview()
		return tea.Quit
	}
	return nil
}

// previewView draws the overlay: the image centred in a frame named for the
// file, its pixel size in the frame's corner, and the way out below.
func (m *Model) previewView() string {
	p := m.preview
	bodyRows := max(m.height-1, 0)
	cols, rows := m.previewBox()
	if p.lines == nil || p.cols != cols || p.rows != rows {
		p.cols, p.rows = cols, rows
		switch {
		case p.kitty != nil:
			p.lines = p.kitty.Placeholders(p.placedCols, p.placedRows)
		case p.sixel != nil:
			// The sixel is drawn over blank cells, which are what the frame
			// holds where it lies.
			p.lines = make([]string, p.sixel.box.rows)
			for i := range p.lines {
				p.lines[i] = strings.Repeat(" ", p.sixel.box.cols)
			}
		default:
			p.lines = p.image.HalfBlocks(cols, rows, m.colorProfile)
		}
		if p.lines == nil {
			p.lines = []string{}
		}
	}

	var body []string
	if m.renderer.note != "" {
		body = append(body, noteStyle.Render(ansi.Truncate(m.renderer.note, cols, "…")))
	}
	body = append(body, make([]string, (rows-len(p.lines))/2, rows)...)
	top := len(body)
	for _, line := range p.lines {
		body = append(body, strings.Repeat(" ", (cols-ansi.StringWidth(line))/2)+line)
	}
	size := fmt.Sprintf("%d×%d", p.image.Width, p.image.Height)
	lines := boxed(body, m.width, bodyRows, p.name, size, true)
	if p.sixel != nil && p.sixel.seq != "" && len(p.lines) > 0 {
		lines[1+top+len(p.lines)-1] += drawSixelAt(p.sixel.seq, 1+top, 1+(cols-p.sixel.box.cols)/2)
	}

	hint := m.keyFor(config.ActionNormalMode)
	if keys, ok := m.bindings.Display(config.ActionClosePane); ok {
		hint += "/" + keys
	}
	footer := m.withPending(statusStyle.Render(m.fit(p.name + " · " + size + " px · " + hint + " close")))
	return strings.Join(append(lines, footer), "\n")
}

// drawSixelAt is seq drawn from the cell row, col, counted from 0, with the
// cursor saved before and restored after, so what follows in the frame lands
// where it would have.
//
// It is appended to the last line the image covers rather than the first: a
// terminal clears the part of an image that text is later written over, so
// every blank line under it has to be written before it is drawn. bubbletea
// then writes the lines again only when they change, which in the overlay is
// only on a resize, when the image changes with them.
func drawSixelAt(seq string, row, col int) string {
	return "\x1b7" + fmt.Sprintf("\x1b[%d;%dH", row+1, col+1) + seq + "\x1b8"
}
