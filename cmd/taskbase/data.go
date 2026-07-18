// taskbase-go reads na_json's flat task index (~/.config/na_json/index.json):
// notes with a "## Tasks" section, each exploded into per-task records.
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

func defaultIndexPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".config", "na_json", "index.json")
}

// IndexRecord is one note's entry in the na_json index.
type IndexRecord struct {
	File    string    `json:"file"`
	Project string    `json:"project"`
	Title   string    `json:"title"`
	Tasks   []RawTask `json:"tasks"`
}

// RawTask is one task line as na_json stores it.
type RawTask struct {
	Line   int            `json:"line"`
	Status *string        `json:"status"`
	Tags   map[string]any `json:"tags"`
	Task   string         `json:"task"`
}

// Task is a flattened task carrying its parent note's fields — port of
// data.py's flat_tasks.
type Task struct {
	Line    int
	Status  string
	Tags    map[string]any
	Text    string
	Project string
	File    string
}

func loadIndex(path string) ([]IndexRecord, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var records []IndexRecord
	if err := json.Unmarshal(data, &records); err != nil {
		return nil, err
	}
	return records, nil
}

func flatTasks(records []IndexRecord) []Task {
	var tasks []Task
	for _, rec := range records {
		for _, rt := range rec.Tasks {
			status := ""
			if rt.Status != nil {
				status = *rt.Status
			}
			tasks = append(tasks, Task{
				Line:    rt.Line,
				Status:  status,
				Tags:    rt.Tags,
				Text:    rt.Task,
				Project: rec.Project,
				File:    rec.File,
			})
		}
	}
	return tasks
}

// isDone/isClosed mirror data.py's filter_tasks: done is a strict subset
// of closed (x/@done), closed also covers deferred/cancelled (>/-/tags).
func isDone(t Task) bool {
	_, done := t.Tags["done"]
	return t.Status == "x" || done
}

func isClosed(t Task) bool {
	if isDone(t) {
		return true
	}
	if t.Status == ">" || t.Status == "-" {
		return true
	}
	_, deferred := t.Tags["deferred"]
	_, cancelled := t.Tags["cancelled"]
	return deferred || cancelled
}

// filterTasks ports data.py's filter_tasks. showDone selects the closed
// set instead of hiding it — there is no "show everything" mode, matching
// the Python original.
func filterTasks(tasks []Task, search, tag, project string, showDone bool) []Task {
	var result []Task
	for _, t := range tasks {
		closed := isClosed(t)
		if !showDone && closed {
			continue
		}
		if showDone && !isDone(t) {
			continue
		}
		if tag != "" {
			if _, ok := t.Tags[tag]; !ok {
				continue
			}
		}
		if project != "" && !strings.Contains(strings.ToLower(t.Project), strings.ToLower(project)) {
			continue
		}
		if search != "" && !strings.Contains(strings.ToLower(t.Text), strings.ToLower(search)) {
			continue
		}
		result = append(result, t)
	}
	return result
}

// formatTags renders a task's tag set like data.py's app.py: "@k" for a
// bare tag, "@k(v)" for a valued one.
func formatTags(tags map[string]any) string {
	if len(tags) == 0 {
		return ""
	}
	keys := make([]string, 0, len(tags))
	for k := range tags {
		keys = append(keys, k)
	}
	// Map order is random in Go; sort so the cell/preview text is stable
	// across refreshes (the Python dict preserved insertion order instead).
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		if v, _ := tags[k].(bool); v {
			parts = append(parts, "@"+k)
		} else {
			parts = append(parts, "@"+k+"("+toString(tags[k])+")")
		}
	}
	return strings.Join(parts, "  ")
}

func toString(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	if v == nil {
		return ""
	}
	return fmt.Sprintf("%v", v)
}

func noteStem(path string) string {
	if path == "" {
		return ""
	}
	base := filepath.Base(path)
	return strings.TrimSuffix(base, filepath.Ext(base))
}
