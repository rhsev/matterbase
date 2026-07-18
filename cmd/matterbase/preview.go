// Adaptive preview rendering — port of matterbase's preview.py. One
// global mode (whole/compact/record) applied to whichever record is
// selected, adapted by the record's source type. Rich markup is replaced
// with lipgloss ANSI styling since the TUI side has no Rich renderer.
package main

import (
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/rhsev/matterbase/basekit/theme"
)

var previewModes = []string{"whole", "compact", "record"}

// nextMode cycles whole → compact → record → whole.
func nextMode(mode string) string {
	for i, m := range previewModes {
		if m == mode {
			return previewModes[(i+1)%len(previewModes)]
		}
	}
	return previewModes[0]
}

var dimStyle = lipgloss.NewStyle().Foreground(theme.Muted)

// renderRecordFields renders record as a field form — the jsonl/"record"
// view. Field order is alphabetical (Go maps have no insertion order to
// preserve; recordtable made the same call for the table columns).
func renderRecordFields(record Record) string {
	var keys []string
	for k, v := range record {
		if strings.HasPrefix(k, "_") || v == nil {
			continue
		}
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var lines []string
	for _, k := range keys {
		v := record[k]
		var s string
		if arr, ok := v.([]any); ok {
			parts := make([]string, len(arr))
			for i, x := range arr {
				parts[i] = pyStr(x)
			}
			s = strings.Join(parts, ", ")
		} else {
			s = pyStr(v)
		}
		lines = append(lines, theme.Title.Render(k)+"  "+s)
	}
	if src, _ := record["_note_file"].(string); src != "" {
		lines = append(lines, "", dimStyle.Render("source  "+src))
	}
	if len(lines) == 0 {
		return dimStyle.Render("(empty record)")
	}
	return strings.Join(lines, "\n")
}

func rawWithDimmedFrontmatter(content string, mmd bool) string {
	fm, body, _ := splitFrontmatter(content, mmd)
	if fm == "" {
		return content
	}
	return dimStyle.Render("---\n"+fm+"\n---") + "\n\n" + strings.TrimLeft(body, "\n")
}

func renderWhole(path string, apexCfg ApexConfig, mmd bool) string {
	if sourceType(path) == "markdown" {
		if ansi := renderWithApex(path, apexCfg, nil); ansi != "" {
			return ansi
		}
	}
	if bat := renderWithBat(path); bat != "" {
		return bat
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "Error reading file: " + err.Error()
	}
	return rawWithDimmedFrontmatter(string(data), mmd)
}

func renderCompact(record Record, path string, apexCfg ApexConfig, mmd bool) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return "Error reading file: " + err.Error()
	}
	content := string(data)

	var parts []string
	fm, body, fmKeys := splitFrontmatter(content, mmd)
	if fm != "" {
		parts = append(parts, "---\n"+fm+"\n---")
	}
	if section := extractSectionForRecord(body, record, fmKeys); section != "" {
		parts = append(parts, section)
	}

	mdText := strings.Join(parts, "\n\n")
	if mdText == "" {
		return renderRecordFields(record)
	}
	if ansi := renderWithApex("", apexCfg, &mdText); ansi != "" {
		return ansi
	}
	return mdText
}

// renderPreview renders record in the global mode, adapted to its source
// type. Returns (title, content) — content is ready to hand to
// preview.Model.SetContent.
func renderPreview(record Record, mode string, apexCfg ApexConfig, mmd bool) (string, string) {
	path, _ := record["_note_file"].(string)
	stype := sourceType(path)
	title := "record"
	if path != "" {
		title = filepath.Base(path)
	}

	if stype == "jsonl" || mode == "record" || path == "" {
		return title, renderRecordFields(record)
	}
	if _, err := os.Stat(path); err != nil {
		return title, dimStyle.Render("(file not found)")
	}
	if mode == "whole" {
		return title, renderWhole(path, apexCfg, mmd)
	}
	return title, renderCompact(record, path, apexCfg, mmd)
}
