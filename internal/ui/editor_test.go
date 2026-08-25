package ui_test

import (
	"errors"
	"os"
	"os/exec"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/nikhil-sharma-b/jira-tui/internal/jira"
	"github.com/nikhil-sharma-b/jira-tui/internal/ui"
)

type editorStep struct {
	check func(*testing.T, *exec.Cmd, string)
	body  *string
	err   error
}

func scriptedEditor(t *testing.T, steps ...editorStep) (ui.EditorExec, *[]string) {
	t.Helper()
	paths := &[]string{}
	index := 0
	return func(command *exec.Cmd, done tea.ExecCallback) tea.Cmd {
		if index >= len(steps) {
			t.Fatalf("editor launched %d times, only %d steps supplied", index+1, len(steps))
		}
		step := steps[index]
		index++
		path := command.Args[len(command.Args)-1]
		*paths = append(*paths, path)
		return func() tea.Msg {
			if step.check != nil {
				step.check(t, command, path)
			}
			if step.body != nil {
				if err := os.WriteFile(path, []byte(*step.body), 0o600); err != nil {
					t.Fatalf("write editor buffer: %v", err)
				}
			}
			return done(step.err)
		}
	}, paths
}

func stringPtr(value string) *string { return &value }

func assertRemoved(t *testing.T, paths []string) {
	t.Helper()
	for _, path := range paths {
		if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("scratch file %s still exists, stat error %v", path, err)
		}
	}
}

func TestCommentEditorPostsExactTextCleansUpAndRefetches(t *testing.T) {
	client := &fakeClient{issues: []jira.Issue{detailedIssue()}}
	editor, paths := scriptedEditor(t, editorStep{
		body: stringPtr("h2. Diagnosis\n\n* Replace the relay\n"),
		check: func(t *testing.T, command *exec.Cmd, path string) {
			if got := command.Args[:len(command.Args)-1]; strings.Join(got, "|") != "nvim|-f" {
				t.Errorf("editor command = %v, want nvim -f", command.Args)
			}
			if initial, err := os.ReadFile(path); err != nil || len(initial) != 0 {
				t.Errorf("comment scratch = %q, %v; want empty", initial, err)
			}
		},
	})
	cfg := testConfig(t, nil)
	cfg.Editor = "nvim -f"
	d := newPausedDriver(t, ui.Options{Client: client, Config: cfg, EditorExec: editor})
	d.flush()
	d.keys("space", "c")

	if len(client.AddCommentCalls) != 1 || client.AddCommentCalls[0].Key != "ENG-1" || client.AddCommentCalls[0].Body != "h2. Diagnosis\n\n* Replace the relay\n" {
		t.Errorf("comment calls = %#v, want one exact plain-markup write", client.AddCommentCalls)
	}
	if got := client.issueRequests(); len(got) != 1 || got[0] != "ENG-1" {
		t.Errorf("issue refetches = %v, want [ENG-1]", got)
	}
	if got := client.commentRequests(); len(got) != 1 || got[0] != "ENG-1" {
		t.Errorf("comment refetches = %v, want [ENG-1]", got)
	}
	assertRemoved(t, *paths)
}

func TestEditorCancellationNeverWritesAndAlwaysCleansUp(t *testing.T) {
	tests := []struct {
		name string
		step editorStep
	}{
		{"empty or unchanged", editorStep{}},
		{"non-zero exit", editorStep{body: stringPtr("draft that must not post"), err: errors.New("signal: killed")}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := &fakeClient{issues: []jira.Issue{detailedIssue()}}
			editor, paths := scriptedEditor(t, tt.step)
			d := newPausedDriver(t, ui.Options{Client: client, Config: testConfig(t, nil), EditorExec: editor})
			d.flush()
			d.keys("space", "c")
			if len(client.AddCommentCalls) != 0 {
				t.Errorf("comment calls = %#v, want none", client.AddCommentCalls)
			}
			assertRemoved(t, *paths)
		})
	}
}

func TestNavigationBeforeScratchCreationCancelsTheEditorLaunch(t *testing.T) {
	client := &fakeClient{issues: sampleIssues(2)}
	editor, paths := scriptedEditor(t)
	d := newPausedDriver(t, ui.Options{Client: client, Config: testConfig(t, nil), EditorExec: editor})
	d.flush()
	d.send(keyMsg("space"))
	d.send(keyMsg("c"))
	d.send(keyMsg("j"))
	d.step()

	if len(*paths) != 0 {
		t.Errorf("editor launched for a stale focused item: %v", *paths)
	}
	if len(client.AddCommentCalls) != 0 {
		t.Errorf("comment calls = %#v, want none", client.AddCommentCalls)
	}
}

func TestCancelledDescriptionLoadCannotLaunchAnEditorLater(t *testing.T) {
	client := &fakeClient{issues: []jira.Issue{detailedIssue()}}
	editor, paths := scriptedEditor(t)
	d := newPausedDriver(t, ui.Options{Client: client, Config: testConfig(t, nil), EditorExec: editor})
	d.flush()
	d.send(keyMsg("space"))
	d.send(keyMsg("e"))
	d.step() // unwrap the live detail batch
	d.send(keyMsg("q"))
	d.flush() // deliver the cancelled fetch responses
	d.keys("enter")

	if len(*paths) != 0 {
		t.Errorf("a later detail fetch launched the cancelled description editor: %v", *paths)
	}
}

func TestDescriptionEditorStartsFromPlainProjectionAndReplacesIt(t *testing.T) {
	client := &fakeClient{issues: []jira.Issue{detailedIssue()}}
	editor, paths := scriptedEditor(t, editorStep{
		body: stringPtr("h2. New diagnosis\n\n* Replace both relays"),
		check: func(t *testing.T, _ *exec.Cmd, path string) {
			initial, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if got := string(initial); !strings.Contains(got, "h2. Diagnosis") || !strings.Contains(got, "* Replace the relay") || strings.Contains(got, "\x1b[") {
				t.Errorf("initial description is not Jira wiki markup: %q", got)
			}
		},
	})
	d := newPausedDriver(t, ui.Options{Client: client, Config: testConfig(t, nil), EditorExec: editor})
	d.flush()
	d.keys("space", "e")

	if len(client.Descriptions) != 1 || client.Descriptions[0].Key != "ENG-1" || client.Descriptions[0].Body != "h2. New diagnosis\n\n* Replace both relays" {
		t.Errorf("description writes = %#v, want one exact replacement", client.Descriptions)
	}
	assertRemoved(t, *paths)
}

func TestUnchangedDescriptionCancelsWithoutWriting(t *testing.T) {
	client := &fakeClient{issues: []jira.Issue{detailedIssue()}}
	editor, paths := scriptedEditor(t, editorStep{})
	d := newPausedDriver(t, ui.Options{Client: client, Config: testConfig(t, nil), EditorExec: editor})
	d.flush()
	d.keys("space", "e")
	if len(client.Descriptions) != 0 {
		t.Errorf("description writes = %#v, want none for unchanged content", client.Descriptions)
	}
	assertRemoved(t, *paths)
}

func TestFailedWriteReportsErrorAndPrefillsTheNextAttempt(t *testing.T) {
	draft := "A draft worth keeping"
	replacement := draft + " after retry"
	client := &fakeClient{issues: []jira.Issue{detailedIssue()}, addCommentErr: errors.New("Jira refused the comment")}
	editor, paths := scriptedEditor(t,
		editorStep{body: &draft},
		editorStep{
			body: &replacement,
			check: func(t *testing.T, _ *exec.Cmd, path string) {
				got, err := os.ReadFile(path)
				if err != nil || string(got) != draft {
					t.Errorf("retry scratch = %q, %v; want preserved draft %q", got, err, draft)
				}
			},
		},
	)
	d := newPausedDriver(t, ui.Options{Client: client, Config: testConfig(t, nil), EditorExec: editor})
	d.flush()
	d.keys("enter")
	d.keys("space", "c")
	if !strings.Contains(d.view(), "Jira refused the comment") || !strings.Contains(d.view(), "Description") || !strings.Contains(d.view(), "Repair the flux capacitor") {
		t.Errorf("failed write did not keep the list and report the error:\n%s", d.view())
	}
	client.mu.Lock()
	client.addCommentErr = nil
	client.mu.Unlock()
	d.keys("space", "c")
	if len(client.AddCommentCalls) != 2 || client.AddCommentCalls[1].Body != replacement {
		t.Errorf("comment calls = %#v, want preserved draft retried after editing", client.AddCommentCalls)
	}
	assertRemoved(t, *paths)
}

func TestFailedDescriptionWritePreservesTheReplacement(t *testing.T) {
	draft := "h2. Replacement kept after failure"
	client := &fakeClient{issues: []jira.Issue{detailedIssue()}, descriptionErr: errors.New("description rejected")}
	editor, paths := scriptedEditor(t,
		editorStep{body: &draft},
		editorStep{check: func(t *testing.T, _ *exec.Cmd, path string) {
			got, err := os.ReadFile(path)
			if err != nil || string(got) != draft {
				t.Errorf("retry description = %q, %v; want %q", got, err, draft)
			}
		}},
	)
	d := newPausedDriver(t, ui.Options{Client: client, Config: testConfig(t, nil), EditorExec: editor})
	d.flush()
	d.keys("space", "e")
	if !strings.Contains(d.view(), "description rejected") {
		t.Errorf("description failure is absent from the status line:\n%s", d.view())
	}
	d.keys("space", "e")
	if len(client.Descriptions) != 1 {
		t.Errorf("unchanged preserved draft retried automatically: %#v", client.Descriptions)
	}
	assertRemoved(t, *paths)
}

func TestCommentTargetsFocusedItemInListSplitZoomAndPinnedViews(t *testing.T) {
	tests := []struct {
		name string
		opts func(*fakeClient, ui.EditorExec) ui.Options
		keys []string
	}{
		{"list", func(c *fakeClient, e ui.EditorExec) ui.Options {
			return ui.Options{Client: c, Config: testConfig(t, nil), EditorExec: e}
		}, nil},
		{"split detail", func(c *fakeClient, e ui.EditorExec) ui.Options {
			return ui.Options{Client: c, Config: testConfig(t, nil), EditorExec: e}
		}, []string{"enter"}},
		{"zoomed detail", func(c *fakeClient, e ui.EditorExec) ui.Options {
			return ui.Options{Client: c, Config: testConfig(t, nil), EditorExec: e}
		}, []string{"enter", "ctrl+w", "o"}},
		{"pinned", func(c *fakeClient, e ui.EditorExec) ui.Options {
			return ui.Options{Client: c, Config: testConfig(t, nil), Pin: "ENG-1", EditorExec: e}
		}, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := &fakeClient{issues: []jira.Issue{detailedIssue()}}
			editor, _ := scriptedEditor(t, editorStep{body: stringPtr("focused")})
			d := newPausedDriver(t, tt.opts(client, editor))
			d.flush()
			d.keys(tt.keys...)
			d.keys("space", "c")
			if len(client.AddCommentCalls) != 1 || client.AddCommentCalls[0].Key != "ENG-1" {
				t.Errorf("comment calls = %#v, want focused ENG-1", client.AddCommentCalls)
			}
		})
	}
}

func TestLateWriteSuccessDoesNotReopenAClosedDetail(t *testing.T) {
	client := &fakeClient{issues: []jira.Issue{detailedIssue()}}
	editor, _ := scriptedEditor(t, editorStep{body: stringPtr("posted after close")})
	d := newPausedDriver(t, ui.Options{Client: client, Config: testConfig(t, nil), EditorExec: editor})
	d.flush()
	d.keys("enter")

	d.send(keyMsg("space"))
	d.send(keyMsg("c"))
	d.step() // create scratch
	d.step() // editor completion
	d.step() // read scratch and queue the Jira write
	d.send(keyMsg("q"))
	d.step() // Jira write completion

	if len(client.AddCommentCalls) != 1 {
		t.Fatalf("comment calls = %#v, want the in-flight write to finish", client.AddCommentCalls)
	}
	if strings.Contains(d.view(), "Loading") || strings.Contains(d.view(), "Description") {
		t.Errorf("late write success reopened the closed detail:\n%s", d.view())
	}
}
