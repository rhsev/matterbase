# matterbase

A database-like TUI for querying frontmatter and YAML in Markdown notes with field filters, full-text search, and a SQL table view. grubber and matterbase are designed to keep data and context together.

## Why?

Markdown notes with frontmatter/YAML are a lightweight alternative to dedicated databases, but querying them usually means writing custom scripts or learning the syntax of a specialised tool.

matterbase puts a TUI in front of that. You pick filters by pressing buttons, refine with a search field, and the matching notes appear immediately. When you have what you want, press `y` to copy the underlying grubber command — ready to pipe into other tools.

A typical workflow: filter your notes by `type=contact`, then add a SQL WHERE clause like `birthday IS NOT NULL` to narrow further. The yanked command pipes grubber through DuckDB, so appending `| jq -r '.[] | .name + " " + .birthday'` gives you a clean list.

Built with [Textual](https://github.com/Textualize/textual). Uses [grubber](https://github.com/rhsev/grubber) for frontmatter-based filtering, [apex](https://github.com/ttscoff/apex) for preview rendering, [bat](https://github.com/sharkdp/bat) for code/text syntax highlighting, and [DuckDB](https://duckdb.org) for table queries. For macOS and Linux.

![Compact preview mode](screenshot-compact-preview.png)
*Note list with compact preview (`m`) showing only frontmatter and YAML blocks.*

![Table view](screenshot-table-view.png)
*Note list with metadata table (`t`). All data fields from frontmatter and YAML at a glance.*

## Requirements

- Python 3.10+
- [grubber](https://github.com/rhsev/grubber) **≥ 0.10.1** — install it and make sure it's on `PATH` (override the location with `$GRUBBER`). matterbase checks the version at startup. It scans the notes dir once per refresh into an NDJSON cache and re-filters by replaying it through `grubber --from-ndjson`; 0.10.1 is the first release where that source-only replay does not also scan the working directory.
- [apex](https://github.com/ttscoff/apex) (optional, for preview rendering)
- [bat](https://github.com/sharkdp/bat) (optional, for syntax-highlighted preview of code and text files)
- [bookmarker](https://github.com/rhsev/bookmarker) (optional, for resolving `type: ref` records to their target files)

DuckDB ships as a Python dependency and is installed automatically.

## Installation

```
pipx install matterbase
```

Updates:

```
pipx upgrade matterbase
```

## Getting started

Run the setup wizard once to create a config file:

```
matterbase --init
```

It asks for your notes directory, editor, optional apex theme, and filter buttons, then writes a ready-to-use `config.yml`.

## Usage

```
matterbase --config path/to/config.yml
```

## Config

```yaml
notes_dir: ~/Notes/Work        # directory of .md files
editor: hx                     # editor command
apex_theme: ralf               # apex theme name (optional)
apex_width: 80                 # apex --width value (optional)
apex_code_highlight: pygments  # apex --code-highlight tool: pygments or skylighting (optional)
compact_tasks_heading: Tasks   # h2 heading shown in compact preview (default: Tasks)
multi_select: true             # multiple active filters → AND-intersect
grubber_search_mode: all       # all | frontmatter | blocks_only  (default: all)
array_fields: [tags, keywords] # fields grubber normalises to arrays
table_columns: [status, project, type]   # columns shown in table view (omit = all)
table_query: "status != 'archive'"       # default SQL WHERE clause for table (optional)

filters:
  - label: "active"
    query:
      - "status=active"

  - label: "business"
    query:
      - "project=business"
      - "status=active"       # AND within one button

  - label: "Q1-2025"
    query:
      - "start^2025-01"
```

### Filter operators (grubber syntax)

| Operator | Meaning      | Example          |
|----------|--------------|------------------|
| `=`      | equals       | `status=active`  |
| `~`      | contains     | `name~hosting`   |
| `^`      | starts with  | `end^2025`       |
| `!`      | not equals   | `status!archive` |

Multiple expressions within one button are ANDed together. With `multi_select: true`, activating multiple buttons ANDs the result sets.

## Keybindings

| Key              | Action                                                |
|------------------|-------------------------------------------------------|
| `Tab`            | Cycle focus: buttons → search → list → table → query  |
| `↑` / `↓`       | Navigate list or table                                 |
| `Enter`          | Open selected note in editor (or run query in table)  |
| `Space`          | Toggle focused filter button                          |
| `/`              | Jump to search field                                  |
| `p`              | Toggle preview pane                                   |
| `t`              | Toggle metadata table                                 |
| `a`              | Toggle all files / single file in table               |
| `m`              | Toggle compact preview (frontmatter + YAML blocks + Tasks) |
| `f`              | Toggle file preview for `type: ref` records           |
| `o`              | Open referenced file for `type: ref` records          |
| `r`              | Refresh file list from disk                           |
| `y`              | Copy current grubber command to clipboard             |
| `Y`              | Copy grubber command to clipboard and quit            |
| `Escape` / `q`   | Quit                                                  |

## Search

Type in the search field to filter by filename. Prefix with a space to search full text (filename + content).

```
meeting          →  filename contains "meeting"
 budget          →  filename or content contains "budget"
```

Fulltext search behaviour:

- Starts after **3 characters** (shorter terms match too broadly)
- Stops collecting results after **25 matches**. Refine the term if you see "25+ matches"

## Metadata table (`t`)

Press `t` to switch the middle pane on and show a metadata table. The table shows frontmatter and YAML-block fields for all currently visible notes. Each row is one record.

A SQL query field appears above the table. Type any DuckDB WHERE clause (without the `WHERE` keyword) and press `Enter` to filter:

```sql
status != 'archive'
amount > 1000
end IS NOT NULL
start > '2025-01-01'
type = 'ref' AND collection = 'project-alpha'
```

DuckDB runs over grubber's JSON output, so the full SQL surface is available: `IN`, `LIKE`, `BETWEEN`, `IS NULL`, comparison operators, and so on.

The file column in the table shows only the filename, but in queries the full path is available as `_note_file`.

The table cursor follows the file list selection. Press `t` again to close the table.

See [SQL-QUERIES.md](SQL-QUERIES.md) for a query reference and worked examples.

## Compact preview (`m`)

Press `m` to toggle compact preview mode. Instead of the full note, apex renders a condensed view containing:

1. **Frontmatter** — the complete YAML front matter block
2. **YAML code blocks** — every ` ```yaml ` block in the document, with its immediately preceding heading (if any)
3. **Tasks section** — the first `## Tasks` h2 section (heading configurable via `compact_tasks_heading`)

Compact mode is useful for quickly scanning note metadata and task lists without scrolling through full content.

## Yank (`y` / `Y`)

Press `y` to copy the current grubber command (with active filters and optional SQL WHERE clause) to the clipboard. `Y` does the same and then quits, printing the command to stdout — useful for piping matterbase into other tools.

The reconstructed command includes the full DuckDB pipeline when a table query is active:

```
grubber extract ~/Notes -a -f status=active | duckdb -json -c "SELECT * FROM read_json_auto('/dev/stdin') WHERE amount > 1000"
```

Clipboard support is cross-platform: `pbcopy` on macOS, `wl-copy` on Wayland, `xclip` or `xsel` on X11.

## Collections

If you use [filecollector](https://github.com/rhsev/filecollector) (not public yet) to manage file collections (cross-folder groupings of PDFs, mail exports, photos, etc.), matterbase queries them through the same filter mechanism as any other field. A filter button like `query: ["collection=project-alpha"]` narrows the table to that collection's `type: ref` records.

For the full collection workflow — adding files, syncing macOS metadata, auditing, cleanup — see filecollector's documentation. matterbase only does the *querying* side.

## Architecture

The design decisions behind this are explained in [ARCHITECTURE.md](ARCHITECTURE.md).
