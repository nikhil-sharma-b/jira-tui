// Package imageview turns an image file into something a terminal can show.
// Decoding happens once, off the UI loop, and keeps only a copy small enough to
// scale again cheaply; rendering then fits that copy to whatever size the
// screen is now, which is what lets a resize simply redraw.
//
// There are three renderers. Half-blocks work everywhere: each cell is two
// square pixels, the upper one in the foreground colour of ▀ and the lower one
// in the background. It is ordinary cell content, so it works in any terminal
// and survives anything tmux does to the screen. Kitty sends the pixels to a
// terminal that speaks the kitty graphics protocol, which draws them at its
// own resolution. Sixel encodes them, in the terminal's palette, for one that
// draws sixel, and for tmux, which draws them on its behalf.
package imageview

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	"image/draw"
	"io"
	"strconv"
	"strings"

	// The formats Jira screenshots and diagrams come in. GIF decodes to its
	// first frame, which is what a still preview can show.
	_ "image/gif"
	_ "image/jpeg"
	"image/png"

	"github.com/muesli/termenv"
)

// MaxSide bounds the copy kept for rendering, except one decoded for sixel.
// It is larger than any terminal
// is wide in cells or tall in half-cells, so the copy never limits what can be
// shown, while scaling it again on a resize stays well under a frame.
const MaxSide = 1024

// maxPixels is the largest image decoded at all: past it, the decoded pixels
// alone would take hundreds of megabytes before anything could be scaled down.
var maxPixels = 100_000_000

var (
	// ErrUnsupported is a file that is not a PNG, JPEG or GIF.
	ErrUnsupported = errors.New("not a PNG, JPEG or GIF image")
	// ErrTooLarge is an image whose pixels would not fit in memory sensibly.
	ErrTooLarge = errors.New("too large to preview")
)

// Image is a decoded picture: its size as stored in the file, which is what
// the user is told, and a copy of at most MaxSide on its longer side, which is
// what is drawn -- larger when decoded for sixel.
type Image struct {
	Width, Height int
	pixels        *image.RGBA
	// PNG is the image encoded for a terminal that draws pixels itself, set
	// only by DecodeForGraphics.
	PNG []byte
}

// Decode reads a PNG, JPEG or GIF. The header is read first, so an image too
// large to hold is refused before any of its pixels are.
func Decode(r io.Reader) (*Image, error) {
	return decode(r, MaxSide, 0)
}

// DecodeForGraphics is Decode that also keeps the image as a PNG of at most
// maxSide on its longer side, for a terminal that is sent the pixels and
// draws them at its own resolution. Every format is encoded afresh, so the
// terminal is sent one format, and never more pixels than it can show.
func DecodeForGraphics(r io.Reader, maxSide int) (*Image, error) {
	return decode(r, MaxSide, maxSide)
}

// DecodeForSixel is Decode that keeps a copy of up to maxSide on its longer
// side in place of MaxSide, since a sixel is drawn from that copy at the
// screen's own resolution.
func DecodeForSixel(r io.Reader, maxSide int) (*Image, error) {
	return decode(r, maxSide, 0)
}

func decode(r io.Reader, keptSide, pngSide int) (*Image, error) {
	var header bytes.Buffer
	cfg, _, err := image.DecodeConfig(io.TeeReader(r, &header))
	if err != nil {
		if errors.Is(err, image.ErrFormat) {
			return nil, ErrUnsupported
		}
		return nil, err
	}
	if cfg.Width <= 0 || cfg.Height <= 0 {
		return nil, ErrUnsupported
	}
	if cfg.Width*cfg.Height > maxPixels {
		return nil, fmt.Errorf("%dx%d: %w", cfg.Width, cfg.Height, ErrTooLarge)
	}
	src, _, err := image.Decode(io.MultiReader(&header, r))
	if err != nil {
		return nil, err
	}
	width, height := src.Bounds().Dx(), src.Bounds().Dy()
	keptWidth, keptHeight := bound(width, height, keptSide)
	img := &Image{Width: width, Height: height, pixels: scale(src, keptWidth, keptHeight)}
	if pngSide > 0 {
		if img.PNG, err = encodeScaled(src, pngSide); err != nil {
			return nil, err
		}
	}
	return img, nil
}

// bound is width x height shrunk, aspect ratio kept, until neither side is
// over maxSide; a smaller size is kept as it is.
func bound(width, height, maxSide int) (int, int) {
	if longest := max(width, height); longest > maxSide {
		return max(width*maxSide/longest, 1), max(height*maxSide/longest, 1)
	}
	return width, height
}

// encodeScaled is src as a PNG of at most maxSide on its longer side. The
// fastest compression is plenty: the PNG only crosses to the terminal once,
// and a slow encode is a wait before anything shows.
func encodeScaled(src image.Image, maxSide int) ([]byte, error) {
	width, height := bound(src.Bounds().Dx(), src.Bounds().Dy(), maxSide)
	var b bytes.Buffer
	enc := png.Encoder{CompressionLevel: png.BestSpeed}
	if err := enc.Encode(&b, scale(src, width, height)); err != nil {
		return nil, err
	}
	return b.Bytes(), nil
}

// Fit is the pixel grid an image of width x height is drawn at inside cols x
// rows cells: as large as fits, with its aspect ratio kept, at two pixel rows
// per cell. It scales up as well as down, since a small icon drawn at its own
// size would be a few cells across. Neither side is ever less than a pixel.
func Fit(width, height, cols, rows int) (pixelCols, pixelRows int) {
	if width <= 0 || height <= 0 || cols <= 0 || rows <= 0 {
		return 0, 0
	}
	// Compare cols/width with 2*rows/height without dividing.
	if cols*height <= 2*rows*width {
		return cols, max((height*cols+width/2)/width, 1)
	}
	return max((width*2*rows+height/2)/height, 1), 2 * rows
}

// HalfBlocks draws the image as large as fits in cols x rows cells. Every line
// is the fitted width, and there are as many lines as the fitted height needs:
// centring them is the caller's business. Colours are written in the given
// profile, so a terminal with fewer colours gets the nearest in its palette.
func (img *Image) HalfBlocks(cols, rows int, profile termenv.Profile) []string {
	width, height := Fit(img.Width, img.Height, cols, rows)
	if width == 0 {
		return nil
	}
	grid := scale(img.pixels, width, height)
	lines := make([]string, 0, (height+1)/2)
	var b strings.Builder
	for y := 0; y < height; y += 2 {
		b.Reset()
		last := ""
		for x := range width {
			seq := profile.Convert(termenv.RGBColor(hexAt(grid, x, y))).Sequence(false)
			if y+1 < height {
				seq += ";" + profile.Convert(termenv.RGBColor(hexAt(grid, x, y+1))).Sequence(true)
			}
			if seq != last && profile != termenv.Ascii {
				// A run of cells in the same colours needs the sequence once,
				// which is most of a flat screenshot.
				b.WriteString("\x1b[" + seq + "m")
				last = seq
			}
			b.WriteString("▀")
		}
		b.WriteString("\x1b[0m")
		lines = append(lines, b.String())
	}
	return lines
}

// hexAt is a pixel as #rrggbb, composited over black: a terminal cell has no
// transparency, and black is the likelier backdrop of the two.
func hexAt(img *image.RGBA, x, y int) string {
	i := img.PixOffset(x, y)
	// The pixels are alpha-premultiplied, so dropping alpha is compositing
	// over black.
	p := img.Pix[i : i+3 : i+3]
	return "#" + hex2(p[0]) + hex2(p[1]) + hex2(p[2])
}

func hex2(v uint8) string {
	s := strconv.FormatUint(uint64(v), 16)
	if len(s) == 1 {
		return "0" + s
	}
	return s
}

// scale resamples src to width x height by averaging the source pixels each
// destination pixel covers, which keeps thin lines and text from vanishing the
// way sampling one pixel would. Enlarging repeats pixels.
//
// The source is converted to RGBA a band of rows at a time -- the rows one
// destination row covers -- so a large image is never held twice over at full
// size, once as decoded and once converted.
func scale(src image.Image, width, height int) *image.RGBA {
	dst := image.NewRGBA(image.Rect(0, 0, width, height))
	bounds := src.Bounds()
	sw, sh := bounds.Dx(), bounds.Dy()
	band := image.NewRGBA(image.Rect(0, 0, sw, (sh+height-1)/height))
	for y := range height {
		y0 := y * sh / height
		y1 := max((y+1)*sh/height, y0+1)
		draw.Draw(band, image.Rect(0, 0, sw, y1-y0), src, image.Pt(bounds.Min.X, bounds.Min.Y+y0), draw.Src)
		for x := range width {
			x0 := x * sw / width
			x1 := max((x+1)*sw/width, x0+1)
			var r, g, b, a, n uint64
			for sy := range y1 - y0 {
				row := band.Pix[band.PixOffset(x0, sy) : band.PixOffset(x1-1, sy)+4]
				for i := 0; i < len(row); i += 4 {
					r += uint64(row[i])
					g += uint64(row[i+1])
					b += uint64(row[i+2])
					a += uint64(row[i+3])
					n++
				}
			}
			o := dst.PixOffset(x, y)
			dst.Pix[o] = uint8((r + n/2) / n)
			dst.Pix[o+1] = uint8((g + n/2) / n)
			dst.Pix[o+2] = uint8((b + n/2) / n)
			dst.Pix[o+3] = uint8((a + n/2) / n)
		}
	}
	return dst
}
