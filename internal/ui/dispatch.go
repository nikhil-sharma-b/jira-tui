package ui

import (
	"github.com/nikhil-sharma-b/jira-tui/internal/config"
)

// Dispatch turns a stream of keypresses into named actions. It is deliberately
// free of bubbletea: the whole modal model -- counts, multi-key sequences, the
// leader, and the two rules the tool rests on -- is decided here and tested by
// feeding keys, so no pane ever hardcodes a key.
//
// The two rules:
//
//   - Esc is resolved before the binding table is consulted, so no mode and no
//     binding can shadow it.
//   - A text-entry mode is entered only by an action that says so. Arriving
//     somewhere never starts typing.

// ResultKind says what a keypress amounted to.
type ResultKind int

const (
	// ResultNone: the key resolves to no binding. Any pending sequence or
	// count was abandoned with nothing fired.
	ResultNone ResultKind = iota
	// ResultPending: the key is the start of a sequence, or a count digit.
	// Nothing has fired yet.
	ResultPending
	// ResultAction: an action fired.
	ResultAction
	// ResultText: the key is literal input for the active text-entry mode.
	ResultText
)

func (k ResultKind) String() string {
	switch k {
	case ResultNone:
		return "none"
	case ResultPending:
		return "pending"
	case ResultAction:
		return "action"
	case ResultText:
		return "text"
	}
	return "unknown"
}

// Result is what one keypress produced.
type Result struct {
	Kind ResultKind

	// Action is set when Kind is ResultAction.
	Action config.Action

	// Count is the numeric prefix, zero when there was none or when the
	// action cannot use one.
	Count int

	// Key is the normalized keypress, set when Kind is ResultText.
	Key string

	// Mode is the mode in effect after this keypress.
	Mode Mode
}

// countable lists the actions a numeric prefix means something for. A count
// typed ahead of anything else is discarded rather than silently repeating an
// action that was never meant to repeat.
var countable = map[config.Action]bool{
	config.ActionDown:         true,
	config.ActionUp:           true,
	config.ActionHalfPageDown: true,
	config.ActionHalfPageUp:   true,
	config.ActionTop:          true,
	config.ActionBottom:       true,
	// H and L count lines in from the edge of the viewport, as in vim. M does
	// not appear here: the middle of the screen admits no count.
	config.ActionViewportTop: true,
	config.ActionViewportBot: true,
	config.ActionSearchNext:  true,
	config.ActionSearchPrev:  true,
	config.ActionJumpBack:    true,
	config.ActionJumpFwd:     true,
}

// entersMode lists the actions that change the modal state. Text entry
// appears here and nowhere else, which is the whole of the guarantee that it
// cannot be reached by navigation.
var entersMode = map[config.Action]Mode{
	config.ActionCommandline:  ModeCommand,
	config.ActionSearchInPane: ModeSearch,
	config.ActionTransition:   ModePicker,
	config.ActionAssign:       ModePicker,
}

// maxCount bounds a typed count. Held-down digits are a slip, not a request
// to scroll a hundred million rows.
const maxCount = 100000

// Dispatcher holds the modal state and the in-flight key sequence.
type Dispatcher struct {
	bindings *config.Bindings
	mode     Mode
	pending  []string
	count    int
	counting bool
}

// NewDispatcher builds a dispatcher over an already-compiled keymap. Compiling
// belongs to config load, where a collision can still be reported against the
// key that caused it.
func NewDispatcher(b *config.Bindings) *Dispatcher {
	return &Dispatcher{bindings: b, mode: ModeNormal}
}

// Mode is the current modal state.
func (d *Dispatcher) Mode() Mode { return d.mode }

// InText reports whether keys are currently literal text rather than commands.
func (d *Dispatcher) InText() bool { return d.mode.isText() }

// Count is the numeric prefix typed so far, zero when none is in flight.
func (d *Dispatcher) Count() int {
	if !d.counting {
		return 0
	}
	return d.count
}

// Pending renders the unfinished key sequence, for a which-key style
// indicator. Empty when no sequence is in flight.
func (d *Dispatcher) Pending() string {
	if len(d.pending) == 0 {
		return ""
	}
	return config.DisplayKeys(d.pending)
}

// Reset returns to normal mode and abandons any sequence or count. A
// text-entry widget calls it when its input is submitted or cancelled.
func (d *Dispatcher) Reset() {
	d.mode = ModeNormal
	d.clear()
}

func (d *Dispatcher) clear() {
	d.pending = nil
	d.count = 0
	d.counting = false
}

// Dispatch resolves one keypress.
func (d *Dispatcher) Dispatch(key string) Result {
	key = config.NormalizeKey(key)

	// Before the binding table, before the mode: Esc is the one key that
	// always means the same thing.
	if key == keyEsc {
		d.Reset()
		return Result{Kind: ResultAction, Action: config.ActionNormalMode, Mode: d.mode}
	}

	if d.mode.isText() {
		return Result{Kind: ResultText, Key: key, Mode: d.mode}
	}

	if d.isCountDigit(key) {
		d.addCountDigit(key)
		return Result{Kind: ResultPending, Mode: d.mode}
	}

	seq := append(append([]string(nil), d.pending...), key)

	if action, ok := d.bindings.Lookup(seq); ok {
		if action == config.ActionNormalMode {
			d.Reset()
			return Result{Kind: ResultAction, Action: action, Mode: d.mode}
		}
		count := 0
		if countable[action] {
			count = d.Count()
		}
		d.clear()
		if m, ok := entersMode[action]; ok {
			d.mode = m
		}
		return Result{Kind: ResultAction, Action: action, Count: count, Mode: d.mode}
	}

	if d.bindings.IsPrefix(seq) {
		d.pending = seq
		return Result{Kind: ResultPending, Mode: d.mode}
	}

	// Nothing matched: the sequence and the count go with it, so a half-typed
	// motion cannot leak into the next keypress.
	d.clear()
	return Result{Kind: ResultNone, Mode: d.mode}
}

// isCountDigit reports whether key extends or starts a count. Zero only ever
// extends one: on its own it is a motion key, not a count of nothing.
func (d *Dispatcher) isCountDigit(key string) bool {
	if len(key) != 1 || key[0] < '0' || key[0] > '9' {
		return false
	}
	if d.counting {
		return true
	}
	return len(d.pending) == 0 && key != "0"
}

func (d *Dispatcher) addCountDigit(key string) {
	d.counting = true
	if d.count <= maxCount {
		d.count = d.count*10 + int(key[0]-'0')
	}
	if d.count > maxCount {
		d.count = maxCount
	}
}

// keyEsc is the normalized spelling of Escape.
var keyEsc = config.NormalizeKey("esc")

// isText reports whether a mode is one where keys are literal input.
func (m Mode) isText() bool { return m == ModeCommand || m == ModeSearch }

func (m Mode) String() string {
	switch m {
	case ModeNormal:
		return "normal"
	case ModeCommand:
		return "command"
	case ModeSearch:
		return "search"
	case ModePicker:
		return "picker"
	}
	return "unknown"
}
