# matterbase – Technical Architecture

*Why all this? Just to keep data and context together.*

## Overview

matterbase is a keyboard-driven terminal UI for exploring structured metadata embedded in Markdown files.

The system is intentionally built as a layered, stateless architecture:

```
Markdown files
      ↓
  grubber (fast extraction + coarse filtering)
      ↓
  JSON records
      ↓
  matterbase (TUI)
      ↓
  optional nushell query
      ↓
  table view or reconstructed CLI command
```

matterbase does not maintain an index or database. Every view is derived directly from the current filesystem state.

## Data Model

### Record Semantics

Each Markdown file may yield multiple structured records:

- The frontmatter produces one record.
- Each YAML code block produces an additional record combined with the frontmatter.

This results in a 1:n relationship between file and records.

```
Note.md
├─ Frontmatter + YAML block → Record A
├─ Frontmatter + YAML block → Record B
```

Records are emitted as JSON objects containing extracted fields plus `_note_file` as the file reference.

### Schema-on-Read

The system applies a schema-on-read principle: no fixed schema is enforced when writing notes. Structure is inferred at query time from whatever fields are present in frontmatter or YAML blocks.

This is what allows heterogeneous Markdown files — with different field sets across notes — to be queried together in a single table view. Missing fields simply produce empty columns; no record is rejected.

This design enables:

- Multiple structured datasets per note
- Contextual, commented records (Markdown text around YAML blocks)
- Human-readable documentation coexisting with machine-readable data
- Zero schema migration requirements

Markdown remains the source of truth.

## System Layers

### grubber (Extraction Engine)

**Language:** Crystal
**Role:** Fast metadata extraction and coarse filtering

Responsibilities:

- Scan directory tree
- Extract frontmatter and YAML blocks
- Apply simple filter expressions
- Emit JSON

Performance characteristics:

- ~10,000 files in < 0.2 seconds (Crystal, multi-threaded)
- No persistent index
- Stateless execution

Instead of maintaining a database or incremental index, grubber performs a full scan on each invocation. Due to native compilation and efficient parsing, the scan cost remains low enough to avoid indexing complexity.

Advantages:

- No index corruption
- No synchronization logic
- No migration layer
- Deterministic output

### matterbase (Terminal UI)

**Language:** Python
**Framework:** Textual
**Role:** Interactive exploration layer

Responsibilities:

- Manage filter buttons
- Trigger grubber calls
- Maintain visible file subset
- Provide preview pane
- Provide metadata table view
- Reconstruct underlying CLI command

matterbase does not store data persistently. All state is ephemeral and derived from:

- Current filesystem
- Active filters
- Optional nushell query

Layout:

- Left panel: list of files identified by grubber as containing frontmatter or YAML blocks
- Right panel: either a Markdown preview, or a table of data records derived from those files

Since a single file can produce multiple records (one per YAML block combined with frontmatter), the record count in the table view does not necessarily match the file count.

### nushell (Query Layer)

**Role:** Fine-grained filtering and projection

After grubber reduces the candidate set, nushell is optionally applied for:

- `where` conditions
- sorting
- projection
- column selection
- complex filtering

This enforces a two-phase query strategy:

1. Cheap structural filter (grubber)
2. Expressive record-level query (nushell)

This mirrors classical query planning: reduce dataset early, apply heavier transformations later.

## Preview Architecture

The preview system renders Markdown via a terminal renderer with plugin support.

On demand, preview can be reduced to structured components only:

- frontmatter
- YAML blocks
- TaskPaper sections

This effectively provides two modes — documentation mode and data mode. The preview is therefore not merely cosmetic but aligned with the underlying record model.

## Architectural Principles

**Statelessness.** No database, no index file, no background daemon, no cached metadata store. Every invocation reflects the current file state.

**Unix Composition.** Each component has a narrow responsibility:

- grubber → extract
- matterbase → interact
- nushell → query

They can be used independently via CLI. External components such as apex (preview renderer) and nushell (query engine) are interchangeable — any tool that accepts the same input format can be substituted without modifying matterbase.

**Transparency.** matterbase can reconstruct the underlying CLI command:

```
grubber extract … | nu …
```

The UI is a convenience layer, not a proprietary execution environment.

## Known Trade-offs

**Full scan on every invocation.** No incremental indexing, repeated filesystem traversal. Native performance makes full scans inexpensive and avoids index invalidation and migration logic. At significantly larger scales (beyond 10k files), indexing may become necessary.

**YAML-only structured data.** No schema validation, no relational joins across notes. Complex relational modeling is intentionally out of scope.

**External query dependency.** Depends on nushell for advanced filtering. This avoids re-implementing a query engine and keeps matterbase focused on UI. nushell has a concise, easy-to-understand syntax.

## Summary

matterbase is not a note-taking application.

It is:

- A structured metadata interface for Markdown
- A stateless exploration layer
- A fast, file-based alternative to small personal databases

The architecture intentionally favors determinism, simplicity, composability, zero lock-in, and performance via native extraction.

## Is matterbase a Database?

Short answer: No.

matterbase does not:

- Store data persistently
- Maintain an index
- Implement transactions
- Provide its own query language

The filesystem is the storage layer. Markdown is the canonical data format.

Despite not being a database, matterbase enables database-like workflows: structured records, field filtering, column projection, tabular views, and query reconstruction.

Conceptually, it is closer to:

```
ripgrep + jq + fzf
```

than to:

```
SQLite
```

The system intentionally avoids becoming a database — no index corruption, no migration layer, no hidden state, full CLI transparency.

matterbase is a stateless, database-like exploration layer for structured Markdown metadata.
