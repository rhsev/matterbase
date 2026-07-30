package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseVersion(t *testing.T) {
	cases := []struct {
		text string
		want [3]int
		ok   bool
	}{
		{"grubber 0.13.0 (Go)", [3]int{0, 13, 0}, true},
		{"1.2", [3]int{1, 2, 0}, true},
		{"nonsense", [3]int{}, false},
	}
	for _, c := range cases {
		got, ok := parseVersion(c.text)
		if ok != c.ok || (ok && got != c.want) {
			t.Errorf("parseVersion(%q) = %v,%v want %v,%v", c.text, got, ok, c.want, c.ok)
		}
	}
}

func TestVersionAtLeast(t *testing.T) {
	if !versionAtLeast([3]int{0, 13, 0}, [3]int{0, 12, 0}) {
		t.Error("0.13.0 should satisfy >= 0.12.0")
	}
	if versionAtLeast([3]int{0, 11, 9}, [3]int{0, 12, 0}) {
		t.Error("0.11.9 should not satisfy >= 0.12.0")
	}
	if !versionAtLeast([3]int{0, 12, 0}, [3]int{0, 12, 0}) {
		t.Error("equal versions should satisfy >=")
	}
}

func TestFindCollectionDir(t *testing.T) {
	t.Run("returns path when jsonl exists", func(t *testing.T) {
		dir := t.TempDir()
		col := filepath.Join(dir, "collections")
		os.Mkdir(col, 0o755)
		os.WriteFile(filepath.Join(col, "inbox.jsonl"), []byte(`{"type":"ref"}`), 0o644)
		if got := findCollectionDir(dir); got != col {
			t.Errorf("got %q, want %q", got, col)
		}
	})

	t.Run("returns empty when no collections dir", func(t *testing.T) {
		if got := findCollectionDir(t.TempDir()); got != "" {
			t.Errorf("got %q, want empty", got)
		}
	})

	t.Run("returns empty when collections dir has no jsonl", func(t *testing.T) {
		dir := t.TempDir()
		col := filepath.Join(dir, "collections")
		os.Mkdir(col, 0o755)
		os.WriteFile(filepath.Join(col, "readme.txt"), []byte("nothing"), 0o644)
		if got := findCollectionDir(dir); got != "" {
			t.Errorf("got %q, want empty", got)
		}
	})
}
