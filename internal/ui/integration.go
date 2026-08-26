package ui

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"runtime"

	"github.com/aymanbagabas/go-osc52/v2"
	tea "github.com/charmbracelet/bubbletea"
)

type integrationMsg struct {
	verb string
	key  string
	err  error
}

func (m *Model) copyFocused(url bool) tea.Cmd {
	key := m.focusedKey()
	if key == "" {
		m.status = errors.New("no focused work item")
		return nil
	}
	text := key
	if url {
		text = m.client.BrowseURL(key)
	}
	copyText := m.copyText
	return func() tea.Msg {
		return integrationMsg{verb: "copy", key: key, err: copyText(text)}
	}
}

func (m *Model) openFocusedInBrowser() tea.Cmd {
	key := m.focusedKey()
	if key == "" {
		m.status = errors.New("no focused work item")
		return nil
	}
	url := m.client.BrowseURL(key)
	openURL := m.openURL
	return func() tea.Msg {
		return integrationMsg{verb: "open", key: key, err: openURL(url)}
	}
}

func (m *Model) handleIntegration(msg integrationMsg) tea.Cmd {
	if msg.err != nil {
		m.status = fmt.Errorf("%s %s: %w", msg.verb, msg.key, msg.err)
	} else {
		m.status = nil
	}
	return nil
}

func copyToClipboard(text string) error {
	_, err := osc52.New(text).WriteTo(os.Stderr)
	return err
}

func openInBrowser(url string) error {
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
