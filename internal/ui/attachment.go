package ui

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/nikhil-sharma-b/jira-tui/internal/adf"
	"github.com/nikhil-sharma-b/jira-tui/internal/imageview"
	"github.com/nikhil-sharma-b/jira-tui/internal/jira"
)

// errMediaServices is why an image embedded through Atlassian's Media Services
// API cannot be opened: reading it takes a separate token exchange, which jt
// does not do. Saying so beats a download that fails for no stated reason.
var errMediaServices = errors.New("this image is held by Atlassian Media Services, which jt cannot download; open the item in the browser instead")

// openable is one entry on the Attachments tab: an attachment, or an image the
// description embeds. Exactly one of attachment, url and err says what Enter
// does with it -- download it, open it in the browser, or say why it cannot.
type openable struct {
	name       string
	meta       string
	embedded   bool
	attachment *jira.Attachment
	url        string
	err        error
}

// openables lists the item's attachments, then the images its description
// embeds. An embedded image names its attachment by filename, since the id it
// carries is a media-store id that the attachment endpoint does not know.
func (d *detailPane) openables() []openable {
	var items []openable
	byName := make(map[string]*jira.Attachment)
	named := make(map[string]int)
	for index := range d.issue.Attachments {
		a := &d.issue.Attachments[index]
		byName[a.Filename] = a
		named[a.Filename]++
		meta := attachmentSize(a.Size) + " · " + valueOr(a.MimeType, "unknown type") +
			" · " + userName(a.Author, "Unknown author") + " · " + attachmentTimestamp(a.Created)
		items = append(items, openable{name: a.Filename, meta: meta, attachment: a})
	}
	if d.issue.Description.IsEmpty() {
		return items
	}
	media, err := adf.MediaNodes(d.issue.Description)
	if err != nil {
		return items
	}
	for _, m := range media {
		item := openable{name: m.Filename, embedded: true}
		switch a := byName[m.Filename]; {
		case m.URL != "":
			item.url, item.meta = m.URL, "linked from "+hostOf(m.URL)
		case m.IsAttachment && named[m.Filename] > 1:
			item.err = fmt.Errorf("several attachments are named %s; open the one wanted from the list above", m.Filename)
			item.meta = "ambiguous: several attachments share this name"
		case m.IsAttachment && a != nil:
			item.attachment, item.meta = a, "attachment · "+attachmentSize(a.Size)
		case m.IsAttachment:
			item.err = fmt.Errorf("%s is not among this item's attachments", m.Filename)
			item.meta = "no matching attachment"
		default:
			item.err, item.meta = errMediaServices, "Atlassian Media Services; not downloadable from jt"
		}
		items = append(items, item)
	}
	return items
}

func hostOf(raw string) string {
	if u, err := url.Parse(raw); err == nil && u.Host != "" {
		return u.Host
	}
	return raw
}

// download is the one attachment being fetched. written is advanced by the
// command writing the file and read by the status line, hence atomic.
type download struct {
	request uint64
	name    string
	total   int64
	written atomic.Int64
	cancel  context.CancelFunc
}

// downloadMsg is how a download ended. A preview's file is decoded into
// image and removed, where any other is handed to the system opener.
type downloadMsg struct {
	request uint64
	name    string
	path    string
	err     error
	openErr error
	preview bool
	image   *imageview.Image
}

type downloadTickMsg struct{ request uint64 }

// openSelected is Enter on the Attachments tab.
func (m *Model) openSelected() tea.Cmd {
	item, ok := m.detail.selectedItem()
	if !ok {
		return nil
	}
	switch {
	case item.err != nil:
		m.status = fmt.Errorf("open %s: %w", item.name, item.err)
		return nil
	case item.url != "":
		openURL, generation, target := m.openURL, m.noticeGeneration, item.url
		return func() tea.Msg {
			return integrationMsg{verb: "open", key: item.name, success: "opened " + item.name, generation: generation, err: openURL(target)}
		}
	case m.downloading():
		return nil
	}
	return m.startDownload(*item.attachment, thenOpen)
}

// downloading reports, on the status line, that a download is already
// running: there is one at a time, so that Esc has one thing to cancel.
func (m *Model) downloading() bool {
	if m.download == nil {
		return false
	}
	m.status = fmt.Errorf("%s is still downloading; Esc cancels it", m.download.name)
	return true
}

// afterDownload is what becomes of a downloaded attachment.
type afterDownload int

const (
	// thenOpen hands the file to the system opener.
	thenOpen afterDownload = iota
	// thenPreview decodes it for the preview overlay and removes it.
	thenPreview
)

// startDownload fetches an attachment, then does with it what then says.
func (m *Model) startDownload(a jira.Attachment, then afterDownload) tea.Cmd {
	preview := then == thenPreview
	m.downloadRequest++
	ctx, cancel := context.WithCancel(context.Background())
	dl := &download{request: m.downloadRequest, name: a.Filename, total: a.Size, cancel: cancel}
	m.download, m.status, m.notice = dl, nil, ""
	client, dir, openFile := m.client, m.downloadDir, m.openFile
	kind := m.renderer.kind
	fetch := func() tea.Msg {
		defer cancel()
		msg := downloadMsg{request: dl.request, name: a.Filename, preview: preview}
		msg.path, msg.err = fetchAttachment(ctx, client, dir, a, &dl.written)
		if msg.err == nil && ctx.Err() != nil {
			// Esc landed after the last byte: the user has said no to the
			// viewer, so the file goes the way a cancelled download's would.
			os.RemoveAll(filepath.Dir(msg.path))
			msg.path, msg.err = "", ctx.Err()
		}
		switch {
		case msg.err != nil:
		case preview:
			// The overlay holds the decoded pixels, so the file has done its
			// job the moment they are read.
			msg.image, msg.err = decodeImage(msg.path, kind)
			os.RemoveAll(filepath.Dir(msg.path))
			msg.path = ""
		default:
			msg.openErr = openFile(msg.path)
		}
		return msg
	}
	return tea.Batch(fetch, downloadTick(dl.request))
}

// fetchAttachment writes the attachment into a fresh directory of its own, so
// the file keeps its name -- which is what the system opener picks a viewer
// by -- without two downloads of the same name colliding. Whatever goes wrong,
// including being cancelled, takes the directory and the partial file with it.
func fetchAttachment(ctx context.Context, client jira.Client, dir string, a jira.Attachment, written *atomic.Int64) (string, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	folder, err := os.MkdirTemp(dir, "jt-")
	if err != nil {
		return "", err
	}
	path := filepath.Join(folder, safeFilename(a.Filename))
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		os.RemoveAll(folder)
		return "", err
	}
	_, err = client.DownloadAttachment(ctx, a.ID, progressWriter{f: f, written: written})
	if closeErr := f.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		os.RemoveAll(folder)
		return "", err
	}
	return path, nil
}

// safeFilename keeps a filename from the server from naming anything outside
// the directory it is written into.
func safeFilename(name string) string {
	name = filepath.Base(strings.ReplaceAll(name, "\\", "/"))
	if name == "." || name == "/" || name == ".." || name == "" {
		return "attachment"
	}
	return name
}

type progressWriter struct {
	f       *os.File
	written *atomic.Int64
}

func (w progressWriter) Write(p []byte) (int, error) {
	n, err := w.f.Write(p)
	w.written.Add(int64(n))
	return n, err
}

func downloadTick(request uint64) tea.Cmd {
	return tea.Tick(100*time.Millisecond, func(time.Time) tea.Msg { return downloadTickMsg{request: request} })
}

// handleDownloadTick only keeps the screen redrawing while bytes arrive; the
// progress itself is read from the download when the status line is drawn.
func (m *Model) handleDownloadTick(msg downloadTickMsg) tea.Cmd {
	if m.download == nil || m.download.request != msg.request {
		return nil
	}
	return downloadTick(msg.request)
}

func (m *Model) handleDownload(msg downloadMsg) tea.Cmd {
	if m.download == nil || m.download.request != msg.request {
		// Cancelled: the command has already cleaned up after itself.
		return nil
	}
	m.download = nil
	switch {
	case msg.err != nil && msg.preview:
		m.status = fmt.Errorf("preview %s: %w", msg.name, msg.err)
	case msg.preview:
		// Whatever was opened while the image downloaded would sit under the
		// overlay taking keys blind, so it goes, as help does.
		m.help.Hide()
		m.closePicker()
		m.closePrompt()
		m.status = nil
		return m.openPreview(msg.name, msg.image)
	case msg.err != nil:
		m.status = fmt.Errorf("download %s: %w", msg.name, msg.err)
	case msg.openErr != nil:
		m.status = fmt.Errorf("open %s (downloaded to %s): %w", msg.name, msg.path, msg.openErr)
	default:
		m.status, m.notice = nil, "opened "+msg.name
	}
	return nil
}

// cancelDownload is Esc while a download runs. The command removes the partial
// file once its context is cancelled; its result is then ignored.
func (m *Model) cancelDownload() {
	if m.download == nil {
		return
	}
	m.download.cancel()
	m.status, m.notice = nil, "download of "+m.download.name+" cancelled"
	m.download = nil
}

func (m *Model) downloadProgress() string {
	dl := m.download
	progress := attachmentSize(dl.written.Load())
	if dl.total > 0 {
		progress += " / " + attachmentSize(dl.total)
	}
	return "downloading " + dl.name + " " + progress + " · Esc cancels"
}

func defaultDownloadDir() string {
	return filepath.Join(os.TempDir(), "jt-attachments")
}
