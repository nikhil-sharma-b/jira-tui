package ui

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/nikhil-sharma-b/jira-tui/internal/jira"
)

// Reassignment is a search, not a list. Who may be assigned to a work item
// depends on the project's permission scheme and on roles the client cannot
// know, so the candidates come from the site -- and they keep coming from it
// as the user types, because a name absent from the first answer must still be
// findable. That is the whole reason this picker asks the server on every
// filter change rather than narrowing what it already has.
//
// The user never sees an account id. It is what is sent and it is the only
// stable identifier a Jira account has, but a display name is what is typed and
// what is read back.

// unassignID is the picker's way of saying "no assignee at all". It is a
// sentinel rather than the empty string that the client takes for the same
// thing, so that a choice can never be confused with nothing being chosen.
const unassignID = "\x00unassign"

// defaultSearchDebounce is how long a keystroke waits to see whether another
// one follows it. Long enough that a typed name is one request rather than
// six, short enough that a user who stops typing does not notice having waited.
const defaultSearchDebounce = 180 * time.Millisecond

// assignIntent says what a user search was for: filling the picker, or
// resolving a name typed on the commandline.
type assignIntent uint8

const (
	intentAssignPicker assignIntent = iota
	intentAssignName
)

// usersMsg carries one answer to "who can be assigned to this item". myself is
// carried alongside because the first search of a session fetches both in one
// command, which keeps the picker's pinned rows from arriving after the rows
// they are pinned above.
type usersMsg struct {
	request uint64
	key     string
	query   string
	intent  assignIntent
	myself  *jira.User
	users   []jira.User
	err     error
}

// assignSearchMsg is a debounced keystroke coming due. It fires the search only
// if no later keystroke has superseded it, which is what makes typing a name
// one request instead of one per letter.
type assignSearchMsg struct {
	request uint64
	query   string
}

// assignDoneMsg reports what an assignment came to. label is who was being
// assigned, kept for the message: by the time this lands the picker that
// displayed them is gone.
type assignDoneMsg struct {
	request       uint64
	detailRequest uint64
	key           string
	label         string
	err           error
}

// beginAssignPicker opens the assignee picker on the focused work item. The
// dispatcher is already in picker mode by the time this runs, so every path out
// of here has to put it back.
func (m *Model) beginAssignPicker() tea.Cmd {
	key := m.focusedKey()
	if key == "" {
		m.closePicker()
		m.status = errors.New("no focused work item")
		return nil
	}
	// A picker under a full-screen overlay is a picker chosen from blind.
	m.help.Hide()
	m.status = nil
	m.picker = &picker{
		kind: pickerAssign, serverFiltered: true, loading: true,
		title: "Assign " + key, waiting: "Searching users…", key: key,
	}
	m.resizePanes()
	return m.fetchUsers(key, "", intentAssignPicker)
}

// beginNamedAssign resolves a typed name against the site and assigns whoever
// it turns out to mean. The commandline never takes an account id: that is the
// identifier this feature exists to keep out of the user's hands.
func (m *Model) beginNamedAssign(name string) tea.Cmd {
	key := m.focusedKey()
	if key == "" {
		m.status = errors.New("no focused work item")
		return nil
	}
	m.status = nil
	return m.fetchUsers(key, name, intentAssignName)
}

// fetchUsers asks the site who is assignable, and on the first search of a
// session asks who the user is at the same time. Both in one command, so the
// picker is filled once rather than twice, and so a session that never assigns
// anything never makes the second call at all.
func (m *Model) fetchUsers(key, query string, intent assignIntent) tea.Cmd {
	m.assignRequest++
	request, client, myself := m.assignRequest, m.client, m.myself
	return func() tea.Msg {
		if myself == nil {
			// A failure here is not the search's failure: not knowing who the
			// user is costs one pinned row, and refusing the whole picker over
			// it would be a worse answer than leaving that row out.
			if me, err := client.Myself(context.Background()); err == nil {
				myself = me
			}
		}
		users, err := client.SearchUsers(context.Background(), key, query)
		return usersMsg{
			request: request, key: key, query: query, intent: intent,
			myself: myself, users: users, err: err,
		}
	}
}

// debounceUserSearch schedules the search a keystroke asks for. Bumping the
// request here rather than when the timer fires is what makes the next
// keystroke supersede this one: the timer that comes due last is the only one
// still holding the current request number.
func (m *Model) debounceUserSearch() tea.Cmd {
	m.assignRequest++
	request, query, delay := m.assignRequest, m.picker.text(), m.searchDebounce
	return tea.Tick(delay, func(time.Time) tea.Msg {
		return assignSearchMsg{request: request, query: query}
	})
}

func (m *Model) handleAssignSearch(msg assignSearchMsg) tea.Cmd {
	if msg.request != m.assignRequest {
		// A later keystroke has already asked for something else.
		return nil
	}
	if m.picker == nil || m.picker.kind != pickerAssign {
		return nil
	}
	return m.fetchUsers(m.picker.key, msg.query, intentAssignPicker)
}

func (m *Model) handleUsers(msg usersMsg) tea.Cmd {
	if msg.request != m.assignRequest {
		// Superseded: the picker was closed, another search was typed, or an
		// assignment has already been started.
		return nil
	}
	if msg.myself != nil {
		m.myself = msg.myself
	}
	if msg.intent == intentAssignName {
		return m.applyNamedAssign(msg)
	}
	return m.showUsers(msg)
}

// showUsers fills the open picker, or explains why there is nothing to choose
// from. A failed search closes it: a picker with nothing in it and no way to
// get anything is a mode the user has to guess their way out of.
func (m *Model) showUsers(msg usersMsg) tea.Cmd {
	if m.picker == nil || m.picker.kind != pickerAssign || m.picker.key != msg.key {
		return nil
	}
	if msg.err != nil {
		m.closePicker()
		m.status = userSearchError(msg.key, msg.err)
		return nil
	}
	m.picker.setItems(m.assignItems(msg.users, msg.query))
	m.resizePanes()
	return nil
}

// userSearchError is how a failed search reads wherever it is reported: what
// was being asked, of which item, and what the site said back.
func userSearchError(key string, err error) error {
	return fmt.Errorf("assignable users for %s: %w", key, err)
}

// assignItems is what the picker offers. An unfiltered picker opens on the two
// answers the user already knows -- their own account and no account at all --
// and a typed search shows only what the site matched, since a pinned row that
// matched nothing would be noise sitting where the best match should be.
func (m *Model) assignItems(users []jira.User, query string) []pickerItem {
	var items []pickerItem
	if query == "" {
		items = append(items, pickerItem{id: unassignID, label: "Unassigned"})
		if m.myself != nil {
			items = append(items, pickerItem{id: m.myself.AccountID, label: userLabel(*m.myself) + " (me)"})
		}
	}
	for _, u := range users {
		if query == "" && m.myself != nil && u.AccountID == m.myself.AccountID {
			// Already pinned above; offering it twice only makes the list longer.
			continue
		}
		items = append(items, pickerItem{id: u.AccountID, label: userLabel(u)})
	}
	return items
}

// userLabel is how an account reads. The email is shown when the site
// discloses one, because two people share a name far more often than they
// share an address -- but a site that hides addresses must not make an account
// unselectable, so a hidden one costs nothing but the disambiguation.
func userLabel(u jira.User) string {
	name := strings.TrimSpace(u.DisplayName)
	email := strings.TrimSpace(u.Email)
	switch {
	case name != "" && email != "":
		return name + " <" + email + ">"
	case name != "":
		return name
	case email != "":
		return email
	default:
		// Neither is disclosed. The account id is not something to type, but it
		// is something to point at, and an unlabelled row cannot be chosen.
		return u.AccountID
	}
}

// chooseAssignee applies what the picker has selected. It sends the account
// id, never the name that was displayed.
func (m *Model) chooseAssignee(item pickerItem, key string) tea.Cmd {
	accountID, label := item.id, item.label
	if accountID == unassignID {
		accountID = ""
	}
	return m.applyAssign(key, accountID, label)
}

func (m *Model) applyNamedAssign(msg usersMsg) tea.Cmd {
	if msg.err != nil {
		m.status = userSearchError(msg.key, msg.err)
		return nil
	}
	chosen, err := resolveUser(msg.users, msg.query)
	if err != nil {
		m.status = err
		return nil
	}
	return m.applyAssign(msg.key, chosen.AccountID, userLabel(chosen))
}

// resolveUser finds the one account a typed name means. An exactly spelled
// display name wins outright, then a case-insensitive one; failing both, a
// search that matched a single person is taken to mean that person, which is
// what lets a surname stand for a colleague. Several candidates are refused
// rather than guessed between, and the refusal names them so the next attempt
// can be more specific.
func resolveUser(users []jira.User, name string) (jira.User, error) {
	var exact, folded []jira.User
	for _, u := range users {
		switch {
		case u.DisplayName == name:
			exact = append(exact, u)
		case strings.EqualFold(u.DisplayName, name):
			folded = append(folded, u)
		}
	}
	matches := exact
	if len(matches) == 0 {
		matches = folded
	}
	if len(matches) == 0 {
		matches = users
	}
	switch {
	case len(matches) == 1:
		return matches[0], nil
	case len(matches) > 1:
		return jira.User{}, fmt.Errorf("several users match %q: %s; use the assign picker",
			name, strings.Join(userNames(matches), ", "))
	default:
		return jira.User{}, fmt.Errorf("no user matches %q", name)
	}
}

// userNames lists candidates for a message, sorted, so the same ambiguity
// reads the same way every time it is reported.
func userNames(users []jira.User) []string {
	names := make([]string, 0, len(users))
	for _, u := range users {
		names = append(names, userLabel(u))
	}
	slices.Sort(names)
	return slices.Compact(names)
}

// applyAssign is the write. Like every write it is never retried, on any
// status: an assignment that silently happened twice is indistinguishable from
// one that happened once, but an error the user never saw is not.
func (m *Model) applyAssign(key, accountID, label string) tea.Cmd {
	m.assignRequest++
	request, client := m.assignRequest, m.client
	detailRequest := m.detail.request
	m.status = nil
	return func() tea.Msg {
		err := client.Assign(context.Background(), key, accountID)
		return assignDoneMsg{
			request: request, detailRequest: detailRequest,
			key: key, label: label, err: err,
		}
	}
}

func (m *Model) handleAssignDone(msg assignDoneMsg) tea.Cmd {
	if msg.err != nil {
		// Reported whatever has happened since: the write really was attempted,
		// and a failure the user never sees is a state they will believe is
		// something else.
		m.status = assignError(msg.key, msg.label, msg.err)
		return nil
	}
	m.status = nil
	if msg.request != m.assignRequest || m.focusedKey() != msg.key {
		// The user moved on. A late success must not pull a pane back to what
		// they have left behind.
		return nil
	}
	return m.readBack(msg.key, msg.detailRequest)
}

// assignError names what was refused and why. A permission failure is called
// what it is, since "403" is not an answer anyone acts on -- but the site's own
// words are kept after it either way: they are the only part of the message
// that can say which permission, or which of several accounts, the site
// objected to.
func assignError(key, label string, err error) error {
	if jira.HasStatus(err, http.StatusForbidden) || jira.HasStatus(err, http.StatusUnauthorized) {
		return fmt.Errorf("%s: not permitted to assign %s: %w", key, label, err)
	}
	return fmt.Errorf("%s: assigning %s: %w", key, label, err)
}
