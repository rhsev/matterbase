// Package cycler provides a single-value chooser that cycles through a
// fixed option list with left/right — the basekit stand-in for a
// dropdown/Select widget, which bubbles doesn't have. Chrome matches
// input: a bordered box, quiet border normally, accent (or warning) on
// focus, so it reads as one family with basekit's text inputs.
package cycler

import (
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/rhsev/matterbase/basekit/theme"
)

// Config controls one cycler's options and chrome. The zero value is
// usable: no options (View shows Placeholder), width falls back to 20.
type Config struct {
	Options     []string
	Placeholder string
	Width       int // 0 falls back to 20

	// Warning switches the focus border from Accent to Warning, mirroring
	// input.Config's field of the same name.
	Warning bool

	// AllowClear enables an unselected state (Value() == "") reachable
	// via Backspace/Delete in addition to left/right cycling, and starts
	// there instead of at the first option — matching Textual Select's
	// blank prompt state. Off by default: use it for an optional choice
	// the app should treat as "not set" until the user actively picks
	// something (matterbase's SQL-form field/operator cyclers); leave it
	// off for a control that should always carry some value.
	AllowClear bool
}

// Changed is emitted whenever the selection moves.
type Changed struct{ Value string }

// Model is the widget. Use New, then SetOptions whenever the valid
// choices change (e.g. a field cycler fed by the current record set's
// keys).
type Model struct {
	cfg     Config
	options []string
	index   int
	width   int
	focused bool
}

func New(cfg Config) Model {
	if cfg.Width <= 0 {
		cfg.Width = 20
	}
	m := Model{cfg: cfg, options: cfg.Options, width: cfg.Width}
	if cfg.AllowClear {
		m.index = -1
	}
	return m
}

func (m *Model) Focus()       { m.focused = true }
func (m *Model) Blur()        { m.focused = false }
func (m Model) Focused() bool { return m.focused }

// Value returns the currently selected option, or "" when there are
// none, or (with AllowClear) nothing is currently selected.
func (m Model) Value() string {
	if m.index < 0 || m.index >= len(m.options) {
		return ""
	}
	return m.options[m.index]
}

// Options returns the current option list (shared slice; treat as read-only).
func (m Model) Options() []string { return m.options }

// SetOptions replaces the option list. The current value stays selected
// if it's still present (e.g. a field cycler surviving a query re-run
// that keeps the same columns). Otherwise: with AllowClear, selection
// resets to no selection (mirroring Textual's Select, which never
// jumps out of its blank state on its own); without it, to the first
// option, or to no selection if the list is now empty.
func (m *Model) SetOptions(opts []string) {
	current := m.Value()
	m.options = opts
	m.index = -1
	if !m.cfg.AllowClear {
		m.index = 0
	}
	for i, o := range opts {
		if o == current && current != "" {
			m.index = i
			break
		}
	}
}

func (m *Model) SetWidth(w int) { m.width = w }

func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	key, ok := msg.(tea.KeyMsg)
	if !ok || len(m.options) == 0 {
		return m, nil
	}
	switch key.String() {
	case "left", "h":
		// From blank (AllowClear), left enters the cycle at the end;
		// arrows never return to blank once inside it — only an
		// explicit clear does, the same way arrow keys in a text input
		// never delete a character on their own.
		if m.index < 0 {
			m.index = len(m.options) - 1
		} else {
			m.index = (m.index - 1 + len(m.options)) % len(m.options)
		}
		return m, m.changedCmd()
	case "right", "l":
		if m.index < 0 {
			m.index = 0
		} else {
			m.index = (m.index + 1) % len(m.options)
		}
		return m, m.changedCmd()
	case "backspace", "delete":
		if !m.cfg.AllowClear || m.index < 0 {
			return m, nil
		}
		m.index = -1
		return m, m.changedCmd()
	}
	return m, nil
}

func (m Model) changedCmd() tea.Cmd {
	value := m.Value()
	return func() tea.Msg { return Changed{Value: value} }
}

func (m Model) View() string {
	inner := m.cfg.Placeholder
	valueStyle := lipgloss.NewStyle().Foreground(theme.Muted)
	if v := m.Value(); v != "" {
		inner = v
		valueStyle = lipgloss.NewStyle().Foreground(theme.Text)
	}
	arrowStyle := lipgloss.NewStyle().Foreground(theme.Muted)
	content := arrowStyle.Render("‹ ") + valueStyle.Render(inner) + arrowStyle.Render(" ›")

	innerWidth := max(m.width-2, 0)
	// Truncate (not wrap) before the bordered box pads to width: Width()
	// word-wraps a line that exceeds it, which would break this one-line
	// box — the same class of bug basekit/frame.Sanitize and
	// matterbase's clampWidth helper exist for.
	content = lipgloss.NewStyle().MaxWidth(innerWidth).Render(content)

	return lipgloss.NewStyle().
		Border(lipgloss.NormalBorder()).
		BorderForeground(m.borderColor()).
		Width(innerWidth).
		Render(content)
}

func (m Model) borderColor() lipgloss.Color {
	if !m.focused {
		return theme.Border
	}
	if m.cfg.Warning {
		return theme.Warning
	}
	return theme.Accent
}
