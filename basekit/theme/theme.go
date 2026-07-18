// Package theme holds the shared palette and styles for all basekit
// widgets and apps. The values are carried over from the Textual
// originals (matterbase / taskbase): Nord background, quiet borders,
// one accent for focus, titles and the cursor row.
package theme

import (
	"github.com/charmbracelet/bubbles/table"
	"github.com/charmbracelet/lipgloss"
)

var (
	Bg        = lipgloss.Color("#2D3440")
	BgLight   = lipgloss.Color("#3B4252")
	BgHover   = lipgloss.Color("#434C5E")
	Selection = lipgloss.Color("#20486C")
	Accent    = lipgloss.Color("#F4A12E")
	Warning   = lipgloss.Color("#EBCB8B")
	Text      = lipgloss.Color("#ECEFF4")
	Muted     = lipgloss.Color("#7B8899")
	Border    = lipgloss.Color("#D8DEE9")
)

var (
	// StatusBar is the one-line footer: count, scope, errors.
	StatusBar = lipgloss.NewStyle().Foreground(Text).Padding(0, 1)

	// StatusError replaces the text foreground when a pipeline step fails.
	StatusError = lipgloss.NewStyle().Foreground(lipgloss.Color("#BF616A")).Padding(0, 1)

	// Pane and PaneFocused frame the main panes; the accent border is the
	// only focus marker, backgrounds stay uniform.
	Pane        = lipgloss.NewStyle().Border(lipgloss.NormalBorder()).BorderForeground(Border)
	PaneFocused = lipgloss.NewStyle().Border(lipgloss.NormalBorder()).BorderForeground(Accent)

	// Title styles the preview pane heading and other single-line labels.
	Title = lipgloss.NewStyle().Foreground(Accent).Bold(true)

	// Label styles the muted section labels of the builder column.
	Label = lipgloss.NewStyle().Foreground(Muted)
)

// Table returns the bubbles/table styles shared by every record table.
func Table() table.Styles {
	s := table.DefaultStyles()
	s.Header = s.Header.
		Foreground(Accent).
		Bold(true).
		BorderStyle(lipgloss.NormalBorder()).
		BorderForeground(Border).
		BorderBottom(true)
	s.Selected = s.Selected.
		Foreground(Text).
		Background(Selection)
	return s
}
