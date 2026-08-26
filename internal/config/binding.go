package config

import (
	"fmt"
	"slices"
	"sort"
	"strings"
)

// A key token is one keypress, spelled the way a terminal UI reports it:
// a single rune ("j", " "), or a name ("enter", "esc"), or a modified key
// ("ctrl+w"). A binding is a sequence of them, and dispatch is a walk over
// those sequences -- so tokenizing is the one place key syntax is understood.

// tokenSep joins tokens into a map key. It cannot occur in a token, so two
// different sequences can never collapse onto the same lookup key.
const tokenSep = "\x00"

// leaderRef is the placeholder a binding uses to mean "whatever leader is
// configured", so that changing leader changes every action behind it.
const leaderRef = "<leader>"

// namedKeys are the keys that have no printable form, with the spellings a
// user might reasonably write for each.
var namedKeys = map[string]string{
	"enter":     "enter",
	"return":    "enter",
	"cr":        "enter",
	"esc":       "esc",
	"escape":    "esc",
	"tab":       "tab",
	"space":     " ",
	"backspace": "backspace",
	"delete":    "delete",
	"up":        "up",
	"down":      "down",
	"left":      "left",
	"right":     "right",
	"home":      "home",
	"end":       "end",
	"pgup":      "pgup",
	"pgdown":    "pgdown",
	"insert":    "insert",
}

// displayNames spell a token back for the help overlay, where an invisible
// key must still be readable.
var displayNames = map[string]string{
	" ": "<space>",
}

// isSingleKey reports whether a whole space-separated part names one key
// rather than a run of characters. Only a whole part is matched, so the
// binding "enter" is the Enter key while "gg" is still two presses of g.
func isSingleKey(part string) bool {
	if _, ok := namedKeys[strings.ToLower(part)]; ok {
		return true
	}
	return strings.Contains(part, "+")
}

// editingKeys are the normalized keys that name an edit or a motion rather
// than a character. Text entry needs the same vocabulary the binding table
// has, and this is derived from it so that there is still one place key syntax
// is understood.
var editingKeys = func() map[string]bool {
	out := make(map[string]bool, len(namedKeys))
	for _, normalized := range namedKeys {
		if normalized == " " {
			// Space is a character wherever it is typed. It has a name only so
			// that a binding can be written for it.
			continue
		}
		out[normalized] = true
	}
	return out
}()

// IsEditingKey reports whether a normalized key means an edit rather than a
// character, which is what a text-entry widget has to know before it types
// what it was sent.
func IsEditingKey(key string) bool { return editingKeys[key] }

// NormalizeKey puts a key into the one spelling the binding table uses, so a
// key arriving from the terminal and a key written in config compare equal.
func NormalizeKey(key string) string {
	lower := strings.ToLower(key)
	if named, ok := namedKeys[lower]; ok {
		return named
	}
	if strings.Contains(key, "+") {
		return lower
	}
	return key
}

// Bindings is a compiled keymap: the thing dispatch actually reads. It is
// built once at config load, which is also where a collision is a config
// error rather than a key that mysteriously does nothing.
type Bindings struct {
	byKeys   map[string]Action
	prefixes map[string]bool
	byAction map[Action][]string
	leader   []string
}

// Compile turns a keymap into a lookup table, expanding <leader> and
// rejecting any pair of bindings that cannot both be reachable.
func Compile(km Keymap, leader string) (*Bindings, error) {
	// A leader that cannot be a key is wrong whether or not a binding
	// happens to use it, and it is the "leader" key the user must fix.
	leaderTokens, err := tokenizeLeader(leader)
	if err != nil && leader != "" {
		return nil, &Error{Key: "leader", Msg: err.Error()}
	}

	b := &Bindings{
		byKeys:   make(map[string]Action, len(km)),
		prefixes: make(map[string]bool),
		byAction: make(map[Action][]string, len(km)),
		leader:   leaderTokens,
	}

	// Sorted so that a collision between two bindings is reported the same
	// way on every run rather than by map order.
	for _, a := range sortedActions(km) {
		tokens, err := tokenize(km[a], leaderTokens)
		if err != nil {
			if err == errNoLeader {
				return nil, &Error{Key: "leader", Msg: fmt.Sprintf("unset, but %s uses %s", configKey(a), leaderRef)}
			}
			return nil, &Error{Key: configKey(a), Msg: err.Error()}
		}
		if len(tokens) == 0 {
			continue
		}
		if reason := reserved(tokens, a); reason != "" {
			return nil, &Error{Key: configKey(a), Msg: reason}
		}
		key := strings.Join(tokens, tokenSep)

		if other, ok := b.byKeys[key]; ok {
			return nil, collision(a, other, km[a], "is already bound to")
		}
		if other, ok := b.prefixAction(tokens); ok {
			return nil, collision(a, other, km[a], "starts with the binding for")
		}
		if other, ok := b.extendedBy(key); ok {
			return nil, collision(a, other, km[a], "is the start of the binding for")
		}

		b.byKeys[key] = a
		b.byAction[a] = tokens
		for i := 1; i < len(tokens); i++ {
			b.prefixes[strings.Join(tokens[:i], tokenSep)] = true
		}
	}
	return b, nil
}

// reserved reports why a binding can never fire, for the two keys dispatch
// resolves before it ever reads the table. Without this they compile happily
// into a key that silently does nothing, which is exactly what reporting
// collisions at load exists to prevent.
func reserved(tokens []string, a Action) string {
	// Esc is resolved ahead of the table wherever it appears, so the only
	// binding it can be part of is the whole of the one it already performs.
	// Anything else compiles into keys that can never be reached.
	if slices.Contains(tokens, "esc") {
		if len(tokens) > 1 {
			return "esc always returns to normal mode; it cannot appear in a sequence"
		}
		if a != ActionNormalMode {
			return fmt.Sprintf("esc always returns to normal mode; it cannot be bound to %q", string(a))
		}
	}
	if len(tokens[0]) == 1 && tokens[0][0] >= '1' && tokens[0][0] <= '9' {
		return fmt.Sprintf("%q starts a count, so a binding cannot begin with it", tokens[0])
	}
	return ""
}

// collision names both actions, because the user's next move is to decide
// which of the two they meant to rebind.
func collision(a, other Action, binding, relation string) error {
	return &Error{
		Key: configKey(a),
		Msg: fmt.Sprintf("%q %s %q", binding, relation, collisionLabel(other)),
	}
}

// configKey is the config key a binding was written under, so an error names
// the line the user must edit rather than the action it compiled to.
func configKey(a Action) string {
	if name, ok := a.TransitionName(); ok {
		return "transitions." + name
	}
	return "keys." + string(a)
}

// collisionLabel spells the action a binding collided with, the way the other
// end of a collision reads best: an action by its name, a direct transition by
// the transition it applies.
func collisionLabel(a Action) string {
	if name, ok := a.TransitionName(); ok {
		return "the " + name + " transition"
	}
	return string(a)
}

func sortedActions(km Keymap) []Action {
	out := make([]Action, 0, len(km))
	for a := range km {
		out = append(out, a)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

// prefixAction reports an already-compiled binding that tokens begins with.
// Such a binding would fire first and the longer one could never be reached.
func (b *Bindings) prefixAction(tokens []string) (Action, bool) {
	for i := 1; i < len(tokens); i++ {
		if a, ok := b.byKeys[strings.Join(tokens[:i], tokenSep)]; ok {
			return a, true
		}
	}
	return "", false
}

// extendedBy reports an already-compiled binding that key is a prefix of.
func (b *Bindings) extendedBy(key string) (Action, bool) {
	for existing, a := range b.byKeys {
		if strings.HasPrefix(existing, key+tokenSep) {
			return a, true
		}
	}
	return "", false
}

// Lookup resolves a complete key sequence to its action.
func (b *Bindings) Lookup(tokens []string) (Action, bool) {
	a, ok := b.byKeys[strings.Join(tokens, tokenSep)]
	return a, ok
}

// IsPrefix reports whether tokens is the unfinished start of some binding,
// which is what tells dispatch to wait for another key rather than give up.
func (b *Bindings) IsPrefix(tokens []string) bool {
	return b.prefixes[strings.Join(tokens, tokenSep)]
}

// Display spells an action's binding for the help overlay.
func (b *Bindings) Display(a Action) (string, bool) {
	tokens, ok := b.byAction[a]
	if !ok {
		return "", false
	}
	return DisplayKeys(tokens), true
}

// DisplayKeys spells a token sequence the way vim documentation would: keys
// that are single presses run together ("gg"), anything with a name stays
// separated ("ctrl+w h").
func DisplayKeys(tokens []string) string {
	sep := ""
	for _, t := range tokens {
		if len([]rune(t)) != 1 {
			sep = " "
			break
		}
	}
	parts := make([]string, len(tokens))
	for i, t := range tokens {
		if name, ok := displayNames[t]; ok {
			parts[i] = name
			continue
		}
		parts[i] = t
	}
	return strings.Join(parts, sep)
}

// Actions lists every bound action, sorted, for callers that render the
// effective keymap rather than a hand-maintained list of it.
func (b *Bindings) Actions() []Action {
	out := make([]Action, 0, len(b.byAction))
	for a := range b.byAction {
		out = append(out, a)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

// errNoLeader marks a binding that needs a leader when none is configured.
var errNoLeader = fmt.Errorf("no leader")

// tokenize splits a binding into key tokens. Parts are separated by spaces;
// a part that names a key or carries a modifier is one token, and any other
// part is one token per rune, which is what makes "gg" two presses.
func tokenize(binding string, leader []string) ([]string, error) {
	var tokens []string
	for _, part := range strings.Fields(binding) {
		for strings.HasPrefix(part, leaderRef) {
			if len(leader) == 0 {
				return nil, errNoLeader
			}
			tokens = append(tokens, leader...)
			part = strings.TrimPrefix(part, leaderRef)
		}
		if part == "" {
			continue
		}
		if isSingleKey(part) {
			tokens = append(tokens, NormalizeKey(part))
			continue
		}
		for _, r := range part {
			tokens = append(tokens, string(r))
		}
	}
	if len(tokens) == 0 && strings.TrimSpace(binding) != "" {
		return nil, fmt.Errorf("%q is not a key sequence", binding)
	}
	return tokens, nil
}

// tokenizeLeader resolves the configured leader to exactly one key. A
// multi-key leader would make every action binding a three-key sequence and
// is far more likely to be a typo than an intention.
func tokenizeLeader(leader string) ([]string, error) {
	if leader == "" {
		return nil, fmt.Errorf("leader is unset but a binding uses %s", leaderRef)
	}
	if strings.TrimSpace(leader) == "" {
		return []string{" "}, nil
	}
	tokens, err := tokenize(leader, nil)
	if err != nil {
		return nil, err
	}
	if len(tokens) != 1 {
		return nil, fmt.Errorf("%q is %d keys; leader must be a single key", leader, len(tokens))
	}
	return tokens, nil
}
