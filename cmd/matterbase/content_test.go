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

func TestExtractMarkdownSection(t *testing.T) {
	t.Run("simple h2 section", func(t *testing.T) {
		content := "## Alpha\nSome text.\n## Beta\nOther text.\n"
		result := extractMarkdownSection(content, 1)
		if !strings.Contains(result, "## Alpha") || !strings.Contains(result, "Some text.") {
			t.Errorf("result = %q", result)
		}
		if strings.Contains(result, "## Beta") {
			t.Errorf("result = %q, must not include the next section", result)
		}
	})

	t.Run("nested headings stay inside", func(t *testing.T) {
		content := "## Section\n### Subsection\nDetail.\n## Next Section\n"
		result := extractMarkdownSection(content, 1)
		if !strings.Contains(result, "### Subsection") {
			t.Errorf("result = %q", result)
		}
		if strings.Contains(result, "## Next Section") {
			t.Errorf("result = %q", result)
		}
	})

	t.Run("no heading returns full content", func(t *testing.T) {
		content := "just some text\nno headings here"
		result := extractMarkdownSection(content, 1)
		if !strings.Contains(result, "just some text") {
			t.Errorf("result = %q", result)
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
		result := extractSectionForRecord(sampleBody(), Record{"id": "task-1", "status": "active"}, nil)
		if !strings.Contains(result, "## Tasks") || !strings.Contains(result, "task-1") {
			t.Errorf("result = %q", result)
		}
		if strings.Contains(result, "## Archive") {
			t.Errorf("result = %q", result)
		}
	})

	t.Run("matches second block", func(t *testing.T) {
		result := extractSectionForRecord(sampleBody(), Record{"id": "task-2", "status": "done"}, nil)
		if !strings.Contains(result, "## Archive") || !strings.Contains(result, "task-2") {
			t.Errorf("result = %q", result)
		}
	})

	t.Run("no match returns empty", func(t *testing.T) {
		result := extractSectionForRecord(sampleBody(), Record{"id": "nonexistent"}, nil)
		if result != "" {
			t.Errorf("result = %q, want empty", result)
		}
	})

	t.Run("frontmatter keys excluded", func(t *testing.T) {
		result := extractSectionForRecord(sampleBody(),
			Record{"id": "task-1", "title": "Test Note"}, map[string]bool{"title": true})
		if !strings.Contains(result, "task-1") {
			t.Errorf("result = %q", result)
		}
	})

	t.Run("empty record returns empty", func(t *testing.T) {
		result := extractSectionForRecord(sampleBody(), Record{}, nil)
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
	if got := extractSectionForRecord(body, rec, map[string]bool{}); !strings.Contains(got, "## Five") {
		t.Errorf("section = %q, want the block under ## Five", got)
	}
}
