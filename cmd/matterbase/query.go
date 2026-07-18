// The constructed query — port of matterbase's query.py. QueryState holds
// the three channels: grubber presets ("-f" expressions), SQL WHERE
// (filename search folded in), and full-text (display only, never
// yanked — grubber | duckdb cannot express it).
package main

import (
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
	"unicode"

	"github.com/rhsev/matterbase/basekit/recordtable"
)

// Record is one schemaless record, shared with recordtable.
type Record = recordtable.Record

// Preset is a named grubber query ("set") from the config.
type Preset struct {
	Label  string
	Exprs  []string
	Active bool
}

// QueryState is the query builder's full state.
type QueryState struct {
	Presets      []Preset
	SQLWhere     string
	FilenameTerm string
	FulltextTerm string
}

// ActiveExpressions flattens every active preset's expressions into one
// grubber call (record-level AND) — identical semantics in display and
// yank.
func (q *QueryState) ActiveExpressions() []string {
	var exprs []string
	for _, p := range q.Presets {
		if p.Active {
			exprs = append(exprs, p.Exprs...)
		}
	}
	return exprs
}

// FilenameClause is the filename search expressed as SQL, or "" when
// inactive.
func (q *QueryState) FilenameClause() string {
	term := strings.TrimSpace(q.FilenameTerm)
	if term == "" {
		return ""
	}
	return "_note_file LIKE '%" + strings.ReplaceAll(term, "'", "''") + "%'"
}

// EffectiveSQL is the user SQL AND the filename clause — the WHERE the
// pipeline actually runs.
func (q *QueryState) EffectiveSQL() string {
	user := strings.TrimSpace(q.SQLWhere)
	fname := q.FilenameClause()
	switch {
	case user != "" && fname != "":
		return "(" + user + ") AND " + fname
	case user != "":
		return user
	default:
		return fname
	}
}

// FulltextActive reports whether the full-text display filter is set.
func (q *QueryState) FulltextActive() bool {
	return strings.TrimSpace(q.FulltextTerm) != ""
}

// BuildCommandOpts are the session settings BuildCommand needs — the
// config-derived values app.py keeps on self.
type BuildCommandOpts struct {
	SearchMode    string
	MMD           bool
	Depth         *int
	CollectionDir string
	ArrayFields   []string
	GrubberSet    string
}

// BuildCommand renders the yankable `grubber … | duckdb 'SQL'` pipeline.
// Full-text is deliberately absent (decision 1: yank ≠ displayed set
// when full-text is active).
func (q *QueryState) BuildCommand(notesDir string, opts BuildCommandOpts) string {
	var parts []string
	if opts.GrubberSet != "" {
		parts = append(parts, shellQuote(grubberBin()), "extract", "--set", shellQuote(opts.GrubberSet))
	} else {
		parts = append(parts, shellQuote(grubberBin()), "extract", shellQuote(notesDir))
	}
	switch opts.SearchMode {
	case "frontmatter":
		parts = append(parts, "--frontmatter-only")
	case "blocks_only":
		parts = append(parts, "--blocks-only")
	default:
		parts = append(parts, "-a")
	}
	if opts.Depth != nil {
		parts = append(parts, "--depth", strconv.Itoa(*opts.Depth))
	}
	if opts.MMD {
		parts = append(parts, "--mmd")
	}
	if opts.CollectionDir != "" && opts.GrubberSet == "" {
		parts = append(parts, "--from-jsonl", shellQuote(opts.CollectionDir))
		parts = append(parts, "--explode", collectionExplodeField)
		parts = append(parts, "--merge-on", collectionMergeKeys)
	}
	for _, expr := range q.ActiveExpressions() {
		parts = append(parts, "-f", shellQuote(expr))
	}
	cmd := strings.Join(parts, " ")
	if len(opts.ArrayFields) > 0 {
		cmd = "GRUBBER_ARRAY_FIELDS=" + strings.Join(opts.ArrayFields, ",") + " " + cmd
	}
	if sql := q.EffectiveSQL(); sql != "" {
		escaped := strings.ReplaceAll(sql, `"`, `\"`)
		cmd += fmt.Sprintf(` | duckdb -json -c "SELECT * FROM read_json_auto('/dev/stdin') WHERE %s"`, escaped)
	}
	return cmd
}

// shellQuote is Go's shlex.quote: return s unchanged if it's already
// shell-safe, else single-quote it (escaping embedded quotes).
func shellQuote(s string) string {
	if s == "" {
		return "''"
	}
	if shellSafeRe.MatchString(s) {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'"'"'`) + "'"
}

var shellSafeRe = regexp.MustCompile(`^[A-Za-z0-9_@%+=:,./-]+$`)

// ---------------------------------------------------------------------------
// SQL form: field → operator → value ⇒ clause. Comfort only — it
// generates into the SQL input, which stays the single source of truth.
// ---------------------------------------------------------------------------

// sqlFormOperatorOrder is the order the SQL form's operator cycler
// steps through — matches the Python original's SQL_FORM_OPERATORS tuple.
var sqlFormOperatorOrder = []string{
	"=", "!=", "LIKE", ">", "<", ">=", "<=", "IS NULL", "IS NOT NULL", "IN",
}

var sqlFormOperators = func() map[string]bool {
	m := make(map[string]bool, len(sqlFormOperatorOrder))
	for _, op := range sqlFormOperatorOrder {
		m[op] = true
	}
	return m
}()

var simpleIdentRe = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

func sqlIdent(field string) string {
	if simpleIdentRe.MatchString(field) {
		return field
	}
	return `"` + strings.ReplaceAll(field, `"`, `""`) + `"`
}

// sqlValue renders a scalar: numbers stay bare, everything else is
// single-quoted.
func sqlValue(value string) string {
	v := strings.TrimSpace(value)
	if _, err := strconv.ParseFloat(v, 64); err == nil {
		return v
	}
	return "'" + strings.ReplaceAll(v, "'", "''") + "'"
}

// buildClause builds one WHERE clause from the SQL form inputs. Returns
// "" when the combination is incomplete.
func buildClause(field, op, value string) string {
	field = strings.TrimSpace(field)
	op = strings.ToUpper(strings.TrimSpace(op))
	if field == "" || !sqlFormOperators[op] {
		return ""
	}
	ident := sqlIdent(field)

	if op == "IS NULL" || op == "IS NOT NULL" {
		return ident + " " + op
	}

	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}

	if op == "LIKE" {
		pattern := value
		if !strings.Contains(value, "%") {
			pattern = "%" + value + "%"
		}
		return ident + " LIKE '" + strings.ReplaceAll(pattern, "'", "''") + "'"
	}

	if op == "IN" {
		var items []string
		for _, part := range strings.Split(value, ",") {
			if p := strings.TrimSpace(part); p != "" {
				items = append(items, p)
			}
		}
		if len(items) == 0 {
			return ""
		}
		vals := make([]string, len(items))
		for i, it := range items {
			vals[i] = sqlValue(it)
		}
		return ident + " IN (" + strings.Join(vals, ", ") + ")"
	}

	return ident + " " + op + " " + sqlValue(value)
}

// appendClause ANDs a generated clause onto existing SQL (or starts with
// it).
func appendClause(sql, clause string) string {
	sql = strings.TrimSpace(sql)
	if clause == "" {
		return sql
	}
	if sql != "" {
		return sql + " AND " + clause
	}
	return clause
}

// removeLastClause strips the last top-level AND-clause; a single clause
// clears to "". Quote- and paren-aware so an AND inside a string literal
// or an IN (…) list doesn't count as a split point.
func removeLastClause(sql string) string {
	s := strings.TrimSpace(sql)
	inStr := false
	depth := 0
	last := -1
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case inStr:
			if c == '\'' {
				if i+1 < len(s) && s[i+1] == '\'' {
					i++
				} else {
					inStr = false
				}
			}
		case c == '\'':
			inStr = true
		case c == '(':
			depth++
		case c == ')':
			depth--
		case depth == 0 && i+5 <= len(s) && strings.EqualFold(s[i:i+5], " and "):
			last = i
		}
	}
	if last < 0 {
		return ""
	}
	return strings.TrimRightFunc(s[:last], unicode.IsSpace)
}

// ---------------------------------------------------------------------------
// Full-text display filter: searches prose + YAML-block body — what
// grubber | duckdb cannot express. Only markdown/typst records have a
// body; jsonl records drop out while full-text is active.
// ---------------------------------------------------------------------------

func filterRecordsFulltext(records []Record, term string, fileCache map[string]string) []Record {
	needle := strings.ToLower(strings.TrimSpace(term))
	if needle == "" {
		return records
	}
	if fileCache == nil {
		fileCache = map[string]string{}
	}
	var result []Record
	for _, rec := range records {
		path, _ := rec["_note_file"].(string)
		st := sourceType(path)
		if st != "markdown" && st != "typst" {
			continue
		}
		content, ok := fileCache[path]
		if !ok {
			if data, err := os.ReadFile(path); err == nil {
				content = strings.ToLower(string(data))
			}
			fileCache[path] = content
		}
		if strings.Contains(content, needle) {
			result = append(result, rec)
		}
	}
	return result
}
