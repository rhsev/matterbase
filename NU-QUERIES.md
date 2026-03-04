# nushell queries in matterbase

Typed in the table query field. matterbase passes the query as:

```
open data.json | <your query> | to json
```

Column names starting with `_` require backticks (nushell syntax).

---

## Filter

```nu
where status == "active"
where status != "archive"
where amount > 1000
where end == null                        # field missing or empty
where end != null                        # field present
```

## Regex match

```nu
where name =~ "alpha"                    # contains "alpha"
where name !~ "draft"                    # does not contain
where `_note_file` =~ "2025"            # filename contains "2025"
```

## Sort

```nu
sort-by date
sort-by date -r                          # descending
sort-by name | sort-by status            # multi-column
```

## Select columns

```nu
select name status date
select name status `_note_file`
```

## Combine

```nu
where status == "active" | sort-by date -r
where type == "contract" | select name end amount | sort-by end
```

## Unique values

```nu
get status | uniq
get type | uniq | sort
```

## Count

```nu
length
where status == "active" | length
```

---

## Known limitations

**`find` does not work** in matterbase table queries. nushell's `find` adds ANSI color codes to matching cells, which breaks the `to json` serialization step. Use `where` with `=~` instead:

```nu
# instead of: find "2025"
where `_note_file` =~ "2025"
```

For full-text search across all fields, use the search input (prefix with space: `<space>word`).
