# SQL queries in matterbase

Typed in the table query field above the metadata table. Press `Enter` to apply.

matterbase passes the input as a SQL WHERE clause via DuckDB:

```sql
SELECT * FROM read_json_auto('data.json') WHERE <your clause>
```

Type only the condition — without `SELECT`, `FROM`, or `WHERE`.

---

## Filter

```sql
status = 'active'
status != 'archive'
amount > 1000
end IS NULL                          -- field missing or empty
end IS NOT NULL                      -- field present
```

## Pattern match

```sql
name LIKE '%alpha%'                  -- contains "alpha"
name NOT LIKE '%draft%'              -- does not contain
"_note_file" LIKE '%2025%'           -- filename contains "2025"
```

## Date comparison

```sql
start > '2025-01-01'
end < '2025-12-31'
start BETWEEN '2025-01-01' AND '2025-03-31'
```

## Combine

```sql
status = 'active' AND amount > 1000
type = 'contract' AND end < '2025-12-31'
status = 'active' OR status = 'pending'
```

## NULL-safe comparisons

DuckDB treats missing fields as `NULL`. Use `IS NULL` / `IS NOT NULL` rather than `= NULL`.

```sql
end IS NULL                          -- end not set
tags IS NOT NULL                     -- tags present
```

---

## Known limitations

**Column names starting with `_`** (like `_note_file`) must be quoted with double quotes in SQL:

```sql
"_note_file" LIKE '%2025%'
```

**Full-text search** across all fields is not supported in the table query field. Use the search input instead (prefix with space: `<space>word`).

**Sorting and projection** are not supported in the WHERE clause field — only filter conditions. For ad-hoc sorting or column selection, use the yanked command (`y`) and pipe it through duckdb manually.
