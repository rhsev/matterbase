// Package preview provides the adaptive preview pane's chrome: a title
// line ("title · mode"), a scrollable ANSI viewport, and a visibility
// toggle. It wraps bubbles/viewport; the content itself — whole/compact/
// record rendering, apex/bat invocation — is app logic (matterbase-go's
// preview.go), not this package's job. Like recordtable, preview doesn't
// draw its own border box; the app wraps View()'s output in theme.Pane.
package preview

import (
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/rhsev/matterbase/basekit/theme"
)

// Model is the widget. Use New, then SetTitle/SetContent whenever the
// selected record or preview mode changes.
type Model struct {
	vp      viewport.Model
	title   string
	mode    string
	visible bool
}

func New() Model {
	return Model{vp: viewport.New(0, 0), visible: true}
}

// SetTitle sets the title line's two parts: the record's label (e.g. the
// source filename) and the current mode (e.g. "whole").
func (m *Model) SetTitle(title, mode string) {
	m.title = title
	m.mode = mode
}

// SetContent replaces the viewport body. Content must already be wrapped
// to the current width — apex/bat take a width flag for this, the
// viewport does not re-wrap ANSI content on its own.
func (m *Model) SetContent(ansi string) { m.vp.SetContent(ansi) }

// Toggle flips visibility — matterbase's action_toggle_preview (key p).
func (m *Model) Toggle() { m.visible = !m.visible }

// Visible reports whether the pane is currently shown.
func (m Model) Visible() bool { return m.visible }

// SetSize sets the content width and height available to the pane
// (already the inner area within any border the caller draws). One line
// is reserved for the title and one blank line beneath it.
func (m *Model) SetSize(w, h int) {
	m.vp.Width = w
	m.vp.Height = max(h-2, 0)
}

func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	if !m.visible {
		return m, nil
	}
	var cmd tea.Cmd
	m.vp, cmd = m.vp.Update(msg)
	return m, cmd
}

// View renders the title line and viewport, or "" when hidden — hidden
// panes drop out cleanly when passed straight into frame.JoinRow.
func (m Model) View() string {
	if !m.visible {
		return ""
	}
	title := theme.Title.Render(m.title) + theme.Label.Render(" · "+m.mode)
	return title + "\n\n" + m.vp.View()
}
