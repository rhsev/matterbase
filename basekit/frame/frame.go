// Package frame provides the three-zone layout shared by basekit apps:
// an optional topbar, a main row (optional builder column | table 1fr |
// optional preview column), and a statusbar. It ports matterbase's CSS
// grid (app.py: #query-bar / #main / #status) into plain arithmetic —
// there is no layout engine here, just the sizing matterbase already had.
package frame

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// Config describes which zones are active and their preferred sizes. The
// zero value is usable: no topbar, no builder, no preview, a one-line
// statusbar — a bare table.
type Config struct {
	ShowTopbar   bool
	TopbarHeight int // default 1

	ShowBuilder  bool
	BuilderWidth int // default 30, matterbase's #builder width

	ShowPreview bool
	PreviewPct  float64 // default 0.4, matterbase's #preview-pane 40%
	PreviewMax  int     // default 80, matterbase's #preview-pane max-width

	StatusHeight int // default 1
}

// Sizes is the computed layout for one terminal size. Widths/heights are
// outer pane sizes — the space each theme.Pane/PaneFocused box occupies
// including its own border. Use Inner to get the content area within.
type Sizes struct {
	Width, Height int

	TopbarHeight int
	MainHeight   int // the middle row, border rows included
	StatusHeight int

	BuilderWidth int // 0 when hidden
	TableWidth   int
	PreviewWidth int // 0 when hidden
}

// minTable is the floor Compute defends when a narrow terminal can't fit
// builder + table + preview: preview shrinks first, then builder, before
// the table is ever squeezed below this.
const minTable = 20

// Compute divides width/height across the three zones. When builder and
// preview together would leave less than minTable for the table, preview
// is narrowed first and then builder — the table is never sacrificed.
func Compute(cfg Config, width, height int) Sizes {
	topbar := 0
	if cfg.ShowTopbar {
		topbar = cfg.TopbarHeight
		if topbar <= 0 {
			topbar = 1
		}
	}
	status := cfg.StatusHeight
	if status <= 0 {
		status = 1
	}
	mainHeight := max(height-topbar-status, 0)

	builder := 0
	if cfg.ShowBuilder {
		builder = cfg.BuilderWidth
		if builder <= 0 {
			builder = 30
		}
	}

	preview := 0
	if cfg.ShowPreview {
		pct := cfg.PreviewPct
		if pct <= 0 {
			pct = 0.4
		}
		maxW := cfg.PreviewMax
		if maxW <= 0 {
			maxW = 80
		}
		preview = int(float64(width) * pct)
		if preview > maxW {
			preview = maxW
		}
	}

shrink:
	for builder+preview+minTable > width {
		switch {
		case preview > 0:
			preview--
		case builder > 0:
			builder--
		default:
			break shrink
		}
	}
	table := max(width-builder-preview, 0)

	return Sizes{
		Width: width, Height: height,
		TopbarHeight: topbar, MainHeight: mainHeight, StatusHeight: status,
		BuilderWidth: builder, TableWidth: table, PreviewWidth: preview,
	}
}

// Inner returns the content area within a bordered pane (theme.Pane /
// PaneFocused draw a one-cell border on every side).
func Inner(n int) int {
	if n < 2 {
		return 0
	}
	return n - 2
}

// JoinRow assembles the main row from already-rendered, already-sized
// panes. Pass "" for builder/preview to omit a hidden zone.
func JoinRow(builder, table, preview string) string {
	parts := make([]string, 0, 3)
	if builder != "" {
		parts = append(parts, builder)
	}
	parts = append(parts, table)
	if preview != "" {
		parts = append(parts, preview)
	}
	return lipgloss.JoinHorizontal(lipgloss.Top, parts...)
}

// JoinColumn assembles the full frame from topbar, main row and status
// line. Pass "" for topbar to omit it.
func JoinColumn(topbar, main, status string) string {
	parts := make([]string, 0, 3)
	if topbar != "" {
		parts = append(parts, topbar)
	}
	parts = append(parts, main, status)
	return lipgloss.JoinVertical(lipgloss.Left, parts...)
}

// InputFocus tracks whether an input-like widget (text input, select, …)
// currently owns focus — matterbase's _input_focused. While active, the
// app routes key messages to the focused widget and skips its own global
// bindings; Esc calls Blur and returns focus to the table
// (action_focus_records).
type InputFocus struct {
	active bool
}

func (f *InputFocus) Focus()      { f.active = true }
func (f *InputFocus) Blur()       { f.active = false }
func (f InputFocus) Active() bool { return f.active }

// Sanitize removes characters that make the ANSI width math and real
// terminals disagree — the renderer-desync class: a single such rune in
// a frame makes one screen line wrap, the terminal scrolls, and Bubble
// Tea's diff renderer leaves stale rows behind (duplicated headers,
// double cursor highlights). Found in the wild via U+00AD SOFT HYPHEN in
// web-clipped notes: the width math counts it 0 (format char), tmux and
// most terminals draw a visible hyphen. Every pane content must pass
// through here (or the app's clamp helpers) before it reaches a frame.
// Newlines and ESC (SGR color sequences) survive; tabs become a space
// (terminals expand them to 8-column stops, the math counts 0).
func Sanitize(s string) string {
	return strings.Map(func(r rune) rune {
		switch {
		case r == '\n' || r == 0x1b:
			return r
		case r == '\t':
			return ' '
		case r < 0x20 || r == 0x7f: // C0 controls + DEL
			return -1
		case r >= 0x80 && r <= 0x9f: // C1 controls
			return -1
		case r == 0x00ad || r == 0xfeff: // soft hyphen, BOM
			return -1
		case r >= 0x200b && r <= 0x200f: // zero-width + bidi marks
			return -1
		case r >= 0x2028 && r <= 0x202e: // line/para separators, bidi embedding
			return -1
		case r >= 0x2060 && r <= 0x2064: // word joiner, invisible operators
			return -1
		}
		return r
	}, s)
}
