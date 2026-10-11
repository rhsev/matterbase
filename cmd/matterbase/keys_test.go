package main

import (
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// key builds the tea.KeyMsg for a key as handleKey spells it.
func key(s string) tea.KeyMsg {
	switch s {
	case "esc":
		return tea.KeyMsg{Type: tea.KeyEsc}
	case "tab":
		return tea.KeyMsg{Type: tea.KeyTab}
	case "shift+tab":
		return tea.KeyMsg{Type: tea.KeyShiftTab}
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	case " ":
		return tea.KeyMsg{Type: tea.KeySpace, Runes: []rune{' '}}
	case "ctrl+c":
		return tea.KeyMsg{Type: tea.KeyCtrlC}
	case "ctrl+f":
		return tea.KeyMsg{Type: tea.KeyCtrlF}
	case "ctrl+b":
		return tea.KeyMsg{Type: tea.KeyCtrlB}
	}
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
}

func press(t *testing.T, m appModel, k string) (appModel, tea.Cmd) {
	t.Helper()
	if got := key(k).String(); got != k {
		t.Fatalf("key(%q) reads as %q", k, got)
	}
	next, cmd := m.Update(key(k))
	return next.(appModel), cmd
}

func isQuit(cmd tea.Cmd) bool {
	if cmd == nil {
		return false
	}
	_, ok := cmd().(tea.QuitMsg)
	return ok
}

// sized gives the model a terminal, so the panes have a size.
func sized(m appModel) appModel {
	next, _ := m.Update(tea.WindowSizeMsg{Width: 160, Height: 40})
	return next.(appModel)
}

// stubClipboard records what y/Y copy instead of touching the clipboard.
func stubClipboard(t *testing.T, err error) *string {
	t.Helper()
	var copied string
	orig := copyToClipboard
	copyToClipboard = func(s string) error {
		copied = s
		return err
	}
	t.Cleanup(func() { copyToClipboard = orig })
	return &copied
}

func TestQuitKeys(t *testing.T) {
	for _, k := range []string{"q", "ctrl+c"} {
		m := newFlowModel(t)
		if _, cmd := press(t, m, k); !isQuit(cmd) {
			t.Errorf("%s does not quit", k)
		}
	}
}

// With an input focused, letters are text, not commands.
func TestGlobalKeysAreTextWhileInputFocused(t *testing.T) {
	m := newFlowModel(t)
	m.setFocus(focusSQL)
	m, cmd := press(t, m, "q")
	if isQuit(cmd) {
		t.Fatal("q quit while the SQL input had focus")
	}
	if got := m.sqlIn.Value(); got != "q" {
		t.Errorf("sqlIn = %q, want %q", got, "q")
	}
}

func TestYankCopiesTheCommand(t *testing.T) {
	copied := stubClipboard(t, nil)
	m := newFlowModel(t)
	m.state.SQLWhere = "amount > 1"
	want := m.buildCommand()

	m, cmd := press(t, m, "y")
	if *copied != want {
		t.Errorf("copied %q, want %q", *copied, want)
	}
	if isQuit(cmd) || m.statusErr || !strings.HasPrefix(m.status, "Copied: ") {
		t.Errorf("y: status %q (err %v), quit %v", m.status, m.statusErr, isQuit(cmd))
	}

	*copied = ""
	_, cmd = press(t, m, "Y")
	if *copied != want || !isQuit(cmd) {
		t.Errorf("Y: copied %q, quit %v", *copied, isQuit(cmd))
	}
}

func TestYankReportsClipboardFailure(t *testing.T) {
	stubClipboard(t, errors.New("no clipboard tool found"))
	for _, k := range []string{"y", "Y"} {
		m := newFlowModel(t)
		m, cmd := press(t, m, k)
		if isQuit(cmd) {
			t.Errorf("%s quit although copying failed", k)
		}
		if !m.statusErr || !strings.Contains(m.status, "no clipboard tool found") {
			t.Errorf("%s: status %q (err %v)", k, m.status, m.statusErr)
		}
	}
}

func TestPreviewModeAndToggle(t *testing.T) {
	m := sized(newFlowModel(t))
	for _, want := range []string{"compact", "record", "whole"} {
		m, _ = press(t, m, "m")
		if m.previewMode != want {
			t.Fatalf("previewMode = %q, want %q", m.previewMode, want)
		}
	}
	m, _ = press(t, m, "p")
	if m.prev.Visible() {
		t.Error("p did not hide the preview")
	}
	m, _ = press(t, m, "p")
	if !m.prev.Visible() {
		t.Error("p did not show the preview again")
	}
}

func TestPreviewScrollKeys(t *testing.T) {
	m := sized(newFlowModel(t))
	lines := make([]string, 200)
	for i := range lines {
		lines[i] = "line" + strconv.Itoa(i)
	}
	m.prev.SetContent(strings.Join(lines, "\n"))
	top := func(m appModel) string { return strings.TrimSpace(strings.Split(m.prev.View(), "\n")[2]) }

	m, _ = press(t, m, "J")
	m, _ = press(t, m, "J")
	if got := top(m); got != "line2" {
		t.Errorf("after J J the preview starts at %q, want line2", got)
	}
	m, _ = press(t, m, "K")
	if got := top(m); got != "line1" {
		t.Errorf("after K the preview starts at %q, want line1", got)
	}
	m, _ = press(t, m, "ctrl+f")
	if got := top(m); got == "line1" {
		t.Error("ctrl+f did not scroll")
	}
	m, _ = press(t, m, "ctrl+b")
	if got := top(m); got != "line1" {
		t.Errorf("ctrl+b after ctrl+f starts at %q, want line1", got)
	}

	m, _ = press(t, m, "p")
	press(t, m, "J") // hidden preview: must not panic
}

func TestFocusKeys(t *testing.T) {
	cases := []struct {
		key  string
		want focusZone
	}{
		{"/", focusSQL}, {"f", focusFilename}, {"t", focusFulltext},
	}
	for _, c := range cases {
		m := newFlowModel(t)
		m, _ = press(t, m, c.key)
		if m.focus != c.want {
			t.Errorf("%s: focus = %v, want %v", c.key, m.focus, c.want)
		}
		m, _ = press(t, m, "esc")
		if m.focus != focusTable {
			t.Errorf("esc after %s: focus = %v, want focusTable", c.key, m.focus)
		}
	}

	m := newFlowModel(t)
	m, _ = press(t, m, "tab")
	if m.focus != focusPresets {
		t.Errorf("tab from the table: focus = %v, want focusPresets", m.focus)
	}
	m, _ = press(t, m, "shift+tab")
	m, _ = press(t, m, "shift+tab")
	if m.focus != focusFulltext {
		t.Errorf("shift+tab twice from presets: focus = %v, want focusFulltext", m.focus)
	}
}

func TestRefreshKeyStartsRescan(t *testing.T) {
	m := newFlowModel(t)
	gen := m.gen
	m, cmd := press(t, m, "r")
	if m.gen != gen+1 || cmd == nil || m.status != "Refreshed" {
		t.Errorf("r: gen %d -> %d, cmd %v, status %q", gen, m.gen, cmd != nil, m.status)
	}
}

func TestMinusRemovesLastClause(t *testing.T) {
	m := newFlowModel(t)
	m.state.SQLWhere = "a = 1 AND b = 2"
	gen := m.gen
	m, _ = press(t, m, "-")
	if m.state.SQLWhere != "a = 1" || m.sqlIn.Value() != "a = 1" {
		t.Errorf("SQLWhere = %q, sqlIn = %q, want %q", m.state.SQLWhere, m.sqlIn.Value(), "a = 1")
	}
	if m.gen != gen+1 {
		t.Error("- did not run the query")
	}
}

func TestReloadKey(t *testing.T) {
	t.Run("without config path", func(t *testing.T) {
		m := newFlowModel(t)
		m, _ = press(t, m, "R")
		if !m.statusErr {
			t.Errorf("status %q, want an error", m.status)
		}
	})

	t.Run("keeps active presets by label", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "c.yml")
		write := func(presets string) {
			content := "notes_dir: " + dir + "\npresets:\n" + presets
			if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		write("  - label: a\n    query: [x=1]\n")
		cfg, errMsg := readConfig(path)
		if cfg == nil {
			t.Fatal(errMsg)
		}
		m := newFlowModelWith(t, cfg)
		m.state.Presets[0].Active = true

		write("  - label: new\n    query: [y=2]\n  - label: a\n    query: [x=1]\n")
		m, cmd := press(t, m, "R")
		if m.statusErr || m.status != "Config reloaded" || cmd == nil {
			t.Fatalf("R: status %q (err %v), cmd %v", m.status, m.statusErr, cmd != nil)
		}
		if len(m.state.Presets) != 2 || m.state.Presets[0].Active || !m.state.Presets[1].Active {
			t.Errorf("presets after reload = %+v, want only \"a\" active", m.state.Presets)
		}
	})
}

func TestColumnPicker(t *testing.T) {
	m := sized(newFlowModel(t))
	m.table.SetRecords([]Record{
		{"_note_file": "/a.md", "amount": 1, "status": "x"},
		{"_note_file": "/b.md", "amount": 2, "status": "y"},
	})
	if got := strings.Join(m.table.Columns(), ","); got != "amount" {
		t.Fatalf("columns before picking = %q, want amount (status does not fit)", got)
	}

	m, _ = press(t, m, "c")
	if !m.picking {
		t.Fatal("c did not open the picker")
	}
	// Items: amount (shown), status. Hide amount, show status.
	m, _ = drive(t, m, key(" "))
	m, _ = press(t, m, "j")
	m, _ = drive(t, m, key(" "))
	if got := strings.Join(m.table.Columns(), ","); got != "status" {
		t.Errorf("columns after picking = %q, want status", got)
	}

	m, cmd := press(t, m, "q")
	if isQuit(cmd) || m.picking {
		t.Errorf("q in the picker: quit %v, still picking %v", isQuit(cmd), m.picking)
	}
	m, _ = press(t, m, "c")
	m, _ = press(t, m, "esc")
	if m.picking {
		t.Error("esc did not close the picker")
	}
}

func TestSpaceTogglesPresetAndRunsQuery(t *testing.T) {
	m := newFlowModelWith(t, &Config{
		NotesDir: t.TempDir(),
		Presets:  []PresetDef{{Label: "a", Query: []string{"x=1"}}},
	})
	m, _ = press(t, m, "tab")
	m, queries := drive(t, m, key(" "))
	if !m.state.Presets[0].Active {
		t.Error("space did not activate the preset")
	}
	if queries != 1 {
		t.Errorf("queries = %d, want 1", queries)
	}
}

func TestEnterOpensOnlyExistingNoteFiles(t *testing.T) {
	note := filepath.Join(t.TempDir(), "n.md")
	if err := os.WriteFile(note, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		path string
		want bool
	}{
		{note, true},
		{filepath.Join(t.TempDir(), "gone.md"), false},
		{"/x/index.jsonl", false},
		{"", false},
	}
	for _, c := range cases {
		m := sized(newFlowModel(t))
		m.table.SetRecords([]Record{{"_note_file": c.path}})
		// The command is only built, never run: it would start the editor.
		if got := m.openEditorCmd() != nil; got != c.want {
			t.Errorf("%q: editor command %v, want %v", c.path, got, c.want)
		}
	}
}
