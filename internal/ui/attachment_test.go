package ui_test

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/nikhil-sharma-b/jira-tui/internal/config"
	"github.com/nikhil-sharma-b/jira-tui/internal/jira"
	"github.com/nikhil-sharma-b/jira-tui/internal/ui"
)

// attachedIssue has two attachments and a description embedding three
// images: one of the attachments, one held by Atlassian's media service, and
// one linked from outside Jira.
func attachedIssue() jira.Issue {
	issue := detailedIssue()
	issue.Attachments = []jira.Attachment{
		{ID: "10", Filename: "trace.log", Size: 2048, Author: &jira.User{DisplayName: "Ada Lovelace"}},
		{ID: "11", Filename: "diagram.png", Size: 4096, Author: &jira.User{DisplayName: "Grace Hopper"}},
	}
	issue.Description = jira.RawDocument(`{"type":"doc","version":1,"content":[` +
		`{"type":"mediaSingle","content":[{"type":"media","attrs":{"id":"uuid-1","type":"file","collection":"","alt":"diagram.png"}}]},` +
		`{"type":"mediaSingle","content":[{"type":"media","attrs":{"id":"uuid-2","type":"file","collection":"contentId-1","alt":"remote.png"}}]},` +
		`{"type":"mediaSingle","content":[{"type":"media","attrs":{"type":"external","url":"https://example.test/cat.png","alt":"cat.png"}}]}` +
		`]}`)
	return issue
}

type attachmentHarness struct {
	*driver
	client *fakeClient
	dir    string
	opened []string
	urls   []string
}

func newAttachmentHarness(t *testing.T, client *fakeClient) *attachmentHarness {
	return newAttachmentHarnessWith(t, client, testConfig(t, nil))
}

func newAttachmentHarnessWith(t *testing.T, client *fakeClient, cfg *config.Config) *attachmentHarness {
	h := &attachmentHarness{client: client, dir: t.TempDir()}
	h.driver = newPausedDriver(t, ui.Options{
		Client:      client,
		Config:      cfg,
		DownloadDir: h.dir,
		OpenFile: func(path string) error {
			h.opened = append(h.opened, path)
			return nil
		},
		OpenURL: func(url string) error {
			h.urls = append(h.urls, url)
			return nil
		},
	})
	h.flush()
	return h
}

// files lists every regular file left under the download directory.
func (h *attachmentHarness) files() []string {
	var out []string
	filepath.WalkDir(h.dir, func(path string, e os.DirEntry, err error) error {
		if err == nil && !e.IsDir() {
			out = append(out, path)
		}
		return nil
	})
	return out
}

func TestGaJumpsToAttachmentsAndEnterOpensTheSelectedOne(t *testing.T) {
	client := &fakeClient{
		issues:    []jira.Issue{attachedIssue()},
		downloads: map[string]string{"10": "log lines", "11": "PNG bytes"},
	}
	h := newAttachmentHarness(t, client)
	h.keys("enter")
	h.flush()
	h.keys("g", "l", "g", "a")

	view := h.view()
	for _, want := range []string{"▸ trace.log", "2.0 kB", "Ada Lovelace", "diagram.png", "Embedded in description"} {
		if !strings.Contains(view, want) {
			t.Errorf("attachments tab does not show %q:\n%s", want, view)
		}
	}

	h.keys("j")
	if view := h.view(); !strings.Contains(view, "▸ diagram.png") {
		t.Fatalf("j did not select the next attachment:\n%s", view)
	}
	h.keys("enter")
	h.flush()

	if got := client.downloadRequests(); !slices.Equal(got, []string{"11"}) {
		t.Fatalf("downloads = %q, want the selected attachment only", got)
	}
	if len(h.opened) != 1 || filepath.Base(h.opened[0]) != "diagram.png" {
		t.Fatalf("opened %q, want the downloaded diagram.png", h.opened)
	}
	if got, _ := os.ReadFile(h.opened[0]); string(got) != "PNG bytes" {
		t.Errorf("downloaded file holds %q, want the attachment's content", got)
	}
	if status := h.statusLine(); !strings.Contains(status, "opened diagram.png") {
		t.Errorf("status line = %q, want it to report the open", status)
	}
}

func TestAFailedDownloadReportsWhyAndLeavesNoPartialFile(t *testing.T) {
	client := &fakeClient{
		issues:      []jira.Issue{attachedIssue()},
		downloads:   map[string]string{"10": "log lines"},
		downloadErr: errors.New("download attachment: 404 The attachment does not exist."),
	}
	h := newAttachmentHarness(t, client)
	h.keys("enter")
	h.flush()
	h.keys("g", "a", "enter")
	h.flush()

	if status := h.statusLine(); !strings.Contains(status, "attachment does not exist") {
		t.Errorf("status line = %q, want the reason for the failure", status)
	}
	if files := h.files(); len(files) != 0 {
		t.Errorf("a failed download left %q behind", files)
	}
	if len(h.opened) != 0 {
		t.Errorf("a failed download was opened: %q", h.opened)
	}
}

func TestADownloadShowsProgressAndEscCancelsIt(t *testing.T) {
	client := &fakeClient{
		issues:        []jira.Issue{attachedIssue()},
		downloads:     map[string]string{"10": "log lines"},
		downloadBlock: true,
	}
	h := newAttachmentHarness(t, client)
	h.keys("enter")
	h.flush()
	h.keys("g", "a")
	h.send(keyMsg("enter"))

	collect := h.runQueued()
	deadline := time.Now().Add(2 * time.Second)
	for !strings.Contains(h.statusLine(), "downloading trace.log") {
		if time.Now().After(deadline) {
			t.Fatalf("no progress on the status line: %q", h.statusLine())
		}
		time.Sleep(5 * time.Millisecond)
	}
	if status := h.statusLine(); !strings.Contains(status, "2.0 kB") {
		t.Errorf("status line = %q, want the size being downloaded", status)
	}

	h.send(keyMsg("esc"))
	for _, msg := range collect() {
		h.deliver(msg)
	}
	h.flush()

	if status := h.statusLine(); !strings.Contains(status, "cancelled") {
		t.Errorf("status line = %q, want the download reported cancelled", status)
	}
	if files := h.files(); len(files) != 0 {
		t.Errorf("a cancelled download left %q behind", files)
	}
	if len(h.opened) != 0 {
		t.Errorf("a cancelled download was opened: %q", h.opened)
	}
}

type runNested struct{ cmd tea.Cmd }

// runQueued starts every queued command on a goroutine of its own, so that a
// download blocked until it is cancelled runs while the test inspects the
// screen and presses keys. collect waits for them all, returning what they
// produced for the test to deliver.
func (h *attachmentHarness) runQueued() (collect func() []tea.Msg) {
	results := make(chan tea.Msg, 16)
	running := 0
	var run func(tea.Cmd)
	run = func(cmd tea.Cmd) {
		running++
		go func() {
			if msg, ok := cmd().(tea.BatchMsg); ok {
				for _, c := range msg {
					if c != nil {
						results <- runNested{c}
					}
				}
			} else {
				results <- msg
			}
			results <- nil
		}()
	}
	for _, cmd := range h.pending {
		run(cmd)
	}
	h.pending = nil
	return func() []tea.Msg {
		var got []tea.Msg
		for running > 0 {
			switch msg := (<-results).(type) {
			case nil:
				running--
			case runNested:
				run(msg.cmd)
			default:
				got = append(got, msg)
			}
		}
		return got
	}
}

func TestDescriptionImagesOpenTheSameWayOrSayWhyTheyCannot(t *testing.T) {
	client := &fakeClient{
		issues:    []jira.Issue{attachedIssue()},
		downloads: map[string]string{"11": "PNG bytes"},
	}
	h := newAttachmentHarness(t, client)
	h.keys("enter")
	h.flush()
	h.keys("g", "a", "j", "j")

	if view := h.view(); !strings.Contains(view, "▸ diagram.png") || !strings.Contains(view, "remote.png") {
		t.Fatalf("the embedded images are not listed and selectable:\n%s", view)
	}
	h.keys("enter")
	h.flush()
	if got := client.downloadRequests(); !slices.Equal(got, []string{"11"}) {
		t.Errorf("downloads = %q, want the attachment the image embeds", got)
	}

	h.keys("j", "enter")
	h.flush()
	if status := h.statusLine(); !strings.Contains(status, "Media Services") {
		t.Errorf("status line = %q, want the media service limitation named", status)
	}
	if got := client.downloadRequests(); len(got) != 1 {
		t.Errorf("a media service image was downloaded: %q", got)
	}

	h.keys("j", "enter")
	h.flush()
	if !slices.Equal(h.urls, []string{"https://example.test/cat.png"}) {
		t.Errorf("opened URLs = %q, want the external image's own URL", h.urls)
	}
}
