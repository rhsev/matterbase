// Adaptive preview rendering. One global mode (whole/compact/record)
// applied to whichever record is selected, adapted by the record's source
// type.
package main

import (
	"context"
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
				parts[i] = valueString(x)
			}
			s = strings.Join(parts, ", ")
		} else {
			s = valueString(v)
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

func renderWhole(ctx context.Context, path string, apexCfg ApexConfig, mmd bool) string {
	if sourceType(path) == "markdown" {
		if ansi := renderWithApex(ctx, path, apexCfg, nil); ansi != "" {
			return ansi
		}
	}
	if bat := renderWithBat(ctx, path); bat != "" {
		return bat
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "Error reading file: " + err.Error()
	}
	return rawWithDimmedFrontmatter(normalizeNote(string(data)), mmd)
}

func renderCompact(ctx context.Context, record Record, path string, apexCfg ApexConfig, mmd bool) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return "Error reading file: " + err.Error()
	}
	content := normalizeNote(string(data))

	var parts []string
	fm, body, fmKeys := splitFrontmatter(content, mmd)
	if fm != "" {
		parts = append(parts, "---\n"+fm+"\n---")
	}
	offset := -1
	if strings.HasSuffix(content, body) {
		offset = strings.Count(content[:len(content)-len(body)], "\n")
	}
	if section := extractSectionForRecord(body, record, fmKeys, offset); section != "" {
		parts = append(parts, section)
	}

	mdText := strings.Join(parts, "\n\n")
	if mdText == "" {
		return renderRecordFields(record)
	}
	if ansi := renderWithApex(ctx, "", apexCfg, &mdText); ansi != "" {
		return ansi
	}
	return mdText
}

// previewTitle is the pane's title for a record: its file's name.
func previewTitle(record Record) string {
	if path, _ := record["_note_file"].(string); path != "" {
		return filepath.Base(path)
	}
	return "record"
}

// previewReadsFile reports whether the preview of record in mode reads the
// note and may start apex or bat. The field view needs neither.
func previewReadsFile(record Record, mode string) bool {
	path, _ := record["_note_file"].(string)
	return path != "" && mode != "record" && sourceType(path) != "jsonl"
}

// renderPreview renders record in the global mode, adapted to its source
// type. Returns (title, content) — content is ready to hand to
// preview.Model.SetContent. Cancelling ctx stops a renderer it started.
func renderPreview(ctx context.Context, record Record, mode string, apexCfg ApexConfig, mmd bool) (string, string) {
	title := previewTitle(record)
	if !previewReadsFile(record, mode) {
		return title, renderRecordFields(record)
	}
	path, _ := record["_note_file"].(string)
	if _, err := os.Stat(path); err != nil {
		return title, dimStyle.Render("(file not found)")
	}
	if mode == "whole" {
		return title, renderWhole(ctx, path, apexCfg, mmd)
	}
	return title, renderCompact(ctx, record, path, apexCfg, mmd)
}
