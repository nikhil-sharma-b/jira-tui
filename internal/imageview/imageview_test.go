package imageview_test

import (
	"bytes"
	"errors"
	"image"
	"image/color"
	"image/gif"
	"image/jpeg"
	"image/png"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"

	"github.com/nikhil-sharma-b/jira-tui/internal/imageview"
)

var (
	red  = color.RGBA{255, 0, 0, 255}
	blue = color.RGBA{0, 0, 255, 255}
)

// filled is a width x height image in one colour.
func filled(width, height int, c color.Color) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	for y := range height {
		for x := range width {
			img.Set(x, y, c)
		}
	}
	return img
}

func encodePNG(t *testing.T, img image.Image) []byte {
	t.Helper()
	var b bytes.Buffer
	if err := png.Encode(&b, img); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func decode(t *testing.T, data []byte) *imageview.Image {
	t.Helper()
	img, err := imageview.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	return img
}

func TestDecodeReadsPNGJPEGAndGIFAndKeepsTheirPixelSize(t *testing.T) {
	src := filled(30, 20, red)
	var jpg, animated bytes.Buffer
	if err := jpeg.Encode(&jpg, src, nil); err != nil {
		t.Fatal(err)
	}
	palette := color.Palette{red, blue}
	frame := func(c color.Color) *image.Paletted {
		p := image.NewPaletted(image.Rect(0, 0, 30, 20), palette)
		for i := range p.Pix {
			p.Pix[i] = uint8(palette.Index(c))
		}
		return p
	}
	if err := gif.EncodeAll(&animated, &gif.GIF{Image: []*image.Paletted{frame(red), frame(blue)}, Delay: []int{0, 0}}); err != nil {
		t.Fatal(err)
	}

	for name, data := range map[string][]byte{"png": encodePNG(t, src), "jpeg": jpg.Bytes(), "gif": animated.Bytes()} {
		t.Run(name, func(t *testing.T) {
			img := decode(t, data)
			if img.Width != 30 || img.Height != 20 {
				t.Errorf("size = %dx%d, want 30x20", img.Width, img.Height)
			}
			// Red everywhere, which for the GIF means its first frame.
			line := img.HalfBlocks(1, 1, termenv.TrueColor)[0]
			if !strings.Contains(line, "38;2;25") || strings.Contains(line, ";255m") {
				t.Errorf("rendered %q, want the image's red and not the second frame's blue", line)
			}
		})
	}
}

func TestDecodeRejectsWhatIsNotAnImage(t *testing.T) {
	_, err := imageview.Decode(strings.NewReader("%PDF-1.7 not an image"))
	if !errors.Is(err, imageview.ErrUnsupported) {
		t.Errorf("err = %v, want ErrUnsupported", err)
	}
}

func TestDecodeRefusesAnImageTooLargeToHoldBeforeDecodingIt(t *testing.T) {
	defer imageview.SetMaxPixels(100)()
	_, err := imageview.Decode(bytes.NewReader(encodePNG(t, filled(20, 10, red))))
	if !errors.Is(err, imageview.ErrTooLarge) {
		t.Errorf("err = %v, want ErrTooLarge", err)
	}
}

func TestDecodeKeepsOnlyADownscaledCopyOfALargeImage(t *testing.T) {
	img := decode(t, encodePNG(t, filled(3000, 1500, red)))
	if img.Width != 3000 || img.Height != 1500 {
		t.Errorf("size = %dx%d, want the original 3000x1500", img.Width, img.Height)
	}
	if got := imageview.Stored(img); got.Dx() != imageview.MaxSide || got.Dy() != imageview.MaxSide/2 {
		t.Errorf("stored copy is %v, want %dx%d", got, imageview.MaxSide, imageview.MaxSide/2)
	}
}

// A cell is about twice as tall as it is wide, and a half-block splits it into
// two square pixels, so the grid is cols pixels wide and 2*rows pixels tall.
func TestFitKeepsTheAspectRatioAtTwoPixelRowsPerCell(t *testing.T) {
	tests := []struct {
		name               string
		width, height      int
		cols, rows         int
		wantCols, wantRows int
	}{
		{"wide, width-bound", 400, 100, 80, 40, 80, 20},
		{"tall, height-bound", 100, 400, 80, 20, 10, 40},
		{"same shape as the overlay", 200, 100, 80, 20, 80, 40},
		{"tiny, scaled up", 4, 2, 80, 20, 80, 40},
		{"never below a pixel", 1000, 1, 10, 10, 10, 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cols, pixelRows := imageview.Fit(tt.width, tt.height, tt.cols, tt.rows)
			if cols != tt.wantCols || pixelRows != tt.wantRows {
				t.Errorf("Fit = %d cols x %d pixel rows, want %d x %d", cols, pixelRows, tt.wantCols, tt.wantRows)
			}
		})
	}
}

func TestHalfBlocksDrawsTwoPixelRowsPerCellInTruecolor(t *testing.T) {
	src := image.NewRGBA(image.Rect(0, 0, 2, 2))
	src.Set(0, 0, red)
	src.Set(1, 0, blue)
	src.Set(0, 1, blue)
	src.Set(1, 1, red)
	lines := decode(t, encodePNG(t, src)).HalfBlocks(2, 1, termenv.TrueColor)

	if len(lines) != 1 {
		t.Fatalf("got %d lines, want 1 row of cells for 2 rows of pixels", len(lines))
	}
	want := "\x1b[38;2;255;0;0;48;2;0;0;255m▀\x1b[38;2;0;0;255;48;2;255;0;0m▀\x1b[0m"
	if lines[0] != want {
		t.Errorf("line = %q\nwant   %q", lines[0], want)
	}
}

func TestHalfBlocksFallsBackToTheNearestColourInASmallerPalette(t *testing.T) {
	img := decode(t, encodePNG(t, filled(2, 2, red)))
	for name, tt := range map[string]struct {
		profile termenv.Profile
		want    string
	}{
		"256 colours": {termenv.ANSI256, "\x1b[38;5;196;48;5;196m▀"},
		"16 colours":  {termenv.ANSI, "\x1b[91;101m▀"},
	} {
		t.Run(name, func(t *testing.T) {
			line := img.HalfBlocks(2, 1, tt.profile)[0]
			if !strings.HasPrefix(line, tt.want) {
				t.Errorf("line = %q, want it to start %q", line, tt.want)
			}
		})
	}
}

func TestHalfBlocksLeavesTheLowerHalfOfAnOddLastRowEmpty(t *testing.T) {
	lines := decode(t, encodePNG(t, filled(1000, 1, red))).HalfBlocks(10, 10, termenv.TrueColor)
	if len(lines) != 1 {
		t.Fatalf("got %d lines, want 1", len(lines))
	}
	if strings.Contains(lines[0], "48;") {
		t.Errorf("line = %q, want no background under a missing pixel row", lines[0])
	}
}

func TestHalfBlocksLinesAreAllTheFittedWidth(t *testing.T) {
	img := decode(t, encodePNG(t, filled(300, 200, red)))
	cols, pixelRows := imageview.Fit(img.Width, img.Height, 50, 12)
	lines := img.HalfBlocks(50, 12, termenv.TrueColor)
	if len(lines) != (pixelRows+1)/2 {
		t.Errorf("got %d lines, want %d", len(lines), (pixelRows+1)/2)
	}
	for i, line := range lines {
		if w := ansi.StringWidth(line); w != cols {
			t.Errorf("line %d is %d cells wide, want %d", i, w, cols)
		}
	}
}
