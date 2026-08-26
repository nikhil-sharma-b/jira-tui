package ui_test

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/nikhil-sharma-b/jira-tui/internal/ui"
)

func TestFocusedIssueIntegrations(t *testing.T) {
	client := &fakeClient{issues: sampleIssues(2)}
	var copied, opened []string
	d := newPausedDriver(t, ui.Options{
		Client: client,
		Config: testConfig(t, nil),
		Copy: func(text string) error {
			copied = append(copied, text)
			return nil
		},
		OpenURL: func(url string) error {
			opened = append(opened, url)
			return nil
		},
	})
	d.flush()

	d.keys("j", " ", "y", " ", "Y", " ", "o")

	if got, want := copied, []string{"ENG-2", "https://example.atlassian.net/browse/ENG-2"}; !slices.Equal(got, want) {
		t.Errorf("copied values = %q, want %q", got, want)
	}
	if got, want := opened, []string{"https://example.atlassian.net/browse/ENG-2"}; !slices.Equal(got, want) {
		t.Errorf("opened URLs = %q, want %q", got, want)
	}
}

func TestFocusedIssueIntegrationUsesThePinnedIssue(t *testing.T) {
	var copied string
	d := newPausedDriver(t, ui.Options{
		Client: &fakeClient{issues: sampleIssues(2)},
		Config: testConfig(t, nil),
		Pin:    "ENG-2",
		Copy: func(text string) error {
			copied = text
			return nil
		},
	})
	d.flush()

	d.keys(" ", "y")
	if copied != "ENG-2" {
		t.Errorf("copied value = %q, want the pinned issue ENG-2", copied)
	}
}

func TestFocusedIssueIntegrationErrorsReachTheStatusLine(t *testing.T) {
	d := newPausedDriver(t, ui.Options{
		Client: &fakeClient{issues: sampleIssues(1)},
		Config: testConfig(t, nil),
		Copy: func(string) error {
			return fmt.Errorf("clipboard unavailable")
		},
		OpenURL: func(string) error {
			return fmt.Errorf("browser unavailable")
		},
	})
	d.flush()

	d.keys(" ", "y")
	if got := d.view(); !strings.Contains(got, "copy ENG-1: clipboard unavailable") {
		t.Errorf("copy failure did not reach the status line:\n%s", got)
	}

	d.keys(" ", "o")
	if got := d.view(); !strings.Contains(got, "open ENG-1: browser unavailable") {
		t.Errorf("browser failure did not reach the status line:\n%s", got)
	}
}
