package main

import (
	"context"
	"strings"
	"testing"
)

// previewModel is a sized model with one markdown note under the cursor
// and fake renderers on PATH.
func previewModel(t *testing.T) appModel {
	t.Helper()
	note := writeFile(t, "n.md", "---\ntitle: X\n---\n\nBody text.\n")
	fakeTools(t, map[string]string{"apex": "echo APEX"})
	m := sized(newFlowModel(t))
	m.table.SetRecords([]Record{{"_note_file": note, "title": "X"}})
	return m
}

// A preview that reads the note is rendered in a command, not in Update:
// the pane keeps what it shows until the result arrives.
func TestPreviewOfANoteRendersInTheBackground(t *testing.T) {
	m := previewModel(t)
	m.prev.SetContent("before")

	cmd := m.updatePreview()
	if cmd == nil {
		t.Fatal("no command: the note was rendered inside Update")
	}
	if !strings.Contains(m.prev.View(), "before") {
		t.Error("the pane lost its content before the new preview arrived")
	}
	next, _ := m.Update(cmd())
	m = next.(appModel)
	if view := m.prev.View(); !strings.Contains(view, "APEX") || !strings.Contains(view, "n.md") {
		t.Errorf("pane shows %q, want the rendered note and its title", view)
	}
}

// The field view starts no renderer and is there at once.
func TestPreviewOfFieldsRendersAtOnce(t *testing.T) {
	m := previewModel(t)
	m.previewMode = "record"
	if cmd := m.updatePreview(); cmd != nil {
		t.Error("the field view needs no command")
	}
	if !strings.Contains(m.prev.View(), "title") {
		t.Errorf("pane shows %q, want the record's fields", m.prev.View())
	}
}

// A hidden pane renders nothing; showing it renders the current record.
func TestHiddenPreviewRendersNothing(t *testing.T) {
	m := previewModel(t)
	m, _ = press(t, m, "p")
	if m.prev.Visible() {
		t.Fatal("p did not hide the pane")
	}
	if cmd := m.updatePreview(); cmd != nil {
		t.Error("a hidden pane started a render")
	}
	m, cmd := press(t, m, "p")
	if cmd == nil {
		t.Fatal("showing the pane started no render")
	}
	next, _ := m.Update(cmd())
	if view := next.(appModel).prev.View(); !strings.Contains(view, "APEX") {
		t.Errorf("pane shows %q after p", view)
	}
}

// The result of an overtaken request is dropped.
func TestStalePreviewIsDropped(t *testing.T) {
	m := previewModel(t)
	first := m.updatePreview()
	m.previewMode = "record"
	m.updatePreview() // the cursor moved on, or the mode changed
	shown := m.prev.View()

	next, _ := m.Update(first())
	if got := next.(appModel).prev.View(); got != shown {
		t.Errorf("the stale preview replaced the pane: %q", got)
	}
}

// A cancelled request does not wait for its renderer.
func TestCancelledRenderStartsNoRenderer(t *testing.T) {
	note := writeFile(t, "n.md", "Body.\n")
	fakeTools(t, map[string]string{"apex": "echo APEX"})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if got := renderWithApex(ctx, note, ApexConfig{}, nil); got != "" {
		t.Errorf("a cancelled render returned %q", got)
	}
}
