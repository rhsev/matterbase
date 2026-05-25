# matterbase

A keyboard-driven terminal UI for browsing, filtering and previewing structured Markdown notes. Markdown files act as containers for YAML blocks — each block can represent a record, a task, a bookmark to an external file, or any structured data you like.

Built with [Textual](https://github.com/Textualize/textual). Uses [grubber](https://github.com/rhsev/grubber) for frontmatter-based filtering, [apex](https://github.com/ttscoff/apex) for Markdown preview rendering, [bat](https://github.com/sharkdp/bat) for code/text syntax highlighting, and [DuckDB](https://duckdb.org) for table queries.

## Layout

Three panels:

- **Left** — file list with search and filter buttons
- **Middle** — metadata table (`t`), toggleable
- **Right** — preview pane: full file, compact view, or YAML block context

The currently focused panel is highlighted with an orange border.

## Requirements

- Python 3.10+
- [grubber](https://github.com/rhsev/grubber) — bundled binary for macOS arm64 (v0.8.1, Go); install separately for other platforms
- [apex](https://github.com/ttscoff/apex) — Markdown preview (optional)
- [bat](https://github.com/sharkdp/bat) — syntax highlighting for code and text files (optional)
- [bookmarker](https://github.com/rhsev/bookmarker) — resolving file bookmarks (optional)

## Installation

```
pipx install git+https://github.com/rhsev/matterbase
```

Updates:

```
pipx upgrade matterbase
```

## Getting started

Run the setup wizard to create a config file:

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
notes_dir: ~/Notes              # directory of Markdown files
editor: hx                      # editor command
apex_theme: ralf                # apex theme name (optional)
apex_width: 80                  # apex --width value (optional)
apex_code_highlight: pygments   # apex --code-highlight: pygments or skylighting (optional)
multi_select: true              # multiple active filters → AND-intersect
grubber_search_mode: all        # all | frontmatter | blocks_only  (default: all)
array_fields: [tags, keywords]  # fields grubber normalises to arrays
table_columns: [status, project, type]  # columns shown in table view (omit = all)
table_query: "status != 'archive'"      # default SQL WHERE clause for table (optional)

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

Multiple expressions within one button are ANDed. With `multi_select: true`, activating multiple buttons ANDs the result sets.

## Keybindings

| Key            | Action                                                        |
|----------------|---------------------------------------------------------------|
| `↑` / `↓`     | Navigate file list or table                                   |
| `Enter`        | Open file in editor (file list) or open file (table)         |
| `Tab`          | Cycle focus between panels                                    |
| `Space`        | Toggle focused filter button                                  |
| `/`            | Jump to search field                                          |
| `t`            | Toggle metadata table — focuses table immediately; `t` again returns to file list |
| `a`            | Toggle all files / single file in table                       |
| `p`            | Toggle preview pane                                           |
| `m`            | Toggle compact preview (frontmatter + YAML blocks)            |
| `f`            | Toggle file preview for `type: ref` records                   |
| `o`            | Open bookmarked file for `type: ref` records                  |
| `r`            | Refresh file list from disk                                   |
| `y`            | Copy current grubber command to clipboard                     |
| `Y`            | Copy grubber command to clipboard and quit                    |
| `Escape` / `q` | Quit                                                          |

## Search

Type in the search field to filter by filename. Prefix with a space to search full text.

```
meeting     →  filename contains "meeting"
 budget     →  filename or content contains "budget"
```

Full-text search starts after 3 characters and stops at 25 matches.

## Metadata table (`t`)

Press `t` to open the metadata table in the middle panel — focus lands there immediately. Each row is one YAML block from a Markdown file. The right pane shows the section of the file containing that block.

A SQL query field appears above the table. Type any SQL WHERE clause and press `Enter` to filter:

```sql
status != 'archive'
amount > 1000
end IS NOT NULL
start > '2025-01-01'
```

Press `t` again to close the table and return to the file list.

## YAML block context

When navigating the table, the right pane shows the frontmatter of the file plus the Markdown section containing the selected YAML block. This keeps data and document context together without leaving the TUI.

## File preview

The right pane previews different file types automatically:

| Type              | Renderer                     |
|-------------------|------------------------------|
| `.md`             | apex (Markdown)              |
| `.pdf`            | pypdf (text extraction)      |
| `.docx`           | python-docx (text + headings)|
| code / text files | bat (syntax highlighting)    |

## Bookmarks (`type: ref`)

A YAML block with `type: ref` and an `id` or `alias` field is treated as a bookmark:

```markdown
---
title: My Collection
---

### Project Alpha brief

```yaml
type: ref
id: project-alpha-brief
```
```

- `f` — toggle between block context and the referenced file's preview
- `o` — open the referenced file in the default app
- `Enter` — open the referenced file in the default app

Bookmarks are resolved via [bookmarker](https://github.com/rhsev/bookmarker).

### bookmarker and Spotlight

When a file is added via bookmarker, the bookmark ID is written to the file's extended attributes (`com.apple.metadata:kMDItemDescription`). macOS Spotlight indexes this attribute — so you can find any bookmarked file by its ID or alias directly from Spotlight or Alfred, independently of matterbase.

### mc-add

`mc-add` is a companion script that adds a file to a collection in one step:

```
mc-add /path/to/file collection.md --title "Project Alpha Brief" --kind pdf
```

It:
1. Creates a macOS bookmark via bookmarker (and writes the xattr)
2. Appends an `### H3` heading and a `type: ref` YAML block to the collection file

```markdown
### Project Alpha Brief

​```yaml
id: abc123
title: Project Alpha Brief
type: ref
kind: pdf
​```
```

The heading is required so matterbase can display the section context in the right pane. The `title` field is duplicated in the YAML so grubber can query it.

## Terminal multiplexer support

When running inside **zellij**, opening a file from the file list launches the editor in a floating pane. When the editor closes, matterbase is immediately visible again. **tmux** is also supported (new window). Without a multiplexer, the editor opens via terminal suspend/resume.

## Compact preview (`m`)

Press `m` to toggle compact preview mode. Instead of the full note, renders:

1. **Frontmatter** — the complete YAML front matter
2. **YAML code blocks** — every ` ```yaml ` block with its preceding heading
3. **Tasks section** — configurable heading (default: `Tasks`)

## Yank (`y` / `Y`)

`y` copies the current grubber command (with active filters and SQL WHERE clause) to the clipboard. `Y` does the same and quits — useful for piping into other tools.

The reconstructed command includes the full DuckDB pipeline when a table query is active:

```
grubber extract ~/Notes -a -f status=active | duckdb -json -c "SELECT * FROM read_json_auto('/dev/stdin') WHERE amount > 1000"
```

Clipboard: `pbcopy` on macOS, `wl-copy` on Wayland, `xclip`/`xsel` on X11.

## Collections

matterbase queries collections as one filter dimension among others — it does not manage them. The data model, the management CLI tools, and the user guide for querying collections from matterbase all live in the [filecollector](https://github.com/rhsev/filecollector) repository:

- [SPEC.md](https://github.com/rhsev/filecollector/blob/main/SPEC.md) — data model and architecture
- [COLLECTIONS.md](https://github.com/rhsev/filecollector/blob/main/COLLECTIONS.md) — querying collections from matterbase
- [RATIONALE.md](https://github.com/rhsev/filecollector/blob/main/RATIONALE.md) — why this exists when macOS already has tags
