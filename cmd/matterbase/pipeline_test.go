package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func sampleRecords() []Record {
	return []Record{
		{"_note_file": "/n/a.md", "status": "active", "amount": float64(50)},
		{"_note_file": "/n/b.md", "status": "done", "amount": float64(200)},
		{"_note_file": "/n/collections/inbox.jsonl", "status": "active", "amount": float64(999)},
	}
}

func requireDuckDB(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("duckdb"); err != nil {
		t.Skip("duckdb not on PATH")
	}
}

func TestApplySQL(t *testing.T) {
	requireDuckDB(t)
	records := sampleRecords()

	t.Run("empty where passthrough", func(t *testing.T) {
		got, errMsg := applySQL(records, "")
		if errMsg != "" || len(got) != len(records) {
			t.Errorf("got %v err=%q", got, errMsg)
		}
	})

	t.Run("filters by field", func(t *testing.T) {
		got, errMsg := applySQL(records, "status = 'active'")
		if errMsg != "" {
			t.Fatalf("err = %q", errMsg)
		}
		files := map[string]bool{}
		for _, r := range got {
			files[r["_note_file"].(string)] = true
		}
		if len(files) != 2 || !files["/n/a.md"] || !files["/n/collections/inbox.jsonl"] {
			t.Errorf("files = %v", files)
		}
	})

	t.Run("comparison", func(t *testing.T) {
		got, errMsg := applySQL(records, "amount > 100")
		if errMsg != "" || len(got) != 2 {
			t.Errorf("got %v err=%q", got, errMsg)
		}
	})

	t.Run("filename like clause", func(t *testing.T) {
		got, errMsg := applySQL(records, "_note_file LIKE '%a.md%'")
		if errMsg != "" || len(got) != 1 {
			t.Errorf("got %v err=%q", got, errMsg)
		}
	})

	t.Run("invalid sql returns error and unfiltered", func(t *testing.T) {
		got, errMsg := applySQL(records, "no_such_column ===")
		if !strings.HasPrefix(errMsg, "SQL:") {
			t.Errorf("errMsg = %q, want SQL: prefix", errMsg)
		}
		if len(got) != len(records) {
			t.Errorf("got %v, want the original records back", got)
		}
	})

	t.Run("empty records passthrough", func(t *testing.T) {
		got, errMsg := applySQL(nil, "status = 'x'")
		if errMsg != "" || len(got) != 0 {
			t.Errorf("got %v err=%q", got, errMsg)
		}
	})
}

func TestRunPipeline(t *testing.T) {
	requireDuckDB(t)
	restore := func(fn func(string, []string, []string, func(string)) []Record) {
		queryCachedRecordsFn = fn
	}
	defer restore(queryCachedRecords)

	t.Run("plain replay", func(t *testing.T) {
		called := false
		restore(func(path string, exprs, arrayFields []string, onError func(string)) []Record {
			called = true
			return sampleRecords()
		})
		result := runPipeline("/cache.jsonl", &QueryState{}, nil, nil, nil)
		if !called || len(result.Records) != 3 || result.StructuredCount != 3 || result.Error != "" {
			t.Errorf("result = %+v", result)
		}
	})

	t.Run("preset expressions forwarded", func(t *testing.T) {
		var gotExprs []string
		restore(func(path string, exprs, arrayFields []string, onError func(string)) []Record {
			gotExprs = exprs
			return nil
		})
		state := QueryState{Presets: []Preset{{Label: "a", Exprs: []string{"status=active"}, Active: true}}}
		runPipeline("/cache.jsonl", &state, nil, nil, nil)
		if len(gotExprs) != 1 || gotExprs[0] != "status=active" {
			t.Errorf("gotExprs = %v", gotExprs)
		}
	})

	t.Run("sql stage applied", func(t *testing.T) {
		restore(func(path string, exprs, arrayFields []string, onError func(string)) []Record {
			return sampleRecords()
		})
		state := QueryState{SQLWhere: "amount > 100"}
		result := runPipeline("/cache.jsonl", &state, nil, nil, nil)
		if len(result.Records) != 2 {
			t.Errorf("result.Records = %v", result.Records)
		}
	})

	t.Run("fulltext narrows display after sql", func(t *testing.T) {
		md := filepath.Join(t.TempDir(), "a.md")
		os.WriteFile(md, []byte("contains the needle"), 0o644)
		records := []Record{
			{"_note_file": md, "status": "active"},
			{"_note_file": "/n/inbox.jsonl", "status": "active"},
		}
		restore(func(path string, exprs, arrayFields []string, onError func(string)) []Record {
			return records
		})
		state := QueryState{FulltextTerm: "needle"}
		result := runPipeline("/cache.jsonl", &state, nil, nil, nil)
		if result.StructuredCount != 2 {
			t.Errorf("structuredCount = %d, want 2", result.StructuredCount)
		}
		if len(result.Records) != 1 || result.Records[0]["_note_file"] != md {
			t.Errorf("records = %v", result.Records)
		}
	})

	t.Run("sql error surfaced", func(t *testing.T) {
		restore(func(path string, exprs, arrayFields []string, onError func(string)) []Record {
			return sampleRecords()
		})
		state := QueryState{SQLWhere: "bogus ==="}
		result := runPipeline("/cache.jsonl", &state, nil, nil, nil)
		if result.Error == "" {
			t.Error("expected an error")
		}
	})
}
