//go:build unix

package ui

import (
	"os"

	"golang.org/x/sys/unix"
)

// cellSize is the size of a cell in pixels, zero when the terminal does not
// say: the window's pixel size over its size in cells.
func cellSize() (width, height int) {
	ws, err := unix.IoctlGetWinsize(int(os.Stdout.Fd()), unix.TIOCGWINSZ)
	if err != nil || ws.Col == 0 || ws.Row == 0 {
		return 0, 0
	}
	return int(ws.Xpixel) / int(ws.Col), int(ws.Ypixel) / int(ws.Row)
}
