package ui

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/nikhil-sharma-b/jira-tui/internal/jira"
)

// Moving a work item through its workflow is three separate things that share
// one fetch: the picker, `:transition <name>`, and a key bound straight to a
// name. All three ask Jira what is available at the moment the key is pressed,
// because availability depends on the item's current status and on the
// caller's permissions -- a remembered answer would be wrong exactly on the
// item being worked on. None of this goes near internal/cache.

// transitionIntent says what a fetch of the available transitions was for,
// since the same live read serves the picker, a named application, and the
// commandline's completion.
type transitionIntent uint8

const (
	intentPicker transitionIntent = iota
	intentName
	intentComplete
)

// transitionsMsg carries one live read of what a work item can do now. key and
// request are echoed back so a response that outlived the question -- the
// picker was closed, the selection moved, another transition was started -- can
// be recognised and dropped.
type transitionsMsg struct {
	request     uint64
	key         string
	intent      transitionIntent
	name        string
	transitions []jira.Transition
	err         error
}

// transitionDoneMsg reports what applying a transition came to. label is the
// transition's name, kept for the message: by the time this lands the picker
// that displayed it is gone.
type transitionDoneMsg struct {
	request       uint64
	detailRequest uint64
	key           string
	label         string
	err           error
}

// beginTransitionPicker opens the picker on the focused work item. The
// dispatcher is already in picker mode by the time this runs, so every path out
// of here -- no focused item, a failed fetch, an item with nothing available --
// has to put it back.
func (m *Model) beginTransitionPicker() tea.Cmd {
	key := m.focusedKey()
	if key == "" {
		m.closePicker()
		m.status = errors.New("no focused work item")
		return nil
	}
	// A picker under a full-screen overlay is a picker chosen from blind.
	m.help.Hide()
	m.status = nil
	m.picker = &picker{title: "Transition " + key, key: key, loading: true}
	m.resizePanes()
	return m.fetchTransitions(key, intentPicker, "")
}

// beginNamedTransition applies a transition by name, from the commandline or
// from a key bound to it. The name is resolved against a live fetch rather than
// against anything remembered, so a binding means "this transition if it is
// available now" and never "this transition id".
func (m *Model) beginNamedTransition(name string) tea.Cmd {
	key := m.focusedKey()
	if key == "" {
		m.status = errors.New("no focused work item")
		return nil
	}
	m.status = nil
	return m.fetchTransitions(key, intentName, name)
}

func (m *Model) fetchTransitions(key string, intent transitionIntent, name string) tea.Cmd {
	m.transitionRequest++
	request, client := m.transitionRequest, m.client
	return func() tea.Msg {
		transitions, err := client.Transitions(context.Background(), key)
		return transitionsMsg{
			request: request, key: key, intent: intent, name: name,
			transitions: transitions, err: err,
		}
	}
}

// closePicker puts the picker away and the dispatcher back in normal mode, and
// makes any fetch still in flight for it harmless. Cancelling and choosing both
// come through here, so neither can leave the modal state behind.
func (m *Model) closePicker() {
	m.picker = nil
	m.transitionRequest++
	m.dispatch.Reset()
	m.resizePanes()
}

func (m *Model) handleTransitions(msg transitionsMsg) tea.Cmd {
	if msg.request != m.transitionRequest {
		// Superseded: the picker was closed, the focus moved, or another
		// transition was asked for while this was in flight.
		return nil
	}
	switch msg.intent {
	case intentPicker:
		return m.showTransitions(msg)
	case intentName:
		return m.applyNamedTransition(msg)
	case intentComplete:
		return m.completeTransitionLine(msg)
	}
	return nil
}

// showTransitions fills the open picker, or explains why there is nothing to
// choose from. An empty workflow closes the picker rather than presenting an
// empty list to filter.
func (m *Model) showTransitions(msg transitionsMsg) tea.Cmd {
	if m.picker == nil || m.picker.key != msg.key {
		return nil
	}
	switch {
	case msg.err != nil:
		m.closePicker()
		m.status = transitionsFetchError(msg.key, msg.err)
	case len(msg.transitions) == 0:
		m.closePicker()
		m.status = fmt.Errorf("%s has no available transitions", msg.key)
	default:
		m.picker.setItems(transitionItems(msg.transitions))
		m.resizePanes()
	}
	return nil
}

// transitionsFetchError is how a failed live read of the available
// transitions reads, wherever it is reported: what was being asked, of which
// item, and what the site said back.
func transitionsFetchError(key string, err error) error {
	return fmt.Errorf("transitions on %s: %w", key, err)
}

// transitionItems labels each choice with the destination when it is not
// simply the transition's own name, because two transitions can be named alike
// and differ only in where they land.
func transitionItems(transitions []jira.Transition) []pickerItem {
	items := make([]pickerItem, 0, len(transitions))
	for _, t := range transitions {
		label := t.Name
		if to := strings.TrimSpace(t.To.Name); to != "" && to != t.Name {
			label += " → " + to
		}
		items = append(items, pickerItem{id: t.ID, label: label})
	}
	return items
}

// chooseFromPicker applies what the picker has selected. It sends the
// transition id, never the status name that was displayed. It is reached only
// once the picker reports a submission, which it does only when there is
// something selected to submit.
func (m *Model) chooseFromPicker() tea.Cmd {
	item, ok := m.picker.selected()
	key := m.picker.key
	if !ok {
		return nil
	}
	m.closePicker()
	return m.applyTransition(key, item.id, item.label)
}

func (m *Model) applyNamedTransition(msg transitionsMsg) tea.Cmd {
	if msg.err != nil {
		m.status = transitionsFetchError(msg.key, msg.err)
		return nil
	}
	chosen, err := resolveTransition(msg.transitions, msg.name, msg.key)
	if err != nil {
		m.status = err
		return nil
	}
	return m.applyTransition(msg.key, chosen.ID, chosen.Name)
}

// resolveTransition finds the one transition a typed name means. An exactly
// spelled name wins outright; otherwise case is ignored, which is what lets a
// user type what they remember rather than what Jira capitalised. Two
// candidates are refused rather than guessed between, and none lists what the
// item can actually do.
func resolveTransition(available []jira.Transition, name, key string) (jira.Transition, error) {
	var exact, folded []jira.Transition
	for _, t := range available {
		switch {
		case t.Name == name:
			exact = append(exact, t)
		case strings.EqualFold(t.Name, name):
			folded = append(folded, t)
		}
	}
	matches := exact
	if len(matches) == 0 {
		matches = folded
	}
	switch {
	case len(matches) == 1:
		return matches[0], nil
	case len(matches) > 1:
		return jira.Transition{}, fmt.Errorf("%s has more than one transition named %q; use the picker", key, name)
	default:
		return jira.Transition{}, fmt.Errorf("%s has no transition named %q; available: %s",
			key, name, strings.Join(transitionNames(available), ", "))
	}
}

// transitionNames lists what is available, sorted, so the same workflow reads
// the same way every time it is reported.
func transitionNames(available []jira.Transition) []string {
	names := make([]string, 0, len(available))
	for _, t := range available {
		names = append(names, t.Name)
	}
	slices.Sort(names)
	return slices.Compact(names)
}

// applyTransition is the write. It is never retried, on any status: a double
// transition is a worse outcome than an error message.
func (m *Model) applyTransition(key, id, label string) tea.Cmd {
	m.transitionRequest++
	request, client := m.transitionRequest, m.client
	detailRequest := m.detail.request
	m.status = nil
	return func() tea.Msg {
		err := client.Transition(context.Background(), key, id)
		return transitionDoneMsg{
			request: request, detailRequest: detailRequest,
			key: key, label: label, err: err,
		}
	}
}

func (m *Model) handleTransitionDone(msg transitionDoneMsg) tea.Cmd {
	if msg.err != nil {
		// Reported whatever has happened since: the write really was attempted,
		// and a failure the user never sees is a status they will believe is
		// something else.
		m.status = transitionError(msg.key, msg.label, msg.err)
		return nil
	}
	m.status = nil
	if msg.request != m.transitionRequest || m.focusedKey() != msg.key {
		// The user moved on. A late success must not pull a pane back to what
		// they have left behind.
		return nil
	}
	// The status just changed, and the focused item is never cached, so the
	// truth is one live read away. Which read depends on what is on screen: the
	// detail pane refetches when it is already showing this item, and otherwise
	// only the row is refreshed -- a write must not open a pane or move focus,
	// which is navigation the user did not ask for.
	if m.detail.open && m.detail.key == msg.key && msg.detailRequest == m.detail.request {
		return m.openKey(msg.key, false)
	}
	return m.refreshRow(msg.key)
}

// rowMsg carries a live reading of one work item for the row that shows it.
// key is echoed back because the result set can be replaced while it is in
// flight, and a row for a work item no longer listed is simply dropped.
type rowMsg struct {
	key   string
	issue *jira.Issue
	err   error
}

// refreshRow reads one work item live to update the row displaying it. It
// touches no pane and no focus: the only thing that has changed is what that
// row says.
func (m *Model) refreshRow(key string) tea.Cmd {
	client := m.client
	fields := append([]string(nil), detailFields...)
	return func() tea.Msg {
		issue, err := client.Issue(context.Background(), key, fields)
		return rowMsg{key: key, issue: issue, err: err}
	}
}

func (m *Model) handleRow(msg rowMsg) tea.Cmd {
	if msg.err != nil {
		// The write itself succeeded; only the reading back of it failed, and
		// saying so beats a row that quietly disagrees with Jira.
		m.status = fmt.Errorf("reloading %s: %w", msg.key, msg.err)
		return nil
	}
	m.list.refresh(msg.issue)
	return nil
}

// completeTransitionLine finishes a Tab that had to go to Jira first. The
// names live only for the length of this call: every Tab is its own live read,
// so completion can never quietly offer a name that has since stopped being
// available, and it never reuses what a picker once saw.
func (m *Model) completeTransitionLine(msg transitionsMsg) tea.Cmd {
	if m.prompt == nil || m.prompt.mode != ModeCommand {
		return nil
	}
	if msg.err != nil {
		// The line being typed is still the user's; the failure is a note above
		// it rather than a status line they cannot see behind the prompt.
		m.prompt.note = transitionsFetchError(msg.key, msg.err).Error()
		return nil
	}
	m.transitionNames = transitionNames(msg.transitions)
	defer func() { m.transitionNames = nil }()
	m.prompt.completeLine()
	return nil
}

// transitionError says which of the three things went wrong, because the
// answer differs: fields we do not collect, a permission the account lacks, or
// anything else. The field case names the fields and points at the escape
// hatch, since collecting them is out of scope rather than missing by accident.
func transitionError(key, label string, err error) error {
	var required *jira.FieldsRequiredError
	if errors.As(err, &required) {
		return fmt.Errorf("%s: %s needs %s; set them with :!acli jira workitem transition %%",
			key, label, strings.Join(required.FieldNames(), ", "))
	}
	if jira.HasStatus(err, http.StatusForbidden) || jira.HasStatus(err, http.StatusUnauthorized) {
		return fmt.Errorf("%s: not permitted to apply %s", key, label)
	}
	return fmt.Errorf("%s: %s: %w", key, label, err)
}
