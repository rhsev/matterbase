package main

import (
	"errors"
	"strings"
	"testing"

	bkexec "github.com/rhsev/matterbase/basekit/exec"
)

// An editor that cannot be started shows in the status line.
func TestEditorErrorShowsInStatus(t *testing.T) {
	m := sized(newFlowModel(t))
	next, _ := m.Update(bkexec.EditorClosed{Err: errors.New(`exec: "nope": executable file not found in $PATH`)})
	m = next.(appModel)
	if !m.statusErr || !strings.Contains(m.status, "nope") {
		t.Errorf("status %q (err %v), want the editor error", m.status, m.statusErr)
	}

	m = sized(newFlowModel(t))
	next, _ = m.Update(bkexec.EditorClosed{})
	if next.(appModel).statusErr {
		t.Error("an editor that closed normally set an error")
	}
}

// Enter on a record whose note is gone says so.
func TestEnterOnAMissingNoteShowsInStatus(t *testing.T) {
	m := sized(newFlowModel(t))
	m.table.SetRecords([]Record{{"_note_file": "/gone/n.md"}})
	if cmd := m.openEditorCmd(); cmd != nil {
		t.Fatal("an editor command for a missing file")
	}
	if !m.statusErr || !strings.Contains(m.status, "/gone/n.md") {
		t.Errorf("status %q (err %v), want the missing path", m.status, m.statusErr)
	}
}
