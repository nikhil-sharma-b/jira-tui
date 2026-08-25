package ui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/nikhil-sharma-b/jira-tui/internal/config"
)

// One text-input widget serves both the commandline and in-pane search. They
// differ in the character they open with, in what completing a line means, and
// in whether there is a history behind it -- not in how typing works, which is
// why there is one of these rather than two.
//
// The widget never sees Esc. The dispatcher resolves it before any mode is
// consulted, so cancelling is the model closing the prompt rather than the
// prompt deciding to be closed.

// prompt is the one-line input at the bottom of the screen.
type prompt struct {
	// mode is what submitting the line means. It is carried here rather than
	// read back from the dispatcher because the widget outlives the keypress
	// that opened it and the dispatcher does not.
	mode Mode
	// sigil is the single character the line opens with, which is also how a
	// user tells the two modes apart at a glance.
	sigil string

	buf    []rune
	cursor int

	// history is the session's record of submitted lines, nil for a mode that
	// keeps none.
	history *history

	// complete produces whole candidate lines for the line as typed, nil for
	// a mode that completes nothing.
	complete func(string) []string
	// candidates is what the last completion offered when it could not choose
	// between them. The next keystroke clears it, so what is shown always
	// describes the line as it stands.
	candidates []string
}

// text is the line without its sigil: what submitting it will act on.
func (p *prompt) text() string { return string(p.buf) }

// handle applies one keypress, reporting whether the line was submitted.
func (p *prompt) handle(key string) (submitted bool) {
	if key != "tab" {
		p.candidates = nil
	}
	switch key {
	case "enter":
		return true
	case "tab":
		p.completeLine()
	case "backspace":
		if p.cursor > 0 {
			p.buf = append(p.buf[:p.cursor-1], p.buf[p.cursor:]...)
			p.cursor--
		}
	case "delete":
		if p.cursor < len(p.buf) {
			p.buf = append(p.buf[:p.cursor], p.buf[p.cursor+1:]...)
		}
	case "left":
		p.cursor = max(p.cursor-1, 0)
	case "right":
		p.cursor = min(p.cursor+1, len(p.buf))
	case "home":
		p.cursor = 0
	case "end":
		p.cursor = len(p.buf)
	case "up":
		p.walk(-1)
	case "down":
		p.walk(1)
	default:
		p.insert(key)
	}
	return false
}

// insert types a key literally, ignoring the ones that are not characters at
// all. A modified key reaching here is a chord with no meaning in text entry,
// and typing "ctrl+a" into a JQL is worse than ignoring it. Everything else
// printable is literal, which is what lets a pasted JQL -- one key carrying
// many runes -- type itself.
func (p *prompt) insert(key string) {
	// What is a key rather than a character is config's to say, because it is
	// config that spells them for the binding table.
	if config.IsEditingKey(key) || strings.Contains(key, "+") {
		return
	}
	runes := []rune(key)
	p.buf = append(p.buf[:p.cursor], append(runes, p.buf[p.cursor:]...)...)
	p.cursor += len(runes)
}

// walk recalls a line from the history, leaving the buffer alone when there is
// nothing in that direction to recall.
func (p *prompt) walk(delta int) {
	if p.history == nil {
		return
	}
	line, ok := p.history.walk(delta, p.buf)
	if !ok {
		return
	}
	p.buf, p.cursor = line, len(line)
}

// completeLine grows the line by as much as the candidates agree on. A single
// candidate is the whole answer; several are shown rather than guessed
// between, because choosing one of them silently is how a user ends up running
// a query they did not write.
func (p *prompt) completeLine() {
	if p.complete == nil {
		return
	}
	candidates := p.complete(p.text())
	if len(candidates) == 0 {
		return
	}
	p.setLine(commonPrefix(candidates))
	p.candidates = nil
	if len(candidates) > 1 {
		p.candidates = candidates
	}
}

func (p *prompt) setLine(line string) {
	p.buf = []rune(line)
	p.cursor = len(p.buf)
}

// render draws the line with a block cursor. The cursor is a styled cell
// rather than a character, so what is on screen is exactly the text that was
// typed and nothing that only looks like it.
func (p *prompt) render() string {
	runes := []rune(p.sigil + p.text())
	at := len([]rune(p.sigil)) + p.cursor
	if at >= len(runes) {
		return string(runes) + cursorStyle.Render(" ")
	}
	return string(runes[:at]) + cursorStyle.Render(string(runes[at])) + string(runes[at+1:])
}

// commonPrefix is the longest run every candidate starts with, which is how
// far a line can grow without choosing between them.
func commonPrefix(candidates []string) string {
	prefix := candidates[0]
	for _, c := range candidates[1:] {
		for !strings.HasPrefix(c, prefix) {
			prefix = prefix[:len(prefix)-1]
		}
	}
	return prefix
}

// history is the session's record of submitted lines, newest last. It is model
// state rather than a file: the ticket asks for recall within a session, and a
// query typed in one window is not obviously wanted in another.
type history struct {
	entries []string
	// at is where a walk stands, len(entries) when no walk is in progress.
	at int
	// draft is the line that was being typed when the walk started. Walking
	// back down past the newest entry restores it, so recalling something by
	// accident costs nothing.
	draft []rune
}

// add records a submitted line and ends any walk.
func (h *history) add(line string) {
	defer h.reset()
	if line == "" {
		return
	}
	// A line submitted twice running is one entry: walking back through the
	// same query several times is not what a history is for.
	if n := len(h.entries); n > 0 && h.entries[n-1] == line {
		return
	}
	h.entries = append(h.entries, line)
}

// reset ends a walk, which is also what opening a prompt does: the walk
// belongs to the line being typed, not to the session.
func (h *history) reset() {
	h.at = len(h.entries)
	h.draft = nil
}

// walk moves delta entries through the history, given the line being typed
// now. It reports the line to show, and false when there is nothing in that
// direction.
func (h *history) walk(delta int, current []rune) ([]rune, bool) {
	if len(h.entries) == 0 {
		return nil, false
	}
	if h.at >= len(h.entries) {
		h.draft = append([]rune(nil), current...)
	}
	at := min(max(h.at+delta, 0), len(h.entries))
	if at == h.at {
		return nil, false
	}
	h.at = at
	if at == len(h.entries) {
		return append([]rune(nil), h.draft...), true
	}
	return []rune(h.entries[at]), true
}

// cursorStyle marks the cell the next character lands in. Styling rather than
// a drawn character keeps the line readable to anything that strips styling.
var cursorStyle = lipgloss.NewStyle().Reverse(true)
