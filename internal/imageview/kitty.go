package imageview

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"io"
	"regexp"
	"strings"
)

// Kitty speaks the kitty graphics protocol for one image, which kitty and
// ghostty draw at the screen's own resolution.
//
// The image is shown through Unicode placeholders rather than placed at the
// cursor: it is sent once, then every cell it covers is written as U+10EEEE
// with marks naming the image and the cell. Those are ordinary characters, so
// tmux keeps them in its grid like any text and draws them again after a pane
// redraw, a window switch or a reattach -- where a placement made at the
// cursor lives only in the outer terminal and is lost the first time tmux
// repaints.
type Kitty struct {
	// ID names the image to the terminal. Its low byte, 1-255, is written as
	// a 256-colour foreground, which tmux passes on unchanged where it might
	// round a truecolour one; its high byte rides in each cell's third mark.
	// The bytes between must be zero.
	ID uint32
	// Tmux wraps every command in tmux's passthrough, which is how it
	// reaches the outer terminal instead of being eaten by tmux.
	Tmux bool
}

// chunkSize is the most payload one command may carry.
const chunkSize = 4096

// placeholder is the character kitty draws a placed image's cell in place of.
const placeholder = "\U0010EEEE"

// MaxPlaceholderCells is the most rows or columns placeholders can name, one
// per mark in kitty's list.
const MaxPlaceholderCells = len(diacritics)

// Transmit sends a PNG to the terminal under k's ID, split into commands of
// at most chunkSize bytes of base64 each. The terminal holds it without
// drawing it until something is placed. q=2 keeps the terminal from answering,
// since an answer would arrive as keyboard input.
func (k Kitty) Transmit(png []byte) []string {
	data := base64.StdEncoding.EncodeToString(png)
	var seqs []string
	for first := true; first || data != ""; first = false {
		chunk := data[:min(len(data), chunkSize)]
		data = data[len(chunk):]
		more := 0
		if data != "" {
			more = 1
		}
		control := fmt.Sprintf("m=%d", more)
		if first {
			control = fmt.Sprintf("a=t,f=100,t=d,i=%d,q=2,", k.ID) + control
		}
		seqs = append(seqs, k.command(control, chunk))
	}
	return seqs
}

// Place makes the image's one virtual placement cols x rows cells: the
// terminal fits the image into that box, keeping its aspect ratio, wherever
// placeholders for it appear. Placing again replaces the placement, which is
// what a resize does.
func (k Kitty) Place(cols, rows int) string {
	return k.command(fmt.Sprintf("a=p,U=1,i=%d,p=1,c=%d,r=%d,q=2", k.ID, cols, rows), "")
}

// Delete removes the image's placements and frees its data in the terminal,
// so nothing of it outlives the preview.
func (k Kitty) Delete() string {
	return k.command(fmt.Sprintf("a=d,d=I,i=%d,q=2", k.ID), "")
}

// Placeholders are the lines of text that show a placed image cols x rows
// cells. Every cell names its row, column and the ID's high byte, rather than
// leaving the column to be inferred from the cell before: tmux redraws a line
// in pieces, and a piece must stand on its own.
func (k Kitty) Placeholders(cols, rows int) []string {
	cols, rows = min(cols, MaxPlaceholderCells), min(rows, MaxPlaceholderCells)
	high := string(diacritics[k.ID>>24])
	lines := make([]string, rows)
	var b strings.Builder
	for row := range rows {
		b.Reset()
		fmt.Fprintf(&b, "\x1b[38;5;%dm", k.ID&0xff)
		for col := range cols {
			b.WriteString(placeholder)
			b.WriteRune(diacritics[row])
			b.WriteRune(diacritics[col])
			b.WriteString(high)
		}
		b.WriteString("\x1b[39m")
		lines[row] = b.String()
	}
	return lines
}

// command is one graphics command, wrapped for tmux when k is inside it.
func (k Kitty) command(control, payload string) string {
	seq := "\x1b_G" + control
	if payload != "" {
		seq += ";" + payload
	}
	seq += "\x1b\\"
	if !k.Tmux {
		return seq
	}
	// tmux hands a passthrough to the outer terminal as is. Every escape
	// inside is doubled, or tmux would take the first one as the end.
	return "\x1bPtmux;" + strings.ReplaceAll(seq, "\x1b", "\x1b\x1b") + "\x1b\\"
}

// queryID is the ID the support query asks about. No image is ever stored
// under it: a query only checks that one could be.
const queryID = 31

// KittyQuery asks whether the terminal speaks the graphics protocol, then
// asks for its primary device attributes. Every terminal answers the second,
// so its answer marks the end of what there is to read: a terminal with
// graphics answers the query first, and one without says nothing to it.
//
// It is not for use inside tmux, which answers the device attributes itself
// before the terminal's answer to the query could arrive.
func KittyQuery() string {
	return fmt.Sprintf("\x1b_Gi=%d,s=1,v=1,a=q,t=d,f=24;AAAA\x1b\\\x1b[c", queryID)
}

var (
	deviceAttributes = regexp.MustCompile(`\x1b\[\?[0-9;]*c$`)
	kittyOK          = fmt.Appendf(nil, "\x1b_Gi=%d;OK\x1b\\", queryID)
)

// AnswersKitty reads the replies to KittyQuery, up to and including the
// device attributes, and reports whether the graphics query was answered OK.
// It reads a byte at a time so that nothing typed after the replies is taken.
func AnswersKitty(r io.Reader) (bool, error) {
	var got []byte
	b := make([]byte, 1)
	for !deviceAttributes.Match(got) {
		if _, err := io.ReadFull(r, b); err != nil {
			return bytes.Contains(got, kittyOK), err
		}
		got = append(got, b[0])
	}
	return bytes.Contains(got, kittyOK), nil
}

// FitCells is the box of cells an image of width x height pixels fills inside
// cols x rows cells of cellWidth x cellHeight pixels: as large as fits, with
// its aspect ratio kept. An unknown cell size is taken to be twice as tall as
// wide, which most fonts are near enough. Neither side is ever less than a
// cell.
func FitCells(width, height, cols, rows, cellWidth, cellHeight int) (fitCols, fitRows int) {
	if width <= 0 || height <= 0 || cols <= 0 || rows <= 0 {
		return 0, 0
	}
	if cellWidth <= 0 || cellHeight <= 0 {
		cellWidth, cellHeight = 1, 2
	}
	// Compare the box's aspect ratio with the image's without dividing.
	boxWidth, boxHeight := cols*cellWidth, rows*cellHeight
	if boxWidth*height <= boxHeight*width {
		fit := (height*boxWidth + width*cellHeight/2) / (width * cellHeight)
		return cols, min(max(fit, 1), rows)
	}
	fit := (width*boxHeight + height*cellWidth/2) / (height * cellWidth)
	return min(max(fit, 1), cols), rows
}
