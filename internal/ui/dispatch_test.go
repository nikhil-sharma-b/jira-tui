package ui_test

import (
	"strings"
	"testing"

	"github.com/nikhil-sharma-b/jira-tui/internal/config"
	"github.com/nikhil-sharma-b/jira-tui/internal/ui"
)

// compile builds the effective bindings from the defaults with any overrides
// merged in, so a test names only the bindings it cares about.
func compile(t *testing.T, leader string, overrides map[string]string) *config.Bindings {
	t.Helper()
	b, err := config.Compile(config.DefaultKeymap().Merge(overrides), leader)
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	return b
}

func dispatcher(t *testing.T, leader string, overrides map[string]string) *ui.Dispatcher {
	t.Helper()
	return ui.NewDispatcher(compile(t, leader, overrides))
}

// feed sends every key and returns the result of the last one, which is the
// only one a sequence test is asserting about.
func feed(d *ui.Dispatcher, keys ...string) ui.Result {
	var r ui.Result
	for _, k := range keys {
		r = d.Dispatch(k)
	}
	return r
}

// assertAction fails unless the result is the named action with the wanted count.
func assertAction(t *testing.T, r ui.Result, want config.Action, count int) {
	t.Helper()
	if r.Kind != ui.ResultAction {
		t.Fatalf("kind = %v, want an action (%q)", r.Kind, want)
	}
	if r.Action != want {
		t.Fatalf("action = %q, want %q", r.Action, want)
	}
	if r.Count != count {
		t.Errorf("count = %d, want %d", r.Count, count)
	}
}

func TestSingleKeyResolvesToAnAction(t *testing.T) {
	d := dispatcher(t, " ", nil)
	assertAction(t, d.Dispatch("j"), config.ActionDown, 0)
}

func TestUnboundKeyResolvesToNoBinding(t *testing.T) {
	d := dispatcher(t, " ", nil)
	if got := d.Dispatch("z").Kind; got != ui.ResultNone {
		t.Errorf("kind = %v, want ResultNone", got)
	}
}

func TestIncompleteSequenceIsPending(t *testing.T) {
	d := dispatcher(t, " ", nil)

	r := d.Dispatch("g")
	if r.Kind != ui.ResultPending {
		t.Fatalf("kind after g = %v, want ResultPending", r.Kind)
	}
	if got := d.Pending(); got != "g" {
		t.Errorf("Pending() = %q, want %q", got, "g")
	}
	assertAction(t, d.Dispatch("g"), config.ActionTop, 0)
	if got := d.Pending(); got != "" {
		t.Errorf("Pending() = %q after the sequence completed, want empty", got)
	}
}

func TestMultiKeySequences(t *testing.T) {
	cases := []struct {
		keys []string
		want config.Action
	}{
		{[]string{"g", "g"}, config.ActionTop},
		{[]string{"g", "l"}, config.ActionGoList},
		{[]string{"g", "d"}, config.ActionGoDetail},
		{[]string{"ctrl+w", "h"}, config.ActionPaneLeft},
		{[]string{"ctrl+w", "l"}, config.ActionPaneRight},
		{[]string{"ctrl+w", "o"}, config.ActionPaneZoom},
	}
	for _, tc := range cases {
		d := dispatcher(t, " ", nil)
		assertAction(t, feed(d, tc.keys...), tc.want, 0)
	}
}

func TestNonMatchingKeyAbandonsThePendingSequence(t *testing.T) {
	d := dispatcher(t, " ", nil)

	d.Dispatch("g")
	r := d.Dispatch("z")
	if r.Kind != ui.ResultNone {
		t.Fatalf("kind = %v, want ResultNone: gz is not a binding", r.Kind)
	}
	if got := d.Pending(); got != "" {
		t.Errorf("Pending() = %q, want the sequence abandoned", got)
	}
	// The abandoned g must not leak into the next keypress.
	assertAction(t, d.Dispatch("j"), config.ActionDown, 0)
}

func TestCountIsDeliveredWithTheMotion(t *testing.T) {
	d := dispatcher(t, " ", nil)
	assertAction(t, feed(d, "5", "j"), config.ActionDown, 5)
}

func TestMultiDigitCount(t *testing.T) {
	d := dispatcher(t, " ", nil)
	assertAction(t, feed(d, "1", "2", "k"), config.ActionUp, 12)
}

func TestCountPrefixesAMultiKeySequence(t *testing.T) {
	d := dispatcher(t, " ", nil)
	assertAction(t, feed(d, "3", "g", "g"), config.ActionTop, 3)
}

func TestZeroDoesNotStartACount(t *testing.T) {
	d := dispatcher(t, " ", nil)

	if got := d.Dispatch("0").Kind; got != ui.ResultNone {
		t.Errorf("kind for a leading 0 = %v, want ResultNone: 0 is a motion key, not a count", got)
	}
	assertAction(t, feed(d, "1", "0", "j"), config.ActionDown, 10)
}

func TestCountWithNoFollowingMotionIsDiscarded(t *testing.T) {
	d := dispatcher(t, " ", nil)

	feed(d, "5")
	if got := d.Count(); got != 5 {
		t.Fatalf("Count() = %d while typing, want 5", got)
	}
	if got := d.Dispatch("z").Kind; got != ui.ResultNone {
		t.Fatalf("5z fired something, want ResultNone")
	}
	if got := d.Count(); got != 0 {
		t.Errorf("Count() = %d after an abandoned count, want 0", got)
	}
	assertAction(t, d.Dispatch("j"), config.ActionDown, 0)
}

func TestCountIsDroppedForActionsThatCannotUseOne(t *testing.T) {
	d := dispatcher(t, " ", nil)
	assertAction(t, feed(d, "5", "?"), config.ActionHelp, 0)
}

func TestEscDiscardsAPendingCount(t *testing.T) {
	d := dispatcher(t, " ", nil)

	feed(d, "5")
	assertAction(t, d.Dispatch("esc"), config.ActionNormalMode, 0)
	if got := d.Count(); got != 0 {
		t.Errorf("Count() = %d after Esc, want 0", got)
	}
}

func TestLeaderBindings(t *testing.T) {
	d := dispatcher(t, " ", nil)
	assertAction(t, feed(d, " ", "c"), config.ActionComment, 0)
	assertAction(t, feed(d, " ", "y"), config.ActionYankKey, 0)
	assertAction(t, feed(d, " ", "Y"), config.ActionYankURL, 0)
}

func TestChangingTheLeaderMovesEveryLeaderBinding(t *testing.T) {
	d := dispatcher(t, ",", nil)

	assertAction(t, feed(d, ",", "c"), config.ActionComment, 0)
	assertAction(t, feed(d, ",", "t"), config.ActionTransition, 0)
	if got := feed(d, " ", "c").Kind; got == ui.ResultAction {
		t.Errorf("space still fires a leader binding after the leader became a comma")
	}
}

func TestRebindingOneKeyKeepsTheRest(t *testing.T) {
	d := dispatcher(t, " ", map[string]string{string(config.ActionDown): "e"})

	assertAction(t, d.Dispatch("e"), config.ActionDown, 0)
	assertAction(t, d.Dispatch("k"), config.ActionUp, 0)
	if got := d.Dispatch("j").Kind; got != ui.ResultNone {
		t.Errorf("j still fires after down was rebound to e")
	}
}

func TestUnboundActionResolvesToNoBinding(t *testing.T) {
	d := dispatcher(t, " ", map[string]string{string(config.ActionHelp): ""})

	if got := d.Dispatch("?").Kind; got != ui.ResultNone {
		t.Errorf("kind = %v after help was unbound, want ResultNone", got)
	}
}

func TestEscReturnsToNormalFromEveryMode(t *testing.T) {
	modes := []struct {
		name string
		keys []string
		want ui.Mode
	}{
		{"command", []string{":"}, ui.ModeCommand},
		{"search", []string{"/"}, ui.ModeSearch},
		{"picker", []string{" ", "t"}, ui.ModePicker},
	}
	for _, m := range modes {
		t.Run(m.name, func(t *testing.T) {
			d := dispatcher(t, " ", nil)
			feed(d, m.keys...)
			if got := d.Mode(); got != m.want {
				t.Fatalf("mode = %v, want %v", got, m.want)
			}
			assertAction(t, d.Dispatch("esc"), config.ActionNormalMode, 0)
			if got := d.Mode(); got != ui.ModeNormal {
				t.Errorf("mode after Esc = %v, want ModeNormal", got)
			}
		})
	}
}

func TestEscIsNeverConsumedByABinding(t *testing.T) {
	// A user who unbinds normal_mode still gets Esc back: it is the one key
	// the modal model cannot lose. (Binding esc to anything else is refused
	// at config load; this is the belt to that pair of braces.)
	d := dispatcher(t, " ", map[string]string{
		string(config.ActionNormalMode): "",
	})
	assertAction(t, d.Dispatch("esc"), config.ActionNormalMode, 0)
	if got := d.Mode(); got != ui.ModeNormal {
		t.Errorf("mode = %v, want ModeNormal", got)
	}
}

func TestRebindingNormalModeStillLeavesTheMode(t *testing.T) {
	// A text-entry mode is deliberately not exitable this way: there, every
	// key but Esc is literal input.
	d := dispatcher(t, " ", map[string]string{
		string(config.ActionNormalMode): "z",
	})
	feed(d, " ", "t")
	if got := d.Mode(); got != ui.ModePicker {
		t.Fatalf("mode = %v, want ModePicker", got)
	}
	assertAction(t, d.Dispatch("z"), config.ActionNormalMode, 0)
	if got := d.Mode(); got != ui.ModeNormal {
		t.Errorf("mode = %v, want ModeNormal", got)
	}
}

func TestResetLeavesTextMode(t *testing.T) {
	// What a commandline calls when its input is submitted rather than
	// cancelled: there is no keypress to resolve, but the mode must end.
	d := dispatcher(t, " ", nil)
	feed(d, ":")

	d.Reset()
	if d.InText() {
		t.Errorf("mode = %v after Reset, want normal", d.Mode())
	}
	assertAction(t, d.Dispatch("j"), config.ActionDown, 0)
}

func TestEscAbandonsAPendingSequence(t *testing.T) {
	d := dispatcher(t, " ", nil)

	d.Dispatch("ctrl+w")
	assertAction(t, d.Dispatch("esc"), config.ActionNormalMode, 0)
	if got := d.Pending(); got != "" {
		t.Errorf("Pending() = %q after Esc, want empty", got)
	}
}

func TestTextEntryIsOnlyReachedByAnExplicitAction(t *testing.T) {
	d := dispatcher(t, " ", nil)

	// No amount of navigation enters a text-entry mode.
	for _, k := range []string{"j", "k", "g", "g", "G", "enter", "ctrl+w", "l", "H", "L", "n", "N"} {
		d.Dispatch(k)
		if d.InText() {
			t.Fatalf("%q entered text mode; only an explicit action may", k)
		}
	}

	assertAction(t, d.Dispatch(":"), config.ActionCommandline, 0)
	if !d.InText() {
		t.Fatal("the commandline action did not enter text mode")
	}
}

func TestKeysInTextModeAreLiteralText(t *testing.T) {
	d := dispatcher(t, " ", nil)
	d.Dispatch(":")

	for _, k := range []string{"j", "q", "g", " ", ":"} {
		r := d.Dispatch(k)
		if r.Kind != ui.ResultText {
			t.Fatalf("kind for %q in command mode = %v, want ResultText", k, r.Kind)
		}
		if r.Key != k {
			t.Errorf("key = %q, want %q", r.Key, k)
		}
	}
	if got := d.Mode(); got != ui.ModeCommand {
		t.Errorf("mode = %v, want ModeCommand throughout", got)
	}
}

func TestPickerModeStillDispatchesNavigation(t *testing.T) {
	d := dispatcher(t, " ", nil)
	feed(d, " ", "t")

	assertAction(t, d.Dispatch("j"), config.ActionDown, 0)
	assertAction(t, d.Dispatch("enter"), config.ActionOpen, 0)
}

func TestKeyAliasesNormalizeBeforeLookup(t *testing.T) {
	d := dispatcher(t, " ", nil)

	assertAction(t, d.Dispatch("escape"), config.ActionNormalMode, 0)
	assertAction(t, d.Dispatch("return"), config.ActionOpen, 0)
	assertAction(t, feed(d, "space", "c"), config.ActionComment, 0)
}

func TestPendingRendersTheKeysTypedSoFar(t *testing.T) {
	d := dispatcher(t, " ", nil)

	feed(d, "3", "ctrl+w")
	if got, want := d.Pending(), "ctrl+w"; got != want {
		t.Errorf("Pending() = %q, want %q", got, want)
	}
	if got := d.Count(); got != 3 {
		t.Errorf("Count() = %d, want 3", got)
	}
}

func TestAnAbsurdCountDoesNotOverflow(t *testing.T) {
	d := dispatcher(t, " ", nil)

	r := feed(d, strings.Split(strings.Repeat("9", 12), "")...)
	if r.Kind == ui.ResultAction {
		t.Fatal("digits alone fired an action")
	}
	r = d.Dispatch("j")
	if r.Count <= 0 {
		t.Errorf("count = %d, want a clamped positive count", r.Count)
	}
}
