package imageview

import (
	"cmp"
	"fmt"
	"image"
	"slices"
	"strconv"
	"strings"
)

// Sixel draws the image as a sixel of exactly width x height pixels, in at
// most colors colours chosen from the image itself. It is one DCS sequence
// the terminal draws at the cursor; tmux, when built with sixel, keeps it in
// its own grid and draws it again after a redraw.
//
// Pixels with nothing drawn are left transparent (P2=1) rather than painted
// in the background colour, though every pixel is drawn: transparency only
// keeps a terminal from filling past the image to a whole band of six.
func (img *Image) Sixel(width, height, colors int) string {
	if width <= 0 || height <= 0 {
		return ""
	}
	grid := scale(img.pixels, width, height)
	palette, lookup := quantise(grid, max(colors, 2))

	var b strings.Builder
	fmt.Fprintf(&b, "\x1bP0;1q\"1;1;%d;%d", width, height)
	for i, c := range palette {
		fmt.Fprintf(&b, "#%d;2;%d;%d;%d", i, percent(c[0]), percent(c[1]), percent(c[2]))
	}
	// bits holds a band's six-pixel columns for every colour, a row of width
	// per colour, of which only the colours the band uses are touched.
	bits := make([]byte, len(palette)*width)
	seen := make([]bool, len(palette))
	var used []int
	for band := 0; band < height; band += 6 {
		used = used[:0]
		for dy := range min(6, height-band) {
			for x := range width {
				c := int(lookup[binAt(grid, x, band+dy)])
				if !seen[c] {
					seen[c] = true
					used = append(used, c)
				}
				bits[c*width+x] |= 1 << dy
			}
		}
		for i, c := range used {
			if i > 0 {
				// Back to the band's start for the next colour.
				b.WriteByte('$')
			}
			b.WriteString("#" + strconv.Itoa(c))
			row := bits[c*width : (c+1)*width]
			writeRuns(&b, row)
			clear(row)
			seen[c] = false
		}
		if band+6 < height {
			b.WriteByte('-')
		}
	}
	b.WriteString("\x1b\\")
	return b.String()
}

// writeRuns writes one colour's row of a band, a run of four or more alike as
// a repeat. The run of nothing that ends most rows is left out.
func writeRuns(b *strings.Builder, row []byte) {
	end := len(row)
	for end > 0 && row[end-1] == 0 {
		end--
	}
	for x := 0; x < end; {
		run := 1
		for x+run < end && row[x+run] == row[x] {
			run++
		}
		ch := '?' + row[x]
		if run > 3 {
			b.WriteString("!" + strconv.Itoa(run))
			b.WriteByte(ch)
		} else {
			for range run {
				b.WriteByte(ch)
			}
		}
		x += run
	}
}

// percent is a channel as sixel gives colours, 0-100.
func percent(v uint8) int { return (int(v)*100 + 127) / 255 }

// SixelFit is the size in pixels an image of width x height is drawn at to
// fill a box of boxWidth x boxHeight pixels, as large as fits with its aspect
// ratio kept. Its height is whole bands of six where it is a band or more, so
// a terminal that draws the last band whole draws nothing past the box.
func SixelFit(width, height, boxWidth, boxHeight int) (int, int) {
	if width <= 0 || height <= 0 || boxWidth <= 0 || boxHeight <= 0 {
		return 0, 0
	}
	var w, h int
	if boxWidth*height <= boxHeight*width {
		w, h = boxWidth, (height*boxWidth+width/2)/width
	} else {
		w, h = (width*boxHeight+height/2)/height, boxHeight
	}
	if h >= 6 && h%6 != 0 {
		h -= h % 6
		w = (width*h + height/2) / height
	}
	return max(w, 1), max(h, 1)
}

// bins is a colour cube of 32 levels a channel, which is as fine as the
// quantiser tells colours apart.
const bins = 32 * 32 * 32

// binAt is the pixel at x, y as its bin in the cube. The pixels are
// alpha-premultiplied, so dropping alpha is compositing over black, as
// half-blocks do.
func binAt(img *image.RGBA, x, y int) int {
	i := img.PixOffset(x, y)
	p := img.Pix[i : i+3 : i+3]
	return int(p[0]>>3)<<10 | int(p[1]>>3)<<5 | int(p[2]>>3)
}

// colourBox is a box of the cube median cut splits: the bins in it that the
// image uses, and how many pixels fall in them.
type colourBox struct {
	bins   []int
	pixels uint64
}

// quantise picks at most n colours for img by median cut, and says which of
// them every bin of the cube is drawn in. Each colour is the average of the
// pixels drawn in it, so a flat colour comes out as itself.
func quantise(img *image.RGBA, n int) (palette [][3]uint8, lookup []uint16) {
	count := make([]uint64, bins)
	sum := make([][3]uint64, bins)
	bounds := img.Bounds()
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			bin := binAt(img, x, y)
			i := img.PixOffset(x, y)
			count[bin]++
			sum[bin][0] += uint64(img.Pix[i])
			sum[bin][1] += uint64(img.Pix[i+1])
			sum[bin][2] += uint64(img.Pix[i+2])
		}
	}
	all := colourBox{}
	for bin, c := range count {
		if c > 0 {
			all.bins = append(all.bins, bin)
			all.pixels += c
		}
	}

	boxes := []colourBox{all}
	for len(boxes) < n {
		// Split the box whose colours are furthest apart, weighted by how
		// much of the image it covers.
		best, bestAxis, bestScore := -1, 0, uint64(0)
		for i, box := range boxes {
			if len(box.bins) < 2 {
				continue
			}
			axis, spread := widestAxis(box.bins)
			if score := uint64(spread) * box.pixels; best < 0 || score > bestScore {
				best, bestAxis, bestScore = i, axis, score
			}
		}
		if best < 0 {
			break
		}
		lower, upper := split(boxes[best], bestAxis, count)
		boxes[best] = lower
		boxes = append(boxes, upper)
	}

	palette = make([][3]uint8, len(boxes))
	lookup = make([]uint16, bins)
	for i, box := range boxes {
		var total [3]uint64
		for _, bin := range box.bins {
			total[0] += sum[bin][0]
			total[1] += sum[bin][1]
			total[2] += sum[bin][2]
			lookup[bin] = uint16(i)
		}
		for c := range 3 {
			palette[i][c] = uint8((total[c] + box.pixels/2) / box.pixels)
		}
	}
	return palette, lookup
}

// level is one channel of a bin, 0-31: 0 red, 1 green, 2 blue.
func level(bin, axis int) int { return bin >> (10 - 5*axis) & 31 }

// widestAxis is the channel the bins spread furthest along, and how far.
func widestAxis(bins []int) (axis, spread int) {
	for a := range 3 {
		lo, hi := 31, 0
		for _, bin := range bins {
			l := level(bin, a)
			lo, hi = min(lo, l), max(hi, l)
		}
		if hi-lo > spread || a == 0 {
			axis, spread = a, hi-lo
		}
	}
	return axis, spread
}

// split cuts a box along axis where half its pixels fall either side, leaving
// at least one bin in each half.
func split(box colourBox, axis int, count []uint64) (colourBox, colourBox) {
	slices.SortFunc(box.bins, func(a, b int) int { return cmp.Compare(level(a, axis), level(b, axis)) })
	var below uint64
	cut := 1
	for i, bin := range box.bins[:len(box.bins)-1] {
		below += count[bin]
		cut = i + 1
		if below*2 >= box.pixels {
			break
		}
	}
	lower := colourBox{bins: box.bins[:cut:cut], pixels: below}
	upper := colourBox{bins: box.bins[cut:], pixels: box.pixels - below}
	return lower, upper
}
