"""
matterbase - keyboard-driven terminal UI for browsing and filtering Markdown notes.

Usage: matterbase --config <config.yml>

Config file format (YAML):

  notes_dir: ~/Notes/Work       # Directory containing .md notes
  editor: hx                    # Editor to open notes with
  apex_theme: nord256           # apex theme name (optional)
  apex_code_highlight: pygments # apex --code-highlight tool: pygments (p) or skylighting (s); omit to disable
  apex_code_highlight_theme: nord  # apex --code-highlight-theme (optional, tool-specific)
  compact_tasks_heading: Tasks  # h2 heading to include in compact preview (default: Tasks)
  grubber_search_mode: all      # "all" (default), "frontmatter", or "blocks_only"
  grubber_mmd: false            # pass --mmd to grubber (MultiMarkdown metadata headers)
  array_fields: [tags, keywords] # passed as GRUBBER_ARRAY_FIELDS env var to grubber
  table_columns: [status, project, type]  # columns shown in table view (omit = all)
  table_query: "status != 'archive'"  # default SQL WHERE clause for table (optional)
  filters:                      # Filter buttons; each is a named grubber query
    - label: "active"
      query:
        - "status=active"
    - label: "C-Resource"
      query:
        - "project=c-resource"
        - "status=active"       # AND within one button
    - label: "Q1-2025"
      query:
        - "start^2025-01"       # starts-with operator
  multi_select: true            # multiple active buttons → AND-intersect results

Filter operators (grubber syntax):
  =   equals         status=active
  ~   contains       name~hosting
  ^   starts with    end^2025
  !   not equals     status!archive

Search:
  word        filename match
  <space>word fulltext match (filename + content)

Layout:
  Left    File list with filters and search
  Middle  Metadata table (toggle with t)
  Right   File preview (toggle with p)

Keybindings:
  Arrow keys  Navigate list
  Enter       Open selected note in editor
  Space       Toggle focused filter button
  p           Toggle preview pane (right)
  m           Toggle compact preview (frontmatter + YAML blocks + ## Tasks)
  t           Toggle metadata table (middle pane)
  Tab         Cycle focus (buttons → search → list / table-query → table)
  Escape / q  Quit
"""

from .app import MatterbaseApp, load_config, main

__all__ = ["main", "MatterbaseApp", "load_config"]
