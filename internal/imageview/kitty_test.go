package imageview_test

import (
	"bytes"
	"encoding/base64"
	"image"
	"image/color"
	"image/png"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/nikhil-sharma-b/jira-tui/internal/imageview"
)

// apc splits one escape into its control keys and payload.
func apc(t *testing.T, seq string) (map[string]string, string) {
	t.Helper()
	body, ok := strings.CutPrefix(seq, "\x1b_G")
	if !ok {
		t.Fatalf("%q is not a kitty graphics command", seq)
	}
	body, ok = strings.CutSuffix(body, "\x1b\\")
	if !ok {
		t.Fatalf("%q is not terminated by ST", seq)
	}
	control, payload, _ := strings.Cut(body, ";")
	keys := map[string]string{}
	for _, kv := range strings.Split(control, ",") {
		k, v, _ := strings.Cut(kv, "=")
		keys[k] = v
	}
	return keys, payload
}

// unwrap takes a sequence out of tmux's passthrough envelope.
func unwrap(t *testing.T, seq string) string {
	t.Helper()
	inner, ok := strings.CutPrefix(seq, "\x1bPtmux;")
	if !ok {
		t.Fatalf("%q is not wrapped for tmux passthrough", seq)
	}
	inner, ok = strings.CutSuffix(inner, "\x1b\\")
	if !ok {
		t.Fatalf("%q does not end the passthrough", seq)
	}
	if strings.Contains(strings.ReplaceAll(inner, "\x1b\x1b", ""), "\x1b") {
		t.Fatalf("an escape inside %q is not doubled, so tmux would end the passthrough there", seq)
	}
	return strings.ReplaceAll(inner, "\x1b\x1b", "\x1b")
}

func TestTransmitSendsThePNGInChunksUnderTheImageID(t *testing.T) {
	data := bytes.Repeat([]byte("0123456789"), 1000)
	k := imageview.Kitty{ID: 7<<24 | 42}
	seqs := k.Transmit(data)
	if len(seqs) < 3 {
		t.Fatalf("%d bytes went in %d chunks; a terminal takes at most 4096 bytes of payload at a time", len(data), len(seqs))
	}

	var payload strings.Builder
	for i, seq := range seqs {
		keys, chunk := apc(t, seq)
		if len(chunk) > 4096 {
			t.Errorf("chunk %d carries %d bytes, over 4096", i, len(chunk))
		}
		payload.WriteString(chunk)
		more := "1"
		if i == len(seqs)-1 {
			more = "0"
		}
		if keys["m"] != more {
			t.Errorf("chunk %d has m=%q, want %s", i, keys["m"], more)
		}
		if i == 0 {
			for k, want := range map[string]string{"a": "t", "f": "100", "i": "117440554", "q": "2"} {
				if keys[k] != want {
					t.Errorf("first chunk has %s=%q, want %q", k, keys[k], want)
				}
			}
		}
	}
	got, err := base64.StdEncoding.DecodeString(payload.String())
	if err != nil || !bytes.Equal(got, data) {
		t.Errorf("the chunks do not reassemble into the image (%v)", err)
	}
}

func TestPlaceMakesAVirtualPlacementOfTheGivenSize(t *testing.T) {
	keys, _ := apc(t, imageview.Kitty{ID: 3<<24 | 9}.Place(30, 12))
	for k, want := range map[string]string{"a": "p", "U": "1", "i": "50331657", "p": "1", "c": "30", "r": "12", "q": "2"} {
		if keys[k] != want {
			t.Errorf("placement has %s=%q, want %q", k, keys[k], want)
		}
	}
}

func TestDeleteFreesTheImageData(t *testing.T) {
	keys, _ := apc(t, imageview.Kitty{ID: 1<<24 | 1}.Delete())
	for k, want := range map[string]string{"a": "d", "d": "I", "i": "16777217", "q": "2"} {
		if keys[k] != want {
			t.Errorf("delete has %s=%q, want %q", k, keys[k], want)
		}
	}
}

func TestInTmuxEveryCommandGoesThroughPassthrough(t *testing.T) {
	plain := imageview.Kitty{ID: 2<<24 | 5}
	wrapped := imageview.Kitty{ID: plain.ID, Tmux: true}
	pairs := [][2]string{
		{wrapped.Place(4, 2), plain.Place(4, 2)},
		{wrapped.Delete(), plain.Delete()},
	}
	for i, seq := range wrapped.Transmit([]byte("png")) {
		pairs = append(pairs, [2]string{seq, plain.Transmit([]byte("png"))[i]})
	}
	for _, p := range pairs {
		if got := unwrap(t, p[0]); got != p[1] {
			t.Errorf("unwrapped %q, want %q", got, p[1])
		}
	}
}

// Placeholders are ordinary text, one cell each: the colour names the image
// and the marks name the cell, so a redraw by tmux draws the same image.
func TestPlaceholdersNameEveryCellOfTheImage(t *testing.T) {
	k := imageview.Kitty{ID: 200<<24 | 17}
	lines := k.Placeholders(5, 3)
	if len(lines) != 3 {
		t.Fatalf("%d lines, want 3", len(lines))
	}
	marks := imageview.Diacritics()
	for row, line := range lines {
		if !strings.HasPrefix(line, "\x1b[38;5;17m") {
			t.Errorf("line %d does not set the foreground to colour 17: %q", row, line)
		}
		if w := ansi.StringWidth(line); w != 5 {
			t.Errorf("line %d is %d cells wide, want 5", row, w)
		}
		cells := strings.Split(ansi.Strip(line), "\U0010EEEE")[1:]
		if len(cells) != 5 {
			t.Fatalf("line %d has %d placeholders, want 5", row, len(cells))
		}
		for col, cell := range cells {
			want := string([]rune{marks[row], marks[col], marks[200]})
			if cell != want {
				t.Errorf("cell %d,%d carries %U, want %U", row, col, []rune(cell), []rune(want))
			}
		}
	}
}

func TestFitCellsKeepsTheAspectRatioForTheCellShape(t *testing.T) {
	tests := []struct {
		name                     string
		w, h, cols, rows, cw, ch int
		wantCols, wantRows       int
	}{
		{"wide image in square cells fills the width", 200, 100, 40, 40, 10, 10, 40, 20},
		{"tall image in square cells fills the height", 100, 200, 40, 40, 10, 10, 20, 40},
		{"cells twice as tall as wide", 100, 100, 40, 40, 10, 20, 40, 20},
		{"height binds in tall cells", 100, 100, 80, 10, 10, 20, 20, 10},
		{"unknown cell size assumes 1:2", 100, 100, 40, 40, 0, 0, 40, 20},
		{"never less than a cell", 1000, 1, 10, 10, 10, 20, 10, 1},
		{"nothing to fit into", 100, 100, 0, 10, 10, 20, 0, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cols, rows := imageview.FitCells(tt.w, tt.h, tt.cols, tt.rows, tt.cw, tt.ch)
			if cols != tt.wantCols || rows != tt.wantRows {
				t.Errorf("FitCells = %dx%d, want %dx%d", cols, rows, tt.wantCols, tt.wantRows)
			}
		})
	}
}

func TestDecodeForGraphicsKeepsAPNGNoLargerThanAsked(t *testing.T) {
	img, err := imageview.DecodeForGraphics(bytes.NewReader(encodePNG(t, filled(300, 150, color.White))), 100)
	if err != nil {
		t.Fatal(err)
	}
	if img.Width != 300 || img.Height != 150 {
		t.Errorf("size = %dx%d, want the file's 300x150", img.Width, img.Height)
	}
	decoded, err := png.Decode(bytes.NewReader(img.PNG))
	if err != nil {
		t.Fatalf("PNG does not decode: %v", err)
	}
	if got := decoded.Bounds(); got != image.Rect(0, 0, 100, 50) {
		t.Errorf("PNG is %v, want 100x50", got)
	}
	if lines := img.HalfBlocks(10, 10, 0); len(lines) == 0 {
		t.Error("an image decoded for graphics cannot also be drawn in half-blocks")
	}
}

func TestDecodeKeepsNoPNG(t *testing.T) {
	img, err := imageview.Decode(bytes.NewReader(encodePNG(t, filled(30, 15, color.White))))
	if err != nil {
		t.Fatal(err)
	}
	if img.PNG != nil {
		t.Error("Decode encoded a PNG nothing asked for")
	}
}

func TestAnswersKittyReadsUpToTheDeviceAttributes(t *testing.T) {
	tests := []struct {
		name  string
		reply string
		want  bool
	}{
		{"kitty answers", "\x1b_Gi=31;OK\x1b\\\x1b[?62;22c", true},
		{"only device attributes", "\x1b[?62;4;22c", false},
		{"graphics error", "\x1b_Gi=31;ENOTSUPPORTED:no\x1b\\\x1b[?62c", false},
		{"a terminal with no graphics", "\x1b[?1;2c", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := strings.NewReader(tt.reply + "typed after")
			got, err := imageview.AnswersKitty(r)
			if err != nil {
				t.Fatal(err)
			}
			if got != tt.want {
				t.Errorf("AnswersKitty = %v, want %v", got, tt.want)
			}
			if rest := r.Len(); rest != len("typed after") {
				t.Errorf("read %d bytes past the device attributes", len("typed after")-rest)
			}
		})
	}
}

func TestKittyQueryAsksForGraphicsThenDeviceAttributes(t *testing.T) {
	q := imageview.KittyQuery()
	seq, da, ok := strings.Cut(q, "\x1b\\")
	if !ok || da != "\x1b[c" {
		t.Fatalf("query = %q, want a graphics command then DA1", q)
	}
	keys, _ := apc(t, seq+"\x1b\\")
	if keys["a"] != "q" || keys["i"] != "31" {
		t.Errorf("query keys = %v", keys)
	}
}
