package ui_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/nikhil-sharma-b/jira-tui/internal/jira"
	"github.com/nikhil-sharma-b/jira-tui/internal/ui"
)

func TestPinnedSessionStartsOnLiveFullWidthDetail(t *testing.T) {
	issue := detailedIssue()
	client := &fakeClient{issues: []jira.Issue{issue}}
	d := newPausedDriver(t, ui.Options{Client: client, Config: testConfig(t, nil), Pin: "ENG-1"})

	if view := d.view(); !strings.Contains(view, "Loading") || strings.Contains(view, "│") || strings.Contains(view, "Summary") {
		t.Fatalf("pinned startup is not a full-width detail load:\n%s", view)
	}
	d.flush()

	if view := d.view(); !strings.Contains(view, "Key: ENG-1") || strings.Contains(view, "│") {
		t.Errorf("pinned detail did not replace the full-width loader:\n%s", view)
	}
	if got := client.issueRequests(); len(got) != 1 || got[0] != "ENG-1" {
		t.Errorf("live detail requests = %v, want [ENG-1]", got)
	}
	if got := client.commentRequests(); len(got) != 1 || got[0] != "ENG-1" {
		t.Errorf("live comment requests = %v, want [ENG-1]", got)
	}
	if len(client.requests()) == 0 {
		t.Error("the hidden list did not continue its startup")
	}
}

func TestPinnedMissingItemIsExplicit(t *testing.T) {
	client := &fakeClient{issueErrFor: map[string]error{
		"ENG-404": &jira.Error{StatusCode: http.StatusNotFound, Op: "issue"},
	}}
	d := newPausedDriver(t, ui.Options{Client: client, Config: testConfig(t, nil), Pin: "ENG-404"})
	d.flush()

	if view := strings.ToLower(d.view()); !strings.Contains(view, "does not exist") {
		t.Errorf("missing pin has no explicit error:\n%s", d.view())
	}
}

func TestSemanticPaneJumpsTargetTheNamedPane(t *testing.T) {
	issues := sampleIssues(3)
	issues[0] = detailedIssue()
	issues[0].Description = longDescription()
	d := newDriver(t, &fakeClient{issues: issues}, testConfig(t, nil))
	d.keys("enter")

	d.keys("g", "l", "j")
	if got := d.selected(); got != "ENG-2" {
		t.Fatalf("gl did not send motion to the list; selection = %s", got)
	}
	d.keys("g", "d", "G")
	if !strings.Contains(d.view(), "Detail line Z") {
		t.Errorf("gd did not send motion to detail:\n%s", d.view())
	}
	if got := d.selected(); got != "ENG-2" {
		t.Errorf("detail motion moved list selection to %s", got)
	}
}

func TestGcAddressesCommentsFromListSplitZoomAndPin(t *testing.T) {
	issue := detailedIssue()
	issue.Description = longDescription()
	comment := jira.Comment{
		Author: &jira.User{DisplayName: "Mia Krystof"}, Created: issue.Created, Updated: issue.Created,
		Body: longCommentBody(),
	}

	t.Run("split from list focus", func(t *testing.T) {
		d := newDriver(t, &fakeClient{issues: []jira.Issue{issue}, comments: map[string][]jira.Comment{"ENG-1": {comment}}}, testConfig(t, nil))
		d.send(keyMsg("enter"))
		d.flush()
		d.keys("g", "l", "g", "c")
		if !strings.Contains(d.view(), "Comments") || !strings.Contains(d.view(), "Directly addressed comment") {
			t.Errorf("gc did not address comments from list focus:\n%s", d.view())
		}
	})

	t.Run("zoomed detail", func(t *testing.T) {
		d := newDriver(t, &fakeClient{issues: []jira.Issue{issue}, comments: map[string][]jira.Comment{"ENG-1": {comment}}}, testConfig(t, nil))
		d.keys("enter", "ctrl+w", "o", "g", "c")
		if first := strings.Split(d.view(), "\n")[0]; first != "Comments" {
			t.Errorf("gc did not put comments at the top of zoomed detail, first line %q:\n%s", first, d.view())
		}
	})

	t.Run("pinned detail", func(t *testing.T) {
		client := &fakeClient{issues: []jira.Issue{issue}, comments: map[string][]jira.Comment{"ENG-1": {comment}}}
		d := newPausedDriver(t, ui.Options{Client: client, Config: testConfig(t, nil), Pin: "ENG-1"})
		d.flush()
		d.keys("g", "c")
		if first := strings.Split(d.view(), "\n")[0]; first != "Comments" {
			t.Errorf("gc did not put comments at the top of pinned detail, first line %q:\n%s", first, d.view())
		}
	})
}

func TestGcWithoutAnOpenDetailIsANoop(t *testing.T) {
	d := listWith(t, 2)
	d.keys("g", "c", "j")
	if got := d.selected(); got != "ENG-2" {
		t.Errorf("gc without detail changed normal list motion; selection = %s", got)
	}
}

func TestGlRevealsPinnedListWithoutRefetchingDetail(t *testing.T) {
	issue := detailedIssue()
	client := &fakeClient{issues: []jira.Issue{issue}}
	d := newPausedDriver(t, ui.Options{Client: client, Config: testConfig(t, nil), Pin: "ENG-1"})
	d.flush()

	d.keys("g", "l")
	if view := d.view(); !strings.Contains(view, "│") || !strings.Contains(view, "Key: ENG-1") || !strings.Contains(view, "Summary") {
		t.Errorf("gl did not reveal the list beside the pinned detail:\n%s", view)
	}
	if got := client.issueRequests(); len(got) != 1 {
		t.Errorf("gl refetched pinned detail: %v", got)
	}
}

func TestSpatialPaneMovementOnlyMovesToAnAdjacentVisiblePane(t *testing.T) {
	issues := sampleIssues(2)
	issues[0] = detailedIssue()
	issues[0].Description = longDescription()
	d := newDriver(t, &fakeClient{issues: issues}, testConfig(t, nil))
	d.keys("enter")

	d.keys("ctrl+w", "h", "j")
	if got := d.selected(); got != "ENG-2" {
		t.Fatalf("Ctrl-w h did not focus the left pane; selection = %s", got)
	}
	d.keys("ctrl+w", "l", "G")
	if !strings.Contains(d.view(), "Detail line Z") {
		t.Errorf("Ctrl-w l did not focus the right pane:\n%s", d.view())
	}
}

func TestZoomTogglesTheFocusedPaneAndRestoresSplit(t *testing.T) {
	issue := detailedIssue()
	issue.Description = jira.RawDocument(`{"type":"doc","version":1,"content":[{"type":"paragraph","content":[{"type":"text","text":"This sentence fits at full width but has to wrap inside the narrower detail pane."}]}]}`)
	d := newDriver(t, &fakeClient{issues: []jira.Issue{issue}}, testConfig(t, nil))
	d.keys("enter")
	if strings.Contains(d.view(), "This sentence fits at full width but has to wrap inside the narrower detail pane.") {
		t.Fatalf("test detail did not wrap in the split:\n%s", d.view())
	}

	d.keys("ctrl+w", "o")
	if view := d.view(); strings.Contains(view, "│") || strings.Contains(view, "Summary") || !strings.Contains(view, "Key: ENG-1") {
		t.Errorf("detail did not zoom full width:\n%s", view)
	}
	if !strings.Contains(d.view(), "This sentence fits at full width but has to wrap inside the narrower detail pane.") {
		t.Errorf("zoom did not re-render detail at full width:\n%s", d.view())
	}
	d.keys("ctrl+w", "o")
	if !strings.Contains(d.view(), "│") {
		t.Errorf("repeated zoom did not restore the split:\n%s", d.view())
	}

	d.keys("g", "l", "ctrl+w", "o")
	if view := d.view(); strings.Contains(view, "│") || strings.Contains(view, "Key: ENG-1") || !strings.Contains(view, "Summary") {
		t.Errorf("list did not zoom full width:\n%s", view)
	}
	d.keys("ctrl+w", "o")
	if !strings.Contains(d.view(), "│") {
		t.Errorf("list zoom did not restore the split:\n%s", d.view())
	}
}

func TestJumplistMovesLiveAndTruncatesForwardHistory(t *testing.T) {
	client := &fakeClient{issues: sampleIssues(3)}
	d := newDriver(t, client, testConfig(t, nil))
	d.keys("enter", "g", "l", "j", "enter")

	d.keys("ctrl+o")
	if !strings.Contains(d.view(), "Key: ENG-1") {
		t.Fatalf("Ctrl-o did not return to ENG-1:\n%s", d.view())
	}
	d.keys("ctrl+i")
	if !strings.Contains(d.view(), "Key: ENG-2") {
		t.Fatalf("Ctrl-i did not move forward to ENG-2:\n%s", d.view())
	}

	d.keys("ctrl+o", "g", "l", "j", "enter")
	before := len(client.issueRequests())
	d.keys("ctrl+i")
	if got := len(client.issueRequests()); got != before {
		t.Errorf("forward history survived a new visit: requests = %v", client.issueRequests())
	}
	if !strings.Contains(d.view(), "Key: ENG-3") {
		t.Errorf("new visit after back did not stay on ENG-3:\n%s", d.view())
	}
	if got := client.commentRequests(); len(got) != len(client.issueRequests()) {
		t.Errorf("jumplist fetched %d issue details but %d comment sets: issues %v, comments %v", len(client.issueRequests()), len(got), client.issueRequests(), got)
	}
}

func TestJumplistEndsAreNoOps(t *testing.T) {
	client := &fakeClient{issues: sampleIssues(2)}
	d := newDriver(t, client, testConfig(t, nil))
	d.keys("enter")
	before := len(client.issueRequests())
	d.keys("ctrl+o")
	if got := len(client.issueRequests()); got != before {
		t.Errorf("back before the first visit fetched again: %v", client.issueRequests())
	}

	d.keys("g", "l", "j", "enter", "9", "ctrl+o")
	if !strings.Contains(d.view(), "Key: ENG-1") {
		t.Errorf("counted back did not stop at the first visit:\n%s", d.view())
	}
	before = len(client.issueRequests())
	d.keys("ctrl+o")
	if got := len(client.issueRequests()); got != before {
		t.Errorf("back at the first visit fetched again: %v", client.issueRequests())
	}
}

func TestClosingFocusedPaneLeavesTheOtherPane(t *testing.T) {
	issue := detailedIssue()
	d := newDriver(t, &fakeClient{issues: []jira.Issue{issue}}, testConfig(t, nil))
	d.keys("enter", "g", "l", "q")
	if view := d.view(); strings.Contains(view, "│") || strings.Contains(view, "Summary") || !strings.Contains(view, "Key: ENG-1") {
		t.Errorf("closing the list did not leave detail full width:\n%s", view)
	}

	d.keys("q")
	if view := d.view(); strings.Contains(view, "Key: ENG-1") || !strings.Contains(view, "Summary") {
		t.Errorf("closing detail did not restore the list:\n%s", view)
	}
}

func TestClosingAZoomedPaneRestoresTheOtherPane(t *testing.T) {
	for _, focus := range []string{"detail", "list"} {
		t.Run(focus, func(t *testing.T) {
			issue := detailedIssue()
			d := newDriver(t, &fakeClient{issues: []jira.Issue{issue}}, testConfig(t, nil))
			d.keys("enter")
			if focus == "list" {
				d.keys("g", "l")
			}
			d.keys("ctrl+w", "o", "q")

			view := d.view()
			if strings.Contains(view, "│") {
				t.Errorf("closing a zoomed pane restored a split:\n%s", view)
			}
			if focus == "detail" && !strings.Contains(view, "Summary") {
				t.Errorf("closing zoomed detail did not restore list:\n%s", view)
			}
			if focus == "list" && !strings.Contains(view, "Key: ENG-1") {
				t.Errorf("closing zoomed list did not restore detail:\n%s", view)
			}
		})
	}
}

func longDescription() jira.RawDocument {
	var paragraphs []string
	for r := 'A'; r <= 'Z'; r++ {
		paragraphs = append(paragraphs, `{"type":"paragraph","content":[{"type":"text","text":"Detail line `+string(r)+`"}]}`)
	}
	return jira.RawDocument(`{"type":"doc","version":1,"content":[` + strings.Join(paragraphs, ",") + `]}`)
}

func longCommentBody() jira.RawDocument {
	paragraphs := []string{`{"type":"paragraph","content":[{"type":"text","text":"Directly addressed comment"}]}`}
	for i := 0; i < 30; i++ {
		paragraphs = append(paragraphs, `{"type":"paragraph","content":[{"type":"text","text":"Comment tail"}]}`)
	}
	return jira.RawDocument(`{"type":"doc","version":1,"content":[` + strings.Join(paragraphs, ",") + `]}`)
}
