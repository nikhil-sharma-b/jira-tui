package config_test

import (
	"strings"
	"testing"

	"github.com/nikhil-sharma-b/jira-tui/internal/config"
)

func TestMergeKeepsUnrelatedDefaults(t *testing.T) {
	merged := config.DefaultKeymap().Merge(map[string]string{string(config.ActionDown): "n"})

	if got := merged[config.ActionDown]; got != "n" {
		t.Errorf("down = %q, want %q", got, "n")
	}
	if got := merged[config.ActionUp]; got != "k" {
		t.Errorf("up = %q, want the default %q", got, "k")
	}
	if len(merged) != len(config.DefaultKeymap()) {
		t.Errorf("merged has %d bindings, want %d", len(merged), len(config.DefaultKeymap()))
	}
}

func TestMergeDoesNotMutateDefaults(t *testing.T) {
	config.DefaultKeymap().Merge(map[string]string{string(config.ActionDown): "n"})

	if got := config.DefaultKeymap()[config.ActionDown]; got != "j" {
		t.Errorf("config.DefaultKeymap down = %q after a merge, want %q", got, "j")
	}
}

func TestMergeEmptyBindingUnbinds(t *testing.T) {
	merged := config.DefaultKeymap().Merge(map[string]string{string(config.ActionComment): ""})

	if _, ok := merged[config.ActionComment]; ok {
		t.Fatalf("comment is still bound after being set to the empty string")
	}

	b, err := config.Compile(merged, " ")
	if err != nil {
		t.Fatalf("config.Compile: %v", err)
	}
	if a, ok := b.Lookup([]string{" ", "c"}); ok {
		t.Errorf("<leader>c resolves to %q, want no binding", a)
	}
}

func TestCompileTokenizesBindings(t *testing.T) {
	km := config.Keymap{
		config.ActionDown:        "j",
		config.ActionTop:         "gg",
		config.ActionPaneLeft:    "ctrl+w h",
		config.ActionOpen:        "enter",
		config.ActionComment:     "<leader>c",
		config.ActionYankURL:     "<leader>Y",
		config.ActionCommandline: ":",
	}
	b, err := config.Compile(km, " ")
	if err != nil {
		t.Fatalf("config.Compile: %v", err)
	}

	cases := []struct {
		keys []string
		want config.Action
	}{
		{[]string{"j"}, config.ActionDown},
		{[]string{"g", "g"}, config.ActionTop},
		{[]string{"ctrl+w", "h"}, config.ActionPaneLeft},
		{[]string{"enter"}, config.ActionOpen},
		{[]string{" ", "c"}, config.ActionComment},
		{[]string{" ", "Y"}, config.ActionYankURL},
		{[]string{":"}, config.ActionCommandline},
	}
	for _, tc := range cases {
		got, ok := b.Lookup(tc.keys)
		if !ok {
			t.Errorf("%v resolves to no binding, want %q", tc.keys, tc.want)
			continue
		}
		if got != tc.want {
			t.Errorf("%v = %q, want %q", tc.keys, got, tc.want)
		}
	}
}

func TestCompileReportsPrefixes(t *testing.T) {
	b, err := config.Compile(config.Keymap{config.ActionTop: "gg"}, " ")
	if err != nil {
		t.Fatalf("config.Compile: %v", err)
	}
	if !b.IsPrefix([]string{"g"}) {
		t.Errorf("g is not reported as a prefix of gg")
	}
	if b.IsPrefix([]string{"g", "g"}) {
		t.Errorf("gg is reported as a prefix of itself")
	}
	if b.IsPrefix([]string{"z"}) {
		t.Errorf("z is reported as a prefix of something")
	}
}

func TestCompileExpandsTheConfiguredLeader(t *testing.T) {
	km := config.Keymap{config.ActionComment: "<leader>c"}

	b, err := config.Compile(km, ",")
	if err != nil {
		t.Fatalf("config.Compile: %v", err)
	}
	if a, ok := b.Lookup([]string{",", "c"}); !ok || a != config.ActionComment {
		t.Errorf("comma leader: ,c = %q/%v, want comment", a, ok)
	}
	if _, ok := b.Lookup([]string{" ", "c"}); ok {
		t.Errorf("space still resolves as leader after the leader was changed")
	}
}

func TestCompileRejectsCollidingBindingsNamingBoth(t *testing.T) {
	_, err := config.Compile(config.Keymap{config.ActionDown: "x", config.ActionUp: "x"}, " ")
	if err == nil {
		t.Fatal("two actions bound to x compiled without error")
	}
	msg := err.Error()
	for _, want := range []string{"down", "up", `"x"`} {
		if !strings.Contains(msg, want) {
			t.Errorf("error %q does not mention %q", msg, want)
		}
	}
}

func TestCompileRejectsABindingThatShadowsAnother(t *testing.T) {
	// g would swallow the first half of gg forever.
	_, err := config.Compile(config.Keymap{config.ActionTop: "gg", config.ActionDown: "g"}, " ")
	if err == nil {
		t.Fatal("g and gg compiled together without error")
	}
	msg := err.Error()
	for _, want := range []string{"top", "down"} {
		if !strings.Contains(msg, want) {
			t.Errorf("error %q does not mention %q", msg, want)
		}
	}
}

func TestCompileRejectsAnEmptyLeaderOnlyWhenUsed(t *testing.T) {
	if _, err := config.Compile(config.Keymap{config.ActionDown: "j"}, ""); err != nil {
		t.Errorf("a keymap with no leader binding needs no leader: %v", err)
	}
	if _, err := config.Compile(config.Keymap{config.ActionComment: "<leader>c"}, ""); err == nil {
		t.Error("<leader>c compiled with no leader configured")
	}
}

func TestDefaultKeymapCompiles(t *testing.T) {
	if _, err := config.Compile(config.DefaultKeymap(), " "); err != nil {
		t.Fatalf("the built-in keymap does not compile: %v", err)
	}
}

func TestNormalizeKeyAcceptsAliases(t *testing.T) {
	cases := map[string]string{
		"escape": "esc",
		"esc":    "esc",
		"return": "enter",
		"cr":     "enter",
		"space":  " ",
		" ":      " ",
		"j":      "j",
		"CTRL+D": "ctrl+d",
	}
	for in, want := range cases {
		if got := config.NormalizeKey(in); got != want {
			t.Errorf("config.NormalizeKey(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestBindingDisplayReadsLikeVim(t *testing.T) {
	km := config.Keymap{
		config.ActionTop:      "gg",
		config.ActionPaneLeft: "ctrl+w h",
		config.ActionComment:  "<leader>c",
	}
	b, err := config.Compile(km, " ")
	if err != nil {
		t.Fatalf("config.Compile: %v", err)
	}
	cases := map[config.Action]string{
		config.ActionTop:      "gg",
		config.ActionPaneLeft: "ctrl+w h",
		config.ActionComment:  "<space>c",
	}
	for a, want := range cases {
		got, ok := b.Display(a)
		if !ok {
			t.Errorf("%q has no display form", a)
			continue
		}
		if got != want {
			t.Errorf("display(%q) = %q, want %q", a, got, want)
		}
	}
}

func TestLoadReportsCollidingUserBindings(t *testing.T) {
	path := write(t, topLevel(`
[keys]
up = "j"
`), 0o600)
	_, err := config.Load(path)
	if err == nil {
		t.Fatal("a user binding colliding with a default loaded without error")
	}
	if !strings.Contains(err.Error(), "down") || !strings.Contains(err.Error(), "up") {
		t.Errorf("error %q does not name both colliding actions", err)
	}
}

func TestCompileRefusesBindingsOnReservedKeys(t *testing.T) {
	tests := []struct {
		name string
		km   config.Keymap
		want string
	}{
		{
			// esc is resolved before the table, so this key would be dead.
			"esc",
			config.Keymap{config.ActionNormalMode: "", config.ActionClosePane: "esc"},
			"esc",
		},
		{
			// 1-9 are swallowed as counts, so this key would be dead too.
			"count digit",
			config.Keymap{config.ActionReload: "5"},
			"count",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := config.Compile(tc.km, " ")
			if err == nil {
				t.Fatal("a binding on a reserved key compiled without error")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error %q does not explain %q", err, tc.want)
			}
		})
	}
}

func TestCompileAllowsACountDigitLaterInASequence(t *testing.T) {
	if _, err := config.Compile(config.Keymap{config.ActionReload: "g5"}, " "); err != nil {
		t.Errorf("g5 is not a count and should compile: %v", err)
	}
}

func TestLoadRejectsAMultiKeyLeader(t *testing.T) {
	path := write(t, topLevel(`
leader = "gg"
`), 0o600)
	_, err := config.Load(path)
	if err == nil {
		t.Fatal("a two-key leader loaded without error")
	}
	if !strings.Contains(err.Error(), "leader") {
		t.Errorf("error %q blames something other than the leader", err)
	}
}

func TestLoadRejectsABadLeaderEvenWithNoLeaderBindings(t *testing.T) {
	// Every leader binding unbound: the leader is still wrong, and finding
	// out at startup beats finding out on the day one is bound again.
	path := write(t, topLevel(`
leader = "gg"

[keys]
comment = ""
edit_description = ""
transition = ""
assign = ""
yank_key = ""
yank_url = ""
open_browser = ""
`), 0o600)
	if _, err := config.Load(path); err == nil {
		t.Fatal("a two-key leader loaded once nothing used it")
	}
}
