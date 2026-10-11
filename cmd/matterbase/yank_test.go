package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// The yanked command must return the records the table shows. These tests
// run the real chain (grubber scan, cache replay, DuckDB) over
// testdata/notes, run the yanked command through sh, and compare the two
// record sets. Skipped without grubber or duckdb.

// hermeticGrubber points HOME at an empty directory and clears the
// GRUBBER_* variables, so the user's grubber config cannot change the
// result. It returns the grubber config path inside that HOME.
func hermeticGrubber(t *testing.T) string {
	t.Helper()
	for _, tool := range []string{"grubber", "duckdb", "sh"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skip(tool + " not on PATH")
		}
	}
	if ok, found := checkGrubberVersion(); !ok {
		t.Skip("grubber too old: " + found)
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	for _, v := range []string{"GRUBBER_NOTES", "GRUBBER_ARRAY_FIELDS", "GRUBBER_EXTENSIONS", "GRUBBER_SET"} {
		t.Setenv(v, "")
		os.Unsetenv(v)
	}
	return filepath.Join(home, ".config", "grubber", "config.yaml")
}

func fixtureNotes(t *testing.T) string {
	t.Helper()
	dir, err := filepath.Abs(filepath.Join("testdata", "notes"))
	if err != nil {
		t.Fatal(err)
	}
	return dir
}

// tableRecords runs a rescan the way the app does and returns what the
// table would show.
func tableRecords(t *testing.T, m *appModel) []Record {
	t.Helper()
	msg := m.refreshCmd()().(pipelineDoneMsg)
	if msg.result.Error != "" {
		t.Fatalf("pipeline error: %s", msg.result.Error)
	}
	return msg.result.Records
}

// yankRecords runs the yanked command through sh.
func yankRecords(t *testing.T, command string) []Record {
	t.Helper()
	out, err := exec.Command("sh", "-c", command).Output()
	if err != nil {
		stderr := ""
		if ee, ok := err.(*exec.ExitError); ok {
			stderr = string(ee.Stderr)
		}
		t.Fatalf("yank failed: %v\n%s\ncommand: %s", err, stderr, command)
	}
	if strings.TrimSpace(string(out)) == "" {
		return nil
	}
	var recs []Record
	if err := json.Unmarshal(out, &recs); err != nil {
		t.Fatalf("yank output is not JSON: %v\n%s", err, out)
	}
	return recs
}

// canonical renders records order-independently. Null fields are dropped:
// grubber fills missing keys per result set and DuckDB per table, so the
// two paths differ only in which absent fields they spell out as null.
func canonical(t *testing.T, recs []Record) []string {
	t.Helper()
	out := make([]string, 0, len(recs))
	for _, rec := range recs {
		clean := Record{}
		for k, v := range rec {
			if v != nil {
				clean[k] = v
			}
		}
		b, err := json.Marshal(clean)
		if err != nil {
			t.Fatal(err)
		}
		out = append(out, string(b))
	}
	slices.Sort(out)
	return out
}

func assertYankMatchesTable(t *testing.T, m appModel, wantCount int) {
	t.Helper()
	table := canonical(t, tableRecords(t, &m))
	command := m.buildCommand()
	yank := canonical(t, yankRecords(t, command))
	if len(table) != wantCount {
		t.Errorf("table has %d records, want %d", len(table), wantCount)
	}
	if !slices.Equal(table, yank) {
		t.Errorf("yank differs from table\ncommand: %s\ntable (%d):\n  %s\nyank (%d):\n  %s",
			command, len(table), strings.Join(table, "\n  "), len(yank), strings.Join(yank, "\n  "))
	}
}

func TestYankReturnsTheTableRecords(t *testing.T) {
	hermeticGrubber(t)
	notes := fixtureNotes(t)

	cases := []struct {
		name  string
		setup func(*appModel)
		want  int
	}{
		{"everything", func(m *appModel) {}, 8},
		{"preset", func(m *appModel) { m.state.Presets[0].Active = true }, 2},
		{"two presets", func(m *appModel) {
			m.state.Presets[0].Active = true
			m.state.Presets[1].Active = true
		}, 1},
		{"sql", func(m *appModel) { m.state.SQLWhere = "amount > 100" }, 2},
		{"sql with keyword field", func(m *appModel) {
			m.state.SQLWhere = buildClause("end", "<", "2027-01-01")
		}, 1},
		{"filename", func(m *appModel) { m.state.FilenameTerm = "people" }, 2},
		{"preset, sql and filename", func(m *appModel) {
			m.state.Presets[0].Active = true
			m.state.SQLWhere = "amount > 100"
			m.state.FilenameTerm = "contracts"
		}, 1},
		{"binder with umlaut file", func(m *appModel) { m.state.SQLWhere = "filename = 'büro.pdf'" }, 1},
		{"array fields", func(m *appModel) {
			m.arrayFields = []string{"tags"}
			m.state.SQLWhere = "tags IS NOT NULL"
		}, 2},
		{"no match", func(m *appModel) {
			m.state.Presets[0].Exprs = []string{"type=nothing"}
			m.state.Presets[0].Active = true
		}, 0},
		{"no match in sql", func(m *appModel) { m.state.SQLWhere = "amount > 100000" }, 0},
		{"blocks only", func(m *appModel) { m.searchMode = "blocks_only" }, 6},
		{"depth 0", func(m *appModel) {
			d := 0
			m.depth = &d
		}, 6},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m := newAppModel(&Config{
				NotesDir: notes,
				Presets: []PresetDef{
					{Label: "contracts", Query: []string{"type=contract"}},
					{Label: "active", Query: []string{"status=active"}},
				},
			})
			t.Cleanup(func() { os.Remove(m.cachePath) })
			c.setup(&m)
			assertYankMatchesTable(t, m, c.want)
		})
	}
}

// With grubber_set, the yank names a set from grubber's config. A set
// with path, from_jsonl, explode and merge_on returns the table's records.
func TestYankWithGrubberSetReturnsTheTableRecords(t *testing.T) {
	configPath := hermeticGrubber(t)
	notes := fixtureNotes(t)
	set := "sets:\n  notes:\n    path: " + notes + "\n" +
		"    from_jsonl: [\"" + filepath.Join(notes, "collections") + "/\"]\n" +
		"    explode: binder\n" +
		"    merge_on: [id, binder]\n"
	if err := os.MkdirAll(filepath.Dir(configPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(configPath, []byte(set), 0o644); err != nil {
		t.Fatal(err)
	}

	m := newAppModel(&Config{NotesDir: notes, GrubberSet: "notes"})
	t.Cleanup(func() { os.Remove(m.cachePath) })
	if !strings.Contains(m.buildCommand(), "--set notes") {
		t.Fatalf("yank does not use the set: %s", m.buildCommand())
	}
	assertYankMatchesTable(t, m, 8)
}
