// matterbase-go: the unified record-query view, ported from the Python
// original onto basekit. builder | records | adaptive preview, a
// yankable grubber | duckdb pipeline, in-session JSONL cache.
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/rhsev/matterbase/basekit/cycler"
	bkexec "github.com/rhsev/matterbase/basekit/exec"
	"github.com/rhsev/matterbase/basekit/frame"
	"github.com/rhsev/matterbase/basekit/input"
	previewpane "github.com/rhsev/matterbase/basekit/preview"
	"github.com/rhsev/matterbase/basekit/recordtable"
	"github.com/rhsev/matterbase/basekit/theme"
	"github.com/rhsev/matterbase/basekit/togglelist"
)

const version = "0.7.0"

// ---------------------------------------------------------------------------
// Model
// ---------------------------------------------------------------------------

type focusZone int

// Order matches app.py's compose(): builder fields top to bottom, table
// last (it's mounted after the builder column and the preview pane,
// which isn't focusable). focusOrder below is the Tab ring itself.
const (
	focusPresets focusZone = iota
	focusSQL
	focusFormField
	focusFormOp
	focusFormValue
	focusFilename
	focusFulltext
	focusTable
)

type appModel struct {
	// session settings, taken over from the config (applyConfig)
	notesDir      string
	editor        string
	configPath    string
	searchMode    string
	grubberSet    string
	mmd           bool
	arrayFields   []string
	depth         *int
	tableColumns  []string
	columnWidths  map[string]int
	apex          ApexConfig
	collectionDir string

	state QueryState

	cachePath     string
	fulltextCache map[string]string
	gen           int

	table      recordtable.Model
	presets    togglelist.Model
	sqlIn      input.Model
	formField  cycler.Model // SQL form: comfort only, generates into sqlIn
	formOp     cycler.Model
	formValue  input.Model
	filenameIn input.Model
	fulltextIn input.Model
	prev       previewpane.Model
	picker     togglelist.Model
	picking    bool

	focus      focusZone
	inputFocus frame.InputFocus

	previewMode    string
	previewVisible bool

	status    string
	statusErr bool

	width, height int
}

func newAppModel(cfg *Config) appModel {
	m := appModel{
		state:         QueryState{Presets: presetsFromConfig(cfg)},
		fulltextCache: map[string]string{},
		previewMode:   "whole",
	}
	if cfg.SQL != "" {
		m.state.SQLWhere = cfg.SQL
	}
	m.applyConfig(cfg)

	f, err := os.CreateTemp("", "matterbase-*.jsonl")
	if err == nil {
		m.cachePath = f.Name()
		f.Close()
	}

	m.table = recordtable.New(recordtable.Config{
		Columns: m.tableColumns,
		Widths:  m.columnWidths,
		Lead:    &recordtable.LeadColumn{Title: "source", Width: 30, Value: leadValue},
	})
	m.presets = togglelist.New(presetItems(m.state.Presets))
	m.sqlIn = input.New(input.Config{Placeholder: "status != 'done'"})
	m.sqlIn.SetValue(m.state.SQLWhere)
	// AllowClear: matches Textual's Select, which starts blank until the
	// user actively picks a field/operator (form-op has no blank entry
	// in its option list either, but Select.BLANK is still its initial
	// value in app.py — nothing pre-selects it).
	m.formField = cycler.New(cycler.Config{Placeholder: "field", AllowClear: true})
	m.formOp = cycler.New(cycler.Config{Options: sqlFormOperatorOrder, Placeholder: "op", AllowClear: true})
	m.formValue = input.New(input.Config{Placeholder: "value ⏎"})
	m.filenameIn = input.New(input.Config{Placeholder: "_note_file LIKE …"})
	m.fulltextIn = input.New(input.Config{Placeholder: "not in yank", Warning: true})
	m.prev = previewpane.New()
	m.previewVisible = true
	m.focus = focusTable
	m.table.Focus()

	return m
}

// applyConfig takes over everything the config controls directly —
// startup and the in-session reload (R). Deliberately doesn't touch the
// query state, so the built query survives a reload.
func (m *appModel) applyConfig(cfg *Config) {
	m.notesDir = cfg.NotesDir
	m.editor = cfg.Editor
	if m.editor == "" {
		m.editor = "hx"
	}
	m.configPath = cfg.ConfigPath
	m.searchMode = cfg.GrubberSearchMode
	if m.searchMode == "" {
		m.searchMode = "all"
	}
	m.grubberSet = cfg.GrubberSet
	m.mmd = cfg.GrubberMMD
	m.arrayFields = cfg.ArrayFields
	m.depth = cfg.Depth
	m.tableColumns = cfg.TableColumns
	m.columnWidths = cfg.ColumnWidths
	m.apex = ApexConfig{
		Theme: cfg.ApexTheme, Width: cfg.ApexWidth,
		CodeHighlight: cfg.ApexCodeHighlight, CodeHighlightTheme: cfg.ApexCodeHighlightTheme,
	}
	m.collectionDir = findCollectionDir(m.notesDir)
}

func leadValue(rec Record) string {
	path, _ := rec["_note_file"].(string)
	return filepath.Base(path)
}

func presetItems(presets []Preset) []togglelist.Item {
	items := make([]togglelist.Item, len(presets))
	for i, p := range presets {
		items[i] = togglelist.Item{Label: p.Label, Active: p.Active}
	}
	return items
}

// formFieldKeys mirrors app.py's _update_form_fields: every non-"_" key
// across the current records, sorted. Unlike recordtable's column
// inference this doesn't drop all-nil fields — the form is just a
// starting point for a clause, the field doesn't need an existing value.
func formFieldKeys(records []Record) []string {
	seen := map[string]bool{}
	for _, rec := range records {
		for k := range rec {
			if !strings.HasPrefix(k, "_") {
				seen[k] = true
			}
		}
	}
	keys := make([]string, 0, len(seen))
	for k := range seen {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// Init must not call refreshCmd directly: cmd builders mutate m.gen via
// pointer receiver, but Init's value receiver is a copy bubbletea throws
// away — the result would arrive with a generation the stored model never
// saw and be dropped as stale. Route through a request message instead;
// Update returns the mutated copy, so the generation survives.
func (m appModel) Init() tea.Cmd {
	return func() tea.Msg { return refreshRequestMsg{} }
}

type refreshRequestMsg struct{}

// ---------------------------------------------------------------------------
// Pipeline plumbing — mirrors app.py's _refresh_data (rescan) / _run_query
// (cache replay). Each request carries the current generation; a result
// whose generation is stale (a newer request started since) is dropped —
// Textual's run_worker(exclusive=True) via a counter instead of cancellation.
// ---------------------------------------------------------------------------

type pipelineDoneMsg struct {
	gen         int
	result      PipelineResult
	rebuiltFrom string // non-empty when this came from a rescan, resets fulltextCache
}

func (m *appModel) buildCommand() string {
	return m.state.BuildCommand(m.notesDir, BuildCommandOpts{
		SearchMode: m.searchMode, MMD: m.mmd, Depth: m.depth,
		CollectionDir: m.collectionDir, ArrayFields: m.arrayFields, GrubberSet: m.grubberSet,
	})
}

func (m *appModel) refreshCmd() tea.Cmd {
	m.gen++
	gen := m.gen
	notesDir, cachePath := m.notesDir, m.cachePath
	opts := ExtractOpts{SearchMode: m.searchMode, MMD: m.mmd, Depth: m.depth, CollectionDir: m.collectionDir}
	arrayFields := m.arrayFields
	state := m.state
	return func() tea.Msg {
		var errMsg string
		ok := extractToJSONL(notesDir, cachePath, opts, func(s string) { errMsg = s })
		result := PipelineResult{}
		if ok {
			result = runPipeline(cachePath, &state, arrayFields, map[string]string{}, func(s string) { errMsg = s })
		}
		if result.Error == "" {
			result.Error = errMsg
		}
		return pipelineDoneMsg{gen: gen, result: result, rebuiltFrom: cachePath}
	}
}

func (m *appModel) runQueryCmd() tea.Cmd {
	m.gen++
	gen := m.gen
	cachePath := m.cachePath
	arrayFields := m.arrayFields
	state := m.state
	cache := m.fulltextCache
	return func() tea.Msg {
		var errMsg string
		result := runPipeline(cachePath, &state, arrayFields, cache, func(s string) { errMsg = s })
		if result.Error == "" {
			result.Error = errMsg
		}
		return pipelineDoneMsg{gen: gen, result: result}
	}
}

func (m *appModel) finishQuery(msg pipelineDoneMsg) {
	if msg.gen != m.gen {
		return
	}
	if msg.rebuiltFrom != "" {
		m.fulltextCache = map[string]string{}
	}
	m.table.SetRecords(msg.result.Records)
	m.formField.SetOptions(formFieldKeys(msg.result.Records))
	if msg.result.Error != "" {
		m.status, m.statusErr = msg.result.Error, true
	} else {
		shown := len(msg.result.Records)
		word := "record"
		if shown != 1 {
			word = "records"
		}
		if m.state.FulltextActive() {
			m.status = fmt.Sprintf("%d of %d %s  │  %s", shown, msg.result.StructuredCount, word, m.notesDir)
		} else {
			m.status = fmt.Sprintf("%d %s  │  %s", shown, word, m.notesDir)
		}
		m.statusErr = false
	}
	m.updatePreview()
}

func (m *appModel) updatePreview() {
	rec, _, ok := m.table.Current()
	if !ok {
		m.prev.SetTitle("", "")
		m.prev.SetContent("")
		return
	}
	title, content := renderPreview(rec, m.previewMode, m.apex, m.mmd)
	m.prev.SetTitle(title, m.previewMode)
	m.prev.SetContent(content)
}

// ---------------------------------------------------------------------------
// Update
// ---------------------------------------------------------------------------

func (m appModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if m.picking {
		return m.updatePicking(msg)
	}

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.applyLayout()
		// A full repaint resets bubbletea's line-diff bookkeeping
		// (lastRenderedLines): the renderer skips redrawing any line
		// that's byte-identical to the previous frame, tracking row
		// position by line COUNT, not the terminal's actual cursor
		// row. A resize is exactly when that count is most likely to
		// have drifted from what's really on screen; ClearScreen
		// forces every line to be rewritten from a known-good state.
		return m, tea.ClearScreen

	case refreshRequestMsg:
		return m, m.refreshCmd()

	case pipelineDoneMsg:
		m.finishQuery(msg)
		// New records usually mean a new column set (recordtable
		// re-fits/re-ranks columns per SetRecords), the header text
		// itself changes — the same repaint-desync risk as a resize.
		return m, tea.ClearScreen

	case recordtable.Highlighted:
		m.updatePreview()
		return m, nil

	case recordtable.Selected:
		return m, m.openEditorCmd()

	case bkexec.EditorClosed:
		return m, nil

	case togglelist.Toggled:
		if m.focus == focusPresets {
			m.state.Presets[msg.Index].Active = msg.Item.Active
			cmd := m.runQueryCmd()
			return m, cmd
		}
		return m, nil

	case input.Changed:
		switch m.focus {
		case focusFilename:
			m.state.FilenameTerm = msg.Value
		case focusFulltext:
			m.state.FulltextTerm = msg.Value
		}
		return m, nil

	case input.Debounced:
		if m.focus == focusFilename || m.focus == focusFulltext {
			return m, m.runQueryCmd()
		}
		return m, nil

	case input.Submitted:
		return m.handleSubmitted(msg)

	case tea.KeyMsg:
		return m.handleKey(msg)
	}

	return m, nil
}

// handleSubmitted fires on Enter in any builder text input. SQL and the
// form's value field also run the query; everything else (filename,
// fulltext) just returns focus to the table — same fallthrough as
// app.py's on_input_submitted. The form only returns focus on success
// (a built clause): on failure the user stays put to fix it, matching
// _apply_sql_form's early return.
func (m appModel) handleSubmitted(msg input.Submitted) (tea.Model, tea.Cmd) {
	if m.focus == focusFormValue {
		clause := buildClause(m.formField.Value(), m.formOp.Value(), msg.Value)
		if clause == "" {
			m.status, m.statusErr = "sql form: field, operator and value needed", true
			return m, nil
		}
		m.state.SQLWhere = appendClause(m.state.SQLWhere, clause)
		m.sqlIn.SetValue(m.state.SQLWhere)
		m.formValue.SetValue("")
		queryCmd := m.runQueryCmd()
		focusCmd := m.setFocus(focusTable)
		return m, tea.Batch(queryCmd, focusCmd)
	}

	var queryCmd tea.Cmd
	if m.focus == focusSQL {
		m.state.SQLWhere = msg.Value
		queryCmd = m.runQueryCmd()
	}
	focusCmd := m.setFocus(focusTable)
	return m, tea.Batch(queryCmd, focusCmd)
}

func (m *appModel) blurInput() {
	m.sqlIn.Blur()
	m.formField.Blur()
	m.formOp.Blur()
	m.formValue.Blur()
	m.filenameIn.Blur()
	m.fulltextIn.Blur()
	m.inputFocus.Blur()
}

// focusOrder is the Tab ring, the builder column's fields plus the
// table, standing in for Textual's automatic tab chain (which cycled
// every focusable widget; basekit's presets are one togglelist stop,
// not one per preset).
var focusOrder = []focusZone{
	focusPresets, focusSQL, focusFormField, focusFormOp, focusFormValue,
	focusFilename, focusFulltext, focusTable,
}

func nextZone(current focusZone, dir int) focusZone {
	idx := 0
	for i, z := range focusOrder {
		if z == current {
			idx = i
			break
		}
	}
	idx = (idx + dir + len(focusOrder)) % len(focusOrder)
	return focusOrder[idx]
}

// setFocus moves focus to zone, blurring whatever had it. Tab/Shift+Tab
// cycle through it; Esc jumps straight to focusTable; "/"/"f"/"t" jump
// straight to their input.
func (m *appModel) setFocus(zone focusZone) tea.Cmd {
	m.blurInput()
	m.table.Blur()
	m.focus = zone
	switch zone {
	case focusTable:
		m.table.Focus()
		return nil
	case focusPresets:
		m.inputFocus.Focus()
		return nil
	case focusSQL:
		m.inputFocus.Focus()
		return m.sqlIn.Focus()
	case focusFormField:
		m.inputFocus.Focus()
		m.formField.Focus()
		return nil
	case focusFormOp:
		m.inputFocus.Focus()
		m.formOp.Focus()
		return nil
	case focusFormValue:
		m.inputFocus.Focus()
		return m.formValue.Focus()
	case focusFilename:
		m.inputFocus.Focus()
		return m.filenameIn.Focus()
	case focusFulltext:
		m.inputFocus.Focus()
		return m.fulltextIn.Focus()
	}
	return nil
}

func (m appModel) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		cmd := m.setFocus(focusTable)
		return m, cmd
	case "tab":
		cmd := m.setFocus(nextZone(m.focus, 1))
		return m, cmd
	case "shift+tab":
		cmd := m.setFocus(nextZone(m.focus, -1))
		return m, cmd
	}

	if m.inputFocus.Active() {
		return m.routeToFocusedInput(msg)
	}

	switch msg.String() {
	case "q", "ctrl+c":
		return m, tea.Quit
	case "y":
		bkexec.Copy(m.buildCommand())
		m.status, m.statusErr = "Copied: "+m.buildCommand(), false
		return m, nil
	case "Y":
		bkexec.Copy(m.buildCommand())
		return m, tea.Quit
	case "m":
		m.previewMode = nextMode(m.previewMode)
		m.updatePreview()
		return m, nil
	case "p":
		m.prev.Toggle()
		m.applyLayout()
		if m.prev.Visible() {
			m.updatePreview()
		}
		return m, nil
	case "r":
		m.status, m.statusErr = "Refreshed", false
		return m, m.refreshCmd()
	case "R":
		return m.reloadConfig()
	case "-":
		m.state.SQLWhere = removeLastClause(m.state.SQLWhere)
		m.sqlIn.SetValue(m.state.SQLWhere)
		return m, m.runQueryCmd()
	case "c":
		m.openPicker()
		return m, nil
	case "/":
		cmd := m.setFocus(focusSQL)
		return m, cmd
	case "f":
		cmd := m.setFocus(focusFilename)
		return m, cmd
	case "t":
		cmd := m.setFocus(focusFulltext)
		return m, cmd
	}

	var cmd tea.Cmd
	m.table, cmd = m.table.Update(msg)
	return m, cmd
}

func (m appModel) routeToFocusedInput(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	switch m.focus {
	case focusSQL:
		m.sqlIn, cmd = m.sqlIn.Update(msg)
	case focusFormField:
		m.formField, cmd = m.formField.Update(msg)
	case focusFormOp:
		m.formOp, cmd = m.formOp.Update(msg)
	case focusFormValue:
		m.formValue, cmd = m.formValue.Update(msg)
	case focusFilename:
		m.filenameIn, cmd = m.filenameIn.Update(msg)
	case focusFulltext:
		m.fulltextIn, cmd = m.fulltextIn.Update(msg)
	case focusPresets:
		m.presets, cmd = m.presets.Update(msg)
	}
	return m, cmd
}

func (m *appModel) openEditorCmd() tea.Cmd {
	rec, _, ok := m.table.Current()
	if !ok {
		return nil
	}
	path, _ := rec["_note_file"].(string)
	if path == "" || sourceType(path) == "jsonl" {
		return nil
	}
	if _, err := os.Stat(path); err != nil {
		return nil
	}
	return bkexec.OpenEditor(m.editor, path)
}

func (m appModel) reloadConfig() (tea.Model, tea.Cmd) {
	if m.configPath == "" {
		m.status, m.statusErr = "config: no file path known, cannot reload", true
		return m, nil
	}
	cfg, errMsg := readConfig(m.configPath)
	if cfg == nil {
		m.status, m.statusErr = "config: "+errMsg, true
		return m, nil
	}
	m.applyConfig(cfg)

	active := map[string]bool{}
	for _, p := range m.state.Presets {
		if p.Active {
			active[p.Label] = true
		}
	}
	m.state.Presets = presetsFromConfig(cfg)
	for i := range m.state.Presets {
		m.state.Presets[i].Active = active[m.state.Presets[i].Label]
	}
	m.presets = togglelist.New(presetItems(m.state.Presets))
	m.table = recordtable.New(recordtable.Config{
		Columns: m.tableColumns, Widths: m.columnWidths,
		Lead: &recordtable.LeadColumn{Title: "source", Width: 30, Value: leadValue},
	})
	m.applyLayout()

	m.status, m.statusErr = "Config reloaded", false
	return m, m.refreshCmd()
}

func (m *appModel) openPicker() {
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

func (m appModel) updatePicking(msg tea.Msg) (tea.Model, tea.Cmd) {
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
		m.table.SetColumnOverride(m.picker.ActiveLabels())
		return m, nil
	}
	return m, nil
}

// ---------------------------------------------------------------------------
// Layout
// ---------------------------------------------------------------------------

func (m *appModel) applyLayout() {
	s := frame.Compute(frame.Config{
		ShowTopbar: true, TopbarHeight: 1,
		ShowBuilder: true, BuilderWidth: 30,
		ShowPreview: m.prev.Visible(), PreviewPct: 0.4, PreviewMax: 80,
		StatusHeight: 2, // status line + a key-bindings hint line beneath it
	}, m.width, m.height)

	m.table.SetSize(frame.Inner(s.TableWidth), frame.Inner(s.MainHeight))
	if s.PreviewWidth > 0 {
		m.prev.SetSize(frame.Inner(s.PreviewWidth), frame.Inner(s.MainHeight))
	}
	// The builder pane offers BuilderWidth minus 2 (padding) columns of
	// content; wider input boxes hard-wrap into broken border fragments.
	inputWidth := s.BuilderWidth - 2
	m.sqlIn.SetWidth(inputWidth)
	m.formField.SetWidth(inputWidth)
	m.formOp.SetWidth(inputWidth)
	m.formValue.SetWidth(inputWidth)
	m.filenameIn.SetWidth(inputWidth)
	m.fulltextIn.SetWidth(inputWidth)
}

// ---------------------------------------------------------------------------
// View
// ---------------------------------------------------------------------------

var (
	builderStyle    = lipgloss.NewStyle().Padding(0, 1)
	sectionLabel    = theme.Label
	footerHintStyle = lipgloss.NewStyle().Padding(0, 1)

	footerKeyStyle  = lipgloss.NewStyle().Foreground(theme.Accent).Bold(true)
	footerDescStyle = lipgloss.NewStyle().Foreground(theme.Muted)
)

// footerHints lists the key-bindings hint line's entries, most useful
// first so a narrow terminal's clampWidth truncation still leaves the
// essentials.
var footerHints = []struct{ key, desc string }{
	{"q", "quit"}, {"y/Y", "yank(+quit)"}, {"m", "mode"}, {"p", "preview"},
	{"tab", "focus"}, {"esc", "table"}, {"/", "sql"}, {"f", "filename"},
	{"t", "fulltext"}, {"c", "columns"}, {"-", "clear clause"}, {"r/R", "refresh/reload"},
}

// keyHints renders the footer's key-bindings line: accent-colored keys,
// muted descriptions — the color itself separates entries, so no
// "·" divider is needed.
var keyHints = func() string {
	parts := make([]string, len(footerHints))
	for i, h := range footerHints {
		parts[i] = footerKeyStyle.Render(h.key) + " " + footerDescStyle.Render(h.desc)
	}
	return strings.Join(parts, "  ")
}()

// sectionHeader marks the builder-column label of whichever field
// currently has focus, the Tab ring's only visual cue for the presets
// list (the text inputs already show it via their own focus border).
func sectionHeader(label string, focused bool) string {
	if focused {
		return theme.Title.Render(label)
	}
	return sectionLabel.Render(label)
}

// clampWidth truncates every line of s to width without word-wrapping.
// lipgloss's Width() wraps a line that exceeds it (needed here only for
// its side effect of padding short lines to a uniform box width);
// MaxWidth() alone truncates instead. A pane's content should already
// fit its declared width exactly, but content-width miscounts (ANSI
// styling, wide/ambiguous-width characters in real note text) can still
// produce one oversized line; wrapping it breaks the pane's fixed
// height, and a taller-than-declared frame is what desyncs Bubble Tea's
// diff renderer into leaving stray fragments or duplicated headers on
// screen across subsequent frames. Clamping first makes the later
// Width() call's own wrap step a no-op.
// clampWidth and clampBox are the last gate before content reaches a
// frame: frame.Sanitize strips the characters that make width math and
// real terminals disagree (soft hyphens in web-clipped notes were the
// found-in-the-wild case), then MaxWidth/MaxHeight truncate to the box.
func clampWidth(s string, width int) string {
	return lipgloss.NewStyle().MaxWidth(width).Render(frame.Sanitize(s))
}

// clampBox truncates pane content to its box in both dimensions. Width
// overflow wraps and height overflow scrolls the whole terminal — either
// desyncs Bubble Tea's renderer, so no pane content may ever exceed its
// budget, wherever it came from.
func clampBox(s string, width, height int) string {
	return lipgloss.NewStyle().MaxWidth(width).MaxHeight(max(height, 1)).Render(frame.Sanitize(s))
}

func (m appModel) View() string {
	if m.width == 0 {
		return ""
	}
	s := frame.Compute(frame.Config{
		ShowTopbar: true, TopbarHeight: 1,
		ShowBuilder: true, BuilderWidth: 30,
		ShowPreview: m.prev.Visible(), PreviewPct: 0.4, PreviewMax: 80,
		StatusHeight: 2, // status line + a key-bindings hint line beneath it
	}, m.width, m.height)

	// MaxWidth truncates instead of wrapping (Width would word-wrap a
	// long yank command across several lines and break the fixed-height
	// topbar row).
	topbar := lipgloss.NewStyle().Padding(0, 1).MaxWidth(s.Width).
		Render(sectionLabel.Render("y ⇒ ") + frame.Sanitize(m.buildCommand()))

	formFocused := m.focus == focusFormField || m.focus == focusFormOp || m.focus == focusFormValue
	builderSections := []string{
		sectionHeader("presets", m.focus == focusPresets) + "\n" + m.presets.View(),
		sectionHeader("sql where", m.focus == focusSQL) + "\n" + m.sqlIn.View(),
		sectionHeader("sql form", formFocused) + "\n" +
			m.formField.View() + "\n" + m.formOp.View() + "\n" + m.formValue.View(),
		sectionHeader("filename (as sql)", m.focus == focusFilename) + "\n" + m.filenameIn.View(),
		sectionHeader("full-text (display only)", m.focus == focusFulltext) + "\n" + m.fulltextIn.View(),
	}
	// Leading "\n": one blank line above "presets" so the builder column
	// doesn't sit flush under the topbar.
	builderContent := clampBox("\n"+strings.Join(builderSections, "\n\n"), s.BuilderWidth-2, s.MainHeight)
	// No border on the builder pane (no divider against the table); Width
	// already includes padding, and Height has none, so it spans the full
	// main row like the table pane.
	builder := builderStyle.Width(s.BuilderWidth).Height(s.MainHeight).Render(builderContent)

	tablePane := theme.Pane
	if m.focus == focusTable {
		tablePane = theme.PaneFocused
	}
	tableContent := clampBox(m.table.View(), s.TableWidth-2, s.MainHeight-2)
	table := tablePane.Width(s.TableWidth - 2).Height(s.MainHeight - 2).Render(tableContent)

	var previewBox string
	if m.prev.Visible() {
		previewContent := clampBox(m.prev.View(), s.PreviewWidth-2, s.MainHeight-2)
		previewBox = theme.Pane.Width(s.PreviewWidth - 2).Height(s.MainHeight - 2).Render(previewContent)
	}

	main := frame.JoinRow(builder, table, previewBox)

	statusStyle := theme.StatusBar
	if m.statusErr {
		statusStyle = theme.StatusError
	}
	statusLine := statusStyle.Width(s.Width).Render(clampWidth(m.status, s.Width-2))
	hintLine := footerHintStyle.Width(s.Width).Render(clampWidth(keyHints, s.Width-2))
	status := statusLine + "\n" + hintLine

	content := frame.JoinColumn(topbar, main, status)

	if m.picking {
		panel := theme.PaneFocused.Padding(0, 1).Render(
			theme.Title.Render("columns") + "\n\n" + m.picker.View() +
				"\n\n" + sectionLabel.Render("space toggle · backspace clear all · esc close"),
		)
		content = lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, panel)
	}

	return content
}

// ---------------------------------------------------------------------------
// CLI entry point
// ---------------------------------------------------------------------------

func main() {
	fs := flag.NewFlagSet("matterbase", flag.ExitOnError)
	configPath := fs.String("config", "", "Path to YAML config file")
	depth := fs.Int("depth", -1, "Limit directory recursion depth (0 = root only)")
	showVersion := fs.Bool("version", false, "Print version and exit")
	fs.Usage = func() {
		fmt.Fprintln(os.Stderr, "Usage: matterbase --config CONFIG.YML [PATH]")
		fmt.Fprintln(os.Stderr, "\nconfig (YAML):")
		fmt.Fprintln(os.Stderr, "  notes_dir: ~/notes          # required")
		fmt.Fprintln(os.Stderr, "  presets:                    # grubber query sets")
		fmt.Fprintln(os.Stderr, "    - label: active")
		fmt.Fprintln(os.Stderr, `      query: ["status=active"]`)
		fmt.Fprintln(os.Stderr, `  sql: "..."                  # default SQL WHERE`)
		fmt.Fprintln(os.Stderr, "  editor: hx")
		fmt.Fprintln(os.Stderr, "  array_fields: [tags]")
	}
	fs.Parse(os.Args[1:])

	if *showVersion {
		fmt.Printf("matterbase %s\n", version)
		return
	}
	if *configPath == "" {
		fmt.Fprintln(os.Stderr, "Error: --config is required")
		fs.Usage()
		os.Exit(1)
	}

	ok, foundVersion := checkGrubberVersion()
	if !ok {
		if foundVersion == "not found" {
			fmt.Fprintf(os.Stderr, "Error: grubber binary not found or not runnable: %s\n", grubberBin())
			fmt.Fprintln(os.Stderr, "Install grubber or make sure it is on PATH.")
		} else {
			fmt.Fprintf(os.Stderr, "Error: grubber %d.%d.%d or newer is required (found: %s).\n",
				minGrubberVersion[0], minGrubberVersion[1], minGrubberVersion[2], foundVersion)
		}
		os.Exit(1)
	}

	cfg := loadConfig(*configPath)
	if fs.NArg() > 0 {
		p, err := filepath.Abs(fs.Arg(0))
		if err != nil {
			fmt.Fprintln(os.Stderr, "Error:", err)
			os.Exit(1)
		}
		info, err := os.Stat(p)
		if err != nil || !info.IsDir() {
			fmt.Fprintln(os.Stderr, "Error: not a directory:", p)
			os.Exit(1)
		}
		cfg.NotesDir = p
	}
	if *depth >= 0 {
		cfg.Depth = depth
	}

	m := newAppModel(cfg)
	defer func() {
		if m.cachePath != "" {
			os.Remove(m.cachePath)
		}
	}()

	prog := tea.NewProgram(m, tea.WithAltScreen())
	if _, err := prog.Run(); err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		os.Exit(1)
	}
}
