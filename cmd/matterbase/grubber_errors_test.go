package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// These tests replace grubber with a shell script (fakeTools), so each
// failure mode can be produced on purpose. The scripts may only use shell
// builtins: fakeTools leaves nothing else on PATH.

func runFake(t *testing.T, script string) ([]Record, string) {
	t.Helper()
	fakeTools(t, map[string]string{"grubber": script})
	t.Setenv("GRUBBER", "")
	var errMsg string
	recs := runGrubberCmd([]string{grubberBin(), "extract"}, func(s string) { errMsg = s })
	return recs, errMsg
}

func TestRunGrubberCmd(t *testing.T) {
	t.Run("records", func(t *testing.T) {
		recs, errMsg := runFake(t, `echo '[{"a":1},{"a":2}]'`)
		if errMsg != "" || len(recs) != 2 {
			t.Errorf("got %d records, error %q", len(recs), errMsg)
		}
	})
	t.Run("no match is no error", func(t *testing.T) {
		recs, errMsg := runFake(t, `echo "No records found" >&2`)
		if errMsg != "" || recs == nil || len(recs) != 0 {
			t.Errorf("got %v, error %q, want an empty result without error", recs, errMsg)
		}
	})
	t.Run("warning on stderr with records", func(t *testing.T) {
		recs, errMsg := runFake(t, `echo "Warning: YAML parse error" >&2; echo '[{"a":1}]'`)
		if errMsg != "" || len(recs) != 1 {
			t.Errorf("got %d records, error %q", len(recs), errMsg)
		}
	})
	t.Run("exit code with stderr", func(t *testing.T) {
		recs, errMsg := runFake(t, `echo "unknown flag: --explode" >&2; echo "usage" >&2; exit 2`)
		if recs != nil || !strings.HasSuffix(errMsg, ": unknown flag: --explode") {
			t.Errorf("got %v, error %q, want the first stderr line", recs, errMsg)
		}
	})
	t.Run("exit code without stderr", func(t *testing.T) {
		recs, errMsg := runFake(t, `exit 3`)
		if recs != nil || !strings.Contains(errMsg, "exit status 3") {
			t.Errorf("got %v, error %q", recs, errMsg)
		}
	})
	t.Run("invalid JSON", func(t *testing.T) {
		recs, errMsg := runFake(t, `echo '[{"a":'`)
		if recs != nil || !strings.HasPrefix(errMsg, "grubber: invalid JSON output") {
			t.Errorf("got %v, error %q", recs, errMsg)
		}
	})
	t.Run("JSON that is no list", func(t *testing.T) {
		recs, errMsg := runFake(t, `echo '{"a":1}'`)
		if recs != nil || !strings.HasPrefix(errMsg, "grubber: invalid JSON output") {
			t.Errorf("got %v, error %q", recs, errMsg)
		}
	})
	t.Run("grubber missing", func(t *testing.T) {
		fakeTools(t, nil)
		t.Setenv("GRUBBER", "")
		var errMsg string
		recs := runGrubberCmd([]string{grubberBin(), "extract"}, func(s string) { errMsg = s })
		if recs != nil || !strings.HasPrefix(errMsg, "grubber: ") {
			t.Errorf("got %v, error %q", recs, errMsg)
		}
	})
	t.Run("no error callback", func(t *testing.T) {
		fakeTools(t, map[string]string{"grubber": "exit 1"})
		if recs := runGrubberCmd([]string{grubberBin()}, nil); recs != nil {
			t.Errorf("got %v", recs)
		}
	})
}

func TestQueryCachedRecordsNeverReturnsNil(t *testing.T) {
	fakeTools(t, map[string]string{"grubber": "exit 1"})
	t.Setenv("GRUBBER", "")
	var errMsg string
	recs := queryCachedRecords("/cache.jsonl", nil, nil, func(s string) { errMsg = s })
	if recs == nil || len(recs) != 0 || errMsg == "" {
		t.Errorf("got %v, error %q, want an empty list and an error", recs, errMsg)
	}
}

func TestExtractToJSONL(t *testing.T) {
	notes := t.TempDir()

	t.Run("writes the output", func(t *testing.T) {
		fakeTools(t, map[string]string{"grubber": `echo '{"a":1}'`})
		out := filepath.Join(t.TempDir(), "cache.jsonl")
		var errMsg string
		if !extractToJSONL(notes, out, ExtractOpts{}, func(s string) { errMsg = s }) {
			t.Fatalf("failed: %s", errMsg)
		}
		if data, _ := os.ReadFile(out); string(data) != "{\"a\":1}\n" {
			t.Errorf("cache = %q", data)
		}
	})
	t.Run("grubber fails", func(t *testing.T) {
		fakeTools(t, map[string]string{"grubber": `echo "notes dir unreadable" >&2; exit 1`})
		var errMsg string
		ok := extractToJSONL(notes, filepath.Join(t.TempDir(), "c.jsonl"), ExtractOpts{}, func(s string) { errMsg = s })
		if ok || !strings.HasSuffix(errMsg, "notes dir unreadable") {
			t.Errorf("ok %v, error %q", ok, errMsg)
		}
	})
	t.Run("cache not writable", func(t *testing.T) {
		fakeTools(t, map[string]string{"grubber": `echo '{"a":1}'`})
		var errMsg string
		out := filepath.Join(t.TempDir(), "missing-dir", "c.jsonl")
		ok := extractToJSONL(notes, out, ExtractOpts{}, func(s string) { errMsg = s })
		if ok || !strings.HasPrefix(errMsg, "grubber: ") {
			t.Errorf("ok %v, error %q", ok, errMsg)
		}
	})
	t.Run("passes the options", func(t *testing.T) {
		argsFile := filepath.Join(t.TempDir(), "args")
		t.Setenv("ARGS_FILE", argsFile)
		fakeTools(t, map[string]string{"grubber": `echo "$@" > "$ARGS_FILE"`})
		depth := 1
		extractToJSONL(notes, filepath.Join(t.TempDir(), "c.jsonl"),
			ExtractOpts{SearchMode: "blocks_only", MMD: true, Depth: &depth, CollectionDir: "/c"}, nil)
		data, _ := os.ReadFile(argsFile)
		args := string(data)
		for _, want := range []string{"--blocks-only", "--format=jsonl", "--depth 1", "--mmd", "--from-jsonl /c", "--explode binder", "--merge-on id,binder"} {
			if !strings.Contains(args, want) {
				t.Errorf("grubber called with %q, missing %q", strings.TrimSpace(args), want)
			}
		}
	})
}

// fakeGrubber answers like grubber: JSONL for the scan, a JSON list for
// the replay. fail makes the scan fail.
func fakeGrubber(t *testing.T, fail bool) {
	t.Helper()
	scan := `echo '{"_note_file":"/n/a.md","status":"active"}'`
	if fail {
		scan = `echo "cannot read notes dir" >&2; exit 1`
	}
	fakeTools(t, map[string]string{"grubber": `case "$*" in
*--format=jsonl*) ` + scan + ` ;;
*) echo '[{"_note_file":"/n/a.md","status":"active"}]' ;;
esac`})
	t.Setenv("GRUBBER", "")
}

// A failing grubber shows in the status line and empties the table; the
// next successful refresh clears the error again.
func TestRefreshShowsGrubberFailureInStatus(t *testing.T) {
	fakeGrubber(t, true)
	m := newAppModel(&Config{NotesDir: t.TempDir()})
	t.Cleanup(func() { os.Remove(m.cachePath) })
	m.table.SetRecords([]Record{{"_note_file": "/old.md"}})

	m.finishQuery(m.refreshCmd()().(pipelineDoneMsg))
	if !m.statusErr || !strings.Contains(m.status, "cannot read notes dir") {
		t.Errorf("status %q (err %v), want the grubber error", m.status, m.statusErr)
	}
	if m.table.Len() != 0 {
		t.Errorf("table keeps %d records after a failed scan", m.table.Len())
	}

	fakeGrubber(t, false)
	m.finishQuery(m.refreshCmd()().(pipelineDoneMsg))
	if m.statusErr || !strings.HasPrefix(m.status, "1 record ") {
		t.Errorf("status %q (err %v) after a good scan", m.status, m.statusErr)
	}
}

// A query without matches is a normal result, not an error.
func TestEmptyResultIsNoError(t *testing.T) {
	fakeTools(t, map[string]string{"grubber": `echo "No records found" >&2`})
	t.Setenv("GRUBBER", "")
	m := newAppModel(&Config{NotesDir: t.TempDir()})
	t.Cleanup(func() { os.Remove(m.cachePath) })

	m.finishQuery(m.runQueryCmd()().(pipelineDoneMsg))
	if m.statusErr || !strings.HasPrefix(m.status, "0 records ") {
		t.Errorf("status %q (err %v), want \"0 records\" without error", m.status, m.statusErr)
	}
}
