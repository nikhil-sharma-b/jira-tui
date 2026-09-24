package ui

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/nikhil-sharma-b/jira-tui/internal/config"
)

// The shell escape hatch is how anything jt does not implement stays
// reachable: :!<cmd> hands the terminal to a shell command, and what it wrote
// comes back in a pager. :acli is the same thing with the Atlassian CLI's
// name already typed, since that is where creating a work item is delegated.

// shellExecDefault releases the terminal to the command and restores it
// afterwards, as the editor does, so a command that takes over the screen
// leaves it as it found it. What the command writes goes to the terminal as
// it runs and to the pager once it is done.
func shellExecDefault(command *exec.Cmd, done tea.ExecCallback) tea.Cmd {
	command.Stdout = io.MultiWriter(os.Stdout, command.Stdout)
	command.Stderr = io.MultiWriter(os.Stderr, command.Stderr)
	return tea.ExecProcess(command, done)
}

// shellDoneMsg carries a finished command back with everything it wrote.
type shellDoneMsg struct {
	line   string
	output string
	err    error
}

// pager shows one command's output until it is dismissed.
type pager struct {
	line  string
	lines []string
	// status is the exit status of a command that failed, empty otherwise.
	status string
	top    int
}

// lockedBuffer collects stdout and stderr in the order they were written. The
// two are written from separate goroutines when they are not files.
type lockedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// runShell is :!. The line is run by sh as typed, with % naming the focused
// work item.
func runShell(m *Model, arg string) tea.Cmd {
	if arg == "" {
		m.status = errors.New("!: needs a command")
		return nil
	}
	line, err := expandPercent(arg, m.focusedKey())
	if err != nil {
		m.status = err
		return nil
	}
	return m.startShell(line)
}

// runAcli is :acli, which passes its arguments to the Atlassian CLI. It is
// looked up first so that a missing CLI is reported by name rather than as
// whatever sh says about a command it cannot find.
func runAcli(m *Model, arg string) tea.Cmd {
	if _, err := exec.LookPath("acli"); err != nil {
		m.status = errors.New("acli is not installed: it was not found on PATH")
		return nil
	}
	return runShell(m, strings.TrimSpace("acli "+arg))
}

// expandPercent replaces each % with key, and each \% with a literal %, as
// vim's commandline does. A % with no work item to name is refused rather
// than expanded to nothing, which would hand the command a different meaning.
func expandPercent(line, key string) (string, error) {
	var b strings.Builder
	for i := 0; i < len(line); i++ {
		switch {
		case line[i] == '\\' && i+1 < len(line) && line[i+1] == '%':
			b.WriteByte('%')
			i++
		case line[i] == '%':
			if key == "" {
				return "", errors.New("no work item for % to expand to")
			}
			b.WriteString(key)
		default:
			b.WriteByte(line[i])
		}
	}
	return b.String(), nil
}

func (m *Model) startShell(line string) tea.Cmd {
	out := &lockedBuffer{}
	command := exec.Command("sh", "-c", line)
	command.Stdout = out
	command.Stderr = out
	return m.shellExec(command, func(err error) tea.Msg {
		return shellDoneMsg{line: line, output: out.String(), err: err}
	})
}

func (m *Model) handleShellDone(msg shellDoneMsg) tea.Cmd {
	p := &pager{line: msg.line}
	output := strings.TrimRight(strings.ReplaceAll(msg.output, "\r\n", "\n"), "\n")
	if output != "" {
		// Colour and cursor movement were for the terminal the command ran
		// in; inside a frame they would only break it.
		for _, l := range strings.Split(output, "\n") {
			p.lines = append(p.lines, strings.ReplaceAll(ansi.Strip(l), "\t", "    "))
		}
	}
	if msg.err != nil {
		// An ExitError reads "exit status 3", or "signal: killed" for a
		// command that never got to exit.
		p.status = msg.err.Error()
	}
	m.help.Hide()
	m.pager = p
	return nil
}

// pagerRows is how many output lines fit inside the pager's frame.
func (m *Model) pagerRows() int { return inner(max(m.height-1, 0)) }

func (m *Model) handlePagerAction(action config.Action, count int) tea.Cmd {
	p := m.pager
	rows := m.pagerRows()
	last := max(len(p.lines)-rows, 0)
	switch action {
	case config.ActionNormalMode, config.ActionClosePane:
		m.pager = nil
	case config.ActionQuit:
		m.pager = nil
		return tea.Quit
	case config.ActionReload:
		// The command may have changed the item, which is the usual reason to
		// run one, so R here means look again at what it did.
		m.pager = nil
		return m.reload()
	case config.ActionDown:
		p.top += max(count, 1)
	case config.ActionUp:
		p.top -= max(count, 1)
	case config.ActionHalfPageDown:
		p.top += max(rows/2, 1)
	case config.ActionHalfPageUp:
		p.top -= max(rows/2, 1)
	case config.ActionTop:
		p.top = 0
	case config.ActionBottom:
		p.top = last
	}
	p.top = min(max(p.top, 0), last)
	return nil
}

func (m *Model) pagerView() string {
	p := m.pager
	rows := m.pagerRows()
	body := p.lines
	if len(body) == 0 {
		body = []string{noteStyle.Render("(no output)")}
	}
	body = body[min(p.top, len(body)):]
	lines := boxed(body, m.width, max(m.height-1, 0), "$ "+p.line, "", true)

	footer := hintStyle.Render("q close · R reload")
	if p.status != "" {
		footer = errorStyle.Render(p.status) + "  " + footer
	}
	if len(p.lines) > rows {
		footer += noteStyle.Render(fmt.Sprintf("  %d-%d/%d", p.top+1, min(p.top+rows, len(p.lines)), len(p.lines)))
	}
	return strings.Join(append(lines, m.fit(footer)), "\n")
}
