// Package preview provides the adaptive preview pane's chrome: a title
// line ("title · mode"), a scrollable ANSI viewport, and a visibility
// toggle. It wraps bubbles/viewport; the content itself — whole/compact/
// record rendering, apex/bat invocation — is app logic (matterbase's
// preview.go), not this package's job. Like recordtable, preview doesn't
// draw its own border box; the app wraps View()'s output in theme.Pane.
package preview

import (
	"github.com/charmbracelet/bubbles/viewport"

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

// SetContent replaces the viewport body and scrolls back to the top. Content
// must already be wrapped to the current width — apex/bat take a width flag
// for this, the viewport does not re-wrap ANSI content on its own.
func (m *Model) SetContent(ansi string) {
	m.vp.SetContent(ansi)
	m.vp.GotoTop()
}

// ScrollLines scrolls by n lines, down for positive n, up for negative.
func (m *Model) ScrollLines(n int) {
	if n > 0 {
		m.vp.ScrollDown(n)
	} else {
		m.vp.ScrollUp(-n)
	}
}

// ScrollPages scrolls by n pages, down for positive n, up for negative.
func (m *Model) ScrollPages(n int) { m.ScrollLines(n * max(m.vp.Height, 1)) }

// Toggle flips visibility.
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

// View renders the title line and viewport, or "" when hidden — hidden
// panes drop out cleanly when passed straight into frame.JoinRow.
func (m Model) View() string {
	if !m.visible {
		return ""
	}
	title := theme.Title.Render(m.title) + theme.Label.Render(" · "+m.mode)
	return title + "\n\n" + m.vp.View()
}
