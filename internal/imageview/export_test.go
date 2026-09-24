package imageview

import "image"

// SetMaxPixels lowers the decode limit for the duration of a test, since
// crafting an image genuinely over it would cost the test the very memory the
// limit exists to protect.
func SetMaxPixels(n int) (restore func()) {
	before := maxPixels
	maxPixels = n
	return func() { maxPixels = before }
}

// Stored is the size of the copy an Image keeps for rendering.
func Stored(img *Image) image.Rectangle { return img.pixels.Bounds() }
