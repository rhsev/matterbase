package main

import (
	"encoding/json"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestSourceType(t *testing.T) {
	cases := map[string]string{
		"/notes/a.md":                    "markdown",
		"/notes/a.markdown":              "markdown",
		"/notes/a.typ":                   "typst",
		"/notes/a.typst":                 "typst",
		"/notes/collections/inbox.jsonl": "jsonl",
		"/notes/a.pdf":                   "other",
		"/notes/A.MD":                    "markdown",
	}
	for path, want := range cases {
		if got := sourceType(path); got != want {
			t.Errorf("sourceType(%q) = %q, want %q", path, got, want)
		}
	}
}

func TestSplitFrontmatter(t *testing.T) {
	t.Run("yaml frontmatter", func(t *testing.T) {
		content := "---\ntitle: X\nstatus: active\n---\n\nBody."
		fm, body, keys := splitFrontmatter(content, false)
		if !strings.Contains(fm, "title: X") {
			t.Errorf("fm = %q", fm)
		}
		if !strings.Contains(body, "Body.") {
			t.Errorf("body = %q", body)
		}
		if !keys["title"] || !keys["status"] || len(keys) != 2 {
			t.Errorf("keys = %v", keys)
		}
	})

	t.Run("no frontmatter", func(t *testing.T) {
		fm, body, keys := splitFrontmatter("# Just a heading\n", false)
		if fm != "" || !strings.HasPrefix(body, "# Just") || len(keys) != 0 {
			t.Errorf("fm=%q body=%q keys=%v", fm, body, keys)
		}
	})

	t.Run("mmd header", func(t *testing.T) {
		fm, body, keys := splitFrontmatter("Title: X\n\nBody.", true)
		if !strings.Contains(fm, "Title: X") || !keys["Title"] || !strings.Contains(body, "Body.") {
			t.Errorf("fm=%q body=%q keys=%v", fm, body, keys)
		}
	})

	t.Run("mmd disabled", func(t *testing.T) {
		fm, _, _ := splitFrontmatter("Title: X\n\nBody.", false)
		if fm != "" {
			t.Errorf("fm = %q, want empty", fm)
		}
	})
}

func TestSplitMMDHeader(t *testing.T) {
	t.Run("no header", func(t *testing.T) {
		content := "# Heading\n\nSome body text."
		y, body := splitMMDHeader(content)
		if y != "" || body != content {
			t.Errorf("y=%q body=%q", y, body)
		}
	})

	t.Run("simple header", func(t *testing.T) {
		content := "Title: My Note\nDate: 2025-01-01\n\nBody here."
		y, body := splitMMDHeader(content)
		if !strings.Contains(y, "Title: My Note") || !strings.Contains(y, "Date: 2025-01-01") {
			t.Errorf("y = %q", y)
		}
		if body != "Body here." {
			t.Errorf("body = %q", body)
		}
	})

	t.Run("stops at non key line", func(t *testing.T) {
		content := "Title: My Note\nThis is not a key line\n\nBody."
		y, body := splitMMDHeader(content)
		if y != "Title: My Note" {
			t.Errorf("y = %q", y)
		}
		if !strings.Contains(body, "This is not a key line") {
			t.Errorf("body = %q", body)
		}
	})

	t.Run("empty string", func(t *testing.T) {
		y, body := splitMMDHeader("")
		if y != "" || body != "" {
			t.Errorf("y=%q body=%q", y, body)
		}
	})
}

const sampleDoc = `---
title: Test Note
---

## Tasks

` + "```yaml" + `
id: task-1
status: active
` + "```" + `

Some prose.

## Archive

` + "```yaml" + `
id: task-2
status: done
` + "```" + `
`

func sampleBody() string {
	return strings.SplitN(sampleDoc, "---", 3)[2]
}

func TestExtractSectionForRecord(t *testing.T) {
	t.Run("matches by id", func(t *testing.T) {
		result := extractSectionForRecord(sampleBody(), Record{"id": "task-1", "status": "active"}, nil, -1)
		if !strings.Contains(result, "## Tasks") || !strings.Contains(result, "task-1") {
			t.Errorf("result = %q", result)
		}
		if strings.Contains(result, "## Archive") {
			t.Errorf("result = %q", result)
		}
	})

	t.Run("matches second block", func(t *testing.T) {
		result := extractSectionForRecord(sampleBody(), Record{"id": "task-2", "status": "done"}, nil, -1)
		if !strings.Contains(result, "## Archive") || !strings.Contains(result, "task-2") {
			t.Errorf("result = %q", result)
		}
	})

	t.Run("no match returns empty", func(t *testing.T) {
		result := extractSectionForRecord(sampleBody(), Record{"id": "nonexistent"}, nil, -1)
		if result != "" {
			t.Errorf("result = %q, want empty", result)
		}
	})

	t.Run("frontmatter keys excluded", func(t *testing.T) {
		result := extractSectionForRecord(sampleBody(),
			Record{"id": "task-1", "title": "Test Note"}, map[string]bool{"title": true}, -1)
		if !strings.Contains(result, "task-1") {
			t.Errorf("result = %q", result)
		}
	})

	t.Run("empty record returns empty", func(t *testing.T) {
		result := extractSectionForRecord(sampleBody(), Record{}, nil, -1)
		if result != "" {
			t.Errorf("result = %q, want empty", result)
		}
	})
}

func TestValueStringAgreesAcrossDecoders(t *testing.T) {
	var fromJSON map[string]any
	if err := json.Unmarshal([]byte(`{"id":5,"amount":12.5,"date":"2026-10-11","ok":true,"none":null}`), &fromJSON); err != nil {
		t.Fatal(err)
	}
	var fromYAML map[string]any
	if err := yaml.Unmarshal([]byte("id: 5\namount: 12.5\ndate: 2026-10-11\nok: true\nnone: null\n"), &fromYAML); err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"id": "5", "amount": "12.5", "date": "2026-10-11", "ok": "true", "none": "null"}
	for k, w := range want {
		if got := valueString(fromJSON[k]); got != w {
			t.Errorf("JSON %s = %q, want %q", k, got, w)
		}
		if got := valueString(fromYAML[k]); got != w {
			t.Errorf("YAML %s = %q, want %q", k, got, w)
		}
	}
}

func TestExtractSectionForRecordWithNumericID(t *testing.T) {
	var rec Record
	if err := json.Unmarshal([]byte(`{"id":5}`), &rec); err != nil {
		t.Fatal(err)
	}
	body := "## Five\n\n```yaml\nid: 5\n```\n"
	if got := extractSectionForRecord(body, rec, map[string]bool{}, -1); !strings.Contains(got, "## Five") {
		t.Errorf("section = %q, want the block under ## Five", got)
	}
}

// grubber reads only ```yaml, so a record never comes from a ```yml block; a
// yml fence with the same id is an example and must not be taken for it.
func TestExtractSectionForRecordSkipsYml(t *testing.T) {
	body := "## Example\n\n```yml\nid: task-1\n```\n\n## Real\n\n```yaml\nid: task-1\n```\n"
	result := extractSectionForRecord(body, Record{"id": "task-1"}, nil, -1)
	if !strings.Contains(result, "## Real") || strings.Contains(result, "## Example") {
		t.Errorf("result = %q", result)
	}
}

// The section is the entry by grubber's rules: heading, prose before the
// block, the block. Text after the block belongs to no entry.
func TestExtractSectionForRecordIsTheEntry(t *testing.T) {
	body := "#### Detail\n\nProse before.\n\n```yaml\nid: d1\n```\n\nText after.\n"
	got := extractSectionForRecord(body, Record{"id": "d1"}, nil, -1)
	want := "#### Detail\n\nProse before.\n\n```yaml\nid: d1\n```"
	if got != want {
		t.Errorf("got %q\nwant %q", got, want)
	}
}

// grubber's _line finds the block even when two blocks carry the same fields.
func TestExtractSectionForRecordByLine(t *testing.T) {
	content := "---\ntitle: x\n---\n\n### A\n\n```yaml\nstatus: open\n```\n\n### B\n\n```yaml\nstatus: open\n```\n"
	_, body, keys := splitFrontmatter(content, false)
	offset := strings.Count(content[:len(content)-len(body)], "\n")
	got := extractSectionForRecord(body, Record{"status": "open", "_line": float64(13)}, keys, offset)
	if !strings.HasPrefix(got, "### B") {
		t.Errorf("by _line: %q", got)
	}
	if got := extractSectionForRecord(body, Record{"status": "open"}, keys, -1); !strings.HasPrefix(got, "### A") {
		t.Errorf("by fields, the first match: %q", got)
	}
}
