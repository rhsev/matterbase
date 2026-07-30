package cycler

import (
	"reflect"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/rhsev/matterbase/basekit/theme"
)

func press(t *testing.T, m *Model, key string) tea.Cmd {
	t.Helper()
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(key)})
	*m = updated
	return cmd
}

func TestValueEmptyWithNoOptions(t *testing.T) {
	m := New(Config{})
	if got := m.Value(); got != "" {
		t.Errorf("Value() = %q, want empty", got)
	}
}

func TestRightCyclesForwardAndWraps(t *testing.T) {
	m := New(Config{Options: []string{"=", "!=", "LIKE"}})
	if got := m.Value(); got != "=" {
		t.Fatalf("initial Value() = %q, want %q", got, "=")
	}
	press(t, &m, "l")
	if got := m.Value(); got != "!=" {
		t.Errorf("after right, Value() = %q, want %q", got, "!=")
	}
	press(t, &m, "l")
	if got := m.Value(); got != "LIKE" {
		t.Errorf("after right x2, Value() = %q, want %q", got, "LIKE")
	}
	press(t, &m, "l")
	if got := m.Value(); got != "=" {
		t.Errorf("after wrapping past the end, Value() = %q, want %q", got, "=")
	}
}

func TestLeftCyclesBackwardAndWraps(t *testing.T) {
	m := New(Config{Options: []string{"=", "!=", "LIKE"}})
	press(t, &m, "h")
	if got := m.Value(); got != "LIKE" {
		t.Errorf("left from index 0 should wrap to the last option, got %q", got)
	}
}

func TestArrowKeysAlsoWork(t *testing.T) {
	m := New(Config{Options: []string{"a", "b"}})
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRight})
	m = updated
	if got := m.Value(); got != "b" {
		t.Errorf("KeyRight: Value() = %q, want %q", got, "b")
	}
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyLeft})
	m = updated
	if got := m.Value(); got != "a" {
		t.Errorf("KeyLeft: Value() = %q, want %q", got, "a")
	}
}

func TestChangedEmittedOnCycle(t *testing.T) {
	m := New(Config{Options: []string{"a", "b"}})
	cmd := press(t, &m, "l")
	if cmd == nil {
		t.Fatal("expected a Changed command")
	}
	msg, ok := cmd().(Changed)
	if !ok {
		t.Fatalf("expected Changed, got %#v", cmd())
	}
	if msg.Value != "b" {
		t.Errorf("Changed.Value = %q, want %q", msg.Value, "b")
	}
}

func TestUnaffectedByOtherKeys(t *testing.T) {
	m := New(Config{Options: []string{"a", "b"}})
	cmd := press(t, &m, "x")
	if cmd != nil {
		t.Error("an unrelated key must not emit Changed")
	}
	if got := m.Value(); got != "a" {
		t.Errorf("Value() = %q, want unchanged %q", got, "a")
	}
}

func TestEmptyOptionsIgnoresKeys(t *testing.T) {
	m := New(Config{})
	cmd := press(t, &m, "l")
	if cmd != nil {
		t.Error("cycling with no options must not emit Changed")
	}
}

func TestSetOptionsKeepsCurrentValueIfStillPresent(t *testing.T) {
	m := New(Config{Options: []string{"status", "amount", "kind"}})
	press(t, &m, "l") // now "amount"
	m.SetOptions([]string{"kind", "amount", "status"})
	if got := m.Value(); got != "amount" {
		t.Errorf("Value() = %q, want amount preserved across SetOptions", got)
	}
}

func TestSetOptionsResetsWhenCurrentValueGone(t *testing.T) {
	m := New(Config{Options: []string{"status", "amount"}})
	press(t, &m, "l") // now "amount"
	m.SetOptions([]string{"kind", "title"})
	if got := m.Value(); got != "kind" {
		t.Errorf("Value() = %q, want reset to the first option %q", got, "kind")
	}
}

func TestSetOptionsToEmptyClearsValue(t *testing.T) {
	m := New(Config{Options: []string{"status"}})
	m.SetOptions(nil)
	if got := m.Value(); got != "" {
		t.Errorf("Value() = %q, want empty", got)
	}
}

func TestOptionsReturnsCurrentList(t *testing.T) {
	m := New(Config{Options: []string{"a", "b"}})
	if got := m.Options(); !reflect.DeepEqual(got, []string{"a", "b"}) {
		t.Errorf("Options() = %v", got)
	}
}

func TestFocusBlur(t *testing.T) {
	m := New(Config{})
	if m.Focused() {
		t.Error("zero value must start blurred")
	}
	m.Focus()
	if !m.Focused() {
		t.Error("Focus() must set Focused()")
	}
	m.Blur()
	if m.Focused() {
		t.Error("Blur() must clear Focused()")
	}
}

func TestViewShowsPlaceholderWhenEmpty(t *testing.T) {
	m := New(Config{Placeholder: "field"})
	if got := m.View(); !strings.Contains(got, "field") {
		t.Errorf("View() = %q, want it to contain the placeholder", got)
	}
}

func TestViewShowsCurrentValue(t *testing.T) {
	m := New(Config{Options: []string{"status", "amount"}})
	if got := m.View(); !strings.Contains(got, "status") {
		t.Errorf("View() = %q, want it to contain %q", got, "status")
	}
}

func TestAllowClearStartsBlank(t *testing.T) {
	m := New(Config{Options: []string{"=", "!="}, AllowClear: true})
	if got := m.Value(); got != "" {
		t.Errorf("Value() = %q, want blank (AllowClear starts unselected)", got)
	}
}

func TestAllowClearRightEntersFromBlank(t *testing.T) {
	m := New(Config{Options: []string{"a", "b", "c"}, AllowClear: true})
	press(t, &m, "l")
	if got := m.Value(); got != "a" {
		t.Errorf("right from blank: Value() = %q, want %q", got, "a")
	}
}

func TestAllowClearLeftEntersFromBlankAtTheEnd(t *testing.T) {
	m := New(Config{Options: []string{"a", "b", "c"}, AllowClear: true})
	press(t, &m, "h")
	if got := m.Value(); got != "c" {
		t.Errorf("left from blank: Value() = %q, want the last option %q", got, "c")
	}
}

func TestAllowClearCyclingNeverReturnsToBlank(t *testing.T) {
	m := New(Config{Options: []string{"a", "b"}, AllowClear: true})
	press(t, &m, "l") // a
	press(t, &m, "l") // b
	press(t, &m, "l") // wraps to a, not blank
	if got := m.Value(); got != "a" {
		t.Errorf("wrapping should cycle among options, not fall back to blank; got %q", got)
	}
}

func TestBackspaceClearsSelection(t *testing.T) {
	m := New(Config{Options: []string{"a", "b"}, AllowClear: true})
	press(t, &m, "l")
	cmd := press(t, &m, "backspace")
	if got := m.Value(); got != "" {
		t.Errorf("Value() after clear = %q, want blank", got)
	}
	if cmd == nil {
		t.Fatal("clearing should emit a Changed command")
	}
	if msg, ok := cmd().(Changed); !ok || msg.Value != "" {
		t.Errorf("Changed = %#v, want Value \"\"", cmd())
	}
}

func TestBackspaceIgnoredWithoutAllowClear(t *testing.T) {
	m := New(Config{Options: []string{"a", "b"}})
	cmd := press(t, &m, "backspace")
	if cmd != nil {
		t.Error("Backspace must be a no-op when AllowClear is off")
	}
	if got := m.Value(); got != "a" {
		t.Errorf("Value() = %q, want unchanged %q", got, "a")
	}
}

func TestBackspaceOnAlreadyBlankIsNoop(t *testing.T) {
	m := New(Config{Options: []string{"a"}, AllowClear: true})
	cmd := press(t, &m, "backspace")
	if cmd != nil {
		t.Error("clearing an already-blank cycler must not emit a command")
	}
}

func TestSetOptionsWithAllowClearStaysBlankWhenNotPreviouslySelected(t *testing.T) {
	m := New(Config{Options: []string{"status", "amount"}, AllowClear: true})
	m.SetOptions([]string{"kind", "title"})
	if got := m.Value(); got != "" {
		t.Errorf("Value() = %q, want blank (never auto-selected)", got)
	}
}

func TestSetOptionsWithAllowClearKeepsExplicitSelection(t *testing.T) {
	m := New(Config{Options: []string{"status", "amount"}, AllowClear: true})
	press(t, &m, "l") // "status"
	m.SetOptions([]string{"amount", "status"})
	if got := m.Value(); got != "status" {
		t.Errorf("Value() = %q, want the selection preserved", got)
	}
}

func TestViewShowsPlaceholderWhenBlankWithOptions(t *testing.T) {
	m := New(Config{Options: []string{"status"}, Placeholder: "field", AllowClear: true})
	got := m.View()
	if !strings.Contains(got, "field") {
		t.Errorf("View() = %q, want the placeholder while unselected", got)
	}
	if strings.Contains(got, "status") {
		t.Errorf("View() = %q, must not show an option before one is chosen", got)
	}
}

func TestBorderColorTracksFocusAndWarning(t *testing.T) {
	plain := New(Config{Options: []string{"a"}})
	if plain.borderColor() != theme.Border {
		t.Error("unfocused cycler should use the quiet border")
	}
	plain.Focus()
	if plain.borderColor() != theme.Accent {
		t.Error("focused cycler should use the accent border")
	}

	warn := New(Config{Options: []string{"a"}, Warning: true})
	warn.Focus()
	if warn.borderColor() != theme.Warning {
		t.Error("focused Warning cycler should use the warning border")
	}
}
