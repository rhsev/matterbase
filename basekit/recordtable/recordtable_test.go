package recordtable

import (
	"reflect"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func testRecords() []Record {
	return []Record{
		{"title": "a", "status": "active", "_note_file": "/n/a.md", "ghost": nil},
		{"title": "b", "amount": 12, "_note_file": "/n/b.md"},
		{"title": "c", "status": "done", "kind": "letter"},
	}
}

func TestInferColumnsSkipsMetaAndAllNil(t *testing.T) {
	got := inferColumns(testRecords(), nil)
	// alphabetical; "_note_file" (meta) and "ghost" (nil everywhere) dropped
	want := []string{"amount", "kind", "status", "title"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("inferColumns = %v, want %v", got, want)
	}
}

func TestInferColumnsExplicitOrderWinsAndFilters(t *testing.T) {
	got := inferColumns(testRecords(), []string{"status", "missing", "title", "ghost"})
	want := []string{"status", "title"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("inferColumns explicit = %v, want %v", got, want)
	}
}

func TestCellString(t *testing.T) {
	cases := []struct {
		in   any
		want string
	}{
		{nil, ""},
		{"x", "x"},
		{42, "42"},
		{true, "true"},
		{[]any{"a", "b"}, "[a b]"},
		{"line one\nline two", "line one line two"},
	}
	for _, c := range cases {
		if got := cellString(c.in); got != c.want {
			t.Errorf("cellString(%v) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestCursorPreservedAcrossReload(t *testing.T) {
	m := New(Config{})
	m.SetSize(80, 10)
	m.SetRecords(testRecords())

	// move cursor to the last row
	for range 2 {
		m, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown})
	}
	if _, i, _ := m.Current(); i != 2 {
		t.Fatalf("cursor = %d, want 2", i)
	}

	// same-size reload keeps the row
	m.SetRecords(testRecords())
	if _, i, _ := m.Current(); i != 2 {
		t.Fatalf("cursor after reload = %d, want 2", i)
	}

	// shrinking reload clamps
	m.SetRecords(testRecords()[:1])
	if _, i, _ := m.Current(); i != 0 {
		t.Fatalf("cursor after shrink = %d, want 0", i)
	}

	// empty reload yields no current record
	m.SetRecords(nil)
	if _, _, ok := m.Current(); ok {
		t.Fatal("Current() should report no record on empty set")
	}
}

func TestUpdateEmitsHighlightedAndSelected(t *testing.T) {
	m := New(Config{})
	m.SetSize(80, 10)
	m.SetRecords(testRecords())

	m2, cmd := m.Update(tea.KeyMsg{Type: tea.KeyDown})
	if cmd == nil {
		t.Fatal("cursor move should emit a command")
	}
	if msg := findMsg[Highlighted](t, cmd); msg.Index != 1 {
		t.Fatalf("Highlighted.Index = %d, want 1", msg.Index)
	}

	_, cmd = m2.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("enter should emit a command")
	}
	if msg := findMsg[Selected](t, cmd); msg.Index != 1 || msg.Record["title"] != "b" {
		t.Fatalf("Selected = %+v, want index 1 / title b", msg)
	}
}

func TestLeadColumnAndView(t *testing.T) {
	m := New(Config{
		Columns: []string{"title", "status"},
		Widths:  map[string]int{"source": 12, "title": 10},
		Lead: &LeadColumn{
			Title: "source",
			Width: 30,
			Value: func(r Record) string {
				if f, ok := r["_note_file"].(string); ok {
					return f
				}
				return ""
			},
		},
	})
	m.SetSize(80, 10)
	m.SetRecords(testRecords())

	view := m.View()
	for _, want := range []string{"source", "title", "status"} {
		if !strings.Contains(view, want) {
			t.Errorf("view missing header %q", want)
		}
	}
	if !strings.Contains(view, "/n/a.md") {
		t.Errorf("view missing lead column value")
	}
	if strings.Contains(view, "amount") {
		t.Errorf("explicit Columns should drop 'amount'")
	}
}

// findMsg runs a (possibly batched) command and returns the first message
// of type T it produces.
func findMsg[T tea.Msg](t *testing.T, cmd tea.Cmd) T {
	t.Helper()
	var zero T
	for _, msg := range collectMsgs(cmd) {
		if m, ok := msg.(T); ok {
			return m
		}
	}
	t.Fatalf("no %T in command output", zero)
	return zero
}

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

func TestColumnOverrideAndCandidates(t *testing.T) {
	m := New(Config{Columns: []string{"title"}})
	m.SetSize(80, 10)
	m.SetRecords(testRecords())

	if got := m.Columns(); !reflect.DeepEqual(got, []string{"title"}) {
		t.Fatalf("config columns = %v, want [title]", got)
	}
	// candidates ignore config and override
	want := []string{"amount", "kind", "status", "title"}
	if got := m.CandidateColumns(); !reflect.DeepEqual(got, want) {
		t.Fatalf("CandidateColumns = %v, want %v", got, want)
	}

	m.SetColumnOverride([]string{"status", "amount"})
	if got := m.Columns(); !reflect.DeepEqual(got, []string{"status", "amount"}) {
		t.Fatalf("override columns = %v", got)
	}

	// empty override falls back to the config
	m.SetColumnOverride(nil)
	if got := m.Columns(); !reflect.DeepEqual(got, []string{"title"}) {
		t.Fatalf("columns after clearing override = %v, want [title]", got)
	}
}

func TestUnconstrainedWidthShowsAllColumns(t *testing.T) {
	m := New(Config{})
	m.SetRecords(testRecords()) // no SetSize call: width 0, unconstrained
	want := []string{"amount", "kind", "status", "title"}
	if got := m.Columns(); !reflect.DeepEqual(got, want) {
		t.Fatalf("Columns = %v, want %v", got, want)
	}
	if got := m.HiddenColumns(); got != 0 {
		t.Fatalf("HiddenColumns = %d, want 0", got)
	}
}

func TestFillRateRankingCapsToWidth(t *testing.T) {
	// testRecords() fill counts: title=3, status=2, amount=1, kind=1.
	// Width 45 fits exactly two DefaultWidth(20) columns.
	m := New(Config{})
	m.SetSize(45, 10)
	m.SetRecords(testRecords())

	want := []string{"status", "title"} // top-2 by fill rate, alphabetical for display
	if got := m.Columns(); !reflect.DeepEqual(got, want) {
		t.Fatalf("Columns = %v, want %v", got, want)
	}
	if got := m.HiddenColumns(); got != 2 {
		t.Fatalf("HiddenColumns = %d, want 2", got)
	}
}

func TestExplicitColumnsCapToWidthPreservingOrder(t *testing.T) {
	m := New(Config{Columns: []string{"amount", "kind", "status", "title"}})
	m.SetSize(45, 10)
	m.SetRecords(testRecords())

	want := []string{"amount", "kind"} // first two in explicit order that fit, order kept
	if got := m.Columns(); !reflect.DeepEqual(got, want) {
		t.Fatalf("Columns = %v, want %v", got, want)
	}
	if got := m.HiddenColumns(); got != 2 {
		t.Fatalf("HiddenColumns = %d, want 2", got)
	}
}

func TestFitColumnsNeverEmpty(t *testing.T) {
	// Even a single DefaultWidth(20) column doesn't fit width 5 — at
	// least one column must survive so the table is never empty.
	m := New(Config{})
	m.SetSize(5, 10)
	m.SetRecords(testRecords())
	if got := m.Columns(); len(got) != 1 {
		t.Fatalf("Columns = %v, want exactly 1 (never empty)", got)
	}
}

func TestResizeRefitsColumnsAndKeepsHeaderRowWidthConsistent(t *testing.T) {
	m := New(Config{})
	m.SetSize(80, 10)
	m.SetRecords(testRecords())
	if got := m.Columns(); len(got) != 4 {
		t.Fatalf("expected all 4 columns to fit at width 80, got %v", got)
	}

	// Narrow the terminal without calling SetRecords again — SetSize
	// alone must re-fit (this is what keeps header and rows aligned
	// across a resize, see recordtable.go's SetSize doc comment).
	m.SetSize(45, 10)
	got := m.Columns()
	if len(got) != 2 {
		t.Fatalf("expected 2 columns to fit at width 45, got %v", got)
	}

	// The declared column width must never exceed the table's width: if
	// it did, bubbles/table's row viewport (cropped to tbl.Width) would
	// show narrower rows than the uncropped header — the header/row
	// misalignment this policy exists to prevent.
	total := 0
	for _, c := range m.tbl.Columns() {
		total += c.Width
	}
	if total > 45 {
		t.Fatalf("declared column width sums to %d, exceeds the table width 45 — header and rows would misalign", total)
	}
}

func TestViewNeverTallerThanSetSize(t *testing.T) {
	// bubbles/table v1.0.0 renders one line taller than SetHeight with a
	// bordered header; SetSize self-calibrates. A view taller than asked
	// breaks the app's fixed frame budget (terminal scroll, renderer
	// desync).
	m := New(Config{})
	recs := make([]Record, 50)
	for i := range recs {
		recs[i] = Record{"title": "t", "status": "s"}
	}
	for _, h := range []int{5, 10, 41} {
		m.SetSize(80, h)
		m.SetRecords(recs)
		if got := lipglossHeight(m.View()); got > h {
			t.Fatalf("View is %d lines after SetSize(80, %d)", got, h)
		}
	}
}

func lipglossHeight(s string) int { return strings.Count(s, "\n") + 1 }
