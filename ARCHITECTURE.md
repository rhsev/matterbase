# matterbase – Technical Architecture

*Why all this? Just to keep data and context together.*

## Overview

matterbase is a keyboard-driven terminal UI for constructing queries over
structured records embedded in plain-text files.

The system is intentionally built as a layered, stateless architecture:

```
record sources (markdown / typst / jsonl)
      ↓
  grubber (fast extraction + record-level filtering + source merging)
      ↓
  JSON / JSONL records
      ↓
  DuckDB (SQL WHERE over the extracted records)
      ↓
  full-text (display-only narrowing)
      ↓
  matterbase TUI: table, adaptive preview, reconstructed CLI command
```

matterbase does not maintain an index or database. Every view is derived
directly from the current filesystem state.

## Data Model

### Record Semantics

The atomic unit is the **record**. Every source is a multi-record file:

- **markdown / typst** — each YAML block produces a record combined with the
  frontmatter; a frontmatter-only file produces one record.
- **jsonl** — each line is a record. (markbinder's central collection index
  lives here: `<notes_dir>/collections/*.jsonl`.)

A source merely *contains* records; it is not a hierarchy. The file a record
came from is a **field on the record** (`_note_file`), not a navigation level.
This is why matterbase has no file list: jsonl is just a source type whose
records sit in the table like any other, with the source as a column.

### Schema-on-Read

No fixed schema is enforced when writing notes. Structure is inferred at query
time from whatever fields are present. Heterogeneous sources — different field
sets across notes — are queried together in a single table; missing fields
simply produce empty columns, no record is rejected.

Plain text remains the source of truth.

### Collection merging

When a markbinder collection index is present, the same logical record exists
in two layers: an index line (capture) and, once promoted, a markdown
annotation (curation). grubber's `--merge-on id,binder` collapses the two at
extraction time — the annotation wins, index-only fields are back-filled. The
merge lives in grubber, not in matterbase, so the yanked command reproduces
exactly what the table shows.

## System Layers

### grubber (Extraction Engine)

**Language:** Go
**Role:** Fast extraction, record-level filtering, source merging

Responsibilities:

- Scan directory tree, extract frontmatter and YAML blocks
- Read and union JSONL sources (`--from-jsonl`), merge layers (`--merge-on`)
- Apply `-f` filter expressions at record level
- Emit JSON or JSONL

~10,000 files in well under one second (Go, multi-threaded); no persistent
index, stateless execution. Instead of maintaining incremental indexes,
grubber performs a full scan per invocation — native performance keeps the
scan cost below indexing complexity.

### In-session JSONL cache

matterbase scans the notes dir **once per refresh** into a temporary JSONL
file and re-filters by replaying it through `grubber --from-jsonl <cache>`.
grubber stays the single filter authority (its `-f` operators are not
reimplemented in Python); the cache is rebuilt on every refresh (`r`), so
freshness equals a normal re-scan and there is no cross-session staleness.
Preset changes replay the cache instead of re-reading the filesystem.

### matterbase (Terminal UI)

**Language:** Python
**Framework:** Textual
**Role:** Query construction and interactive exploration

Module structure — the logic lives in Textual-free modules, the app is wiring:

| Module              | Responsibility                                                        |
|---------------------|------------------------------------------------------------------------|
| `query.py`          | QueryState: presets, SQL (filename folded in), full-text; yank construction; SQL-form clause generation |
| `pipeline.py`       | The chain: cache replay → DuckDB WHERE → full-text display filter     |
| `preview.py`        | Type-adaptive rendering in the global mode (whole / compact / record) |
| `grubber_client.py` | grubber subprocess integration, cache build, version gate             |
| `content.py`        | Frontmatter/MMD parsing, section extraction, apex/bat rendering       |
| `widgets.py`        | PresetItem, RecordTable                                               |
| `app.py`            | Layout, event handlers, config loading/reloading, CLI entry point     |

All state is ephemeral and derived from the current filesystem, the built
query, and the config. The built query has three channels:

1. **Presets** — grubber `-f` expressions; multiple active presets flatten
   into one grubber call (record-level AND), identical in display and yank.
2. **SQL WHERE** — DuckDB over the extracted records. The filename search is
   folded in as SQL (`_note_file LIKE …`); a small form generates clauses
   into the SQL input, which stays the single source of truth.
3. **Full-text** — a *display filter* with special status: it searches
   prose/YAML-block text of markdown/typst sources, which `grubber | duckdb`
   cannot express. It never enters the yanked command; jsonl records drop out
   while it is active.

### DuckDB (Query Layer)

After grubber reduces the record set, the SQL WHERE clause is applied via
DuckDB (embedded Python dependency, no daemon):

```sql
SELECT * FROM read_json_auto('records.json') WHERE <clause>
```

This is classical two-phase query planning: cheap structural filter first
(grubber), expressive record-level query second (DuckDB).

## Preview Architecture

One **global mode** — set once with `m`, applied to whichever record is
selected, according to its source type:

| Mode      | markdown                            | typst        | jsonl       |
|-----------|-------------------------------------|--------------|-------------|
| `whole`   | full file via apex                  | bat          | field form  |
| `compact` | frontmatter + the record's YAML block with its markdown section | section extraction, else field form | field form |
| `record`  | field form                          | field form   | field form  |

The renderer chain degrades gracefully: apex → bat → raw text with dimmed
frontmatter. External renderers are interchangeable.

## Architectural Principles

**Statelessness.** No database, no index file, no background daemon. Every
invocation reflects the current file state.

**Unix Composition.** Each component has one job: grubber extracts and
filters, DuckDB queries, matterbase interacts. Each is usable independently.

**Transparency.** The top bar always shows the shell command the current view
corresponds to; `y` copies it:

```
grubber extract ~/notes -a --from-jsonl ~/notes/collections/ --merge-on id,binder -f status=active | duckdb -json -c "SELECT * FROM read_json_auto('/dev/stdin') WHERE amount > 1000"
```

The UI is a convenience layer, not a proprietary execution environment. With
a grubber config set, the same command shrinks to
`grubber extract --set notes …` — the database definition moves into config.

The one deliberate exception to yank-fidelity is full-text: it narrows the
display only, because no shell pipeline can reproduce it.

**Non-blocking UI.** grubber and DuckDB run in background worker threads; the
Textual event loop is never blocked by filesystem I/O.

## Known Trade-offs

**Full scan on every refresh.** No incremental indexing. Native Go performance
makes full scans inexpensive on local SSD; beyond ~10k files on slow storage,
indexing may become necessary.

**YAML-only structured data.** No schema validation, no relational joins
across notes. Complex relational modeling is intentionally out of scope.

**SSD assumption.** On network filesystems, scan times grow and the in-session
cache replay becomes the more important optimisation.

## Is matterbase a Database?

No. It does not store data, maintain an index, implement transactions, or
define its own query language. The filesystem is the storage layer; plain
text is the canonical format. Conceptually it is closer to `ripgrep + jq +
fzf` than to SQLite — a stateless, database-like query layer for structured
records in text files.
