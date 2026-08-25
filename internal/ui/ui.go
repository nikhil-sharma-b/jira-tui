// Package ui is the bubbletea layer. It depends on jira.Client and nothing
// else from that package, so UI tests substitute a fake client.
//
// Every Jira call is an asynchronous tea.Cmd; the loop never blocks. Cached
// data renders stale-while-revalidate (show cache immediately, replace
// silently when the response lands); uncached data shows a spinner in the
// affected pane only, leaving the rest interactive.
package ui

import (
	"github.com/nikhil-sharma-b/jira-tui/internal/cache"
	"github.com/nikhil-sharma-b/jira-tui/internal/config"
	"github.com/nikhil-sharma-b/jira-tui/internal/jira"
)

// Mode is the vim modal state. Esc returns to ModeNormal from anywhere; no
// widget may consume it.
type Mode int

const (
	ModeNormal Mode = iota
	ModeCommand
	ModeSearch
	ModePicker
)

// Pane identifies a focus target. Panes are addressed directly by chord, never
// cycled -- Tab-cycling is the specific friction this tool exists to remove.
type Pane int

const (
	PaneList Pane = iota
	PaneDetail
)

// Section is a region within the detail pane. Comments and attachments live
// inside detail rather than as sibling panes: three panes is unreadable at 80
// columns in a tmux split.
type Section int

const (
	SectionDescription Section = iota
	SectionComments
	SectionAttachments
)

// Options wires a Model. Pin is the work item this session is bound to, empty
// when unpinned.
type Options struct {
	Client jira.Client
	Cache  *cache.Cache
	Config *config.Config
	Pin    string
}

// Model is the root bubbletea model.
type Model struct {
	// unexported
}

// New builds the root model. When Pin is set, detail opens full-width with the
// list hidden; gl reveals it.
func New(opts Options) (*Model, error) {
	panic("not implemented")
}

// Run starts the event loop.
func Run(opts Options) error {
	panic("not implemented")
}
