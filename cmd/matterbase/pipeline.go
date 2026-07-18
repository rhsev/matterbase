// The record pipeline: cache replay → DuckDB SQL → full-text display
// filter. Port of matterbase's pipeline.py. DuckDB runs as a CLI
// subprocess (decision 1: no CGO, the yank command and the UI share the
// same codepath).
package main

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"time"

	bkexec "github.com/rhsev/matterbase/basekit/exec"
)

// PipelineResult is one query's outcome.
type PipelineResult struct {
	Records         []Record
	StructuredCount int // count before the full-text display filter
	Error           string
}

// applySQL filters records with a DuckDB WHERE clause via the duckdb CLI,
// the same codepath the yanked command uses. Returns (records, "") on
// success, or the original records and an error message on a bad clause.
func applySQL(records []Record, where string) ([]Record, string) {
	where = strings.TrimSpace(where)
	if where == "" || len(records) == 0 {
		return records, ""
	}

	tmp, err := os.CreateTemp("", "matterbase-*.json")
	if err != nil {
		return records, "SQL: " + firstLine(err.Error())
	}
	defer os.Remove(tmp.Name())
	enc := json.NewEncoder(tmp)
	if err := enc.Encode(records); err != nil {
		tmp.Close()
		return records, "SQL: " + firstLine(err.Error())
	}
	tmp.Close()

	safePath := strings.ReplaceAll(tmp.Name(), "'", "''")
	sql := "SELECT * FROM read_json_auto('" + safePath + "') WHERE " + where
	out, err := bkexec.Run(context.Background(), 15*time.Second, nil, "duckdb", "-json", "-c", sql)
	if err != nil {
		return records, "SQL: " + firstLine(err.Error())
	}
	if len(strings.TrimSpace(string(out))) == 0 {
		return []Record{}, ""
	}
	var result []Record
	if err := json.Unmarshal(out, &result); err != nil {
		return records, "SQL: " + firstLine(err.Error())
	}
	return result, ""
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

// queryCachedRecordsFn is swappable in tests (Python's
// patch("matterbase.pipeline.query_cached_records")).
var queryCachedRecordsFn = queryCachedRecords

// runPipeline runs the full chain over the in-session cache: grubber
// replay with the active preset expressions, DuckDB WHERE (user SQL with
// the filename clause folded in), then the full-text display filter.
func runPipeline(cachePath string, state *QueryState, arrayFields []string, fulltextCache map[string]string, onError func(string)) PipelineResult {
	records := queryCachedRecordsFn(cachePath, state.ActiveExpressions(), arrayFields, onError)

	errMsg := ""
	if sql := state.EffectiveSQL(); sql != "" {
		records, errMsg = applySQL(records, sql)
	}

	structuredCount := len(records)
	if state.FulltextActive() {
		records = filterRecordsFulltext(records, state.FulltextTerm, fulltextCache)
	}

	return PipelineResult{Records: records, StructuredCount: structuredCount, Error: errMsg}
}
