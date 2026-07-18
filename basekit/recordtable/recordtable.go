// Package recordtable provides the spine widget of basekit apps: a
// row-cursor table over schemaless records with dynamic column inference,
// config-driven column order and widths, and cursor preservation across
// reloads. It wraps bubbles/table; the wrapper owns the mapping from
// records to columns/rows, the inner table owns rendering and navigation.
package recordtable

import (
	"fmt"
	"sort"
	"strings"

	"github.com/charmbracelet/bubbles/table"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/rhsev/matterbase/basekit/theme"
)

// Record is one schemaless record. Keys starting with "_" are metadata
// (grubber's _note_file etc.) and never become columns.
type Record = map[string]any

// LeadColumn is an optional computed first column, e.g. matterbase's
// "source" column derived from _note_file.
type LeadColumn struct {
	Title string
	Width int
	Value func(Record) string
}

// Config controls column layout. The zero value is usable: columns are
// inferred from the records, every column is DefaultWidth wide.
type Config struct {
	// Columns fixes the column order; keys absent from the current record
	// set are dropped. Empty means: infer from the records.
	//
	// Inferred order is alphabetical, not the Python original's first-seen
	// order — Go maps don't preserve key order, so records arriving as
	// map[string]any have already lost it. Configs that care set Columns.
	Columns []string
	// Widths overrides the width per column key (including the lead
	// column's title). DefaultWidth applies otherwise.
	Widths       map[string]int
	DefaultWidth int // 0 means 20
	// Lead, when set, is rendered before the record columns.
	Lead *LeadColumn
}

// Highlighted is emitted as a tea.Msg when the cursor lands on a
// different record; apps typically update their preview pane on it.
type Highlighted struct {
	Record Record
	Index  int
}

// Selected is emitted on enter; apps typically open the record's source.
type Selected struct {
	Record Record
	Index  int
}

// Model is the widget. Use New, then SetRecords whenever the pipeline
// delivers a new result set.
type Model struct {
	cfg      Config
	tbl      table.Model
	records  []Record
	keys     []string
	override []string // runtime column selection (the TUI column picker)

	width       int // content width from SetSize; 0 means unconstrained
	height      int // content height from SetSize; View must never exceed it
	hiddenCount int // candidate columns dropped because they didn't fit
}

func New(cfg Config) Model {
	if cfg.DefaultWidth <= 0 {
		cfg.DefaultWidth = 20
	}
	tbl := table.New(
		table.WithFocused(true),
		table.WithStyles(theme.Table()),
	)
	return Model{cfg: cfg, tbl: tbl}
}

// SetRecords replaces the record set, re-derives the columns and clamps
// the cursor to the previous row (the Textual original's behaviour, so a
// refresh doesn't throw the user back to the top).
//
// There is no horizontal scrolling (bubbles/table can't), so the shown
// columns are capped to what fits the current width (see SetSize): with
// no explicit Columns/override, candidates are ranked by fill rate (the
// fraction of records with a non-nil value, ties alphabetical) and as
// many as fit are kept, then re-sorted alphabetically for display: an
// explicit order is capped the same way but keeps its given order.
// Dropped columns are visible via HiddenColumns and the record preview.
func (m *Model) SetRecords(recs []Record) {
	prev := m.tbl.Cursor()
	m.records = recs
	explicit := m.override
	if len(explicit) == 0 {
		explicit = m.cfg.Columns
	}
	full := inferColumns(recs, explicit)

	var shown []string
	if len(explicit) > 0 {
		shown = m.fitColumns(full)
	} else {
		shown = m.fitColumns(rankByFillRate(recs, full))
		sort.Strings(shown)
	}
	m.keys = shown
	m.hiddenCount = len(full) - len(shown)

	cols := make([]table.Column, 0, len(m.keys)+1)
	if m.cfg.Lead != nil {
		cols = append(cols, table.Column{
			Title: m.cfg.Lead.Title,
			Width: m.columnWidth(m.cfg.Lead.Title, m.cfg.Lead.Width),
		})
	}
	for _, k := range m.keys {
		cols = append(cols, table.Column{Title: k, Width: m.columnWidth(k, 0)})
	}

	rows := make([]table.Row, len(recs))
	for i, rec := range recs {
		row := make(table.Row, 0, len(cols))
		if m.cfg.Lead != nil {
			row = append(row, m.cfg.Lead.Value(rec))
		}
		for _, k := range m.keys {
			row = append(row, cellString(rec[k]))
		}
		rows[i] = row
	}

	// Rows are rendered against the current column set; clear them first
	// so the swap never renders rows against mismatched columns.
	m.tbl.SetRows(nil)
	m.tbl.SetColumns(cols)
	m.tbl.SetRows(rows)
	m.applyHeight()

	if len(recs) == 0 {
		return
	}
	cursor := 0
	if prev > 0 {
		cursor = min(prev, len(recs)-1)
	}
	m.tbl.SetCursor(cursor)
}

func (m *Model) columnWidth(key string, fallback int) int {
	if w, ok := m.cfg.Widths[key]; ok {
		return w
	}
	if fallback > 0 {
		return fallback
	}
	return m.cfg.DefaultWidth
}

// fitColumns keeps columns, in order, while they still fit the content
// width budget (lead column deducted first). At least one column always
// survives so the table is never empty. Width 0 (not yet sized) means
// unconstrained — every candidate is kept.
func (m *Model) fitColumns(keys []string) []string {
	if m.width <= 0 {
		return keys
	}
	budget := m.width
	if m.cfg.Lead != nil {
		budget -= m.columnWidth(m.cfg.Lead.Title, m.cfg.Lead.Width)
	}
	var fit []string
	for _, k := range keys {
		w := m.columnWidth(k, 0)
		if budget-w < 0 && len(fit) > 0 {
			break
		}
		budget -= w
		fit = append(fit, k)
	}
	return fit
}

// HiddenColumns reports how many candidate columns didn't fit the
// current width — apps surface this as "+N fields → preview (m)".
func (m Model) HiddenColumns() int { return m.hiddenCount }

// rankByFillRate orders keys by the fraction of records with a non-nil
// value, most-filled first, ties broken alphabetically.
func rankByFillRate(records []Record, keys []string) []string {
	counts := make(map[string]int, len(keys))
	for _, rec := range records {
		for _, k := range keys {
			if rec[k] != nil {
				counts[k]++
			}
		}
	}
	ranked := append([]string(nil), keys...)
	sort.Slice(ranked, func(i, j int) bool {
		if counts[ranked[i]] != counts[ranked[j]] {
			return counts[ranked[i]] > counts[ranked[j]]
		}
		return ranked[i] < ranked[j]
	})
	return ranked
}

// Current returns the record under the cursor.
func (m Model) Current() (Record, int, bool) {
	i := m.tbl.Cursor()
	if i >= 0 && i < len(m.records) {
		return m.records[i], i, true
	}
	return nil, -1, false
}

// Len returns the number of records currently shown.
func (m Model) Len() int { return len(m.records) }

// Columns returns the record column keys currently shown (without lead).
func (m Model) Columns() []string { return m.keys }

// CandidateColumns returns every column key the current record set could
// show (inference without config or override) — the column picker's
// item list.
func (m Model) CandidateColumns() []string {
	return inferColumns(m.records, nil)
}

// SetColumnOverride replaces the column selection at runtime (the TUI
// column picker). The override is session state, like the built query —
// it never touches the config. Empty restores config/inference.
func (m *Model) SetColumnOverride(keys []string) {
	m.override = keys
	m.SetRecords(m.records)
}

func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	if key, ok := msg.(tea.KeyMsg); ok && key.String() == "enter" {
		if rec, i, ok := m.Current(); ok {
			return m, func() tea.Msg { return Selected{rec, i} }
		}
		return m, nil
	}
	prev := m.tbl.Cursor()
	var cmd tea.Cmd
	m.tbl, cmd = m.tbl.Update(msg)
	if cur := m.tbl.Cursor(); cur != prev {
		if rec, i, ok := m.Current(); ok {
			highlight := func() tea.Msg { return Highlighted{rec, i} }
			return m, tea.Batch(cmd, highlight)
		}
	}
	return m, cmd
}

func (m Model) View() string { return m.tbl.View() }

// SetSize sets the table's content dimensions. A width change re-fits
// the shown columns (see SetRecords) — this is what keeps the header
// row aligned with the (possibly narrower) row content after a resize.
func (m *Model) SetSize(w, h int) {
	m.tbl.SetWidth(w)
	m.height = h
	m.applyHeight()
	if w != m.width {
		m.width = w
		if m.records != nil {
			m.SetRecords(m.records)
		}
	}
}

// applyHeight sizes the inner table so its View never exceeds the height
// SetSize asked for. bubbles/table v1.0.0 doesn't fully account for the
// header's border-bottom line: with theme.Table()'s bordered header,
// View() comes out one line taller than SetHeight asked for — and by how
// much depends on the current columns/rows, so this runs from SetSize
// AND at the end of SetRecords (the empty table measures differently
// than a filled one; sizing typically happens before the first records
// arrive). A frame even one line over the terminal budget makes the
// terminal scroll and desyncs Bubble Tea's renderer (duplicated headers,
// lost underline). Self-calibrating instead of hardcoding the off-by-one
// so a future bubbles or theme change can't silently reintroduce it.
func (m *Model) applyHeight() {
	if m.height <= 0 {
		return
	}
	m.tbl.SetHeight(m.height)
	if over := lipgloss.Height(m.tbl.View()) - m.height; over > 0 && m.height > over {
		m.tbl.SetHeight(m.height - over)
	}
}

func (m *Model) Focus() { m.tbl.Focus() }
func (m *Model) Blur()  { m.tbl.Blur() }

// inferColumns ports the matterbase table logic: "_"-prefixed keys are
// skipped, columns whose value is nil in every record are dropped, and an
// explicit order wins but is filtered to keys actually present.
func inferColumns(recs []Record, explicit []string) []string {
	present := map[string]bool{} // key occurs at all
	nonNil := map[string]bool{}  // key has at least one non-nil value
	for _, rec := range recs {
		for k, v := range rec {
			if strings.HasPrefix(k, "_") {
				continue
			}
			present[k] = true
			if v != nil {
				nonNil[k] = true
			}
		}
	}

	var keys []string
	if len(explicit) > 0 {
		for _, k := range explicit {
			if present[k] && nonNil[k] {
				keys = append(keys, k)
			}
		}
		return keys
	}
	for k := range present {
		if nonNil[k] {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	return keys
}

// cellString renders a field value like the Python original's str():
// nil becomes empty, everything else goes through fmt. Newlines are
// flattened so multiline YAML values can't break the row layout.
func cellString(v any) string {
	if v == nil {
		return ""
	}
	s := fmt.Sprintf("%v", v)
	if strings.ContainsRune(s, '\n') {
		s = strings.Join(strings.Fields(s), " ")
	}
	return s
}
