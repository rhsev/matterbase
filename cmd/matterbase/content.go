// Content extraction and rendering — port of matterbase's content.py.
// Pure functions plus apex/bat subprocess rendering; no TUI dependency.
package main

import (
	"context"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"

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

var headingRe = regexp.MustCompile(`^(#{1,6})\s`)

// extractMarkdownSection returns the section (nearest heading → next
// same-level heading) containing the line at targetBlockStart.
func extractMarkdownSection(content string, targetBlockStart int) string {
	lines := strings.Split(content, "\n")
	headingLineIdx, headingLevel := 0, 0
	for i := targetBlockStart - 1; i >= 0; i-- {
		if m := headingRe.FindStringSubmatch(lines[i]); m != nil {
			headingLineIdx = i
			headingLevel = len(m[1])
			break
		}
	}
	sectionEndIdx := len(lines)
	if headingLevel > 0 {
		stop := regexp.MustCompile(fmt.Sprintf(`^#{1,%d}\s`, headingLevel))
		for i := headingLineIdx + 1; i < len(lines); i++ {
			if stop.MatchString(lines[i]) {
				sectionEndIdx = i
				break
			}
		}
	}
	return strings.TrimRightFunc(strings.Join(lines[headingLineIdx:sectionEndIdx], "\n"), unicode.IsSpace)
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

var (
	yamlFenceOpenRe  = regexp.MustCompile("^```ya?ml\\s*$")
	yamlFenceCloseRe = regexp.MustCompile("^```\\s*$")
)

// extractSectionForRecord returns the markdown section containing the
// YAML block matching record, fingerprinted by id (or, absent an id, by
// every non-underscore, non-frontmatter field).
func extractSectionForRecord(body string, record Record, frontmatterKeys map[string]bool) string {
	lines := strings.Split(body, "\n")
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
		return ""
	}

	blockStart := -1
	i := 0
	for i < len(lines) {
		if !yamlFenceOpenRe.MatchString(lines[i]) {
			i++
			continue
		}
		j := i + 1
		for j < len(lines) && !yamlFenceCloseRe.MatchString(lines[j]) {
			j++
		}
		var blockData map[string]any
		if err := yaml.Unmarshal([]byte(strings.Join(lines[i+1:j], "\n")), &blockData); err == nil {
			match := true
			for k, want := range matchFields {
				got := ""
				if v, ok := blockData[k]; ok {
					got = valueString(v)
				}
				if got != want {
					match = false
					break
				}
			}
			if match {
				blockStart = i
				break
			}
		}
		i = j + 1
	}
	if blockStart < 0 {
		return ""
	}
	return extractMarkdownSection(body, blockStart)
}

// splitFrontmatter splits content into (frontmatterYAML, body, keys).
// Falls back to MMD header parsing when mmd is set.
func splitFrontmatter(content string, mmd bool) (string, string, map[string]bool) {
	if strings.HasPrefix(content, "---") {
		sections := strings.SplitN(content, "---", 3)
		if len(sections) >= 3 {
			fm, body := sections[1], sections[2]
			keys := map[string]bool{}
			var fmData map[string]any
			if err := yaml.Unmarshal([]byte(fm), &fmData); err == nil {
				for k := range fmData {
					keys[k] = true
				}
			}
			return strings.Trim(fm, "\n"), body, keys
		}
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
