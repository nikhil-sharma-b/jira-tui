package ui

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"runtime"
	"strings"

	"github.com/aymanbagabas/go-osc52/v2"
	tea "github.com/charmbracelet/bubbletea"
)

type integrationMsg struct {
	verb       string
	key        string
	success    string
	generation uint64
	err        error
}

func (m *Model) copyFocused(url bool) tea.Cmd {
	if m.focus == PaneDetail && m.detail.open && m.detail.tab == tabComments {
		comment, ok := m.detail.selectedComment()
		if !ok {
			m.status = errors.New("no selected comment")
			return nil
		}
		text, success := comment.ID, "yanked comment ID for "+comment.ID
		if url {
			text, success = m.commentURL(comment.ID), "yanked comment URL for "+comment.ID
		}
		return m.copyValue("comment "+comment.ID, text, success)
	}
	key := m.focusedKey()
	if m.focus == PaneDetail && m.detail.open && m.detail.relatedTab() {
		key = m.detail.selectedRelatedKey()
	}
	if key == "" {
		m.status = errors.New("no focused work item")
		return nil
	}
	text := key
	if url {
		text = m.client.BrowseURL(key)
	}
	success := fmt.Sprintf("yanked key for %s", key)
	if url {
		success = fmt.Sprintf("yanked URL for %s", key)
	}
	return m.copyValue(key, text, success)
}

func (m *Model) copyValue(key, text, success string) tea.Cmd {
	copyText := m.copyText
	generation := m.noticeGeneration
	return func() tea.Msg {
		return integrationMsg{
			verb: "copy", key: key, success: success, generation: generation,
			err: copyText(text),
		}
	}
}

func (m *Model) commentURL(id string) string {
	return m.client.BrowseURL(m.detail.key) + "?focusedCommentId=" + url.QueryEscape(id)
}

func (m *Model) openFocusedInBrowser() tea.Cmd {
	key := m.focusedKey()
	if m.focus == PaneDetail && m.detail.open && m.detail.relatedTab() {
		key = m.detail.selectedRelatedKey()
	}
	if m.focus == PaneDetail && m.detail.open && m.detail.tab == tabComments {
		comment, ok := m.detail.selectedComment()
		if !ok {
			m.status = errors.New("no selected comment")
			return nil
		}
		return m.openBrowserURL("comment "+comment.ID, m.commentURL(comment.ID))
	}
	if key == "" {
		m.status = errors.New("no focused work item")
		return nil
	}
	return m.openBrowserURL(key, m.client.BrowseURL(key))
}

func (m *Model) openBrowserURL(key, url string) tea.Cmd {
	openURL := m.openURL
	return func() tea.Msg {
		return integrationMsg{verb: "open", key: key, err: openURL(url)}
	}
}

func (m *Model) handleIntegration(msg integrationMsg) tea.Cmd {
	if msg.err != nil {
		m.notice = ""
		m.status = fmt.Errorf("%s %s: %w", msg.verb, msg.key, msg.err)
	} else {
		m.status = nil
		if msg.generation == m.noticeGeneration {
			m.notice = msg.success
		}
	}
	return nil
}

// errNoClipboard is what a yank reports when nothing can reach a clipboard.
var errNoClipboard = errors.New("no clipboard tool available: install wl-copy, xclip or xsel")

// copyToClipboard prefers the desktop's own clipboard tool, whose success
// means the text arrived. Without one -- over ssh, typically -- it falls back
// to an OSC 52 sequence, which the terminal (and tmux, with set-clipboard on)
// carries to the clipboard of the machine the user is sitting at.
func copyToClipboard(text string) error {
	tool, err := clipboardTool(runtime.GOOS, os.Getenv, exec.LookPath)
	if err != nil {
		return err
	}
	if tool == nil {
		_, err := osc52.New(text).WriteTo(os.Stderr)
		return err
	}
	command := exec.Command(tool[0], tool[1:]...)
	command.Stdin = strings.NewReader(text)
	// Output stays unattached: wl-copy and xclip leave a child behind to own
	// the selection, and a pipe that child inherits would hold the yank open
	// until something else took the clipboard.
	if err := command.Run(); err != nil {
		return fmt.Errorf("%s: %w", tool[0], err)
	}
	return nil
}

// clipboardTool chooses the command that writes stdin to the clipboard. nil
// with no error means OSC 52, which needs only a terminal to write it to.
func clipboardTool(goos string, getenv func(string) string, lookPath func(string) (string, error)) ([]string, error) {
	var candidates [][]string
	switch goos {
	case "darwin":
		candidates = [][]string{{"pbcopy"}}
	case "windows":
		candidates = [][]string{{"clip"}}
	default:
		if getenv("WAYLAND_DISPLAY") != "" {
			candidates = append(candidates, []string{"wl-copy"})
		}
		if getenv("DISPLAY") != "" {
			candidates = append(candidates,
				[]string{"xclip", "-selection", "clipboard"},
				[]string{"xsel", "--clipboard", "--input"})
		}
	}
	for _, c := range candidates {
		if _, err := lookPath(c[0]); err == nil {
			return c, nil
		}
	}
	if term := getenv("TERM"); term != "" && term != "dumb" {
		return nil, nil
	}
	return nil, errNoClipboard
}

// openWithSystem hands a URL or a file path to the platform opener, which picks
// the browser for one and the viewer for the other.
func openWithSystem(url string) error {
	var command *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		command = exec.Command("open", url)
	case "windows":
		command = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	default:
		command = exec.Command("xdg-open", url)
	}
	return command.Run()
}
