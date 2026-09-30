package ui

import (
	"context"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/nikhil-sharma-b/jira-tui/internal/jira"
	"golang.org/x/sys/unix"
)

// Exercise the real process handoff with the same wrapped output used by Run.
// A pipe on stdout makes terminal editors fall back to an 80x24 screen.
func TestEditorInheritsTerminalOutput(t *testing.T) {
	t.Run("terminal dimensions", func(t *testing.T) {
		testEditorTerminal(t, func(result string) ([]string, string, string) {
			return []string{"sh", "-c", `stty size <&1 > "$1"`, "editor"}, "", "53 160"
		})
	})
	t.Run("neovim command input", func(t *testing.T) {
		if _, err := exec.LookPath("nvim"); err != nil {
			t.Skip("nvim is not installed")
		}
		testEditorTerminal(t, func(result string) ([]string, string, string) {
			return []string{"nvim", "-u", "NONE", "-n", "-i", "NONE", "-c", "call writefile(['ready'], '" + result + ".ready')"},
				":call writefile([printf('%d %d', &lines, &columns)], '" + result + "')\r:qa!\r", "53 160"
		})
	})
}

func testEditorTerminal(t *testing.T, editor func(string) ([]string, string, string)) {
	t.Helper()
	fd, err := unix.Open("/dev/ptmx", unix.O_RDWR|unix.O_NOCTTY|unix.O_CLOEXEC, 0)
	if err != nil {
		t.Fatal(err)
	}
	master := os.NewFile(uintptr(fd), "ptmx")
	defer master.Close()
	if err := unix.IoctlSetPointerInt(fd, unix.TIOCSPTLCK, 0); err != nil {
		t.Fatal(err)
	}
	n, err := unix.IoctlGetInt(fd, unix.TIOCGPTN)
	if err != nil {
		t.Fatal(err)
	}
	slave, err := os.OpenFile("/dev/pts/"+strconv.Itoa(n), os.O_RDWR, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer slave.Close()
	if err := unix.IoctlSetWinsize(int(slave.Fd()), unix.TIOCSWINSZ, &unix.Winsize{Row: 53, Col: 160}); err != nil {
		t.Fatal(err)
	}
	go io.Copy(io.Discard, master)
	stdout := os.Stdout
	os.Stdout = slave
	defer func() { os.Stdout = stdout }()

	result := filepath.Join(t.TempDir(), "terminal-size")
	args, input, want := editor(result)
	m := &Model{editorCommand: args, editorExec: tea.ExecProcess}
	m.list.issues = []*jira.Issue{{Key: "ENG-1"}}
	cmd := m.handleEditorStart(editorStartMsg{operation: editorOperation{key: "ENG-1", path: result}})
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if input != "" {
		go func() {
			for {
				if _, err := os.Stat(result + ".ready"); err == nil {
					_, _ = master.WriteString(input)
					return
				}
				select {
				case <-ctx.Done():
					return
				case <-time.After(10 * time.Millisecond):
				}
			}
		}()
	}
	handoff := &editorTerminalModel{command: cmd}
	_, err = tea.NewProgram(handoff, tea.WithContext(ctx), tea.WithInput(slave), tea.WithOutput(&output{File: slave}), tea.WithAltScreen()).Run()
	if err != nil {
		t.Fatal(err)
	}
	if handoff.err != nil {
		t.Fatalf("editor could not query its terminal: %v", handoff.err)
	}
	size, err := os.ReadFile(result)
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(size)) != want {
		t.Fatalf("editor terminal size = %q, want %s", size, want)
	}
}

type editorTerminalModel struct {
	command tea.Cmd
	err     error
}

func (m *editorTerminalModel) Init() tea.Cmd { return m.command }
func (m *editorTerminalModel) View() string  { return "" }
func (m *editorTerminalModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if done, ok := msg.(editorDoneMsg); ok {
		m.err = done.err
		return m, tea.Quit
	}
	return m, nil
}
