//go:build !unix

package ui

// cellSize is unknown where the terminal has no way to say, which the fit
// takes as a cell twice as tall as wide.
func cellSize() (width, height int) { return 0, 0 }
