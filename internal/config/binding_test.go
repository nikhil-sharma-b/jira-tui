package config_test

import (
	"testing"

	"github.com/nikhil-sharma-b/jira-tui/internal/config"
)

func TestContinuationsListsWhatMayFollowAPrefix(t *testing.T) {
	b, err := config.Compile(config.DefaultKeymap(), " ")
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	got := b.Continuations([]string{"ctrl+w"})
	if len(got) == 0 {
		t.Fatalf("Continuations(ctrl+w) is empty")
	}
	for _, c := range got {
		if c.Key == "" {
			t.Errorf("continuation with no key: %+v", c)
		}
		if !c.Group && c.Action == "" {
			t.Errorf("continuation %q names no action", c.Key)
		}
	}
}
