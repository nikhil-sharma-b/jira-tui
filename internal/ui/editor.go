package ui

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/nikhil-sharma-b/jira-tui/internal/adf"
)

// EditorExec runs an editor process while Bubble Tea owns terminal release
// and restoration. Tests replace it with an adapter that edits the scratch
// file without opening a terminal program.
type EditorExec func(*exec.Cmd, tea.ExecCallback) tea.Cmd

type writeKind uint8

const (
	writeComment writeKind = iota
	writeDescription
)

type draftKey struct {
	kind writeKind
	key  string
}

type editorOperation struct {
	request       uint64
	detailRequest uint64
	kind          writeKind
	key           string
	initial       string
	path          string
}

type editorStartMsg struct {
	operation editorOperation
	err       error
}

type editorDoneMsg struct {
	operation editorOperation
	err       error
}

type editorReadMsg struct {
	operation editorOperation
	body      string
	err       error
}

type writeDoneMsg struct {
	operation editorOperation
	body      string
	err       error
}

func (m *Model) beginWrite(kind writeKind) tea.Cmd {
	key := m.focusedKey()
	if key == "" {
		m.status = errors.New("no focused work item")
		return nil
	}
	if kind == writeDescription && (m.detail.issue == nil || m.detail.issue.Key != key) {
		m.pendingDescription = key
		cmd := m.openKey(key, true)
		m.pendingDescriptionRequest = m.detail.request
		return cmd
	}
	return m.openEditor(kind, key)
}

// cancelPendingWrite abandons an editor that has been asked for but has not
// opened yet -- a description edit still waiting on its live read, or a scratch
// file still being created. Both windows are short, and both end with the
// terminal being handed away, so Esc landing in one of them has to mean what it
// means everywhere else rather than an editor appearing a moment later.
//
// It deliberately stops there. Once the editor has opened, what comes back is
// text the user wrote and saved, and the window between quitting the editor and
// the write landing is exactly where a keystroke struck at a returning terminal
// arrives. Cancelling there would discard work silently, which is a worse
// outcome than an Esc that did nothing.
func (m *Model) cancelPendingWrite() {
	m.pendingDescription = ""
	if !m.editorOpening {
		return
	}
	// The counter is what every in-flight step checks itself against, so moving
	// it is the whole of the cancellation: the scratch file that lands after it
	// is removed rather than opened.
	m.editorOpening = false
	m.editorRequest++
}

func (m *Model) openEditor(kind writeKind, key string) tea.Cmd {
	initial := m.drafts[draftKey{kind: kind, key: key}]
	if initial == "" && kind == writeDescription && m.detail.issue != nil && m.detail.issue.Key == key {
		var err error
		initial, err = editableDescription(m.detail.issue.Description)
		if err != nil {
			m.status = fmt.Errorf("edit description: %w", err)
			return nil
		}
	}
	m.editorRequest++
	m.editorOpening = true
	operation := editorOperation{
		request: m.editorRequest, detailRequest: m.detail.request,
		kind: kind, key: key, initial: initial,
	}
	return createScratch(operation)
}

func createScratch(operation editorOperation) tea.Cmd {
	return func() tea.Msg {
		file, err := os.CreateTemp("", "jt-editor-*.jira")
		if err != nil {
			return editorStartMsg{operation: operation, err: err}
		}
		operation.path = file.Name()
		if _, err = file.WriteString(operation.initial); err == nil {
			err = file.Close()
		} else {
			_ = file.Close()
		}
		if err != nil {
			_ = os.Remove(operation.path)
		}
		return editorStartMsg{operation: operation, err: err}
	}
}

func (m *Model) handleEditorStart(msg editorStartMsg) tea.Cmd {
	op := msg.operation
	if op.request != m.editorRequest || op.detailRequest != m.detail.request || m.focusedKey() != op.key {
		if op.path != "" {
			_ = os.Remove(op.path)
		}
		return nil
	}
	// From here the terminal is handed away and whatever comes back is text the
	// user wrote, so this operation has left the window Esc can cancel.
	m.editorOpening = false
	if msg.err != nil {
		m.status = fmt.Errorf("open editor scratch file: %w", msg.err)
		return nil
	}
	args := append(append([]string(nil), m.editorCommand[1:]...), op.path)
	command := exec.Command(m.editorCommand[0], args...)
	return m.editorExec(command, func(err error) tea.Msg {
		return editorDoneMsg{operation: op, err: err}
	})
}

func (m *Model) handleEditorDone(msg editorDoneMsg) tea.Cmd {
	op := msg.operation
	if op.request != m.editorRequest || msg.err != nil {
		_ = os.Remove(op.path)
		if op.request == m.editorRequest && msg.err != nil {
			m.status = fmt.Errorf("editor exited without saving: %w", msg.err)
		}
		return nil
	}
	return func() tea.Msg {
		body, err := os.ReadFile(op.path)
		removeErr := os.Remove(op.path)
		if err == nil {
			err = removeErr
		}
		return editorReadMsg{operation: op, body: string(body), err: err}
	}
}

func (m *Model) handleEditorRead(msg editorReadMsg) tea.Cmd {
	op := msg.operation
	if op.request != m.editorRequest {
		return nil
	}
	if msg.err != nil {
		m.status = fmt.Errorf("read editor scratch file: %w", msg.err)
		return nil
	}
	if msg.body == "" || msg.body == op.initial {
		return nil
	}
	client := m.client
	op.detailRequest = m.detail.request
	return func() tea.Msg {
		var err error
		if op.kind == writeComment {
			_, err = client.AddComment(context.Background(), op.key, msg.body)
		} else {
			err = client.SetDescription(context.Background(), op.key, msg.body)
		}
		return writeDoneMsg{operation: op, body: msg.body, err: err}
	}
}

func (m *Model) handleWriteDone(msg writeDoneMsg) tea.Cmd {
	op := msg.operation
	draft := draftKey{kind: op.kind, key: op.key}
	if msg.err != nil {
		m.drafts[draft] = msg.body
		m.status = fmt.Errorf("%s %s: %w", writeVerb(op.kind), op.key, msg.err)
		return nil
	}
	delete(m.drafts, draft)
	m.status = nil
	if op.request != m.editorRequest || op.detailRequest != m.detail.request || m.focusedKey() != op.key {
		return nil
	}
	return m.openKey(op.key, false)
}

func writeVerb(kind writeKind) string {
	if kind == writeComment {
		return "add comment to"
	}
	return "set description on"
}

// editableDescription converts the v3 ADF read model into the v2 wiki markup
// accepted by description writes. The conversion belongs to the ADF module,
// rather than treating terminal rendering as editable source.
func editableDescription(doc []byte) (string, error) {
	if len(doc) == 0 || string(doc) == "null" {
		return "", nil
	}
	return adf.WikiMarkup(doc)
}

var _ EditorExec = tea.ExecProcess
