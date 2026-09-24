package ui

import (
	"errors"
	"slices"
	"testing"
)

func TestClipboardTool(t *testing.T) {
	tests := []struct {
		name      string
		goos      string
		env       map[string]string
		installed []string
		want      []string
		wantErr   error
	}{
		{"wayland", "linux", map[string]string{"WAYLAND_DISPLAY": "wayland-1", "TERM": "foot"}, []string{"wl-copy", "xclip"}, []string{"wl-copy"}, nil},
		{"x11 prefers xclip", "linux", map[string]string{"DISPLAY": ":0"}, []string{"xclip", "xsel"}, []string{"xclip", "-selection", "clipboard"}, nil},
		{"x11 falls back to xsel", "linux", map[string]string{"DISPLAY": ":0"}, []string{"xsel"}, []string{"xsel", "--clipboard", "--input"}, nil},
		{"ssh uses OSC 52", "linux", map[string]string{"TERM": "tmux-256color"}, []string{"xclip"}, nil, nil},
		{"desktop without a tool uses OSC 52", "linux", map[string]string{"DISPLAY": ":0", "TERM": "xterm"}, nil, nil, nil},
		{"nothing at all", "linux", map[string]string{"TERM": "dumb"}, nil, nil, errNoClipboard},
		{"macOS", "darwin", nil, []string{"pbcopy"}, []string{"pbcopy"}, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			getenv := func(k string) string { return tt.env[k] }
			lookPath := func(name string) (string, error) {
				if slices.Contains(tt.installed, name) {
					return "/usr/bin/" + name, nil
				}
				return "", errors.New("not found")
			}
			got, err := clipboardTool(tt.goos, getenv, lookPath)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("err = %v, want %v", err, tt.wantErr)
			}
			if !slices.Equal(got, tt.want) {
				t.Errorf("tool = %q, want %q", got, tt.want)
			}
		})
	}
}
