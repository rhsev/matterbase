package togglelist

import (
	"reflect"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func items() []Item {
	return []Item{{"alpha", true}, {"beta", false}, {"gamma", false}}
}

func TestToggleEmitsAndFlips(t *testing.T) {
	m := New(items())
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m, cmd := m.Update(tea.KeyMsg{Type: tea.KeySpace})
	if cmd == nil {
		t.Fatal("toggle should emit a command")
	}
	msg, ok := cmd().(Toggled)
	if !ok {
		t.Fatalf("expected Toggled, got %T", cmd())
	}
	if msg.Index != 1 || !msg.Item.Active {
		t.Fatalf("Toggled = %+v, want index 1 active", msg)
	}
	if got := m.ActiveLabels(); !reflect.DeepEqual(got, []string{"alpha", "beta"}) {
		t.Fatalf("ActiveLabels = %v", got)
	}
}

func TestCursorBoundsAndSetItems(t *testing.T) {
	m := New(items())
	for range 10 {
		m, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown})
	}
	if m.cursor != 2 {
		t.Fatalf("cursor = %d, want 2 (clamped)", m.cursor)
	}
	m.SetItems(items()[:1])
	if m.cursor != 0 {
		t.Fatalf("cursor after shrink = %d, want 0", m.cursor)
	}
}

func TestViewMarksActive(t *testing.T) {
	view := New(items()).View()
	if !strings.Contains(view, "■ alpha") {
		t.Error("active item should render with ■")
	}
	if strings.Contains(view, "■ beta") {
		t.Error("inactive item must not render with ■")
	}
}

// collectMsgs runs a (possibly batched) command and returns every
// resulting message.
func collectMsgs(cmd tea.Cmd) []tea.Msg {
	if cmd == nil {
		return nil
	}
	msg := cmd()
	if batch, ok := msg.(tea.BatchMsg); ok {
		var out []tea.Msg
		for _, c := range batch {
			out = append(out, collectMsgs(c)...)
		}
		return out
	}
	return []tea.Msg{msg}
}

func TestClearDeactivatesEveryActiveItem(t *testing.T) {
	m := New([]Item{{"alpha", true}, {"beta", false}, {"gamma", true}})
	m, cmd := m.Update(tea.KeyMsg{Type: tea.KeyBackspace})
	if cmd == nil {
		t.Fatal("clearing active items should emit commands")
	}
	if got := m.ActiveLabels(); len(got) != 0 {
		t.Fatalf("ActiveLabels = %v, want none active", got)
	}

	msgs := collectMsgs(cmd)
	if len(msgs) != 2 {
		t.Fatalf("got %d Toggled messages, want 2 (one per active item)", len(msgs))
	}
	for _, raw := range msgs {
		msg, ok := raw.(Toggled)
		if !ok {
			t.Fatalf("expected Toggled, got %T", raw)
		}
		if msg.Item.Active {
			t.Errorf("Toggled for index %d still reports Active", msg.Index)
		}
	}
}

func TestClearWithNothingActiveEmitsNoCommand(t *testing.T) {
	m := New(items()[1:]) // beta, gamma — both inactive
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyBackspace})
	if cmd != nil {
		t.Error("clearing with nothing active must not emit a command")
	}
}

func TestClearWithDeleteAndCtrlU(t *testing.T) {
	for _, key := range []tea.KeyType{tea.KeyDelete, tea.KeyCtrlU} {
		m := New(items()) // alpha active
		m, cmd := m.Update(tea.KeyMsg{Type: key})
		if cmd == nil {
			t.Fatalf("key %v should clear the active item", key)
		}
		if got := m.ActiveLabels(); len(got) != 0 {
			t.Fatalf("key %v: ActiveLabels = %v, want none active", key, got)
		}
	}
}
