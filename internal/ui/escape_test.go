package ui_test

import (
	"strings"
	"testing"
	"time"

	"github.com/nikhil-sharma-b/jira-tui/internal/jira"
	"github.com/nikhil-sharma-b/jira-tui/internal/ui"
)

// escapeClient answers everything a widget in this file needs to open: the
// assign picker's users, plus the transitions the transition picker offers.
func escapeClient() *fakeClient {
	client := assignClient()
	client.transitions = map[string][]jira.Transition{"ENG-1": availableTransitions()}
	return client
}

func escapeDriver(t *testing.T) *driver {
	t.Helper()
	d := newPausedDriver(t, ui.Options{
		Client:         escapeClient(),
		Config:         testConfig(t, nil),
		SearchDebounce: time.Millisecond,
	})
	d.flush()
	return d
}

// TestEscReturnsToNormalFromEveryStateTheUIShows is the whole of the promise that no
// widget swallows Esc, asserted through the model rather than the dispatcher:
// whatever is on screen, one Esc takes it away and hands keys back to the
// binding table. The proof that keys are commands again is that "?" opens the
// help overlay rather than being typed into something.
func TestEscReturnsToNormalFromEveryStateTheUIShows(t *testing.T) {
	tests := []struct {
		name string
		// enter puts the UI into the state under test.
		enter func(*driver)
		// gone is text that is on screen in that state and must not survive Esc.
		// Empty for states that show nothing of their own -- a half-typed
		// sequence, a count -- where the assertion is only that keys work again.
		gone string
	}{
		{"commandline", func(d *driver) { d.keys(":") }, ":"},
		{"commandline with a line typed", func(d *driver) {
			d.keys(":")
			d.typeText("jql project = ENG")
		}, ":jql project = ENG"},
		{"commandline showing completions", func(d *driver) {
			d.keys(":")
			d.typeText("c")
			d.keys("tab")
		}, ":cache"},
		{"search prompt", func(d *driver) {
			d.keys("/")
			d.typeText("number")
		}, "/number"},
		{"transition picker", func(d *driver) { d.keys("space", "t") }, "Start Progress"},
		{"transition picker filtered", func(d *driver) {
			d.keys("space", "t")
			d.typeText("prog")
		}, "Start Progress"},
		{"assign picker", func(d *driver) { d.keys("space", "a") }, "Assign ENG-1"},
		{"assign picker mid-search", func(d *driver) {
			d.keys("space", "a")
			d.typeText("hop")
		}, "Assign ENG-1"},
		{"help overlay", func(d *driver) { d.keys("?") }, "MOTION"},
		{"unfinished key sequence", func(d *driver) { d.keys("ctrl+w") }, ""},
		{"pending count", func(d *driver) { d.keys("5") }, ""},
		{"detail pane", func(d *driver) { d.keys("enter") }, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := escapeDriver(t)
			tt.enter(d)

			d.keys("esc")

			if tt.gone != "" && strings.Contains(d.view(), tt.gone) {
				t.Errorf("%q survived Esc:\n%s", tt.gone, d.view())
			}
			// Keys are commands again rather than input for whatever was up.
			d.keys("?")
			if !strings.Contains(d.view(), "MOTION") {
				t.Fatalf("after Esc, ? did not open the help overlay -- keys are still being swallowed:\n%s", d.view())
			}
			d.keys("esc")
			if strings.Contains(d.view(), "MOTION") {
				t.Errorf("Esc did not dismiss the help overlay:\n%s", d.view())
			}
		})
	}
}

// A count typed before Esc is abandoned rather than carried into the next
// motion, which is what stops a half-typed 5 from moving five rows later.
func TestEscDiscardsAHalfTypedCountInTheList(t *testing.T) {
	d := escapeDriver(t)

	d.keys("5")
	d.keys("esc")
	d.keys("j")

	if got := d.selected(); got != "ENG-2" {
		t.Errorf("selection = %q after a count abandoned by Esc, want ENG-2", got)
	}
}

// Esc means "I have changed my mind", including about an editor that has been
// asked for but has not opened yet: the description edit waits for a live read
// of the item, and an editor appearing after Esc would be the modal model
// letting go of the user in the one place it cannot.
func TestEscCancelsADescriptionEditWaitingOnItsDetailRead(t *testing.T) {
	client := &fakeClient{issues: []jira.Issue{detailedIssue()}}
	editor, paths := scriptedEditor(t)
	d := newPausedDriver(t, ui.Options{Client: client, Config: testConfig(t, nil), EditorExec: editor})
	d.flush()

	d.send(keyMsg("space"))
	d.send(keyMsg("e"))
	d.step() // unwrap the live detail batch
	d.send(keyMsg("esc"))
	d.flush() // deliver the detail read the editor was waiting on

	if len(*paths) != 0 {
		t.Errorf("an editor opened after Esc: %v", *paths)
	}
	if len(client.Descriptions) != 0 {
		t.Errorf("description writes = %#v, want none", client.Descriptions)
	}
}

// The same for the gap between asking for a comment editor and the scratch
// file existing: Esc in that window cancels the launch, and takes the scratch
// file that lands afterwards with it.
func TestEscCancelsACommentEditorBeforeItOpens(t *testing.T) {
	client := &fakeClient{issues: sampleIssues(2)}
	editor, paths := scriptedEditor(t)
	d := newPausedDriver(t, ui.Options{Client: client, Config: testConfig(t, nil), EditorExec: editor})
	d.flush()

	d.send(keyMsg("space"))
	d.send(keyMsg("c"))
	d.send(keyMsg("esc"))
	d.flush()

	if len(*paths) != 0 {
		t.Errorf("an editor opened after Esc: %v", *paths)
	}
	if len(client.AddCommentCalls) != 0 {
		t.Errorf("comment calls = %#v, want none", client.AddCommentCalls)
	}
	assertRemoved(t, *paths)
}

// The overlay is a widget like any other: what Esc does everywhere else it
// does under the overlay too, rather than being spent on dismissing it.
func TestEscUnderTheHelpOverlayStillCancelsAPendingEdit(t *testing.T) {
	client := &fakeClient{issues: []jira.Issue{detailedIssue()}}
	editor, paths := scriptedEditor(t)
	d := newPausedDriver(t, ui.Options{Client: client, Config: testConfig(t, nil), EditorExec: editor})
	d.flush()

	d.send(keyMsg("space"))
	d.send(keyMsg("e"))
	d.step() // unwrap the live detail batch
	d.send(keyMsg("?"))
	d.send(keyMsg("esc"))
	d.flush() // deliver the detail read the editor was waiting on

	if len(*paths) != 0 {
		t.Errorf("an editor opened after Esc under the help overlay: %v", *paths)
	}
	if strings.Contains(d.view(), "MOTION") {
		t.Errorf("Esc did not also dismiss the overlay:\n%s", d.view())
	}
}

// Esc cancels an editor that has not opened. It does not reach back into one
// that has already been written and quit: the text is on its way to Jira, and
// dropping it there would lose work the user believes they saved.
func TestEscAfterTheEditorQuitsStillPostsWhatWasWritten(t *testing.T) {
	client := &fakeClient{issues: sampleIssues(2)}
	editor, _ := scriptedEditor(t, editorStep{body: stringPtr("h2. Written and saved\n")})
	d := newPausedDriver(t, ui.Options{Client: client, Config: testConfig(t, nil), EditorExec: editor})
	d.flush()

	d.send(keyMsg("space"))
	d.send(keyMsg("c"))
	d.step() // create the scratch file
	d.step() // run the editor, which writes and quits
	// A key struck as the terminal comes back lands after the editor is done
	// but before the body has been read and posted.
	d.send(keyMsg("esc"))
	d.flush()

	if len(client.AddCommentCalls) != 1 || client.AddCommentCalls[0].Body != "h2. Written and saved\n" {
		t.Errorf("comment calls = %#v, want the saved body posted once", client.AddCommentCalls)
	}
}
