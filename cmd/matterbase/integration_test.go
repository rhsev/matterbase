package main

import (
	"os"
	"os/exec"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// TestIntegrationRealGrubberData exercises the extract → replay → render
// chain against the real rhsev tree — the "Headless-Render gegen echte
// grubber-Daten" verification the AUFTRAG asks for, skipped when grubber
// or the tree aren't available (e.g. CI).
func TestIntegrationRealGrubberData(t *testing.T) {
	if _, err := exec.LookPath("grubber"); err != nil {
		t.Skip("grubber not on PATH")
	}
	const notesDir = "/Volumes/lightning/Git/rhsev/twin"
	if info, err := os.Stat(notesDir); err != nil || !info.IsDir() {
		t.Skip("rhsev tree not available")
	}

	cache, err := os.CreateTemp("", "matterbase-integration-*.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	cache.Close()
	defer os.Remove(cache.Name())

	var extractErr string
	ok := extractToJSONL(notesDir, cache.Name(), ExtractOpts{}, func(s string) { extractErr = s })
	if !ok {
		t.Fatalf("extractToJSONL failed: %s", extractErr)
	}

	info, err := os.Stat(cache.Name())
	if err != nil || info.Size() == 0 {
		t.Fatal("expected a non-empty JSONL cache")
	}

	var pipelineErr string
	state := QueryState{}
	result := runPipeline(cache.Name(), &state, nil, map[string]string{}, func(s string) { pipelineErr = s })
	if pipelineErr != "" {
		t.Fatalf("runPipeline error: %s", pipelineErr)
	}
	if len(result.Records) == 0 {
		t.Fatal("expected at least one record from the real tree")
	}
	t.Logf("scanned %d records from %s", len(result.Records), notesDir)

	rec := result.Records[0]
	title, content := renderPreview(rec, "record", ApexConfig{}, false)
	if title == "" {
		t.Error("expected a non-empty preview title")
	}
	if content == "" {
		t.Error("expected non-empty record-mode preview content")
	}
	t.Logf("first record preview title: %q", title)
}

// TestStartupDeliversRecords drives the real Init → Update → pipeline →
// Update chain the way bubbletea does (value model, commands run outside).
// Regression: Init used to call refreshCmd on a discarded copy, so the
// initial result arrived with a generation the stored model never saw and
// every startup showed zero records.
func TestStartupDeliversRecords(t *testing.T) {
	if _, err := exec.LookPath("grubber"); err != nil {
		t.Skip("grubber not on PATH")
	}
	dir := t.TempDir()
	note := "---\ntitle: startup probe\nstatus: active\n---\n\nbody\n"
	if err := os.WriteFile(dir+"/probe.md", []byte(note), 0o644); err != nil {
		t.Fatal(err)
	}

	m := newAppModel(&Config{NotesDir: dir})
	defer os.Remove(m.cachePath)

	var model tea.Model = m
	cmd := m.Init()
	// run the command chain to quiescence, feeding results back like the
	// bubbletea loop does
	for i := 0; cmd != nil && i < 10; i++ {
		msg := cmd()
		if msg == nil {
			break
		}
		model, cmd = model.Update(msg)
	}

	app := model.(appModel)
	if app.table.Len() != 1 {
		t.Fatalf("table has %d records after startup, want 1 (status: %q)", app.table.Len(), app.status)
	}
	if rec, _, ok := app.table.Current(); !ok || rec["title"] != "startup probe" {
		t.Fatalf("unexpected current record: %v", rec)
	}
}
