package config_test

import (
	"reflect"
	"testing"

	"github.com/nikhil-sharma-b/jira-tui/internal/config"
)

func TestResolveEditorUsesConfigThenEnvironmentThenVi(t *testing.T) {
	tests := []struct {
		name   string
		config string
		env    string
		want   []string
	}{
		{"config wins", `nvim -f "+set ft=jira"`, "emacs -nw", []string{"nvim", "-f", "+set ft=jira"}},
		{"environment", "", `code --wait`, []string{"code", "--wait"}},
		{"fallback", "", "", []string{"vi"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := config.Defaults()
			cfg.Editor = tt.config
			got, err := cfg.ResolveEditor(func(name string) string {
				if name == "EDITOR" {
					return tt.env
				}
				return ""
			})
			if err != nil {
				t.Fatalf("ResolveEditor: %v", err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("editor = %#v, want %#v", got, tt.want)
			}
		})
	}
}

func TestResolveEditorRejectsMalformedCommands(t *testing.T) {
	cfg := config.Defaults()
	cfg.Editor = `nvim "unfinished`
	if _, err := cfg.ResolveEditor(func(string) string { return "" }); err == nil {
		t.Fatal("ResolveEditor succeeded, want an unterminated-quote error")
	}
}
