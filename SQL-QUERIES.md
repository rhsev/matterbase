# SQL queries in matterbase

SQL goes into the **sql where** field of the query builder (`/` jumps there).
`Enter` applies it.

matterbase passes the input to DuckDB as a WHERE clause:

```sql
SELECT * FROM read_json_auto('records.json') WHERE <clause>
```

The field takes only the condition, without `SELECT`, `FROM` or `WHERE`.

The **form** below the field (field, operator, value) writes such clauses.
Pick a field from the current result set and an operator, type a value and
press `Enter`. Each generated clause is added to the input with AND, and `-`
removes the last clause again. The form handles quoting. Strings get single
quotes, numbers stay bare, `LIKE` values are wrapped in `%…%`, `IN` lists are
split on commas, and field names that are SQL keywords get double quotes.

The **filename** input works the same way. It adds
`_note_file LIKE '%…%'` to the WHERE clause, and the yanked command contains
it as plain SQL.

---

## Filter

```sql
status = 'active'
status != 'archive'
amount > 1000
"end" IS NULL                        -- field missing or empty
"end" IS NOT NULL                    -- field present
```

## Pattern match

```sql
name LIKE '%alpha%'                  -- contains "alpha"
name NOT LIKE '%draft%'              -- does not contain
_note_file LIKE '%2026%'             -- source path contains "2026"
```

## Date comparison

```sql
start > '2026-01-01'
"end" < '2026-12-31'
start BETWEEN '2026-01-01' AND '2026-03-31'
```

## Combine

```sql
status = 'active' AND amount > 1000
type = 'contract' AND "end" < '2026-12-31'
status = 'active' OR status = 'pending'
binder IN ('project-alpha', 'project-beta')
```

## NULL-safe comparisons

DuckDB treats missing fields as `NULL`. `IS NULL` and `IS NOT NULL` work,
`= NULL` does not.

```sql
"end" IS NULL                        -- end not set
tags IS NOT NULL                     -- tags present
```

## Sorting

The clause is inserted after `WHERE`, so trailing SQL is accepted:

```sql
status = 'active' ORDER BY amount DESC
```

This sorts the table, and the yanked command keeps the order. It is not a
dedicated feature and has limits. With a filename search active, matterbase
puts the SQL in parentheses, and DuckDB rejects `ORDER BY` inside them. The
form appends its clauses after the `ORDER BY`, which DuckDB rejects as well.

---

## Notes

**Column names** like `_note_file` work unquoted in DuckDB. A name that
contains spaces or special characters, or that is an SQL keyword such as
`end`, `order` or `group`, needs double quotes. The form adds them on its own.

**Full-text search** across record *bodies* is not a SQL concern and has its
own input. It narrows the display only and is never part of the yanked
command. `LIKE` on a field is ordinary SQL and is part of the yank.

**Projection** (column selection) is not supported in the WHERE field. For
other columns, yank (`y`) and edit the `SELECT *` in the copied command.
