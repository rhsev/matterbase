package main

import (
	"encoding/json"
	"os/exec"
	"reflect"
	"testing"
)

// DuckDB reads a timestamp, a time and a very large number as typed
// values and prints them its own way: 10:00:00Z in another time zone,
// 10:30 with seconds, the number as a string. The table must show what
// the note holds, with and without SQL.
func TestSQLKeepsTheValuesOfTheNote(t *testing.T) {
	if _, err := exec.LookPath("duckdb"); err != nil {
		t.Skip("duckdb not on PATH")
	}
	var records []Record
	src := `[
	 {"id":1,"ts":"2026-01-05T10:00:00","utc":"2026-01-05T10:00:00Z","off":"2026-01-05T10:00:00+02:00","at":"10:30","big":12345678901234567890,"nest":{"a":1}},
	 {"id":2,"ts":"2026-02-01T08:30:00","utc":"2026-02-01T08:30:00Z","off":"2026-02-01T08:30:00+02:00","at":"08:00","big":1,"nest":{"b":2}},
	 {"id":3,"ts":"2026-03-01T08:30:00","utc":"2026-03-01T08:30:00Z","off":"2026-03-01T08:30:00+02:00","at":"09:15","big":2}
	]`
	if err := json.Unmarshal([]byte(src), &records); err != nil {
		t.Fatal(err)
	}

	got, errMsg := applySQL(records, "id <> 2")
	if errMsg != "" {
		t.Fatal(errMsg)
	}
	want := []Record{records[0], records[2]}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("records changed under SQL\n got: %v\nwant: %v", got, want)
	}
	if _, has := got[0][rowField]; has {
		t.Errorf("the row number leaked into the record: %v", got[0])
	}

	// The clause still sees typed values: a timestamp compares as one.
	got, errMsg = applySQL(records, "ts > TIMESTAMP '2026-01-31 00:00:00' AND year(utc) = 2026")
	if errMsg != "" {
		t.Fatal(errMsg)
	}
	if len(got) != 2 || got[0]["id"] != records[1]["id"] || got[1]["id"] != records[2]["id"] {
		t.Errorf("typed clause returned %v, want records 2 and 3 in that order", got)
	}
}
