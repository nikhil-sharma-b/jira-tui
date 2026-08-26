package ui_test

import (
	"strings"
	"testing"

	"github.com/nikhil-sharma-b/jira-tui/internal/config"
	"github.com/nikhil-sharma-b/jira-tui/internal/ui"
)

func help(t *testing.T, leader string, overrides map[string]string) *ui.Help {
	t.Helper()
	return ui.NewHelp(compile(t, leader, overrides))
}

func TestHelpStartsHidden(t *testing.T) {
	if help(t, " ", nil).Visible() {
		t.Error("the help overlay is visible before anything asked for it")
	}
}

func TestHelpIsToggledByItsOwnAction(t *testing.T) {
	h := help(t, " ", nil)

	if !h.HandleAction(config.ActionHelp) {
		t.Fatal("the help action was not consumed")
	}
	if !h.Visible() {
		t.Fatal("the help action did not show the overlay")
	}
	h.HandleAction(config.ActionHelp)
	if h.Visible() {
		t.Error("the help action did not dismiss the overlay")
	}
}

// Esc is resolved ahead of every widget, so the overlay does not consume it:
// it is dismissed by the same Esc that clears everything else, which is why
// an overlay cannot absorb a keypress meant for what is behind it.
func TestHelpDoesNotConsumeEsc(t *testing.T) {
	h := help(t, " ", nil)
	h.HandleAction(config.ActionHelp)

	if h.HandleAction(config.ActionNormalMode) {
		t.Error("the overlay consumed normal_mode; Esc belongs to everything at once")
	}
	h.Hide()
	if h.Visible() {
		t.Error("Hide did not dismiss the overlay")
	}
}

func TestHelpConsumesNothingWhileHidden(t *testing.T) {
	h := help(t, " ", nil)

	if h.HandleAction(config.ActionNormalMode) {
		t.Error("a hidden overlay consumed normal_mode")
	}
	if h.HandleAction(config.ActionDown) {
		t.Error("a hidden overlay consumed a motion")
	}
}

func TestHelpDoesNotConsumeUnrelatedActionsWhileVisible(t *testing.T) {
	h := help(t, " ", nil)
	h.HandleAction(config.ActionHelp)

	if h.HandleAction(config.ActionDown) {
		t.Error("the overlay consumed a motion; scrolling it is the pane's business")
	}
}

func TestHelpListsEffectiveBindingsByCategory(t *testing.T) {
	out := strings.Join(help(t, " ", nil).Lines(), "\n")

	for _, want := range []string{"MOTION", "PANES", "ACTIONS"} {
		if !strings.Contains(out, want) {
			t.Errorf("help is missing the %s category:\n%s", want, out)
		}
	}
	for _, want := range []string{"gg", "ctrl+w h", "<space>c", "half page down"} {
		if !strings.Contains(out, want) {
			t.Errorf("help is missing %q:\n%s", want, out)
		}
	}
}

func TestHelpReflectsRebindings(t *testing.T) {
	out := strings.Join(help(t, ",", map[string]string{
		string(config.ActionDown): "e",
	}).Lines(), "\n")

	if !strings.Contains(out, ",c") {
		t.Errorf("help does not show the comma leader:\n%s", out)
	}
	if strings.Contains(out, "<space>c") {
		t.Errorf("help still shows the space leader after it changed:\n%s", out)
	}

	if !hasBindingFor(out, "e", "down") {
		t.Errorf("help does not show down bound to e:\n%s", out)
	}
	if hasBindingFor(out, "j", "down") {
		t.Errorf("help still shows the replaced j binding:\n%s", out)
	}
}

func TestHelpOmitsUnboundActions(t *testing.T) {
	out := strings.Join(help(t, " ", map[string]string{
		string(config.ActionComment): "",
	}).Lines(), "\n")

	if strings.Contains(out, "<space>c ") || hasBindingFor(out, "<space>c", "comment") {
		t.Errorf("help still lists the unbound comment action:\n%s", out)
	}
	if !hasBindingFor(out, "<space>e", "edit description via $EDITOR") {
		t.Errorf("unbinding one action removed others:\n%s", out)
	}
}

func TestHelpCoversEveryDefaultAction(t *testing.T) {
	// The overlay renders the keymap that is in effect; an action missing
	// from its table would be a binding no user could discover.
	h := help(t, " ", nil)
	out := strings.Join(h.Lines(), "\n")

	b, err := config.Compile(config.DefaultKeymap(), " ")
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range b.Actions() {
		display, _ := b.Display(a)
		if !strings.Contains(out, display) {
			t.Errorf("action %q (bound to %q) is missing from the help overlay", a, display)
		}
	}
}

// hasBindingFor reports whether a help line binds keys to a description.
func hasBindingFor(out, keys, description string) bool {
	for _, line := range strings.Split(out, "\n") {
		f := strings.TrimSpace(line)
		if strings.HasPrefix(f, keys+" ") && strings.HasSuffix(f, description) {
			return true
		}
	}
	return false
}
