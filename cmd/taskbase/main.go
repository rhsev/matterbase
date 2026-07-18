// taskbase-go: task TUI over na_json's flat index, ported from the Python
// original onto basekit. Layout follows matterbase-go: search field left,
// task list centre, preview right — not the Python original's top search
// bar (no parity requirement here, basekit's builder-column shape wins).
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	osexec "os/exec"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	bkexec "github.com/rhsev/matterbase/basekit/exec"
	"github.com/rhsev/matterbase/basekit/frame"
	"github.com/rhsev/matterbase/basekit/input"
	previewpane "github.com/rhsev/matterbase/basekit/preview"
	"github.com/rhsev/matterbase/basekit/recordtable"
	"github.com/rhsev/matterbase/basekit/theme"
)

const version = "0.1.0"

// ---------------------------------------------------------------------------
// Model
// ---------------------------------------------------------------------------

type appModel struct {
	indexPath string

	tasks   []Task
	visible []Task

	search input.Model
	table  recordtable.Model
	prev   previewpane.Model

	activeTag     string
	activeProject string
	showDone      bool

	searchFocused bool
	inputFocus    frame.InputFocus

	status    string
	statusErr bool

	width, height int
}

func newAppModel(indexPath string) appModel {
	m := appModel{indexPath: indexPath}
	m.search = input.New(input.Config{Placeholder: "task text…"})
	m.table = recordtable.New(recordtable.Config{
		Columns: []string{"Task", "Project", "Tags", "Note"},
		Widths:  map[string]int{"Task": 44, "Project": 14, "Tags": 20, "Note": 20},
	})
	m.prev = previewpane.New()
	m.table.Focus()
	return m
}

// Init loads the index once at startup — a pure read with no captured
// pointer-mutated state, so (unlike matterbase's refreshCmd/gen) it's safe
// to build straight from Init's value-receiver copy.
func (m appModel) Init() tea.Cmd {
	return loadCmd(m.indexPath)
}

type loadResultMsg struct {
	tasks []Task
	err   error
}

func loadCmd(path string) tea.Cmd {
	return func() tea.Msg {
		records, err := loadIndex(path)
		if err != nil {
			return loadResultMsg{err: err}
		}
		return loadResultMsg{tasks: flatTasks(records)}
	}
}

// reindexCmd shells out to `na_json reindex`, then reloads — port of
// app.py's action_reindex followed by _load.
func reindexCmd(path string) tea.Cmd {
	return func() tea.Msg {
		if _, err := bkexec.Run(context.Background(), bkexec.DefaultTimeout, nil, "na_json", "reindex"); err != nil {
			return loadResultMsg{err: fmt.Errorf("reindex failed: %w", err)}
		}
		records, err := loadIndex(path)
		if err != nil {
			return loadResultMsg{err: err}
		}
		return loadResultMsg{tasks: flatTasks(records)}
	}
}

func openNoteCmd(path string) tea.Cmd {
	if path == "" {
		return nil
	}
	return func() tea.Msg {
		_ = osexec.Command("open", path).Start()
		return nil
	}
}

// ---------------------------------------------------------------------------
// Filtering / rendering
// ---------------------------------------------------------------------------

func (m *appModel) refresh() {
	m.visible = filterTasks(m.tasks, m.search.Value(), m.activeTag, m.activeProject, m.showDone)

	recs := make([]recordtable.Record, len(m.visible))
	for i, t := range m.visible {
		task := t.Text
		if t.Status != "" {
			task = "[" + t.Status + "] " + task
		}
		recs[i] = recordtable.Record{
			"Task":    task,
			"Project": t.Project,
			"Tags":    formatTags(t.Tags),
			"Note":    noteStem(t.File),
		}
	}
	m.table.SetRecords(recs)
	m.updateStatus()
	m.updatePreview()
}

func (m *appModel) updateStatus() {
	mode := "pending"
	if m.showDone {
		mode = "done"
	}
	m.status = fmt.Sprintf("%d %s task(s)  │  %s", len(m.visible), mode, m.indexPath)
	m.statusErr = false
}

func (m *appModel) currentTask() (Task, bool) {
	_, i, ok := m.table.Current()
	if !ok || i < 0 || i >= len(m.visible) {
		return Task{}, false
	}
	return m.visible[i], true
}

func (m *appModel) updatePreview() {
	t, ok := m.currentTask()
	if !ok {
		m.prev.SetTitle("", "")
		m.prev.SetContent("")
		return
	}

	mode := t.Project
	if mode == "" {
		mode = "–"
	}
	m.prev.SetTitle(noteStem(t.File), mode)

	taskLine := t.Text
	if t.Status != "" {
		taskLine = "[" + t.Status + "] " + taskLine
	}
	lines := []string{lipgloss.NewStyle().Foreground(theme.Text).Render(taskLine)}
	if tags := formatTags(t.Tags); tags != "" {
		lines = append(lines, "", lipgloss.NewStyle().Foreground(theme.Warning).Render(tags))
	}
	loc := t.File
	if t.Line > 0 {
		loc = fmt.Sprintf("%s:%d", loc, t.Line)
	}
	lines = append(lines, "", theme.Label.Render(loc))

	content := ""
	for i, l := range lines {
		if i > 0 {
			content += "\n"
		}
		content += l
	}
	m.prev.SetContent(content)
}

// ---------------------------------------------------------------------------
// Update
// ---------------------------------------------------------------------------

func (m appModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.applyLayout()
		return m, tea.ClearScreen

	case loadResultMsg:
		if msg.err != nil {
			m.status, m.statusErr = "Index not found: "+m.indexPath+" — run: na_json reindex", true
			if os.IsNotExist(msg.err) {
				return m, nil
			}
			m.status = msg.err.Error()
			return m, nil
		}
		m.tasks = msg.tasks
		m.refresh()
		return m, tea.ClearScreen

	case recordtable.Highlighted:
		m.updatePreview()
		return m, nil

	case recordtable.Selected:
		t, ok := m.currentTask()
		if !ok {
			return m, nil
		}
		return m, openNoteCmd(t.File)

	case input.Changed:
		m.refresh()
		return m, nil

	case input.Submitted:
		cmd := m.setSearchFocused(false)
		return m, cmd

	case tea.KeyMsg:
		return m.handleKey(msg)
	}

	return m, nil
}

func (m *appModel) setSearchFocused(v bool) tea.Cmd {
	m.searchFocused = v
	if v {
		m.inputFocus.Focus()
		m.table.Blur()
		return m.search.Focus()
	}
	m.inputFocus.Blur()
	m.search.Blur()
	m.table.Focus()
	return nil
}

func (m appModel) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.activeTag, m.activeProject = "", ""
		m.search.SetValue("")
		cmd := m.setSearchFocused(false)
		m.refresh()
		return m, cmd
	case "tab", "shift+tab":
		// Only two focus zones (search, table), so either direction just
		// toggles — no ring to walk like matterbase-go's nextZone.
		cmd := m.setSearchFocused(!m.searchFocused)
		return m, cmd
	}

	if m.searchFocused {
		var cmd tea.Cmd
		m.search, cmd = m.search.Update(msg)
		return m, cmd
	}

	switch msg.String() {
	case "q", "ctrl+c":
		return m, tea.Quit
	case "/":
		cmd := m.setSearchFocused(true)
		return m, cmd
	case "d":
		m.showDone = !m.showDone
		m.refresh()
		return m, nil
	case "p":
		m.prev.Toggle()
		m.applyLayout()
		if m.prev.Visible() {
			m.updatePreview()
		}
		return m, nil
	case "t":
		if t, ok := m.currentTask(); ok {
			for tag := range t.Tags {
				m.activeTag = tag
				break
			}
			m.refresh()
		}
		return m, nil
	case "P":
		if t, ok := m.currentTask(); ok && t.Project != "" {
			m.activeProject = t.Project
			m.refresh()
		}
		return m, nil
	case "r":
		m.status, m.statusErr = "Reindexing…", false
		return m, reindexCmd(m.indexPath)
	case "o":
		if t, ok := m.currentTask(); ok {
			return m, openNoteCmd(t.File)
		}
		return m, nil
	}

	var cmd tea.Cmd
	m.table, cmd = m.table.Update(msg)
	return m, cmd
}

// ---------------------------------------------------------------------------
// Layout / View
// ---------------------------------------------------------------------------

func (m *appModel) applyLayout() {
	s := frame.Compute(frame.Config{
		ShowBuilder: true, BuilderWidth: 30,
		ShowPreview: m.prev.Visible(), PreviewPct: 0.4, PreviewMax: 80,
		StatusHeight: 2,
	}, m.width, m.height)

	m.table.SetSize(frame.Inner(s.TableWidth), frame.Inner(s.MainHeight))
	if s.PreviewWidth > 0 {
		m.prev.SetSize(frame.Inner(s.PreviewWidth), frame.Inner(s.MainHeight))
	}
	m.search.SetWidth(s.BuilderWidth - 2)
}

func clampWidth(s string, width int) string {
	return lipgloss.NewStyle().MaxWidth(width).Render(frame.Sanitize(s))
}

func clampBox(s string, width, height int) string {
	return lipgloss.NewStyle().MaxWidth(width).MaxHeight(max(height, 1)).Render(frame.Sanitize(s))
}

func sectionHeader(label string, focused bool) string {
	if focused {
		return theme.Title.Render(label)
	}
	return theme.Label.Render(label)
}

func filterLine(label, value string) string {
	if value == "" {
		return theme.Label.Render(label + ": –")
	}
	return theme.Label.Render(label+": ") + lipgloss.NewStyle().Foreground(theme.Accent).Render(value)
}

func (m appModel) View() string {
	if m.width == 0 {
		return ""
	}
	s := frame.Compute(frame.Config{
		ShowBuilder: true, BuilderWidth: 30,
		ShowPreview: m.prev.Visible(), PreviewPct: 0.4, PreviewMax: 80,
		StatusHeight: 2,
	}, m.width, m.height)

	mode := "pending"
	if m.showDone {
		mode = "done"
	}
	builderSections := []string{
		sectionHeader("search", m.searchFocused) + "\n" + m.search.View(),
		theme.Label.Render("mode: ") + lipgloss.NewStyle().Foreground(theme.Accent).Render(mode),
		filterLine("tag", m.activeTag),
		filterLine("project", m.activeProject),
	}
	builderContent := clampBox("\n"+joinSections(builderSections), s.BuilderWidth-2, s.MainHeight)
	builder := lipgloss.NewStyle().Padding(0, 1).Width(s.BuilderWidth).Height(s.MainHeight).Render(builderContent)

	tablePane := theme.Pane
	if !m.searchFocused {
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
	status := lipgloss.JoinVertical(lipgloss.Left, statusLine, hintLine)

	return frame.JoinColumn("", main, status)
}

// footerHints lists taskbase's own bindings, most useful first so a
// narrow terminal's clampWidth truncation still leaves the essentials —
// not matterbase-go's (yank/SQL-form keys don't apply here).
var (
	footerHintStyle = lipgloss.NewStyle().Padding(0, 1)
	footerKeyStyle  = lipgloss.NewStyle().Foreground(theme.Accent).Bold(true)
	footerDescStyle = lipgloss.NewStyle().Foreground(theme.Muted)

	footerHints = []struct{ key, desc string }{
		{"q", "quit"}, {"/", "search"}, {"tab", "focus"}, {"esc", "clear"},
		{"d", "done"}, {"o", "open"}, {"t", "tag"}, {"P", "project"},
		{"r", "reindex"}, {"p", "preview"},
	}

	// keyHints renders the footer's key-bindings line: accent-colored
	// keys, muted descriptions — the color itself separates entries, so
	// no "·" divider is needed.
	keyHints = func() string {
		parts := make([]string, len(footerHints))
		for i, h := range footerHints {
			parts[i] = footerKeyStyle.Render(h.key) + " " + footerDescStyle.Render(h.desc)
		}
		return strings.Join(parts, "  ")
	}()
)

func joinSections(sections []string) string {
	out := ""
	for i, s := range sections {
		if i > 0 {
			out += "\n\n"
		}
		out += s
	}
	return out
}

// ---------------------------------------------------------------------------
// CLI entry point
// ---------------------------------------------------------------------------

func main() {
	fs := flag.NewFlagSet("taskbase", flag.ExitOnError)
	indexPath := fs.String("index", "", "Path to na_json index (default: ~/.config/na_json/index.json)")
	showVersion := fs.Bool("version", false, "Print version and exit")
	fs.Usage = func() {
		fmt.Fprintln(os.Stderr, "Usage: taskbase [--index PATH]")
	}
	fs.Parse(os.Args[1:])

	if *showVersion {
		fmt.Printf("taskbase %s\n", version)
		return
	}

	path := *indexPath
	if path == "" {
		path = defaultIndexPath()
	}

	m := newAppModel(path)
	prog := tea.NewProgram(m, tea.WithAltScreen())
	if _, err := prog.Run(); err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		os.Exit(1)
	}
}
