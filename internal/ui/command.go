package ui

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

// The commandline is where capability goes. Single letters navigate and the
// leader key acts; anything that needs a word, an argument or a query is typed
// here instead of eating another bare key.
//
// A command is named, run and completed in one table entry, for the same
// reason the keymap compiles every binding in one place: a name that is
// reachable but not completable, or completable but not run, is a name that
// only half exists.

// command is one commandline word.
type command struct {
	name string
	// run acts on the argument, which arrives trimmed and may be empty.
	run func(m *Model, arg string) tea.Cmd
	// completeArg offers the arguments this command takes, nil when it takes
	// none worth guessing at.
	completeArg func(m *Model, partial string) []string
	// liveArgs fetches the candidates completeArg needs when they can only be
	// known by asking Jira, nil for a command whose arguments are known from
	// config. Completion is synchronous; this is how a command says that its
	// candidates are not, so that the model can fetch and complete afterwards
	// rather than quietly completing over something remembered.
	liveArgs func(m *Model) tea.Cmd
}

// commands is the whole vocabulary. Ticket 07 names these four; the rest of
// the spec's commandline arrives with the tickets that implement them.
var commands = []command{
	{name: "q", run: func(m *Model, _ string) tea.Cmd { return m.closePane() }},
	{name: "qa", run: func(m *Model, _ string) tea.Cmd { return tea.Quit }},
	{name: "jql", run: runJQL, completeArg: completeSavedQueries},
	{name: "transition", run: runTransition, completeArg: completeTransitions, liveArgs: liveTransitions},
	{name: "cache", run: runCache, completeArg: func(_ *Model, partial string) []string {
		if !strings.HasPrefix("clear", partial) {
			return nil
		}
		return []string{"clear"}
	}},
}

func lookupCommand(name string) (command, bool) {
	for _, c := range commands {
		if c.name == name {
			return c, true
		}
	}
	return command{}, false
}

// runCommand acts on a submitted commandline.
func (m *Model) runCommand(line string) tea.Cmd {
	name, arg := splitCommand(line)
	if name == "" {
		return nil
	}
	c, ok := lookupCommand(name)
	if !ok {
		// The view stays exactly as it was. An unknown command is a typo, and
		// clearing what the user was reading is a poor reply to one.
		m.status = fmt.Errorf("not a command: %s", name)
		return nil
	}
	return c.run(m, arg)
}

// splitCommand separates the command name from its argument. The argument
// keeps its interior spacing, because a JQL is mostly spaces.
func splitCommand(line string) (name, arg string) {
	name, arg, _ = strings.Cut(strings.TrimSpace(line), " ")
	return name, strings.TrimSpace(arg)
}

// runJQL puts a different query on screen, expanding a saved name first.
func runJQL(m *Model, arg string) tea.Cmd {
	if arg == "" {
		m.status = errors.New("jql: needs a query, or @name for a saved one")
		return nil
	}
	query := arg
	if name, saved := strings.CutPrefix(arg, "@"); saved {
		expanded, ok := m.cfg.SavedQueries[name]
		if !ok {
			// Nothing is sent. A mistyped name is not a query, and asking Jira
			// about it would cost a round trip to be told what the config
			// already knows.
			m.status = fmt.Errorf("no saved query named @%s", name)
			return nil
		}
		query = expanded
	}
	return m.runQuery(query)
}

// runTransition moves the focused work item by name. The name is resolved
// against a live fetch when the line is submitted, never against whatever
// completion last saw: between typing a name and pressing Enter the item may
// have moved, and applying a stale id is exactly the wrong answer.
func runTransition(m *Model, arg string) tea.Cmd {
	if arg == "" {
		m.status = errors.New("transition: needs a name; <leader>t lists what is available")
		return nil
	}
	return m.beginNamedTransition(arg)
}

// completeTransitions offers the live names the model has just fetched for
// this keystroke. They are gone again by the next one.
func completeTransitions(m *Model, partial string) []string {
	var out []string
	for _, name := range m.transitionNames {
		if strings.HasPrefix(strings.ToLower(name), strings.ToLower(partial)) {
			out = append(out, name)
		}
	}
	slices.Sort(out)
	return out
}

// liveTransitions fetches the names :transition completes over. There is
// nothing to complete without a focused work item, since what is available is
// a property of one item at one moment.
func liveTransitions(m *Model) tea.Cmd {
	key := m.focusedKey()
	if key == "" {
		return nil
	}
	return m.fetchTransitions(key, intentComplete, "")
}

// runCache is :cache clear and nothing else, so that a mistyped subcommand is
// reported rather than silently taken for the one operation there is.
func runCache(m *Model, arg string) tea.Cmd {
	if arg != "clear" {
		m.status = fmt.Errorf("cache: no such subcommand %q; the only one is clear", arg)
		return nil
	}
	return m.clearCache()
}

// completeSavedQueries offers the configured query names, spelled the way :jql
// takes them. The @ need not have been typed yet: a saved name is the only
// thing here that can be completed at all, so a bare partial is taken for the
// start of one and the @ is supplied. A partial that names none of them
// completes to nothing, which leaves a JQL being typed by hand alone.
func completeSavedQueries(m *Model, partial string) []string {
	name := strings.TrimPrefix(partial, "@")
	var out []string
	for saved := range m.cfg.SavedQueries {
		if strings.HasPrefix(saved, name) {
			out = append(out, "@"+saved)
		}
	}
	slices.Sort(out)
	return out
}

// completeCommandLine offers whole lines for a partially typed command. Whole
// lines rather than words, because only the table knows whether what is being
// typed is still a name or has become an argument.
func (m *Model) completeCommandLine(line string) []string {
	name, arg, hasArg := strings.Cut(line, " ")
	if !hasArg {
		return completeNames(name)
	}
	c, ok := lookupCommand(name)
	if !ok || c.completeArg == nil {
		return nil
	}
	var out []string
	for _, candidate := range c.completeArg(m, strings.TrimSpace(arg)) {
		out = append(out, name+" "+candidate)
	}
	return out
}

// liveCompletion reports the fetch a Tab needs before the line can be
// completed at all, nil when the candidates are already knowable. It is asked
// before the keypress reaches the widget, so that completion itself stays the
// synchronous thing it reads as.
func (m *Model) liveCompletion(line string) tea.Cmd {
	name, _, hasArg := strings.Cut(line, " ")
	if !hasArg {
		return nil
	}
	c, ok := lookupCommand(name)
	if !ok || c.liveArgs == nil {
		return nil
	}
	return c.liveArgs(m)
}

// completeNames offers the command names starting with what has been typed,
// in a stable order so the same keystrokes always offer the same list.
func completeNames(partial string) []string {
	var out []string
	for _, c := range commands {
		if strings.HasPrefix(c.name, partial) {
			out = append(out, c.name)
		}
	}
	slices.Sort(out)
	return out
}
