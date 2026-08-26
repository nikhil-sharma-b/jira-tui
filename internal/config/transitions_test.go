package config_test

import (
	"strings"
	"testing"

	"github.com/nikhil-sharma-b/jira-tui/internal/config"
)

func TestTransitionBindingsCompileAlongsideTheKeymap(t *testing.T) {
	cfg, err := config.Load(write(t, minimal+`
[transitions]
"In Progress" = "<leader>ms"
"Done" = "<leader>md"
`, 0o600))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	bindings, err := cfg.Bindings()
	if err != nil {
		t.Fatalf("Bindings: %v", err)
	}
	action, ok := bindings.Lookup([]string{" ", "m", "s"})
	if !ok {
		t.Fatal("<leader>ms is bound to nothing")
	}
	name, ok := action.TransitionName()
	if !ok || name != "In Progress" {
		t.Errorf("action %q names transition %q (%v), want In Progress", action, name, ok)
	}
	// The name is carried by the action rather than looked up elsewhere, so a
	// binding still resolves against live transitions at the keypress.
	if display, ok := bindings.Display(config.TransitionAction("Done")); !ok || !strings.HasSuffix(display, "md") {
		t.Errorf("Display(Done) = %q, %v; want the compiled leader sequence", display, ok)
	}
}

func TestTransitionBindingsReportTheirMistakes(t *testing.T) {
	tests := []struct {
		name string
		toml string
		want string
	}{
		{
			"collides with a keymap binding",
			"[transitions]\n\"Done\" = \"<leader>c\"\n",
			"comment",
		},
		{
			"collides with another transition",
			"[transitions]\n\"Done\" = \"<leader>md\"\n\"Deployed\" = \"<leader>md\"\n",
			"transitions.Done",
		},
		{
			"unbound",
			"[transitions]\n\"Done\" = \"\"\n",
			"transitions.Done",
		},
		{
			"not a key sequence",
			"[transitions]\n\"Done\" = \"<leader>\"\n",
			"transitions.Done",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := config.Load(write(t, minimal+tt.toml, 0o600))
			if err == nil {
				t.Fatal("Load succeeded, want a config error")
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Errorf("error %q does not mention %q", err, tt.want)
			}
		})
	}
}

func TestKeysTableStillRejectsUnknownActions(t *testing.T) {
	// A transition is bound in its own table, so the keys table can keep
	// refusing anything that is not a known action rather than accepting a
	// misspelling as a binding that silently never fires.
	_, err := config.Load(write(t, minimal+"[keys]\ntransition_done = \"<leader>md\"\n", 0o600))
	if err == nil || !strings.Contains(err.Error(), "unknown action") {
		t.Fatalf("Load error = %v, want an unknown action", err)
	}
}

func TestTransitionActionRoundTripsAName(t *testing.T) {
	action := config.TransitionAction("In Progress")
	name, ok := action.TransitionName()
	if !ok || name != "In Progress" {
		t.Errorf("TransitionName() = %q, %v; want In Progress, true", name, ok)
	}
	if _, ok := config.ActionComment.TransitionName(); ok {
		t.Error("a fixed action reports a transition name")
	}
}
