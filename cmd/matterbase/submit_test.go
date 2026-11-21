package main

import (
	"errors"
	"os"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/rhsev/matterbase/basekit/input"
)

// Enter in the filename or full-text input runs the query only when the
// debounce has not run it for this value. The focus returns to the table
// either way.
func TestEnterDoesNotRepeatTheDebouncedQuery(t *testing.T) {
	for _, focus := range []focusZone{focusFilename, focusFulltext} {
		for _, pending := range []bool{true, false} {
			m := newAppModel(&Config{NotesDir: t.TempDir()})
			t.Cleanup(func() { os.Remove(m.cachePath) })
			m.focus = focus
			before := m.gen

			updated, _ := m.handleSubmitted(input.Submitted{Value: "report", Pending: pending})
			got := updated.(appModel)

			if ran := got.gen != before; ran != pending {
				t.Errorf("focus %v, pending %v: query ran = %v", focus, pending, ran)
			}
			if got.focus != focusTable {
				t.Errorf("focus %v, pending %v: focus = %v, want the table", focus, pending, got.focus)
			}
			term := got.state.FilenameTerm
			if focus == focusFulltext {
				term = got.state.FulltextTerm
			}
			if term != "report" {
				t.Errorf("focus %v, pending %v: term = %q", focus, pending, term)
			}
		}
	}
}

// Enter in the SQL input always runs the query: nothing else does.
func TestEnterInSQLAlwaysRunsTheQuery(t *testing.T) {
	m := newAppModel(&Config{NotesDir: t.TempDir()})
	t.Cleanup(func() { os.Remove(m.cachePath) })
	m.focus = focusSQL
	before := m.gen

	updated, _ := m.handleSubmitted(input.Submitted{Value: "amount > 1"})
	if got := updated.(appModel); got.gen == before || got.state.SQLWhere != "amount > 1" {
		t.Errorf("gen %d → %d, sql %q", before, got.gen, got.state.SQLWhere)
	}
}

func TestCacheIsRemovedWhenTheProgramFails(t *testing.T) {
	for _, runErr := range []error{nil, errors.New("program was killed")} {
		f, err := os.CreateTemp(t.TempDir(), "matterbase-*.jsonl")
		if err != nil {
			t.Fatal(err)
		}
		f.Close()

		if got := runWithCache(f.Name(), func() error { return runErr }); got != runErr {
			t.Errorf("runWithCache returned %v, want %v", got, runErr)
		}
		if _, err := os.Stat(f.Name()); !os.IsNotExist(err) {
			t.Errorf("run error %v: the cache is still there", runErr)
		}
	}
}

// The whole path: typing, the debounce, then Enter. One query in all.
func TestTypingThenEnterRunsOneQuery(t *testing.T) {
	m := newFlowModel(t)
	m.setFocus(focusFilename)

	m, typed := drive(t, m, typeKey('x'))
	m, entered := drive(t, m, tea.KeyMsg{Type: tea.KeyEnter})

	if typed != 1 || entered != 0 {
		t.Errorf("queries: %d after typing, %d after Enter, want 1 and 0", typed, entered)
	}
	if m.focus != focusTable {
		t.Errorf("focus = %v, want focusTable", m.focus)
	}
}
