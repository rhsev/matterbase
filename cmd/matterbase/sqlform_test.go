package main

import (
	"os"
	"reflect"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/rhsev/matterbase/basekit/input"
)

func TestFormFieldKeys(t *testing.T) {
	records := []Record{
		{"_note_file": "/a.md", "status": "active", "amount": 50},
		{"_note_file": "/b.md", "status": "done", "kind": "letter"},
	}
	got := formFieldKeys(records)
	want := []string{"amount", "kind", "status"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("formFieldKeys = %v, want %v", got, want)
	}
}

func TestFormFieldKeysEmpty(t *testing.T) {
	if got := formFieldKeys(nil); len(got) != 0 {
		t.Fatalf("formFieldKeys(nil) = %v, want empty", got)
	}
}

func TestFocusOrderReachesFormFields(t *testing.T) {
	cases := []struct{ from, want focusZone }{
		{focusSQL, focusFormField},
		{focusFormField, focusFormOp},
		{focusFormOp, focusFormValue},
		{focusFormValue, focusFilename},
	}
	for _, c := range cases {
		if got := nextZone(c.from, 1); got != c.want {
			t.Errorf("nextZone(%v, 1) = %v, want %v", c.from, got, c.want)
		}
	}
}

func TestFinishQueryPopulatesFormFieldOptions(t *testing.T) {
	m := newAppModel(&Config{NotesDir: t.TempDir()})
	defer os.Remove(m.cachePath)

	records := []Record{
		{"_note_file": "/a.md", "status": "active"},
		{"_note_file": "/b.md", "kind": "letter"},
	}
	m.finishQuery(pipelineDoneMsg{gen: m.gen, result: PipelineResult{Records: records}})

	want := []string{"kind", "status"}
	if got := m.formField.Options(); !reflect.DeepEqual(got, want) {
		t.Fatalf("formField.Options() = %v, want %v", got, want)
	}
}

func TestSQLFormBuildsClauseAndAppendsToSQL(t *testing.T) {
	m := newAppModel(&Config{NotesDir: t.TempDir()})
	defer os.Remove(m.cachePath)

	m.state.SQLWhere = "amount > 100"
	m.sqlIn.SetValue(m.state.SQLWhere)
	m.formField.SetOptions([]string{"status", "amount"}) // right -> "status"
	m.formField, _ = m.formField.Update(tea.KeyMsg{Type: tea.KeyRight})
	m.formOp, _ = m.formOp.Update(tea.KeyMsg{Type: tea.KeyRight}) // right -> "="
	m.focus = focusFormValue

	updated, _ := m.handleSubmitted(input.Submitted{Value: "active"})
	got := updated.(appModel)

	want := "amount > 100 AND status = 'active'"
	if got.state.SQLWhere != want {
		t.Fatalf("SQLWhere = %q, want %q", got.state.SQLWhere, want)
	}
	if got.sqlIn.Value() != want {
		t.Fatalf("sqlIn.Value() = %q, want %q", got.sqlIn.Value(), want)
	}
	if got.formValue.Value() != "" {
		t.Fatalf("formValue.Value() = %q, want cleared", got.formValue.Value())
	}
	if got.focus != focusTable {
		t.Fatalf("focus = %v, want focusTable (success returns to the table)", got.focus)
	}
}

func TestSQLFormWithoutFieldShowsErrorAndKeepsFocus(t *testing.T) {
	m := newAppModel(&Config{NotesDir: t.TempDir()})
	defer os.Remove(m.cachePath)

	m.focus = focusFormValue
	// formField has no options yet, so Value() == "" and buildClause fails.

	updated, cmd := m.handleSubmitted(input.Submitted{Value: "active"})
	got := updated.(appModel)

	if cmd != nil {
		t.Error("a failed form submission must not trigger a query")
	}
	if !got.statusErr {
		t.Error("expected statusErr to be set")
	}
	if got.focus != focusFormValue {
		t.Fatalf("focus = %v, want focusFormValue (stay put to fix the input)", got.focus)
	}
}

func TestSQLFormWithoutOperatorShowsErrorAndKeepsFocus(t *testing.T) {
	m := newAppModel(&Config{NotesDir: t.TempDir()})
	defer os.Remove(m.cachePath)

	m.formField.SetOptions([]string{"status"})
	m.formField, _ = m.formField.Update(tea.KeyMsg{Type: tea.KeyRight})
	// formOp is untouched, starts blank — matches Textual's Select.
	m.focus = focusFormValue

	updated, cmd := m.handleSubmitted(input.Submitted{Value: "active"})
	got := updated.(appModel)

	if cmd != nil {
		t.Error("a missing operator must not trigger a query")
	}
	if !got.statusErr {
		t.Error("expected statusErr to be set")
	}
	if got.focus != focusFormValue {
		t.Fatalf("focus = %v, want focusFormValue (stay put to fix the input)", got.focus)
	}
}

func TestSQLFormIsNullNeedsNoValue(t *testing.T) {
	m := newAppModel(&Config{NotesDir: t.TempDir()})
	defer os.Remove(m.cachePath)

	m.formField.SetOptions([]string{"binder"})
	m.formField, _ = m.formField.Update(tea.KeyMsg{Type: tea.KeyRight})
	for m.formOp.Value() != "IS NULL" {
		m.formOp, _ = m.formOp.Update(tea.KeyMsg{Type: tea.KeyRight})
	}
	m.focus = focusFormValue

	updated, _ := m.handleSubmitted(input.Submitted{Value: ""})
	got := updated.(appModel)

	want := "binder IS NULL"
	if got.state.SQLWhere != want {
		t.Fatalf("SQLWhere = %q, want %q", got.state.SQLWhere, want)
	}
	if got.focus != focusTable {
		t.Fatalf("focus = %v, want focusTable", got.focus)
	}
}
