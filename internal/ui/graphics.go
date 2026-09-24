package ui

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/charmbracelet/x/term"
	"github.com/nikhil-sharma-b/jira-tui/internal/config"
	"github.com/nikhil-sharma-b/jira-tui/internal/imageview"
)

// Terminal is what startup learned about the terminal's graphics. It is
// learned once, before bubbletea owns the terminal's input: the answer to a
// query arrives as input, and once the UI is up it would be read as keys.
type Terminal struct {
	// Kitty is a terminal that speaks the kitty graphics protocol.
	Kitty bool
	// Tmux is jt running inside tmux, and Passthrough tmux's
	// allow-passthrough being on, without which tmux drops every graphics
	// command on the floor.
	Tmux, Passthrough bool
}

// rendererKind is how the preview draws an image.
type rendererKind int

const (
	drawHalfBlocks rendererKind = iota
	drawKitty
)

// renderer is the images setting resolved against the terminal.
type renderer struct {
	kind rendererKind
	// note says why auto settled for half-blocks when the terminal could do
	// better, shown on the overlay so the fix is one line away.
	note string
	// err is why an explicit setting cannot work, reported on p rather than
	// quietly drawing something else.
	err error
}

// graphicsSide bounds the PNG sent to a kitty terminal. It is about a large
// screen's width, so a fullscreen preview is drawn at close to the screen's
// resolution, while the transfer through tmux stays a moment.
const graphicsSide = 2560

var errNoPassthrough = errors.New("tmux allow-passthrough is off; " +
	"`tmux set -g allow-passthrough on` lets images through")

// chooseRenderer resolves the images setting. An explicit kitty is trusted
// even when the terminal did not answer for it -- the user may know better
// than a query -- except inside a tmux that would drop every command.
func chooseRenderer(setting string, t Terminal) renderer {
	switch setting {
	case config.ImagesKitty:
		if t.Tmux && !t.Passthrough {
			return renderer{kind: drawKitty, err: fmt.Errorf("images = %q: %w", config.ImagesKitty, errNoPassthrough)}
		}
		return renderer{kind: drawKitty}
	case config.ImagesAuto:
		switch {
		case !t.Kitty:
		case t.Tmux && !t.Passthrough:
			return renderer{note: "half-blocks: " + errNoPassthrough.Error()}
		default:
			return renderer{kind: drawKitty}
		}
	}
	return renderer{}
}

// probeTerminal finds out what the images setting needs to know, and nothing
// when the setting does not depend on the terminal.
//
// Inside tmux, tmux itself says whether passthrough is on and what terminal
// the client is. The terminal is not queried there: tmux answers the device
// attributes itself, at once, so the terminal's own answer to the graphics
// query would arrive after the probe had stopped reading, as keys.
func probeTerminal(setting string) Terminal {
	if setting != config.ImagesAuto && setting != config.ImagesKitty {
		return Terminal{}
	}
	if os.Getenv("TMUX") == "" {
		return Terminal{Kitty: setting == config.ImagesAuto && queryKitty() && drawsPlaceholders(os.Getenv)}
	}
	t := Terminal{Tmux: true}
	args := []string{"display-message", "-p"}
	if pane := os.Getenv("TMUX_PANE"); pane != "" {
		args = append(args, "-t", pane)
	}
	out, err := exec.Command("tmux", append(args, "#{allow-passthrough}\t#{client_termname}\t#{client_termtype}")...).Output()
	if err != nil {
		// tmux would not say. An explicit kitty is then tried rather than
		// refused over a passthrough that may well be on; auto knows no
		// terminal name, so draws half-blocks either way.
		t.Passthrough = true
		return t
	}
	fields := strings.Split(strings.TrimSpace(string(out)), "\t")
	t.Passthrough = fields[0] == "on" || fields[0] == "all"
	for _, name := range fields[1:] {
		t.Kitty = t.Kitty || kittyTerminal(name)
	}
	return t
}

// drawsPlaceholders rules out the terminals known to answer the graphics
// query but not to draw Unicode placeholders, which would leave the overlay
// blank.
func drawsPlaceholders(getenv func(string) string) bool {
	return getenv("TERM_PROGRAM") != "WezTerm" && getenv("KONSOLE_VERSION") == ""
}

// kittyTerminal is a terminal name, as TERM or as it reports its version,
// of one that speaks kitty graphics with Unicode placeholders.
func kittyTerminal(name string) bool {
	name = strings.ToLower(name)
	return strings.Contains(name, "kitty") || strings.Contains(name, "ghostty")
}

// probeTimeout is how long a terminal has to answer. Every terminal answers
// the device attributes at once; this only bounds one that never does.
const probeTimeout = time.Second

// queryKitty asks the terminal whether it speaks kitty graphics, reading the
// answer from the controlling terminal in raw mode so it is not echoed and
// arrives without waiting for Enter.
func queryKitty() bool {
	tty, err := os.OpenFile("/dev/tty", os.O_RDWR, 0)
	if err != nil {
		return false
	}
	defer tty.Close()
	// Without a deadline a terminal that never answers would hang startup.
	if tty.SetReadDeadline(time.Now().Add(probeTimeout)) != nil {
		return false
	}
	// The descriptor is borrowed through SyscallConn, not Fd: Fd puts the
	// file back in blocking mode, and the deadline with it out of action.
	conn, err := tty.SyscallConn()
	if err != nil {
		return false
	}
	var state *term.State
	if err := conn.Control(func(fd uintptr) { state, err = term.MakeRaw(fd) }); err != nil || state == nil {
		return false
	}
	defer conn.Control(func(fd uintptr) { term.Restore(fd, state) })
	if _, err := tty.WriteString(imageview.KittyQuery()); err != nil {
		return false
	}
	ok, _ := imageview.AnswersKitty(tty)
	return ok
}

// output is the terminal bubbletea draws on, shared with the graphics
// commands the preview sends outside the frame. Writes take turns, so a
// command never lands in the middle of a frame or a frame in the middle of a
// command. It is still the *os.File underneath, which is how bubbletea finds
// the terminal's size and puts it in raw mode.
type output struct {
	*os.File
	mu sync.Mutex
}

func (o *output) Write(b []byte) (int, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.File.Write(b)
}

// graphicsQueue sends graphics commands in the order they were queued, from
// a goroutine of its own: an image is megabytes through tmux, and sending it
// from the update loop would freeze the screen until it was through. Order is
// what matters -- a delete must never overtake the image it deletes.
type graphicsQueue struct {
	mu     sync.Mutex
	wake   *sync.Cond
	queue  []string
	closed bool
	done   chan struct{}
}

func newGraphicsQueue(w io.Writer) *graphicsQueue {
	q := &graphicsQueue{done: make(chan struct{})}
	q.wake = sync.NewCond(&q.mu)
	go q.run(w)
	return q
}

// send queues one command, never waiting on the terminal.
func (q *graphicsQueue) send(seq string) {
	q.mu.Lock()
	q.queue = append(q.queue, seq)
	q.mu.Unlock()
	q.wake.Signal()
}

func (q *graphicsQueue) run(w io.Writer) {
	defer close(q.done)
	for {
		q.mu.Lock()
		for len(q.queue) == 0 && !q.closed {
			q.wake.Wait()
		}
		batch := q.queue
		q.queue = nil
		q.mu.Unlock()
		if len(batch) == 0 {
			return
		}
		for _, seq := range batch {
			io.WriteString(w, seq)
		}
	}
}

// close sends what is queued and returns once it is sent.
func (q *graphicsQueue) close() {
	q.mu.Lock()
	q.closed = true
	q.mu.Unlock()
	q.wake.Signal()
	<-q.done
}
