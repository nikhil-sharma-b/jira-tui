package ui_test

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/nikhil-sharma-b/jira-tui/internal/jira"
	"github.com/nikhil-sharma-b/jira-tui/internal/ui"
)

func TestLinkedStorySelectionCopyAndJump(t *testing.T) {
	issue := relatedIssue()
	issue.Links = append(issue.Links, jira.IssueLink{Key: "ENG-10", Summary: "Second linked story"})
	client := &fakeClient{
		issues:   []jira.Issue{issue},
		issueFor: map[string]jira.Issue{"ENG-10": {Key: "ENG-10", Summary: "Opened linked story"}},
	}
	var copied, opened []string
	d := newPausedDriver(t, ui.Options{
		Client: client, Config: testConfig(t, nil),
		Copy:    func(text string) error { copied = append(copied, text); return nil },
		OpenURL: func(url string) error { opened = append(opened, url); return nil },
	})
	d.flush()
	d.keys("enter")
	d.flush()
	d.keys("g", "a", "]", "j")
	if got := d.view(); !strings.Contains(got, "▸ ENG-10") {
		t.Fatalf("linked story was not selected:\n%s", got)
	}
	d.keys(" ", "y", " ", "Y", " ", "o")
	want := []string{"ENG-10", "https://example.atlassian.net/browse/ENG-10"}
	if !slices.Equal(copied, want) {
		t.Fatalf("copied = %q, want %q", copied, want)
	}
	if want := []string{"https://example.atlassian.net/browse/ENG-10"}; !slices.Equal(opened, want) {
		t.Fatalf("opened = %q, want %q", opened, want)
	}
	d.keys("[", "]")
	if got := d.view(); !strings.Contains(got, "▸ ENG-10") {
		t.Fatalf("tab switch lost selection:\n%s", got)
	}
	d.keys("g", "g")
	if got := d.view(); !strings.Contains(got, "▸ ENG-9") {
		t.Fatalf("gg did not select first story:\n%s", got)
	}
	d.keys("G", "enter")
	d.flush()
	if got := d.view(); !strings.Contains(got, "Opened linked story") {
		t.Fatalf("Enter did not open selected story:\n%s", got)
	}
	d.keys("ctrl+o")
	d.flush()
	if got := d.view(); !strings.Contains(got, "Repair the flux capacitor") {
		t.Fatalf("jump back did not return to original story:\n%s", got)
	}
}

func TestEmptyLinksDoNotOpenOrCopyAnotherStory(t *testing.T) {
	var copied, opened []string
	d := newPausedDriver(t, ui.Options{
		Client: &fakeClient{issues: []jira.Issue{detailedIssue()}}, Config: testConfig(t, nil),
		Copy:    func(text string) error { copied = append(copied, text); return nil },
		OpenURL: func(url string) error { opened = append(opened, url); return nil },
	})
	d.flush()
	d.keys("enter")
	d.flush()
	d.keys("g", "a", "]", "enter", " ", "y", " ", "o")
	if len(copied) != 0 || len(opened) != 0 {
		t.Fatalf("empty Links copied %q, opened %q", copied, opened)
	}
	if got := d.view(); !strings.Contains(got, "No links.") {
		t.Fatalf("Enter left empty Links tab:\n%s", got)
	}
}

func TestSubtaskAndChildSelectionCopyAndJump(t *testing.T) {
	for _, children := range []bool{false, true} {
		name := "subtasks"
		if children {
			name = "children"
		}
		t.Run(name, func(t *testing.T) {
			issue := relatedIssue()
			issue.Subtasks = []jira.Subtask{{Key: "ENG-101"}, {Key: "ENG-102"}}
			if children {
				issue = epicIssue()
			}
			client := &fakeClient{
				issues:   []jira.Issue{issue},
				issueFor: map[string]jira.Issue{"ENG-102": {Key: "ENG-102", Summary: "Selected related story"}},
				searchFor: map[string][]jira.Issue{
					`parent = "ENG-100" ORDER BY created ASC`: {{Key: "ENG-101"}, {Key: "ENG-102"}},
				},
			}
			var copied string
			d := newPausedDriver(t, ui.Options{
				Client: client, Config: testConfig(t, nil),
				Copy: func(text string) error { copied = text; return nil },
			})
			d.flush()
			d.keys("enter")
			d.flush()
			d.keys("[", "2", "j", " ", "y")
			if copied != "ENG-102" {
				t.Fatalf("copied = %q, want ENG-102", copied)
			}
			d.keys("enter")
			d.flush()
			if got := d.view(); !strings.Contains(got, "Selected related story") {
				t.Fatalf("Enter did not open selected story:\n%s", got)
			}
		})
	}
}

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

	d.keys("j", " ", "y")
	if got := d.view(); !strings.Contains(got, "yanked key for ENG-2") {
		t.Errorf("key yank feedback is absent:\n%s", got)
	}

	d.keys(" ", "Y")
	if got := d.view(); !strings.Contains(got, "yanked URL for ENG-2") {
		t.Errorf("URL yank feedback is absent:\n%s", got)
	}

	d.keys(" ", "o")

	if got, want := copied, []string{"ENG-2", "https://example.atlassian.net/browse/ENG-2"}; !slices.Equal(got, want) {
		t.Errorf("copied values = %q, want %q", got, want)
	}
	if got, want := opened, []string{"https://example.atlassian.net/browse/ENG-2"}; !slices.Equal(got, want) {
		t.Errorf("opened URLs = %q, want %q", got, want)
	}
}

func TestFocusedIssueIntegrationFeedbackClearsOnTheNextKey(t *testing.T) {
	d := newPausedDriver(t, ui.Options{
		Client: &fakeClient{issues: sampleIssues(2)},
		Config: testConfig(t, nil),
		Copy: func(string) error {
			return nil
		},
	})
	d.flush()

	d.send(keyMsg(" "))
	d.send(keyMsg("y"))
	d.send(keyMsg("j"))
	d.flush()
	if got := d.view(); strings.Contains(got, "yanked key for ENG-1") {
		t.Errorf("late key yank feedback survived the next keypress:\n%s", got)
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
