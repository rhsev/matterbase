// Package input wraps bubbles/textinput with the basekit chrome: a
// bordered box (quiet border, accent on focus), a debounced Debounced
// message for live-filter fields, and an immediate Submitted message for
// Enter — matterbase's #sql / #filename / #fulltext inputs.
package input

import (
	"sync/atomic"
	"time"

	"github.com/charmbracelet/bubbles/cursor"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/rhsev/matterbase/basekit/theme"
)

// DefaultDebounce matches matterbase's set_timer(0.25, self._run_query).
const DefaultDebounce = 250 * time.Millisecond

// Config controls one input's chrome and behaviour. The zero value is
// usable: no placeholder, 250 ms debounce, accent focus border.
type Config struct {
	Placeholder string
	Width       int           // 0 falls back to 20
	Debounce    time.Duration // 0 falls back to DefaultDebounce

	// Warning switches the focus border from Accent to Warning — the
	// fulltext field's "this isn't in the yanked query" cue.
	Warning bool
}

// Changed is emitted on every keystroke — apps use it to update their
// own state immediately (e.g. the filename term) even though the query
// re-run waits for Debounced. ID names the emitting input (Model.ID), so
// an app with several inputs routes by source, not by whichever input
// has focus by the time the message arrives.
type Changed struct {
	ID    int
	Value string
}

// Debounced is emitted DefaultDebounce after the last Changed with no
// further edits — the signal to actually re-run a query. ID as in
// Changed.
type Debounced struct {
	ID    int
	Value string
}

// Submitted is emitted immediately on Enter, bypassing the debounce.
type Submitted struct{ Value string }

// tickMsg carries the input's id: an app forwards every non-key message
// to all of its inputs, and generations alone collide across inputs.
type tickMsg struct {
	id    int
	gen   int
	value string
}

var lastID atomic.Int64

// Model is the widget. Use New, mount it in the app model, and forward
// tea.Msg through Update like any other Bubble Tea component.
type Model struct {
	id       int
	ti       textinput.Model
	cfg      Config
	width    int
	gen      int
	debounce time.Duration
}

func New(cfg Config) Model {
	if cfg.Width <= 0 {
		cfg.Width = 20
	}
	if cfg.Debounce <= 0 {
		cfg.Debounce = DefaultDebounce
	}
	ti := textinput.New()
	// A static cursor: a blinking one redraws the whole app frame twice a
	// second for nothing.
	ti.Cursor.SetMode(cursor.CursorStatic)
	ti.Placeholder = cfg.Placeholder
	ti.Width = cfg.Width - 2 // border consumes 2 columns
	return Model{id: int(lastID.Add(1)), ti: ti, cfg: cfg, width: cfg.Width, debounce: cfg.Debounce}
}

// ID identifies this input in its Changed and Debounced messages.
func (m Model) ID() int { return m.id }

func (m *Model) Focus() tea.Cmd { return m.ti.Focus() }
func (m *Model) Blur()          { m.ti.Blur() }
func (m Model) Focused() bool   { return m.ti.Focused() }
func (m Model) Value() string   { return m.ti.Value() }

// SetValue replaces the content without emitting Changed/Debounced — for
// programmatic resets (e.g. clearing the SQL form's value field).
func (m *Model) SetValue(v string) { m.ti.SetValue(v) }

func (m *Model) SetWidth(w int) {
	m.width = w
	m.ti.Width = max(w-2, 0)
}

func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	if tick, ok := msg.(tickMsg); ok {
		if tick.id == m.id && tick.gen == m.gen {
			return m, func() tea.Msg { return Debounced{ID: m.id, Value: tick.value} }
		}
		return m, nil
	}

	if key, ok := msg.(tea.KeyMsg); ok && key.Type == tea.KeyEnter {
		return m, func() tea.Msg { return Submitted{Value: m.ti.Value()} }
	}

	prev := m.ti.Value()
	var cmd tea.Cmd
	m.ti, cmd = m.ti.Update(msg)
	if m.ti.Value() == prev {
		return m, cmd
	}

	m.gen++
	id, gen, value, debounce := m.id, m.gen, m.ti.Value(), m.debounce
	changed := func() tea.Msg { return Changed{ID: id, Value: value} }
	tick := tea.Tick(debounce, func(time.Time) tea.Msg {
		return tickMsg{id: id, gen: gen, value: value}
	})
	return m, tea.Batch(cmd, changed, tick)
}

func (m Model) View() string {
	style := lipgloss.NewStyle().
		Border(lipgloss.NormalBorder()).
		BorderForeground(m.borderColor()).
		Width(max(m.width-2, 0))
	return style.Render(m.ti.View())
}

func (m Model) borderColor() lipgloss.Color {
	if !m.ti.Focused() {
		return theme.Border
	}
	if m.cfg.Warning {
		return theme.Warning
	}
	return theme.Accent
}
