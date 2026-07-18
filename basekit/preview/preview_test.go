package preview

import (
	"strings"
	"testing"
)

func TestNewIsVisible(t *testing.T) {
	m := New()
	if !m.Visible() {
		t.Error("New() must start visible")
	}
}

func TestToggleFlipsVisibility(t *testing.T) {
	m := New()
	m.Toggle()
	if m.Visible() {
		t.Error("Toggle() must hide a visible pane")
	}
	m.Toggle()
	if !m.Visible() {
		t.Error("Toggle() must show a hidden pane")
	}
}

func TestViewEmptyWhenHidden(t *testing.T) {
	m := New()
	m.SetSize(40, 20)
	m.SetTitle("q1.md", "whole")
	m.SetContent("body")
	m.Toggle()
	if got := m.View(); got != "" {
		t.Errorf("hidden pane's View() = %q, want empty (so frame.JoinRow omits it)", got)
	}
}

func TestViewShowsTitleAndMode(t *testing.T) {
	m := New()
	m.SetSize(40, 20)
	m.SetTitle("q1.md", "whole")
	m.SetContent("body text")

	got := m.View()
	if !strings.Contains(got, "q1.md") {
		t.Errorf("View() missing title, got %q", got)
	}
	if !strings.Contains(got, "whole") {
		t.Errorf("View() missing mode, got %q", got)
	}
	if !strings.Contains(got, "body text") {
		t.Errorf("View() missing content, got %q", got)
	}
}

func TestSetSizeReservesTitleLines(t *testing.T) {
	m := New()
	m.SetSize(40, 10)
	if m.vp.Width != 40 {
		t.Errorf("viewport width = %d, want 40", m.vp.Width)
	}
	if m.vp.Height != 8 {
		t.Errorf("viewport height = %d, want 8 (10 - title line - blank line)", m.vp.Height)
	}
}

func TestSetSizeNeverNegative(t *testing.T) {
	m := New()
	m.SetSize(10, 1)
	if m.vp.Height != 0 {
		t.Errorf("viewport height = %d, want 0 (clamped, not negative)", m.vp.Height)
	}
}
