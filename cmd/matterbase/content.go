// Content extraction and rendering: pure functions plus apex/bat
// subprocess rendering; no TUI dependency.
package main

import (
	"context"
	"fmt"
	"github.com/rhsev/grubber/entry"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	bkexec "github.com/rhsev/matterbase/basekit/exec"
)

var (
	markdownSuffixes = map[string]bool{".md": true, ".markdown": true, "": true}
	typstSuffixes    = map[string]bool{".typ": true, ".typst": true}
)

// sourceType classifies a record's source file, driving the adaptive
// preview: "markdown", "typst", "jsonl", or "other".
func sourceType(path string) string {
	suffix := strings.ToLower(filepath.Ext(path))
	switch {
	case markdownSuffixes[suffix]:
		return "markdown"
	case typstSuffixes[suffix]:
		return "typst"
	case suffix == ".jsonl":
		return "jsonl"
	default:
		return "other"
	}
}

var mmdKeyRe = regexp.MustCompile(`^[\w. -]+\s*:`)

// splitMMDHeader returns (yamlLines, body) if content starts with MMD
// key: value pairs, or ("", content) if no MMD header is found.
func splitMMDHeader(content string) (string, string) {
	lines := strings.Split(content, "\n")
	var mmdLines []string
	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			break
		}
		if !mmdKeyRe.MatchString(line) {
			break
		}
		mmdLines = append(mmdLines, line)
	}
	if len(mmdLines) == 0 {
		return "", content
	}
	restStart := len(mmdLines)
	if restStart < len(lines) && strings.TrimSpace(lines[restStart]) == "" {
		restStart++
	}
	return strings.Join(mmdLines, "\n"), strings.Join(lines[restStart:], "\n")
}

// valueString renders a decoded field value as it reads in the note. It
// serves both the record view and the fingerprint matching in
// extractSectionForRecord, which compares a record decoded from JSON with
// a YAML block decoded by yaml.v3. The two decoders disagree on types:
// JSON gives float64 for 5 where YAML gives int, and YAML turns
// 2026-10-11 into a time.Time where JSON keeps the string. Both sides
// must come out as the same text.
func valueString(v any) string {
	switch x := v.(type) {
	case nil:
		return "null"
	case string:
		return x
	case float64:
		if x == math.Trunc(x) && math.Abs(x) < 1e15 {
			return strconv.FormatInt(int64(x), 10)
		}
		return strconv.FormatFloat(x, 'g', -1, 64)
	case time.Time:
		if x.Hour() == 0 && x.Minute() == 0 && x.Second() == 0 && x.Nanosecond() == 0 {
			return x.Format(time.DateOnly)
		}
		return x.Format(time.RFC3339)
	default:
		return fmt.Sprint(x)
	}
}

// extractSectionForRecord returns the entry of the YAML block that holds
// record: its heading, its prose and the block itself, by the rules of
// grubber's package entry (the prose is the text between the block and the
// nearest boundary above it; text after a block belongs to no entry). The
// block is found by the record's _line where grubber gave one, else by its id,
// else by every field it carries. lineOffset is the number of file lines
// before body; a negative one means _line cannot be used.
func extractSectionForRecord(body string, record Record, frontmatterKeys map[string]bool, lineOffset int) string {
	lines := strings.Split(body, "\n")
	blocks := entry.ParseBlocks(lines)
	b, ok := blockByLine(blocks, record, lineOffset)
	if !ok {
		b, ok = blockByFields(blocks, record, frontmatterKeys)
	}
	if !ok {
		return ""
	}
	var parts []string
	if h := headingLineFor(lines, b); h != "" {
		parts = append(parts, h)
	}
	if prose := b.Prose(lines); prose != "" {
		parts = append(parts, prose)
	}
	parts = append(parts, strings.Join(lines[b.Open:b.Close+1], "\n"))
	return strings.Join(parts, "\n\n")
}

// blockByLine finds the block whose opening fence is at the record's _line.
func blockByLine(blocks []entry.Block, record Record, lineOffset int) (entry.Block, bool) {
	line, ok := record["_line"].(float64)
	if !ok || lineOffset < 0 {
		return entry.Block{}, false
	}
	for _, b := range blocks {
		if b.Open+lineOffset+1 == int(line) {
			return b, true
		}
	}
	return entry.Block{}, false
}

// blockByFields finds the first block that carries the record's id, or,
// without an id, every field of the record that is not injected or inherited
// from the frontmatter.
func blockByFields(blocks []entry.Block, record Record, frontmatterKeys map[string]bool) (entry.Block, bool) {
	matchFields := map[string]string{}
	if id, ok := record["id"]; ok && id != nil && !frontmatterKeys["id"] {
		matchFields["id"] = valueString(id)
	} else {
		for k, v := range record {
			if strings.HasPrefix(k, "_") || frontmatterKeys[k] || v == nil {
				continue
			}
			matchFields[k] = valueString(v)
		}
	}
	if len(matchFields) == 0 {
		return entry.Block{}, false
	}
	for _, b := range blocks {
		if b.Fields == nil {
			continue
		}
		match := true
		for k, want := range matchFields {
			got := ""
			if v, ok := b.Fields[k]; ok {
				got = valueString(v)
			}
			if got != want {
				match = false
				break
			}
		}
		if match {
			return b, true
		}
	}
	return entry.Block{}, false
}

// headingLineFor returns the heading line of b's entry as it stands in the
// note, so the preview keeps its level.
func headingLineFor(lines []string, b entry.Block) string {
	if b.Heading == "" {
		return ""
	}
	for i := b.Open - 1; i >= 0; i-- {
		l := lines[i]
		if strings.HasPrefix(l, "#") && strings.TrimSpace(strings.TrimRight(strings.TrimSpace(strings.TrimLeft(l, "#")), "#")) == b.Heading {
			return l
		}
	}
	return ""
}

// normalizeNote drops a byte order mark and turns \r\n into \n, the form
// in which grubber's package entry reads a note's lines.
func normalizeNote(content string) string {
	content = strings.TrimPrefix(content, "\ufeff")
	return strings.ReplaceAll(content, "\r\n", "\n")
}

// splitFrontmatter splits content into (frontmatterYAML, body, keys). Where
// the frontmatter ends is grubber's rule (entry.FrontmatterEnd), so the
// preview cuts a note where the records were cut. content is normalized
// (normalizeNote). Falls back to MMD header parsing when mmd is set.
func splitFrontmatter(content string, mmd bool) (string, string, map[string]bool) {
	lines := strings.Split(content, "\n")
	if end := entry.FrontmatterEnd(lines); end >= 0 {
		fm := strings.Join(lines[1:end], "\n")
		body := strings.Join(lines[end+1:], "\n")
		keys := map[string]bool{}
		var fmData map[string]any
		if err := yaml.Unmarshal([]byte(fm), &fmData); err == nil {
			for k := range fmData {
				keys[k] = true
			}
		}
		return strings.Trim(fm, "\n"), body, keys
	}
	if mmd {
		mmdYAML, mmdBody := splitMMDHeader(content)
		if mmdYAML != "" {
			keys := map[string]bool{}
			for _, line := range strings.Split(mmdYAML, "\n") {
				if idx := strings.Index(line, ":"); idx >= 0 {
					keys[strings.TrimSpace(line[:idx])] = true
				}
			}
			return mmdYAML, mmdBody, keys
		}
	}
	return "", content, map[string]bool{}
}

// ApexConfig mirrors matterbase's apex_* config keys.
type ApexConfig struct {
	Theme              string
	Width              int
	CodeHighlight      string
	CodeHighlightTheme string
}

func withTermEnv() []string {
	env := os.Environ()
	has := func(key string) bool {
		prefix := key + "="
		for _, e := range env {
			if strings.HasPrefix(e, prefix) {
				return true
			}
		}
		return false
	}
	if !has("TERM") {
		env = append(env, "TERM=xterm-256color")
	}
	if !has("COLORTERM") {
		env = append(env, "COLORTERM=truecolor")
	}
	return env
}

// renderWithApex renders path (or tmpContent, written to a .md tempfile
// when non-nil) via apex. Returns ANSI text, or "" if apex is
// unavailable or fails.
func renderWithApex(path string, cfg ApexConfig, tmpContent *string) string {
	renderPath := path
	if tmpContent != nil {
		f, err := os.CreateTemp("", "matterbase-*.md")
		if err != nil {
			return ""
		}
		defer os.Remove(f.Name())
		defer f.Close()
		if _, err := f.WriteString(*tmpContent); err != nil {
			return ""
		}
		renderPath = f.Name()
	}
	args := []string{renderPath, "--plugins", "-t", "terminal256"}
	if cfg.CodeHighlight != "" {
		args = append(args, "--code-highlight", cfg.CodeHighlight)
	}
	if cfg.CodeHighlightTheme != "" {
		args = append(args, "--code-highlight-theme", cfg.CodeHighlightTheme)
	}
	if cfg.Theme != "" {
		args = append(args, "--theme", cfg.Theme)
	}
	if cfg.Width > 0 {
		args = append(args, "--width", strconv.Itoa(cfg.Width))
	}
	out, err := bkexec.Run(context.Background(), 10*time.Second, withTermEnv(), "apex", args...)
	if err != nil {
		return ""
	}
	return string(out)
}

// renderWithBat syntax-highlights path via bat. Returns ANSI text or "".
func renderWithBat(path string) string {
	out, err := bkexec.Run(context.Background(), 5*time.Second, withTermEnv(),
		"bat", "--color=always", "--style=plain", "--paging=never", path)
	if err != nil {
		return ""
	}
	return string(out)
}
