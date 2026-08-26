package ui

import (
	"strings"

	"github.com/nikhil-sharma-b/jira-tui/internal/config"
)

// The picker is how a choice is made out of a set the user cannot be expected
// to have memorised: it lists what is actually available and narrows as they
// type. It is deliberately not the commandline widget. A commandline submits
// text and completes it; a picker holds a set fetched from Jira, filters it
// locally, and submits the identity of one member -- the transition id rather
// than the status name that was displayed.
//
// Like the prompt, it never sees Esc: the dispatcher resolves that before any
// mode is consulted, so closing is the model's decision, not the widget's.

// pickerItem is one choice. id is what acting on it sends; label is what is
// read and what filtering matches against.
type pickerItem struct {
	id    string
	label string
}

// picker is the filterable list at the bottom of the screen.
type picker struct {
	// title says what is being chosen and for which work item, since a picker
	// opened from the list is about a row that may have scrolled away.
	title string
	// key is the work item the choices belong to. A response for a different
	// one is a response to a question no longer being asked.
	key string

	loading bool
	items   []pickerItem

	filter []rune
	cursor int
	// matches indexes items, in order, and is what the cursor moves over.
	matches []int
}

// pickerRows bounds how much of the screen a picker may take. Enough to see a
// workflow's worth of transitions; not so much that the pane behind it stops
// being readable, which is the whole reason this is a strip and not a modal.
const pickerRows = 8

func (p *picker) setItems(items []pickerItem) {
	p.loading = false
	p.items = items
	p.refilter()
}

// refilter recomputes the matches and keeps the cursor inside them. The filter
// is a case-insensitive substring: a user typing "prog" is describing what they
// remember of a name, not writing a pattern.
func (p *picker) refilter() {
	needle := strings.ToLower(string(p.filter))
	p.matches = p.matches[:0]
	for i, item := range p.items {
		if needle == "" || strings.Contains(strings.ToLower(item.label), needle) {
			p.matches = append(p.matches, i)
		}
	}
	p.cursor = min(max(p.cursor, 0), max(len(p.matches)-1, 0))
}

// selected is the item Enter would act on, and false when nothing matches what
// has been typed.
func (p *picker) selected() (pickerItem, bool) {
	if p.cursor < 0 || p.cursor >= len(p.matches) {
		return pickerItem{}, false
	}
	return p.items[p.matches[p.cursor]], true
}

// handle applies one keypress, reporting whether a choice was submitted.
func (p *picker) handle(key string) (submitted bool) {
	switch key {
	case "enter":
		// Enter means "this one". While the choices are still being fetched, or
		// while the filter matches none of them, there is no such one -- and
		// closing on it would make Enter a second, silent cancel. Esc is the
		// only way out that changes nothing.
		_, ok := p.selected()
		return ok
	case "up", "ctrl+p":
		p.move(-1)
	case "down", "ctrl+n":
		p.move(1)
	case "backspace":
		if n := len(p.filter); n > 0 {
			p.filter = p.filter[:n-1]
			p.refilter()
		}
	default:
		p.insert(key)
	}
	return false
}

func (p *picker) move(delta int) {
	if len(p.matches) == 0 {
		return
	}
	p.cursor = min(max(p.cursor+delta, 0), len(p.matches)-1)
}

// insert types a key into the filter, ignoring anything that is a key rather
// than a character -- the same rule the commandline uses, and for the same
// reason: a chord with no meaning here should do nothing rather than be typed.
func (p *picker) insert(key string) {
	if config.IsEditingKey(key) || strings.Contains(key, "+") {
		return
	}
	p.filter = append(p.filter, []rune(key)...)
	p.refilter()
}

// lines renders the picker: what is being chosen, the choices that still
// match, and the filter as typed.
func (p *picker) lines(width int) []string {
	lines := []string{statusStyle.Render(fitWidth(p.title, width))}
	switch {
	case p.loading:
		lines = append(lines, fitWidth("Loading transitions…", width))
	case len(p.matches) == 0:
		lines = append(lines, fitWidth("Nothing matches.", width))
	default:
		for row, index := range p.visible() {
			text := strings.Repeat(" ", gutterWidth) + p.items[index].label
			style := plainStyle
			if p.top()+row == p.cursor {
				text = SelectedMarker + p.items[index].label
				style = selectedStyle
			}
			lines = append(lines, style.Render(fitWidth(text, width)))
		}
	}
	return append(lines, fitWidth(">"+string(p.filter), width)+cursorStyle.Render(" "))
}

// visible is the window of matches on screen, scrolled to keep the cursor in
// it, so a filter that matches more than fits is still navigable.
func (p *picker) visible() []int {
	top := p.top()
	return p.matches[top:min(top+pickerRows, len(p.matches))]
}

func (p *picker) top() int {
	if p.cursor < pickerRows {
		return 0
	}
	return p.cursor - pickerRows + 1
}
