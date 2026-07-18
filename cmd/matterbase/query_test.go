package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestActiveExpressions(t *testing.T) {
	t.Run("inactive presets ignored", func(t *testing.T) {
		s := QueryState{Presets: []Preset{{Label: "a", Exprs: []string{"x=1"}}, {Label: "b", Exprs: []string{"y=2"}}}}
		if got := s.ActiveExpressions(); len(got) != 0 {
			t.Errorf("got %v, want empty", got)
		}
	})
	t.Run("active presets flattened in order", func(t *testing.T) {
		s := QueryState{Presets: []Preset{
			{Label: "a", Exprs: []string{"x=1", "x2=1"}, Active: true},
			{Label: "b", Exprs: []string{"y=2"}, Active: false},
			{Label: "c", Exprs: []string{"z=3"}, Active: true},
		}}
		want := []string{"x=1", "x2=1", "z=3"}
		got := s.ActiveExpressions()
		if strings.Join(got, ",") != strings.Join(want, ",") {
			t.Errorf("got %v, want %v", got, want)
		}
	})
}

func TestEffectiveSQL(t *testing.T) {
	cases := []struct {
		name  string
		state QueryState
		want  string
	}{
		{"empty", QueryState{}, ""},
		{"user sql only", QueryState{SQLWhere: "status != 'done'"}, "status != 'done'"},
		{"filename only becomes like clause", QueryState{FilenameTerm: "2025"}, "_note_file LIKE '%2025%'"},
		{"filename and sql combined with and", QueryState{SQLWhere: "status = 'x'", FilenameTerm: "rep"}, "(status = 'x') AND _note_file LIKE '%rep%'"},
		{"whitespace only filename inactive", QueryState{FilenameTerm: "   "}, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := c.state.EffectiveSQL(); got != c.want {
				t.Errorf("got %q, want %q", got, c.want)
			}
		})
	}
	t.Run("filename quote escaped", func(t *testing.T) {
		s := QueryState{FilenameTerm: "o'brien"}
		if got := s.EffectiveSQL(); !strings.Contains(got, "o''brien") {
			t.Errorf("got %q, want it to contain o''brien", got)
		}
	})
}

func TestBuildCommand(t *testing.T) {
	t.Run("baseline command", func(t *testing.T) {
		cmd := (&QueryState{}).BuildCommand("/notes", BuildCommandOpts{})
		if !strings.HasPrefix(cmd, "grubber extract /notes -a") {
			t.Errorf("got %q", cmd)
		}
		if strings.Contains(cmd, "duckdb") {
			t.Errorf("got %q, must not mention duckdb without a WHERE", cmd)
		}
	})

	t.Run("presets become f flags", func(t *testing.T) {
		s := QueryState{Presets: []Preset{{Label: "a", Exprs: []string{"status=active"}, Active: true}}}
		cmd := s.BuildCommand("/notes", BuildCommandOpts{})
		if !strings.Contains(cmd, "-f status=active") {
			t.Errorf("got %q", cmd)
		}
	})

	t.Run("sql appends duckdb stage", func(t *testing.T) {
		s := QueryState{SQLWhere: "amount > 100"}
		cmd := s.BuildCommand("/notes", BuildCommandOpts{})
		for _, want := range []string{"| duckdb -json -c", "WHERE amount > 100", "read_json_auto('/dev/stdin')"} {
			if !strings.Contains(cmd, want) {
				t.Errorf("got %q, want it to contain %q", cmd, want)
			}
		}
	})

	t.Run("filename search is in the yank as sql", func(t *testing.T) {
		s := QueryState{FilenameTerm: "2025"}
		cmd := s.BuildCommand("/notes", BuildCommandOpts{})
		if !strings.Contains(cmd, "WHERE _note_file LIKE '%2025%'") {
			t.Errorf("got %q", cmd)
		}
	})

	t.Run("fulltext never in yank", func(t *testing.T) {
		s := QueryState{FulltextTerm: "needle", SQLWhere: "x = 1"}
		cmd := s.BuildCommand("/notes", BuildCommandOpts{})
		if strings.Contains(cmd, "needle") {
			t.Errorf("got %q, must not leak fulltext term", cmd)
		}
	})

	t.Run("collection dir included with explode and merge on", func(t *testing.T) {
		cmd := (&QueryState{}).BuildCommand("/notes", BuildCommandOpts{CollectionDir: "/notes/collections"})
		for _, want := range []string{"--from-jsonl /notes/collections", "--explode binder", "--merge-on id,binder"} {
			if !strings.Contains(cmd, want) {
				t.Errorf("got %q, want it to contain %q", cmd, want)
			}
		}
	})

	t.Run("options included", func(t *testing.T) {
		depth := 2
		cmd := (&QueryState{}).BuildCommand("/notes", BuildCommandOpts{
			SearchMode: "frontmatter", MMD: true, Depth: &depth, ArrayFields: []string{"tags"},
		})
		for _, want := range []string{"--frontmatter-only", "--mmd", "--depth 2"} {
			if !strings.Contains(cmd, want) {
				t.Errorf("got %q, want it to contain %q", cmd, want)
			}
		}
		if !strings.HasPrefix(cmd, "GRUBBER_ARRAY_FIELDS=tags ") {
			t.Errorf("got %q", cmd)
		}
	})

	t.Run("notes dir with space quoted", func(t *testing.T) {
		cmd := (&QueryState{}).BuildCommand("/my notes", BuildCommandOpts{})
		if !strings.Contains(cmd, "'/my notes'") {
			t.Errorf("got %q", cmd)
		}
	})

	t.Run("grubber set shrinks command", func(t *testing.T) {
		s := QueryState{
			Presets:  []Preset{{Label: "a", Exprs: []string{"binder=x"}, Active: true}},
			SQLWhere: "kind = 'pdf'",
		}
		cmd := s.BuildCommand("/notes", BuildCommandOpts{CollectionDir: "/notes/collections", GrubberSet: "notes"})
		if !strings.HasPrefix(cmd, "grubber extract --set notes -a") {
			t.Errorf("got %q", cmd)
		}
		if strings.Contains(cmd, "/notes/collections") || strings.Contains(cmd, "--merge-on") {
			t.Errorf("got %q, must not leak collection dir/merge flags with a grubber_set", cmd)
		}
		if !strings.Contains(cmd, "-f binder=x") || !strings.Contains(cmd, "WHERE kind = 'pdf'") {
			t.Errorf("got %q", cmd)
		}
	})

	t.Run("grubber set keeps explicit overrides", func(t *testing.T) {
		depth := 2
		cmd := (&QueryState{}).BuildCommand("/notes", BuildCommandOpts{MMD: true, Depth: &depth, GrubberSet: "notes"})
		if !strings.Contains(cmd, "--mmd") || !strings.Contains(cmd, "--depth 2") {
			t.Errorf("got %q", cmd)
		}
	})
}

func writeFulltextFixtures(t *testing.T) (mdPath, typPath string) {
	t.Helper()
	dir := t.TempDir()
	md := filepath.Join(dir, "a.md")
	if err := os.WriteFile(md, []byte("# Title\n\nThe needle is here.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	typ := filepath.Join(dir, "b.typ")
	if err := os.WriteFile(typ, []byte("= Doc\nNothing relevant.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return md, typ
}

func TestFilterRecordsFulltext(t *testing.T) {
	t.Run("matching markdown kept", func(t *testing.T) {
		md, typ := writeFulltextFixtures(t)
		records := []Record{{"_note_file": md}, {"_note_file": typ}}
		got := filterRecordsFulltext(records, "needle", nil)
		if len(got) != 1 || got[0]["_note_file"] != md {
			t.Errorf("got %v", got)
		}
	})

	t.Run("jsonl records drop out", func(t *testing.T) {
		md, _ := writeFulltextFixtures(t)
		records := []Record{{"_note_file": md}, {"_note_file": "/x/inbox.jsonl", "filename": "needle.pdf"}}
		got := filterRecordsFulltext(records, "needle", nil)
		if len(got) != 1 || got[0]["_note_file"] != md {
			t.Errorf("got %v", got)
		}
	})

	t.Run("case insensitive", func(t *testing.T) {
		md, _ := writeFulltextFixtures(t)
		got := filterRecordsFulltext([]Record{{"_note_file": md}}, "NEEDLE", nil)
		if len(got) != 1 {
			t.Errorf("got %v", got)
		}
	})

	t.Run("empty term passthrough", func(t *testing.T) {
		records := []Record{{"_note_file": "/x.jsonl"}}
		got := filterRecordsFulltext(records, "  ", nil)
		if len(got) != 1 {
			t.Errorf("got %v", got)
		}
	})

	t.Run("cache prevents rereads", func(t *testing.T) {
		md, _ := writeFulltextFixtures(t)
		cache := map[string]string{}
		filterRecordsFulltext([]Record{{"_note_file": md}}, "needle", cache)
		if _, ok := cache[md]; !ok {
			t.Fatal("expected the file to be cached")
		}
		os.WriteFile(md, []byte("changed"), 0o644)
		got := filterRecordsFulltext([]Record{{"_note_file": md}}, "needle", cache)
		if len(got) != 1 {
			t.Errorf("cached content should still match, got %v", got)
		}
	})

	t.Run("unreadable file dropped", func(t *testing.T) {
		records := []Record{{"_note_file": filepath.Join(t.TempDir(), "missing.md")}}
		got := filterRecordsFulltext(records, "needle", nil)
		if len(got) != 0 {
			t.Errorf("got %v, want none", got)
		}
	})
}

func TestBuildClause(t *testing.T) {
	cases := []struct {
		field, op, value, want string
	}{
		{"status", "=", "active", "status = 'active'"},
		{"amount", "=", "450", "amount = 450"},
		{"amount", ">", "100", "amount > 100"},
		{"title", "LIKE", "report", "title LIKE '%report%'"},
		{"title", "LIKE", "re%t", "title LIKE 're%t'"},
		{"binder", "IS NULL", "", "binder IS NULL"},
		{"binder", "IS NOT NULL", "ignored", "binder IS NOT NULL"},
		{"binder", "IN", "a, b, 3", "binder IN ('a', 'b', 3)"},
		{"name", "=", "o'brien", "name = 'o''brien'"},
		{"my field", "=", "x", `"my field" = 'x'`},
		{"", "=", "x", ""},
		{"status", "=", "", ""},
		{"status", "bogus", "x", ""},
		{"title", "like", "x", "title LIKE '%x%'"},
	}
	for _, c := range cases {
		if got := buildClause(c.field, c.op, c.value); got != c.want {
			t.Errorf("buildClause(%q,%q,%q) = %q, want %q", c.field, c.op, c.value, got, c.want)
		}
	}
}

func TestAppendClause(t *testing.T) {
	if got := appendClause("", "a = 1"); got != "a = 1" {
		t.Errorf("got %q", got)
	}
	if got := appendClause("a = 1", "b = 2"); got != "a = 1 AND b = 2" {
		t.Errorf("got %q", got)
	}
	if got := appendClause("a = 1", ""); got != "a = 1" {
		t.Errorf("got %q", got)
	}
}

func TestRemoveLastClause(t *testing.T) {
	cases := []struct{ sql, want string }{
		{"status = 'open'", ""},
		{"a = 1 AND b = 2", "a = 1"},
		{"a = 1 AND b = 2 AND c = 3", "a = 1 AND b = 2"},
		{"title = 'salt AND pepper' AND b = 2", "title = 'salt AND pepper'"},
		{"binder IN ('a', 'b') AND c = 3", "binder IN ('a', 'b')"},
		{"a = 1 and b = 2", "a = 1"},
		{"name = 'o''brien AND co' AND b = 2", "name = 'o''brien AND co'"},
		{"  ", ""},
	}
	for _, c := range cases {
		if got := removeLastClause(c.sql); got != c.want {
			t.Errorf("removeLastClause(%q) = %q, want %q", c.sql, got, c.want)
		}
	}
}

func TestShellQuote(t *testing.T) {
	if got := shellQuote("plain"); got != "plain" {
		t.Errorf("got %q", got)
	}
	if got := shellQuote("has space"); got != "'has space'" {
		t.Errorf("got %q", got)
	}
	if got := shellQuote(""); got != "''" {
		t.Errorf("got %q", got)
	}
}
