package ui

import tea "github.com/charmbracelet/bubbletea"

// jumplist stores only visited work item keys. Detail is always fetched live,
// including when history moves to a key that was displayed earlier.
type jumplist struct {
	keys   []string
	cursor int
}

func newJumplist() jumplist { return jumplist{cursor: -1} }

func (j *jumplist) visit(key string) {
	if j.cursor >= 0 && j.keys[j.cursor] == key {
		j.keys = j.keys[:j.cursor+1]
		return
	}
	if j.cursor+1 < len(j.keys) {
		j.keys = j.keys[:j.cursor+1]
	}
	j.keys = append(j.keys, key)
	j.cursor = len(j.keys) - 1
}

func (j *jumplist) move(direction, count int) (string, bool) {
	if len(j.keys) == 0 {
		return "", false
	}
	n := max(count, 1)
	target := min(max(j.cursor+direction*n, 0), len(j.keys)-1)
	if target == j.cursor {
		return "", false
	}
	j.cursor = target
	return j.keys[target], true
}

func (m *Model) visiblePanes() (list, detail bool) {
	if !m.zoomed {
		return m.listVisible, m.detailVisible && m.detail.open
	}
	if m.focus == PaneList {
		return m.listVisible, false
	}
	return false, m.detailVisible && m.detail.open
}

func (m *Model) openKey(key string, recordVisit bool) tea.Cmd {
	if recordVisit {
		m.jumps.visit(key)
	}
	m.detailVisible = true
	m.focus = PaneDetail
	cmd := m.detail.fetch(m.client, key)
	m.resizePanes()
	return tea.Batch(cmd, m.detail.tick())
}

func (m *Model) jump(direction, count int) tea.Cmd {
	key, ok := m.jumps.move(direction, count)
	if !ok {
		return nil
	}
	return m.openKey(key, false)
}

func (m *Model) goList() {
	m.listVisible = true
	m.focus = PaneList
	m.resizePanes()
}

func (m *Model) goDetail() {
	if !m.detail.open {
		return
	}
	m.detailVisible = true
	m.focus = PaneDetail
	m.resizePanes()
}

func (m *Model) moveFocus(target Pane) {
	listVisible, detailVisible := m.visiblePanes()
	switch target {
	case PaneList:
		if m.focus == PaneDetail && listVisible {
			m.focus = PaneList
		}
	case PaneDetail:
		if m.focus == PaneList && detailVisible {
			m.focus = PaneDetail
		}
	}
}

func (m *Model) toggleZoom() {
	if !m.listVisible || !m.detailVisible || !m.detail.open {
		return
	}
	m.zoomed = !m.zoomed
	m.resizePanes()
}
