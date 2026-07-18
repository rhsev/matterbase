// Package togglelist provides a list of toggleable line items — the
// widget behind matterbase's preset list and the column picker. It
// renders bare lines ("■ label" / "  label"); panel chrome (border,
// title) belongs to the caller.
package togglelist

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/rhsev/matterbase/basekit/theme"
)

// Item is one toggleable line.
type Item struct {
	Label  string
	Active bool
}

// Toggled is emitted as a tea.Msg whenever an item flips.
type Toggled struct {
	Index int
	Item  Item
}

var (
	lineStyle   = lipgloss.NewStyle().Foreground(theme.Border)
	activeStyle = lipgloss.NewStyle().Foreground(theme.Accent).Bold(true)
	cursorStyle = lipgloss.NewStyle().Background(theme.BgHover)
)

type Model struct {
	items  []Item
	cursor int
}

func New(items []Item) Model {
	return Model{items: items}
}

func (m *Model) SetItems(items []Item) {
	m.items = items
	if m.cursor >= len(items) {
		m.cursor = max(len(items)-1, 0)
	}
}

// Items returns the current items (shared slice; treat as read-only).
func (m Model) Items() []Item { return m.items }

// ActiveLabels returns the labels of all active items, in list order.
func (m Model) ActiveLabels() []string {
	var out []string
	for _, it := range m.items {
		if it.Active {
			out = append(out, it.Label)
		}
	}
	return out
}

func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	key, ok := msg.(tea.KeyMsg)
	if !ok || len(m.items) == 0 {
		return m, nil
	}
	switch key.String() {
	case "up", "k":
		if m.cursor > 0 {
			m.cursor--
		}
	case "down", "j":
		if m.cursor < len(m.items)-1 {
			m.cursor++
		}
	case " ", "enter":
		m.items[m.cursor].Active = !m.items[m.cursor].Active
		it, i := m.items[m.cursor], m.cursor
		return m, func() tea.Msg { return Toggled{Index: i, Item: it} }
	case "backspace", "delete", "ctrl+u":
		// Deactivate every active item at once — toggling each off one
		// by one otherwise has no shortcut (the column picker's "restore
		// inference" path is an empty ActiveLabels(), reached only by
		// clearing every item).
		var cmds []tea.Cmd
		for i := range m.items {
			if !m.items[i].Active {
				continue
			}
			m.items[i].Active = false
			it, idx := m.items[i], i
			cmds = append(cmds, func() tea.Msg { return Toggled{Index: idx, Item: it} })
		}
		if len(cmds) == 0 {
			return m, nil
		}
		return m, tea.Batch(cmds...)
	}
	return m, nil
}

func (m Model) View() string {
	var b strings.Builder
	for i, it := range m.items {
		marker, style := "  ", lineStyle
		if it.Active {
			marker, style = "■ ", activeStyle
		}
		line := style.Render(marker + it.Label)
		if i == m.cursor {
			line = cursorStyle.Render(marker + it.Label)
		}
		b.WriteString(line)
		if i < len(m.items)-1 {
			b.WriteByte('\n')
		}
	}
	return b.String()
}
