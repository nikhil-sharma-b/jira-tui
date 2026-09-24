package imageview

import (
	"bytes"
	"fmt"
	"io"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// queryID is the ID the kitty support query asks about. No image is ever
// stored under it: a query only checks that one could be.
const queryID = 31

// xtsmgraphics asks how many sixel colour registers the terminal has.
const xtsmgraphics = "\x1b[?1;1;0S"

// Query asks the terminal what it can draw images with: whether it speaks
// kitty graphics, when kitty is set, and how many sixel colours it has, then
// for its primary device attributes. Every terminal answers the last, so its
// answer marks the end of what there is to read: a terminal answers the
// questions it understands first, and says nothing to the rest. The device
// attributes also say whether it draws sixel.
//
// Inside tmux, kitty must be false. tmux answers the device attributes itself
// at once, before the terminal's answer to a graphics query could arrive; the
// colour registers and the device attributes it answers itself, for the
// sixel it draws on the terminal's behalf.
func Query(kitty bool) string {
	q := xtsmgraphics + "\x1b[c"
	if kitty {
		q = fmt.Sprintf("\x1b_Gi=%d,s=1,v=1,a=q,t=d,f=24;AAAA\x1b\\", queryID) + q
	}
	return q
}

// Answers is what the terminal said to Query.
type Answers struct {
	// Kitty is the graphics query answered OK.
	Kitty bool
	// Sixel is a device attribute of 4, a terminal that draws sixel.
	Sixel bool
	// SixelColors is how many colour registers it has, zero when it did not
	// say.
	SixelColors int
}

var (
	deviceAttributes = regexp.MustCompile(`\x1b\[\?([0-9;]*)c$`)
	colorRegisters   = regexp.MustCompile(`\x1b\[\?1;0;([0-9]+)S`)
	kittyOK          = fmt.Appendf(nil, "\x1b_Gi=%d;OK\x1b\\", queryID)
)

// ReadAnswers reads the replies to Query, up to and including the device
// attributes. It reads a byte at a time so that nothing typed after the
// replies is taken. What was read before an error is still answered from.
func ReadAnswers(r io.Reader) (Answers, error) {
	var got []byte
	b := make([]byte, 1)
	var err error
	for !deviceAttributes.Match(got) {
		if _, err = io.ReadFull(r, b); err != nil {
			break
		}
		got = append(got, b[0])
	}
	a := Answers{Kitty: bytes.Contains(got, kittyOK)}
	if m := colorRegisters.FindSubmatch(got); m != nil {
		a.SixelColors, _ = strconv.Atoi(string(m[1]))
	}
	if m := deviceAttributes.FindSubmatch(got); m != nil {
		a.Sixel = slices.Contains(strings.Split(string(m[1]), ";"), "4")
	}
	return a, err
}
