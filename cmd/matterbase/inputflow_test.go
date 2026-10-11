package main

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// drive feeds msg through Update and then runs the resulting command
// tree the way Bubble Tea would, feeding every message back in. It
// returns the model and the number of queries started (each one bumps
// m.gen). Query results are not fed back.
func drive(t *testing.T, m appModel, msg tea.Msg) (appModel, int) {
	t.Helper()
	queries := 0
	var step func(tea.Msg)
	var run func(tea.Cmd)
	steps := 0
	step = func(msg tea.Msg) {
		if steps++; steps > 100 {
			t.Fatal("drive: message loop does not settle")
		}
		gen := m.gen
		next, cmd := m.Update(msg)
		m = next.(appModel)
		queries += m.gen - gen
		run(cmd)
	}
	run = func(cmd tea.Cmd) {
		if cmd == nil {
			return
		}
		msg := cmd()
		switch msg := msg.(type) {
		case nil:
		case tea.BatchMsg:
			for _, c := range msg {
				run(c)
			}
		case pipelineDoneMsg:
		default:
			step(msg)
		}
	}
	step(msg)
	return m, queries
}

// newFlowModel stubs the grubber replay out, so query commands run
// without the grubber binary.
func newFlowModel(t *testing.T) appModel {
	t.Helper()
	return newFlowModelWith(t, &Config{NotesDir: t.TempDir()})
}

func newFlowModelWith(t *testing.T, cfg *Config) appModel {
	t.Helper()
	orig := queryCachedRecordsFn
	queryCachedRecordsFn = func(string, []string, []string, func(string)) []Record { return nil }
	m := newAppModel(cfg)
	t.Cleanup(func() {
		queryCachedRecordsFn = orig
		os.Remove(m.cachePath)
	})
	return m
}

func typeKey(r rune) tea.KeyMsg { return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}} }

func TestTypingInFilenameRunsQueryAfterDebounce(t *testing.T) {
	m := newFlowModel(t)
	m.setFocus(focusFilename)

	m, queries := drive(t, m, typeKey('x'))

	if m.state.FilenameTerm != "x" {
		t.Errorf("FilenameTerm = %q, want %q", m.state.FilenameTerm, "x")
	}
	if queries != 1 {
		t.Errorf("queries after the debounce = %d, want 1", queries)
	}
}

func TestEnterInFulltextRunsQuery(t *testing.T) {
	m := newFlowModel(t)
	m.setFocus(focusFulltext)
	m.fulltextIn.SetValue("needle")

	m, queries := drive(t, m, tea.KeyMsg{Type: tea.KeyEnter})

	if m.state.FulltextTerm != "needle" {
		t.Errorf("FulltextTerm = %q, want %q", m.state.FulltextTerm, "needle")
	}
	if queries != 1 {
		t.Errorf("queries after Enter = %d, want 1", queries)
	}
	if m.focus != focusTable {
		t.Errorf("focus = %v, want focusTable", m.focus)
	}
}

// Esc inside the debounce window moves focus to the table; the term
// typed before it must still reach the state and the query.
func TestDebouncedQueryRunsAfterFocusMovedAway(t *testing.T) {
	m := newFlowModel(t)
	m.setFocus(focusFilename)

	next, cmd := m.Update(typeKey('x'))
	m = next.(appModel)
	m, _ = drive(t, m, tea.KeyMsg{Type: tea.KeyEsc})
	if m.focus != focusTable {
		t.Fatalf("focus = %v, want focusTable", m.focus)
	}

	queries := 0
	for _, msg := range runAll(cmd) {
		var n int
		m, n = drive(t, m, msg)
		queries += n
	}
	if m.state.FilenameTerm != "x" {
		t.Errorf("FilenameTerm = %q, want %q", m.state.FilenameTerm, "x")
	}
	if queries != 1 {
		t.Errorf("queries = %d, want 1", queries)
	}
}

// runAll executes a command tree and returns its messages without
// feeding them anywhere.
func runAll(cmd tea.Cmd) []tea.Msg {
	if cmd == nil {
		return nil
	}
	msg := cmd()
	if batch, ok := msg.(tea.BatchMsg); ok {
		var out []tea.Msg
		for _, c := range batch {
			out = append(out, runAll(c)...)
		}
		return out
	}
	return []tea.Msg{msg}
}

func TestQueryResultArrivesWhilePickerIsOpen(t *testing.T) {
	m := newFlowModel(t)
	m.width, m.height = 120, 40
	m.applyLayout()
	m.openPicker()
	m.runQueryCmd()

	records := []Record{{"_note_file": "/a.md"}, {"_note_file": "/b.md"}}
	next, _ := m.Update(pipelineDoneMsg{gen: m.gen, result: PipelineResult{Records: records}})
	m = next.(appModel)

	if !m.picking {
		t.Error("the picker closed on a query result")
	}
	if got := m.table.Len(); got != 2 {
		t.Errorf("table has %d records, want 2 (result dropped behind the picker)", got)
	}
}

// Overlapping queries with full-text active each fill their own copy of
// the file cache. Run with -race: a shared map shows up as a data race.
func TestOverlappingFulltextQueriesDoNotShareCache(t *testing.T) {
	m := newFlowModel(t)
	dir := t.TempDir()
	var records []Record
	for i := range 50 {
		path := filepath.Join(dir, "n"+strconv.Itoa(i)+".md")
		if err := os.WriteFile(path, []byte("needle"), 0o644); err != nil {
			t.Fatal(err)
		}
		records = append(records, Record{"_note_file": path})
	}
	queryCachedRecordsFn = func(string, []string, []string, func(string)) []Record { return records }

	m.state.FulltextTerm = "needle"
	first, second := m.runQueryCmd(), m.runQueryCmd()

	done := make(chan tea.Msg)
	go func() { done <- first() }()
	latest := second().(pipelineDoneMsg)
	<-done

	m.finishQuery(latest)
	if got := len(m.fulltextCache); got != 50 {
		t.Errorf("fulltextCache has %d entries, want 50", got)
	}
	if got := m.table.Len(); got != 50 {
		t.Errorf("table has %d records, want 50", got)
	}
}
