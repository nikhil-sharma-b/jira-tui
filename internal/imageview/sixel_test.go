package imageview_test

import (
	"bytes"
	"image"
	"image/color"
	"strconv"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/nikhil-sharma-b/jira-tui/internal/imageview"
)

// sixelImage is a sixel sequence read back: its declared size, the colours it
// defined, and every pixel it set, as a colour register.
type sixelImage struct {
	width, height int
	palette       map[int]color.RGBA
	pixels        map[image.Point]int
}

// readSixel decodes the subset of sixel the encoder writes: raster
// attributes, RGB colour definitions, colour selection, repeats, carriage
// returns and new lines.
func readSixel(t *testing.T, seq string) sixelImage {
	t.Helper()
	body, ok := strings.CutPrefix(seq, "\x1bP")
	if !ok {
		t.Fatalf("%q does not start a DCS", seq[:min(len(seq), 20)])
	}
	body, ok = strings.CutSuffix(body, "\x1b\\")
	if !ok {
		t.Fatalf("sixel does not end with ST")
	}
	_, body, ok = strings.Cut(body, "q")
	if !ok {
		t.Fatal("DCS is not a sixel (no q)")
	}
	img := sixelImage{palette: map[int]color.RGBA{}, pixels: map[image.Point]int{}}
	// number reads the decimal at the front of body.
	number := func() int {
		end := 0
		for end < len(body) && body[end] >= '0' && body[end] <= '9' {
			end++
		}
		n, err := strconv.Atoi(body[:end])
		if err != nil {
			t.Fatalf("expected a number at %q", body[:min(len(body), 10)])
		}
		body = body[end:]
		return n
	}
	// params reads n;n;n... at the front of body.
	params := func() []int {
		ps := []int{number()}
		for strings.HasPrefix(body, ";") {
			body = body[1:]
			ps = append(ps, number())
		}
		return ps
	}
	x, band, reg, repeat := 0, 0, -1, 1
	for body != "" {
		c := body[0]
		switch {
		case c == '"':
			body = body[1:]
			ps := params()
			if len(ps) != 4 {
				t.Fatalf("raster attributes %v, want 4", ps)
			}
			img.width, img.height = ps[2], ps[3]
		case c == '#':
			body = body[1:]
			ps := params()
			reg = ps[0]
			if len(ps) == 5 {
				if ps[1] != 2 {
					t.Fatalf("colour %d defined in space %d, want RGB (2)", reg, ps[1])
				}
				pct := func(p int) uint8 { return uint8((p*255 + 50) / 100) }
				img.palette[reg] = color.RGBA{pct(ps[2]), pct(ps[3]), pct(ps[4]), 255}
			} else if _, ok := img.palette[reg]; !ok {
				t.Fatalf("colour %d selected before it was defined", reg)
			}
		case c == '!':
			body = body[1:]
			repeat = number()
		case c == '$':
			body, x = body[1:], 0
		case c == '-':
			body, x = body[1:], 0
			band++
		case c >= '?' && c <= '~':
			body = body[1:]
			bits := c - '?'
			for range repeat {
				for i := range 6 {
					if bits&(1<<i) != 0 {
						img.pixels[image.Pt(x, band*6+i)] = reg
					}
				}
				x++
			}
			repeat = 1
		default:
			t.Fatalf("unexpected %q in sixel data", c)
		}
	}
	return img
}

// at is the colour drawn at x, y, and whether anything was.
func (s sixelImage) at(x, y int) (color.RGBA, bool) {
	reg, ok := s.pixels[image.Pt(x, y)]
	return s.palette[reg], ok
}

// gradient is width x height in a gradient with no two pixels alike.
func gradient(width, height int) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	for y := range height {
		for x := range width {
			img.Set(x, y, color.RGBA{uint8(x * 255 / width), uint8(y * 255 / height), uint8((x + y) * 255 / (width + height)), 255})
		}
	}
	return img
}

func near(a, b color.RGBA, within int) bool {
	d := func(x, y uint8) bool { return max(int(x)-int(y), int(y)-int(x)) <= within }
	return d(a.R, b.R) && d(a.G, b.G) && d(a.B, b.B)
}

func TestSixelDrawsEveryPixelAtTheRequestedSize(t *testing.T) {
	src := filled(8, 6, color.RGBA{255, 0, 0, 255})
	for x := 4; x < 8; x++ {
		for y := range 6 {
			src.Set(x, y, color.RGBA{0, 0, 255, 255})
		}
	}
	img := decode(t, encodePNG(t, src))

	s := readSixel(t, img.Sixel(16, 12, 256))

	if s.width != 16 || s.height != 12 {
		t.Errorf("raster size %dx%d, want 16x12", s.width, s.height)
	}
	red, blue := color.RGBA{255, 0, 0, 255}, color.RGBA{0, 0, 255, 255}
	for y := range 12 {
		for x := range 16 {
			got, ok := s.at(x, y)
			want := red
			if x >= 8 {
				want = blue
			}
			if !ok || !near(got, want, 3) {
				t.Fatalf("pixel %d,%d = %v (drawn %v), want %v", x, y, got, ok, want)
			}
		}
	}
	if len(s.pixels) != 16*12 {
		t.Errorf("drew %d pixels, want %d", len(s.pixels), 16*12)
	}
}

func TestSixelQuantisesToThePaletteSize(t *testing.T) {
	img := decode(t, encodePNG(t, gradient(64, 48)))
	for _, colors := range []int{16, 256} {
		s := readSixel(t, img.Sixel(64, 48, colors))
		if len(s.palette) > colors {
			t.Errorf("%d colours defined for a palette of %d", len(s.palette), colors)
		}
		if len(s.palette) < colors/2 {
			t.Errorf("only %d of %d colours used on a gradient", len(s.palette), colors)
		}
		// Every pixel is still near its source: quantising picks colours
		// from the image rather than a fixed palette.
		within := 255 / 4
		if colors == 256 {
			within = 255 / 12
		}
		for y := range 48 {
			for x := range 64 {
				got, _ := s.at(x, y)
				if want := gradient(64, 48).RGBAAt(x, y); !near(got, want, within) {
					t.Fatalf("with %d colours pixel %d,%d = %v, want near %v", colors, x, y, got, want)
				}
			}
		}
	}
}

func TestSixelIsOneDCSThatTakesNoCells(t *testing.T) {
	img := decode(t, encodePNG(t, gradient(30, 20)))
	seq := img.Sixel(30, 20, 64)
	if strings.Count(seq, "\x1b") != 2 {
		t.Errorf("sixel holds %d escapes, want only its DCS and ST", strings.Count(seq, "\x1b"))
	}
	if w := ansi.StringWidth(seq); w != 0 {
		t.Errorf("sixel is %d cells wide as text; a frame's width would count it", w)
	}
}

func TestSixelFitKeepsTheAspectRatioInWholeBands(t *testing.T) {
	tests := []struct {
		name                      string
		width, height, boxW, boxH int
		wantW, wantH              int
	}{
		{"wide image in a tall box", 400, 100, 200, 300, 192, 48},
		{"tall image in a wide box", 100, 400, 300, 200, 50, 198},
		{"small image scaled up", 10, 10, 120, 60, 60, 60},
		{"thinner than a band", 1000, 2, 100, 100, 100, 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w, h := imageview.SixelFit(tt.width, tt.height, tt.boxW, tt.boxH)
			if w != tt.wantW || h != tt.wantH {
				t.Errorf("SixelFit = %dx%d, want %dx%d", w, h, tt.wantW, tt.wantH)
			}
		})
	}
}

func TestDecodeForSixelKeepsALargerCopyToDrawAtScreenResolution(t *testing.T) {
	img, err := imageview.DecodeForSixel(bytes.NewReader(encodePNG(t, filled(3000, 1500, color.RGBA{255, 0, 0, 255}))), 2000)
	if err != nil {
		t.Fatal(err)
	}
	if got := imageview.Stored(img); got.Dx() != 2000 || got.Dy() != 1000 {
		t.Errorf("stored copy is %v, want 2000x1000", got)
	}
	if img.Width != 3000 || img.Height != 1500 || img.PNG != nil {
		t.Errorf("size %dx%d with a PNG of %d bytes, want 3000x1500 and no PNG", img.Width, img.Height, len(img.PNG))
	}
}
