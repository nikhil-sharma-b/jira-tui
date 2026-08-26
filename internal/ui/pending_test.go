package ui_test

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/nikhil-sharma-b/jira-tui/internal/ui"
)

// A prefix swallows the next keypress, so the status line has to say one is in
// flight -- and stop saying it the moment the sequence resolves or is dropped.
func TestPendingSequenceShowsInTheStatusLine(t *testing.T) {
	d := newPausedDriver(t, ui.Options{
		Client: &fakeClient{issues: sampleIssues(2)},
		Config: testConfig(t, nil),
	})
	d.flush()
	d.send(tea.WindowSizeMsg{Width: 100, Height: 22})

	d.keys("ctrl+w")
	if got := d.view(); !strings.Contains(got, "ctrl+w") {
		t.Errorf("pending prefix is absent from the status line:\n%s", got)
	}

	d.keys("esc")
	last := lastLine(d.view())
	if strings.Contains(last, "ctrl+w") {
		t.Errorf("pending prefix survived Esc: %q", last)
	}
}

// A count in flight is pending input too, and reads as one indicator with the
// keys typed after it.
func TestPendingCountShowsInTheStatusLine(t *testing.T) {
	d := newPausedDriver(t, ui.Options{
		Client: &fakeClient{issues: sampleIssues(2)},
		Config: testConfig(t, nil),
	})
	d.flush()
	d.send(tea.WindowSizeMsg{Width: 100, Height: 22})

	d.keys("1", "2")
	if got := lastLine(d.view()); !strings.HasSuffix(strings.TrimRight(got, " "), "12") {
		t.Errorf("count in flight is absent from the status line: %q", got)
	}
}

func lastLine(view string) string {
	lines := strings.Split(strings.TrimRight(view, "\n"), "\n")
	return lines[len(lines)-1]
}
