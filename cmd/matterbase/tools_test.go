package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fakeTools puts executable shell scripts named after the keys of tools on
// a fresh PATH, which holds nothing else. An empty map leaves PATH empty.
func fakeTools(t *testing.T, tools map[string]string) {
	t.Helper()
	dir := t.TempDir()
	for name, script := range tools {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte("#!/bin/sh\n"+script+"\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", dir)
}

func writeFile(t *testing.T, name, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestRenderWholeFallsBack(t *testing.T) {
	md := writeFile(t, "n.md", "---\ntitle: X\n---\n\nBody text.\n")
	typ := writeFile(t, "n.typ", "= Heading\n")

	t.Run("apex for markdown", func(t *testing.T) {
		fakeTools(t, map[string]string{"apex": "echo APEX", "bat": "echo BAT"})
		if got := renderWhole(md, ApexConfig{}, false); !strings.Contains(got, "APEX") {
			t.Errorf("got %q, want apex output", got)
		}
	})
	t.Run("bat for typst", func(t *testing.T) {
		fakeTools(t, map[string]string{"apex": "echo APEX", "bat": "echo BAT"})
		if got := renderWhole(typ, ApexConfig{}, false); !strings.Contains(got, "BAT") {
			t.Errorf("got %q, want bat output", got)
		}
	})
	t.Run("bat when apex fails", func(t *testing.T) {
		fakeTools(t, map[string]string{"apex": "exit 1", "bat": "echo BAT"})
		if got := renderWhole(md, ApexConfig{}, false); !strings.Contains(got, "BAT") {
			t.Errorf("got %q, want bat output", got)
		}
	})
	t.Run("raw text without renderers", func(t *testing.T) {
		fakeTools(t, nil)
		got := renderWhole(md, ApexConfig{}, false)
		if !strings.Contains(got, "title: X") || !strings.Contains(got, "Body text.") {
			t.Errorf("got %q, want frontmatter and body", got)
		}
	})
	t.Run("error for an unreadable file", func(t *testing.T) {
		fakeTools(t, nil)
		got := renderWhole(filepath.Join(t.TempDir(), "gone.md"), ApexConfig{}, false)
		if !strings.HasPrefix(got, "Error reading file") {
			t.Errorf("got %q", got)
		}
	})
}

func TestRenderCompactWithoutApexShowsMarkdown(t *testing.T) {
	fakeTools(t, nil)
	md := writeFile(t, "n.md", "---\ntitle: X\n---\n\n## Block\n\n```yaml\nid: b1\n```\n\nOther prose.\n")
	got := renderCompact(Record{"id": "b1"}, md, ApexConfig{}, false)
	for _, want := range []string{"title: X", "## Block", "id: b1"} {
		if !strings.Contains(got, want) {
			t.Errorf("compact preview %q lacks %q", got, want)
		}
	}
}

func TestApexGetsConfiguredFlags(t *testing.T) {
	fakeTools(t, map[string]string{"apex": `echo "$@"`})
	md := writeFile(t, "n.md", "x\n")
	got := renderWithApex(md, ApexConfig{Theme: "nord256", Width: 72, CodeHighlight: "pygments", CodeHighlightTheme: "nord"}, nil)
	for _, want := range []string{"--theme nord256", "--width 72", "--code-highlight pygments", "--code-highlight-theme nord"} {
		if !strings.Contains(got, want) {
			t.Errorf("apex called with %q, missing %q", strings.TrimSpace(got), want)
		}
	}
}

func TestCheckGrubberVersion(t *testing.T) {
	cases := []struct {
		output string
		ok     bool
	}{
		{"grubber 0.19.0 (Go)", true},
		{"grubber 0.19.1 (Go)", true},
		{"grubber v1.2.0", true},
		{"grubber 0.18.0 (Go)", false},
		{"grubber dev", false},
	}
	for _, c := range cases {
		fakeTools(t, map[string]string{"grubber": "echo '" + c.output + "'"})
		t.Setenv("GRUBBER", "")
		ok, found := checkGrubberVersion()
		if ok != c.ok || found != c.output {
			t.Errorf("%q: ok %v, found %q", c.output, ok, found)
		}
	}

	fakeTools(t, nil)
	if ok, found := checkGrubberVersion(); ok || found != "not found" {
		t.Errorf("missing grubber: ok %v, found %q", ok, found)
	}

	custom := filepath.Join(t.TempDir(), "my-grubber")
	if err := os.WriteFile(custom, []byte("#!/bin/sh\necho 'grubber 0.20.0'\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GRUBBER", custom)
	if ok, _ := checkGrubberVersion(); !ok {
		t.Error("$GRUBBER is not used")
	}
}
