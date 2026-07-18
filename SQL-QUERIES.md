# SQL queries in matterbase

Typed in the **sql where** field of the query builder (jump there with `/`).
Press `Enter` to apply.

matterbase passes the input as a SQL WHERE clause via DuckDB:

```sql
SELECT * FROM read_json_auto('records.json') WHERE <your clause>
```

Type only the condition — without `SELECT`, `FROM`, or `WHERE`.

The **form** below the field (field → operator → value) generates these
clauses for you: pick a field from the current result set, an operator, type a
value, `Enter`. Each generated clause is ANDed onto the input; `-` removes the
last clause again. Quoting is handled (strings quoted, numbers bare, `LIKE`
values wrapped in `%…%`, `IN` lists split on commas).

The **filename** input is the same mechanism: it folds into the WHERE clause
as `_note_file LIKE '%…%'` and shows up in the yanked command as plain SQL.

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
_note_file LIKE '%2026%'             -- source path contains "2026"
```

## Date comparison

```sql
start > '2026-01-01'
end < '2026-12-31'
start BETWEEN '2026-01-01' AND '2026-03-31'
```

## Combine

```sql
status = 'active' AND amount > 1000
type = 'contract' AND end < '2026-12-31'
status = 'active' OR status = 'pending'
binder IN ('project-alpha', 'project-beta')
```

## NULL-safe comparisons

DuckDB treats missing fields as `NULL`. Use `IS NULL` / `IS NOT NULL` rather
than `= NULL`.

```sql
end IS NULL                          -- end not set
tags IS NOT NULL                     -- tags present
```

## Sorting (a useful trick)

The clause is interpolated after `WHERE`, so trailing SQL is legal:

```sql
status = 'active' ORDER BY amount DESC
```

This sorts the table — and survives in the yanked command unchanged. Use it
deliberately; it rides on the interpolation rather than being a dedicated
feature.

---

## Notes

**Column names** like `_note_file` work unquoted in current DuckDB. Quote with
double quotes only when a name contains spaces or special characters (the
form does this automatically).

**Full-text search** across record *bodies* is not a SQL concern — use the
full-text input. It narrows the display only and is never part of the yanked
command; `LIKE` on a field, by contrast, is ordinary SQL and yanks fine.

**Projection** (column selection) is not supported in the WHERE field. For
ad-hoc projections, yank (`y`) and edit the `SELECT *` in the copied command.
