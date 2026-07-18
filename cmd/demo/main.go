// Demo harness for the basekit record table.
//
// Feed it real data:
//
//	grubber extract ~/notes -a --format=jsonl > /tmp/records.jsonl
//	go run ./cmd/demo /tmp/records.jsonl
//
// or run it bare for built-in sample records. Keys: arrows/jk move,
// enter selects, r reloads (cursor must survive), c picks columns,
// q quits.
//
// The column picker doubles as the reference for basekit's overlay
// pattern: Bubble Tea has no modals, so an open overlay (a) captures all
// key input before the widgets underneath and (b) is drawn with
// lipgloss.Place over the content area. Apps copy this pattern for any
// future modal surface.
package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/rhsev/matterbase/basekit/recordtable"
	"github.com/rhsev/matterbase/basekit/theme"
	"github.com/rhsev/matterbase/basekit/togglelist"
)

func sampleRecords() []recordtable.Record {
	return []recordtable.Record{
		{"title": "quarterly report", "status": "active", "amount": 1200, "_note_file": "/notes/q1.md"},
		{"title": "insurance letter", "status": "done", "kind": "letter", "_note_file": "/notes/allianz.md"},
		{"title": "server invoice", "status": "active", "amount": 89, "kind": "invoice", "_note_file": "/notes/hetzner.md"},
		{"title": "reading list", "status": nil, "_note_file": "/notes/books.md"},
	}
}

func loadJSONL(path string) ([]recordtable.Record, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var recs []recordtable.Record
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 1024*1024), 16*1024*1024)
	for sc.Scan() {
		line := sc.Bytes()
		if len(line) == 0 {
			continue
		}
		var rec recordtable.Record
		if err := json.Unmarshal(line, &rec); err != nil {
			return nil, fmt.Errorf("bad JSONL line: %w", err)
		}
		recs = append(recs, rec)
	}
	return recs, sc.Err()
}

type model struct {
	table   recordtable.Model
	picker  togglelist.Model
	picking bool
	status  string
	width   int
	height  int
	source  string // JSONL path, empty for samples
}

func (m model) Init() tea.Cmd { return nil }

// openPicker builds the picker items from the candidate columns, marking
// the currently shown ones active.
func (m *model) openPicker() {
	shown := map[string]bool{}
	for _, k := range m.table.Columns() {
		shown[k] = true
	}
	var items []togglelist.Item
	for _, k := range m.table.CandidateColumns() {
		items = append(items, togglelist.Item{Label: k, Active: shown[k]})
	}
	m.picker = togglelist.New(items)
	m.picking = true
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	// An open overlay captures all key input before anything underneath.
	if m.picking {
		switch msg := msg.(type) {
		case tea.KeyMsg:
			switch msg.String() {
			case "c", "esc", "q":
				m.picking = false
				return m, nil
			}
			var cmd tea.Cmd
			m.picker, cmd = m.picker.Update(msg)
			return m, cmd
		case togglelist.Toggled:
			// Apply immediately; empty selection restores inference.
			m.table.SetColumnOverride(m.picker.ActiveLabels())
			return m, nil
		}
	}

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.table.SetSize(msg.Width, msg.Height-2)
		return m, nil

	case tea.KeyMsg:
		switch msg.String() {
		case "q", "ctrl+c":
			return m, tea.Quit
		case "c":
			m.openPicker()
			return m, nil
		case "r":
			// Reload: cursor position must survive (clamped).
			recs := sampleRecords()
			if m.source != "" {
				loaded, err := loadJSONL(m.source)
				if err != nil {
					m.status = "reload failed: " + err.Error()
					return m, nil
				}
				recs = loaded
			}
			m.table.SetRecords(recs)
			m.status = fmt.Sprintf("reloaded — %d records", m.table.Len())
			return m, nil
		}

	case recordtable.Highlighted:
		m.status = fmt.Sprintf("%d/%d  %s", msg.Index+1, m.table.Len(), leadValue(msg.Record))
		return m, nil

	case recordtable.Selected:
		m.status = fmt.Sprintf("selected: %s", leadValue(msg.Record))
		return m, nil
	}

	var cmd tea.Cmd
	m.table, cmd = m.table.Update(msg)
	return m, cmd
}

func (m model) View() string {
	status := m.status
	if status == "" {
		status = fmt.Sprintf("%d records — j/k move, enter select, c columns, r reload, q quit", m.table.Len())
	}
	content := m.table.View()

	if m.picking {
		panel := theme.PaneFocused.Padding(0, 1).Render(
			theme.Title.Render("columns") + "\n\n" + m.picker.View() +
				"\n\n" + theme.Label.Render("space toggle · backspace clear all · esc close"),
		)
		// Draw the overlay centered over the content area. lipgloss v1
		// has no true compositing; Place fills the area around the panel,
		// which reads as a dimmed modal and is deliberately good enough.
		content = lipgloss.Place(
			m.width, m.height-2,
			lipgloss.Center, lipgloss.Center,
			panel,
		)
	}

	return content + "\n" + theme.StatusBar.Width(m.width).Render(status)
}

func leadValue(rec recordtable.Record) string {
	if f, ok := rec["_note_file"].(string); ok {
		return filepath.Base(f)
	}
	return ""
}

func main() {
	recs := sampleRecords()
	source := ""
	if len(os.Args) > 1 {
		loaded, err := loadJSONL(os.Args[1])
		if err != nil {
			fmt.Fprintln(os.Stderr, "Error:", err)
			os.Exit(1)
		}
		recs = loaded
		source = os.Args[1]
	}

	tbl := recordtable.New(recordtable.Config{
		Lead: &recordtable.LeadColumn{
			Title: "source",
			Width: 30,
			Value: leadValue,
		},
	})
	tbl.SetRecords(recs)

	prog := tea.NewProgram(
		model{table: tbl, source: source},
		tea.WithAltScreen(),
	)
	if _, err := prog.Run(); err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		os.Exit(1)
	}
}
