package input

import (
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/rhsev/matterbase/basekit/theme"
)

func typeRune(t *testing.T, m *Model, r rune) tea.Cmd {
	t.Helper()
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	*m = updated
	return cmd
}

// runCmds executes a (possibly batched) tea.Cmd tree and returns every
// resulting tea.Msg — enough to inspect what a keystroke produced without
// running a full Bubble Tea program.
func runCmds(cmd tea.Cmd) []tea.Msg {
	if cmd == nil {
		return nil
	}
	msg := cmd()
	if batch, ok := msg.(tea.BatchMsg); ok {
		var out []tea.Msg
		for _, c := range batch {
			out = append(out, runCmds(c)...)
		}
		return out
	}
	return []tea.Msg{msg}
}

func TestChangedFiresOnKeystroke(t *testing.T) {
	m := New(Config{Debounce: time.Millisecond})
	m.Focus()
	cmd := typeRune(t, &m, 'x')

	var sawChanged bool
	for _, msg := range runCmds(cmd) {
		if c, ok := msg.(Changed); ok {
			sawChanged = true
			if c.Value != "x" {
				t.Errorf("Changed.Value = %q, want %q", c.Value, "x")
			}
		}
	}
	if !sawChanged {
		t.Error("expected a Changed message on keystroke")
	}
}

func TestDebouncedIgnoresStaleGeneration(t *testing.T) {
	m := New(Config{Debounce: time.Millisecond})
	m.Focus()

	cmd1 := typeRune(t, &m, 'a')
	// A second keystroke bumps the generation before the first tick fires.
	cmd2 := typeRune(t, &m, 'b')

	// The first tick belongs to the stale generation and must produce nothing.
	var stale tickMsg
	for _, msg := range runCmds(cmd1) {
		if tick, ok := msg.(tickMsg); ok {
			stale = tick
		}
	}
	updated, out := m.Update(stale)
	m = updated
	if out != nil && out() != nil {
		t.Errorf("stale tick produced a message, want nil: %#v", out())
	}

	// The second (current) tick must fire Debounced with the latest value.
	var current tickMsg
	for _, msg := range runCmds(cmd2) {
		if tick, ok := msg.(tickMsg); ok {
			current = tick
		}
	}
	_, out2 := m.Update(current)
	msg := out2()
	deb, ok := msg.(Debounced)
	if !ok {
		t.Fatalf("expected Debounced, got %#v", msg)
	}
	if deb.Value != "ab" {
		t.Errorf("Debounced.Value = %q, want %q", deb.Value, "ab")
	}
}

func TestSubmittedOnEnterBypassesDebounce(t *testing.T) {
	m := New(Config{})
	m.Focus()
	typeRune(t, &m, 'z')

	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	msg := cmd()
	sub, ok := msg.(Submitted)
	if !ok {
		t.Fatalf("expected Submitted, got %#v", msg)
	}
	if sub.Value != "z" {
		t.Errorf("Submitted.Value = %q, want %q", sub.Value, "z")
	}
}

func TestBorderColorTracksFocusAndWarning(t *testing.T) {
	plain := New(Config{})
	if plain.borderColor() != theme.Border {
		t.Error("unfocused input should use the quiet border")
	}
	plain.Focus()
	if plain.borderColor() != theme.Accent {
		t.Error("focused input should use the accent border")
	}

	warn := New(Config{Warning: true})
	warn.Focus()
	if warn.borderColor() != theme.Warning {
		t.Error("focused Warning input should use the warning border")
	}
}
