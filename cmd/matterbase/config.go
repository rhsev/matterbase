// Config loading — port of matterbase's app.py read_config/load_config.
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// PresetDef is one entry of the config's "presets" (or legacy "filters")
// list.
type PresetDef struct {
	Label string   `yaml:"label"`
	Query []string `yaml:"query"`
	Exprs []string `yaml:"exprs"`
}

// Config is the YAML config file, notes_dir required. See main.go's
// usage epilog for the on-disk shape.
type Config struct {
	NotesDir               string         `yaml:"notes_dir"`
	Editor                 string         `yaml:"editor"`
	SQL                    string         `yaml:"sql"`
	Presets                []PresetDef    `yaml:"presets"`
	Filters                []PresetDef    `yaml:"filters"`
	GrubberSearchMode      string         `yaml:"grubber_search_mode"`
	GrubberSet             string         `yaml:"grubber_set"`
	GrubberMMD             bool           `yaml:"grubber_mmd"`
	ArrayFields            []string       `yaml:"array_fields"`
	Depth                  *int           `yaml:"depth"`
	TableColumns           []string       `yaml:"table_columns"`
	ColumnWidths           map[string]int `yaml:"column_widths"`
	ApexTheme              string         `yaml:"apex_theme"`
	ApexWidth              int            `yaml:"apex_width"`
	ApexCodeHighlight      string         `yaml:"apex_code_highlight"`
	ApexCodeHighlightTheme string         `yaml:"apex_code_highlight_theme"`

	ConfigPath string `yaml:"-"`
}

func expandUser(path string) string {
	if path == "~" {
		if home, err := os.UserHomeDir(); err == nil {
			return home
		}
	}
	if strings.HasPrefix(path, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, strings.TrimPrefix(path, "~/"))
		}
	}
	return path
}

// readConfig reads and validates the config. Returns (config, "") or
// (nil, error) — non-exiting so the app can reload in-session and keep
// the old config on failure (action_reload_config, key R).
func readConfig(path string) (*Config, string) {
	expanded := expandUser(path)
	data, err := os.ReadFile(expanded)
	if err != nil {
		return nil, fmt.Sprintf("Config file not found: %s", path)
	}

	var raw any
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return nil, "Invalid YAML: " + firstLine(err.Error())
	}
	if _, ok := raw.(map[string]any); !ok {
		return nil, fmt.Sprintf("Config file is empty or not a YAML mapping: %s", path)
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, "Invalid YAML: " + firstLine(err.Error())
	}

	if cfg.NotesDir == "" {
		return nil, "Config missing required key: notes_dir"
	}
	notesDir := expandUser(cfg.NotesDir)
	info, err := os.Stat(notesDir)
	if err != nil || !info.IsDir() {
		return nil, fmt.Sprintf("notes_dir does not exist or is not a directory: %s", notesDir)
	}
	cfg.NotesDir = notesDir
	cfg.ConfigPath = expanded
	return &cfg, ""
}

// loadConfig is readConfig's exiting variant, used only at CLI startup.
func loadConfig(path string) *Config {
	cfg, errMsg := readConfig(path)
	if cfg == nil {
		fmt.Fprintln(os.Stderr, "Error:", errMsg)
		fmt.Fprintln(os.Stderr, "Run `matterbase --help` for config format.")
		os.Exit(1)
	}
	return cfg
}

// presetsFromConfig builds Presets from the config's "presets" key
// ("filters" accepted for matterbase-next configs).
func presetsFromConfig(cfg *Config) []Preset {
	defs := cfg.Presets
	if len(defs) == 0 {
		defs = cfg.Filters
	}
	presets := make([]Preset, 0, len(defs))
	for _, d := range defs {
		exprs := d.Query
		if len(exprs) == 0 {
			exprs = d.Exprs
		}
		presets = append(presets, Preset{Label: d.Label, Exprs: exprs})
	}
	return presets
}
