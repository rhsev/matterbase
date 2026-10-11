# matterbase – Technical Architecture

## Overview

matterbase is a keyboard-driven terminal UI for constructing queries over
structured records in plain-text files.

It is built as a stack of stateless layers:

```
record sources (markdown / typst / jsonl)
      ↓
  grubber (fast extraction + record-level filtering + source merging)
      ↓
  JSON / JSONL records
      ↓
  DuckDB CLI (SQL WHERE over the extracted records)
      ↓
  full-text (display-only narrowing)
      ↓
  matterbase TUI: table, adaptive preview, reconstructed CLI command
```

matterbase keeps no index and no database. Every view is derived from the
current state of the files.

## Data Model

### Record Semantics

The atomic unit is the **record**. Every source can hold several records.

- **markdown / typst**: each YAML block produces a record combined with the
  frontmatter. A file with frontmatter only produces one record.
- **jsonl**: each line is a record. fileregister's central collection index
  lives here (`<notes_dir>/collections/*.jsonl`).

A source *contains* records and forms no hierarchy. The file a record came
from is a **field on the record** (`_note_file`), not a navigation level.
matterbase therefore has no file list. JSONL is a source type like any other,
and its records sit in the same table with the source as a column.

### Schema-on-Read

Notes follow no fixed schema. The structure is inferred at query time from the
fields present. Notes with different field sets are queried together in one
table. A missing field is an empty column, and no record is rejected.

The text files stay the only copy of the data.

### Collection merging

When a fileregister collection index is present, the same logical record
exists in two layers, an index line (capture) and, once promoted, a Markdown
annotation (curation). The index keeps one line per file with `binder` as an
array. grubber's `--explode binder` splits it into one row per binder, and
`--merge-on id,binder` then merges each row with its annotation at
extraction time. The annotation's fields win, and fields only the index has
are filled in. The merge happens in grubber, so the yanked command returns the
same records as the table.

## System Layers

### grubber (Extraction Engine)

**Language:** Go
**Role:** Extraction, record-level filtering, source merging

Responsibilities:

- Scan directory tree, extract frontmatter and YAML blocks
- Read and union JSONL sources (`--from-jsonl`), split array fields into rows
  (`--explode`), merge layers (`--merge-on`)
- Apply `-f` filter expressions at record level
- Emit JSON or JSONL

grubber scans about 10,000 files in well under one second (Go, multi-threaded).
It keeps no persistent index and scans everything on each call. On a local SSD
that costs less than maintaining an incremental index would.

matterbase calls grubber without `--no-config`, unlike fileregister. The yanked
command runs in the user's shell, where grubber reads `config.yaml` and the
`GRUBBER_*` variables. If matterbase ignored them, table and yank would show
different records. What matterbase sets itself (search mode, depth, array
fields) goes to grubber as flags, which outrank every config source.

### In-session JSONL cache

matterbase scans the notes dir **once per refresh** into a temporary JSONL
file and filters by replaying it through `grubber --from-jsonl <cache>`.
grubber stays the only place where `-f` filters are evaluated, and matterbase
does not reimplement its operators. The cache is rebuilt on every refresh
(`r`), so it is as fresh as a new scan, and nothing carries over between
sessions. Preset changes replay the cache instead of reading the files again.

### matterbase (Terminal UI)

**Language:** Go
**Framework:** [Bubble Tea](https://github.com/charmbracelet/bubbletea), on
the shared **basekit** foundation in this repo
**Role:** Query construction and interactive exploration

The logic lives in files without TUI code. The app model connects them.

| File                  | Responsibility                                                        |
|-----------------------|------------------------------------------------------------------------|
| `query.go`            | QueryState: presets, SQL (filename folded in), full-text; yank construction; SQL-form clause generation |
| `pipeline.go`         | The chain: cache replay → DuckDB WHERE → full-text display filter     |
| `preview.go`          | Type-adaptive rendering in the global mode (whole / compact / record) |
| `grubber.go`          | grubber subprocess integration, cache build, version gate             |
| `content.go`          | Frontmatter/MMD parsing, section extraction, apex/bat rendering       |
| `config.go`           | YAML config loading and in-session reload                             |
| `main.go`             | The Bubble Tea model: layout, key handling, pipeline plumbing, CLI    |

All state is held in memory and derived from the files, the built query and
the config. The built query has three channels.

1. **Presets** are grubber `-f` expressions. Several active presets go into
   one grubber call (record-level AND), the same in display and yank.
2. **SQL WHERE** is DuckDB over the extracted records. The filename search
   is added as SQL (`_note_file LIKE …`). A small form writes clauses into the
   SQL input, and the SQL input is what the query uses.
3. **Full-text** is a *display filter*. It searches the prose and YAML block
   text of Markdown and typst sources, which `grubber | duckdb` cannot do. It
   never enters the yanked command, and JSONL records drop out while it is
   active.

### basekit (TUI Foundation)

The widgets and rules shared by the family's TUIs live in `basekit/`:

| Package       | Responsibility                                                     |
|---------------|--------------------------------------------------------------------|
| `recordtable` | Row-cursor table over schemaless records: column inference, width fitting by fill rate, session column override, cursor preservation |
| `frame`       | Three-zone layout arithmetic, focus tracking, content sanitizing   |
| `togglelist`  | Toggleable line items (presets, column picker)                     |
| `input`       | Debounced text inputs                                              |
| `cycler`      | Compact value selects (the SQL form's field/op)                    |
| `preview`     | Title + ANSI viewport pane                                         |
| `theme`       | The Nord-based palette, defined once                               |
| `exec`        | Editor/subprocess/clipboard helpers                                |

basekit enforces two rules for every app. The first is the **frame budget**.
A frame is exactly as high as the terminal and never wider. One line too many
scrolls the terminal and confuses Bubble Tea's renderer, so the content of
every pane is clamped in both dimensions. The second is **content
sanitizing**. Real notes contain characters whose width terminals and the
ANSI width calculation count differently, such as soft hyphens from web
clippings and zero-width marks. They are removed before any content reaches a
frame.

bubbles/table cannot scroll horizontally, so the table shows what fits.
`table_columns` decides when it is set. Otherwise columns are ranked by fill
rate and cut off at the terminal width. The column picker (`c`) changes the
selection for the session, and the `record` preview mode always shows the
full record.

### DuckDB (Query Layer)

After grubber has reduced the record set, the DuckDB CLI applies the SQL WHERE
clause. It runs as a subprocess, not as an embedded library.

```sh
duckdb -json -c "SELECT * FROM read_json_auto('records.json') WHERE <clause>"
```

grubber applies the structural filters first, and DuckDB then evaluates the
SQL on the smaller set. Running the CLI keeps the binary pure Go. The UI and
the yanked command run the same `duckdb` call, so the table shows what the
copied command returns.

## Preview Architecture

One **global mode**, set with `m`, applies to whichever record is selected,
according to its source type.

| Mode      | markdown                            | typst        | jsonl       |
|-----------|-------------------------------------|--------------|-------------|
| `whole`   | full file via apex                  | bat          | field form  |
| `compact` | frontmatter + the record's YAML block with its markdown section | section extraction, else field form | field form |
| `record`  | field form                          | field form   | field form  |

If a renderer is missing or fails, the next one takes over, in the order apex,
bat, raw text with dimmed frontmatter. Each renderer can be replaced.

## Architectural Principles

**Statelessness.** There is no database, no index file and no background
daemon. Every call reflects the current state of the files.

**Unix Composition.** Each component has one job. grubber extracts and
filters, DuckDB queries, matterbase handles the interaction. Each can be used
on its own.

**Transparency.** The top bar always shows the shell command behind the
current view, and `y` copies it:

```
grubber extract ~/notes -a --from-jsonl ~/notes/collections/ --explode binder --merge-on id,binder -f status=active | duckdb -json -c "SELECT * FROM read_json_auto('/dev/stdin') WHERE amount > 1000"
```

The copied command runs without matterbase. With a grubber config set it
shrinks to `grubber extract --set notes …`, and the database definition moves
into grubber's config.

Full-text is the one deliberate exception to this. It narrows the display only,
because no shell pipeline can reproduce it.

**Non-blocking UI.** grubber and DuckDB run in goroutines behind Bubble Tea
commands. A generation counter drops results that arrive after a newer query
has started.

## Known Trade-offs

**Full scan on every refresh.** There is no incremental index. Full scans are
cheap on a local SSD. Beyond about 10,000 files on slow storage, an index may
become necessary.

**YAML-only structured data.** There is no schema validation and no join
across notes. Complex relational modelling is out of scope.

**SSD assumption.** On network file systems, scans take longer and the
in-session cache matters more.

**DuckDB as external CLI.** The SQL WHERE clause and the filename search need
`duckdb` on `PATH`. The binary stays pure Go in return, and UI and yank always
run the same SQL.

## Is matterbase a Database?

No. It stores no data, keeps no index, has no transactions and defines no
query language of its own. The file system is the storage layer and plain
text the format. matterbase is a stateless query layer for structured records
in text files.
