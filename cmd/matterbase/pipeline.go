// The record pipeline: cache replay → DuckDB SQL → full-text display
// filter. DuckDB runs as a CLI subprocess: no CGO, and the UI asks it
// with the reader and the WHERE clause of the yank command.
package main

import (
	"context"
	"encoding/json"
	"os"
	"slices"
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

// rowField numbers the records in the file DuckDB reads. It exists only
// there: DuckDB answers with the numbers of the records that pass.
const rowField = "_matterbase_row"

// applySQL filters records with a DuckDB WHERE clause via the duckdb CLI,
// with the reader and the clause of the yanked command. DuckDB only says
// which records pass. The records returned are the ones passed in, in
// their order, so a value reads the same with and without SQL: DuckDB
// would print a timestamp, a time or a very large number its own way.
// Returns (records, "") on success, or the original records and an error
// message on a bad clause.
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
	numbered := make([]Record, len(records))
	for i, rec := range records {
		row := make(Record, len(rec)+1)
		for k, v := range rec {
			row[k] = v
		}
		row[rowField] = i
		numbered[i] = row
	}
	enc := json.NewEncoder(tmp)
	if err := enc.Encode(numbered); err != nil {
		tmp.Close()
		return records, "SQL: " + firstLine(err.Error())
	}
	tmp.Close()

	safePath := strings.ReplaceAll(tmp.Name(), "'", "''")
	sql := "SELECT " + rowField + " FROM read_json_auto('" + safePath + "') WHERE " + where
	out, err := bkexec.Run(context.Background(), 15*time.Second, nil, "duckdb", "-json", "-c", sql)
	if err != nil {
		return records, "SQL: " + firstLine(err.Error())
	}
	if len(strings.TrimSpace(string(out))) == 0 {
		return []Record{}, ""
	}
	var rows []map[string]int
	if err := json.Unmarshal(out, &rows); err != nil {
		return records, "SQL: " + firstLine(err.Error())
	}
	passed := make([]int, 0, len(rows))
	for _, r := range rows {
		i, ok := r[rowField]
		if !ok || i < 0 || i >= len(records) {
			return records, "SQL: unexpected answer from duckdb"
		}
		passed = append(passed, i)
	}
	slices.Sort(passed)
	result := make([]Record, 0, len(passed))
	for _, i := range passed {
		result = append(result, records[i])
	}
	return result, ""
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

// queryCachedRecordsFn is swappable in tests, which run without grubber.
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
