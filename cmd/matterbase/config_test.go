package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeConfig(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "c.yml")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestReadConfig(t *testing.T) {
	t.Run("valid config", func(t *testing.T) {
		dir := t.TempDir()
		path := writeConfig(t, "notes_dir: "+dir+"\neditor: vi\n")
		cfg, errMsg := readConfig(path)
		if errMsg != "" {
			t.Fatalf("errMsg = %q", errMsg)
		}
		if cfg.NotesDir != dir {
			t.Errorf("NotesDir = %q, want %q", cfg.NotesDir, dir)
		}
		if cfg.ConfigPath != path {
			t.Errorf("ConfigPath = %q, want %q", cfg.ConfigPath, path)
		}
	})

	t.Run("missing file", func(t *testing.T) {
		_, errMsg := readConfig(filepath.Join(t.TempDir(), "nope.yml"))
		if !strings.Contains(errMsg, "not found") {
			t.Errorf("errMsg = %q", errMsg)
		}
	})

	t.Run("invalid yaml", func(t *testing.T) {
		path := writeConfig(t, "notes_dir: [unclosed\n")
		cfg, errMsg := readConfig(path)
		if cfg != nil || !strings.Contains(errMsg, "YAML") {
			t.Errorf("cfg=%v errMsg=%q", cfg, errMsg)
		}
	})

	t.Run("not a mapping", func(t *testing.T) {
		path := writeConfig(t, "- just\n- a list\n")
		cfg, _ := readConfig(path)
		if cfg != nil {
			t.Errorf("cfg = %v, want nil", cfg)
		}
	})

	t.Run("missing notes_dir key", func(t *testing.T) {
		path := writeConfig(t, "editor: vi\n")
		cfg, errMsg := readConfig(path)
		if cfg != nil || !strings.Contains(errMsg, "notes_dir") {
			t.Errorf("cfg=%v errMsg=%q", cfg, errMsg)
		}
	})

	t.Run("nonexistent notes_dir", func(t *testing.T) {
		dir := t.TempDir()
		path := writeConfig(t, "notes_dir: "+dir+"/missing\n")
		cfg, errMsg := readConfig(path)
		if cfg != nil || !strings.Contains(errMsg, "not exist") {
			t.Errorf("cfg=%v errMsg=%q", cfg, errMsg)
		}
	})
}

func TestPresetsFromConfig(t *testing.T) {
	t.Run("presets key", func(t *testing.T) {
		cfg := &Config{Presets: []PresetDef{{Label: "active", Query: []string{"status=active"}}}}
		got := presetsFromConfig(cfg)
		if len(got) != 1 || got[0].Label != "active" || got[0].Exprs[0] != "status=active" {
			t.Errorf("got %+v", got)
		}
	})

	t.Run("filters key as fallback", func(t *testing.T) {
		cfg := &Config{Filters: []PresetDef{{Label: "old", Exprs: []string{"x=1"}}}}
		got := presetsFromConfig(cfg)
		if len(got) != 1 || got[0].Label != "old" {
			t.Errorf("got %+v", got)
		}
	})
}
