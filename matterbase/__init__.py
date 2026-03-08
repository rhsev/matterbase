#!/usr/bin/env python3
"""
matterbase – keyboard-driven terminal UI for browsing and filtering Markdown notes.

Usage: matterbase --config <config.yml>

Config file format (YAML):

  notes_dir: ~/Notes/Work       # Directory containing .md notes
  editor: hx                    # Editor to open notes with
  apex_theme: ralf              # apex theme name (optional)
  apex_code_highlight: pygments # apex --code-highlight tool: pygments (p) or skylighting (s); omit to disable
  apex_code_highlight_theme: nord  # apex --code-highlight-theme (optional, tool-specific)
  compact_tasks_heading: Tasks  # h2 heading to include in compact preview (default: Tasks)
  grubber_search_mode: all      # "all" (default), "frontmatter", or "blocks_only"
  grubber_mmd: false            # pass --mmd to grubber (MultiMarkdown metadata headers)
  array_fields: [tags, keywords] # passed as GRUBBER_ARRAY_FIELDS env var to grubber
  table_columns: [status, project, type]  # columns shown in table view (omit = all)
  table_query: "where status != 'archive'"  # default nushell query for table (optional)
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

Keybindings:
  Arrow keys  Navigate list
  Enter       Open selected note in editor
  Space       Toggle focused filter button
  p           Toggle preview pane
  m           Toggle compact preview (frontmatter + YAML blocks + ## Tasks)
  t           Toggle metadata table (right pane)
  Tab         Cycle focus (buttons → search → list / table-query → table)
  Escape / q  Quit
"""

import argparse
import json
import os
import platform
import re
import shlex
import shutil
import subprocess
import sys
import tempfile
from pathlib import Path

# ---------------------------------------------------------------------------
# Bundled grubber binary / config resolution
# ---------------------------------------------------------------------------

def _grubber_binary() -> str:
    """Return path to bundled grubber binary, falling back to PATH."""
    script_dir = Path(__file__).resolve().parent
    arch = platform.machine().lower()   # "arm64" or "x86_64"
    candidate = script_dir / "vendor" / f"grubber-macos-{arch}"
    if candidate.exists():
        return str(candidate)
    return "grubber"  # fall back to whatever is on PATH


GRUBBER_BIN = _grubber_binary()

import yaml
from rich.text import Text
from textual.app import App, ComposeResult
from textual.binding import Binding
from textual.containers import Horizontal, Vertical, ScrollableContainer
from textual.widgets import Button, DataTable, Footer, Input, Label, ListItem, ListView, Static

# ---------------------------------------------------------------------------
# Config
# ---------------------------------------------------------------------------

def load_config(config_path: str) -> dict:
    """Load and validate the YAML config file."""
    path = Path(config_path).expanduser()
    if not path.exists():
        print(f"Error: Config file not found: {config_path}", file=sys.stderr)
        print("Run `matterbase --help` for config format.", file=sys.stderr)
        sys.exit(1)

    with open(path) as f:
        config = yaml.safe_load(f)

    if "notes_dir" not in config:
        print("Error: Config missing required key: notes_dir", file=sys.stderr)
        sys.exit(1)

    notes_dir = Path(config["notes_dir"]).expanduser()
    if not notes_dir.is_dir():
        print(f"Error: notes_dir does not exist or is not a directory: {notes_dir}", file=sys.stderr)
        sys.exit(1)
    config["notes_dir"] = str(notes_dir)
    return config


# ---------------------------------------------------------------------------
# MMD header parsing
# ---------------------------------------------------------------------------

_MMD_KEY_RE = re.compile(r'^[\w. -]+\s*:')

def _split_mmd_header(content: str) -> tuple[str, str]:
    """If content starts with MMD key: value pairs, return (yaml_lines, body).

    MMD headers are key: value lines at the very start of the file,
    terminated by the first blank line. Returns ("", content) if none found.
    """
    lines = content.split("\n")
    mmd_lines: list[str] = []
    for line in lines:
        if not line.strip():
            break
        if _MMD_KEY_RE.match(line):
            mmd_lines.append(line)
        else:
            break
    if not mmd_lines:
        return "", content
    rest_start = len(mmd_lines)
    if rest_start < len(lines) and not lines[rest_start].strip():
        rest_start += 1
    return "\n".join(mmd_lines), "\n".join(lines[rest_start:])


# ---------------------------------------------------------------------------
# grubber integration
# ---------------------------------------------------------------------------

def _run_grubber_cmd(cmd: list[str], array_fields: list[str] | None = None) -> list[dict]:
    """Run a grubber command and return parsed JSON records."""
    env = os.environ.copy()
    if array_fields:
        env["GRUBBER_ARRAY_FIELDS"] = ",".join(array_fields)
    try:
        result = subprocess.run(cmd, capture_output=True, text=True, timeout=15, env=env)
        if result.returncode != 0:
            return []
        return json.loads(result.stdout)
    except subprocess.TimeoutExpired:
        return []
    except (json.JSONDecodeError, FileNotFoundError, OSError):
        return []


def run_grubber(
    notes_dir: str,
    expressions: list[str] | None = None,
    search_mode: str = "all",
    array_fields: list[str] | None = None,
    mmd: bool = False,
    depth: int | None = None,
) -> list[str]:
    """Run grubber and return deduplicated matching file paths."""
    def base_cmd(flag: str) -> list[str]:
        cmd = [GRUBBER_BIN, "extract", notes_dir, flag]
        if depth is not None:
            cmd += ["--depth", str(depth)]
        if mmd:
            cmd.append("--mmd")
        if expressions:
            for expr in expressions:
                cmd.extend(["-f", expr])
        return cmd

    af = array_fields or []

    if search_mode == "frontmatter":
        records = _run_grubber_cmd(base_cmd("--frontmatter-only"), af)
    elif search_mode == "blocks_only":
        records = _run_grubber_cmd(base_cmd("--blocks-only"), af)
    else:
        records = _run_grubber_cmd(base_cmd("--all"), af)

    seen: set[str] = set()
    paths: list[str] = []
    for record in records:
        file_path = record.get("_note_file", "")
        if file_path and file_path not in seen:
            seen.add(file_path)
            paths.append(file_path)
    return paths


def query_files(
    notes_dir: str,
    active_queries: list[list[str]],
    multi_select: bool,
    search_mode: str = "all",
    array_fields: list[str] | None = None,
    mmd: bool = False,
    depth: int | None = None,
) -> list[str]:
    """Return file list for active filter buttons; multiple buttons AND-intersect."""
    kw = dict(search_mode=search_mode, array_fields=array_fields, mmd=mmd, depth=depth)

    if not active_queries:
        return run_grubber(notes_dir, **kw)

    if multi_select and len(active_queries) > 1:
        # Each button runs independently; intersect the result sets
        sets: list[set[str]] = [set(run_grubber(notes_dir, q, **kw)) for q in active_queries]
        combined = sets[0]
        for s in sets[1:]:
            combined &= s
        return sorted(combined)

    # Single button (or multi_select=False): flatten all expressions into one call
    all_exprs = [expr for q in active_queries for expr in q]
    return run_grubber(notes_dir, all_exprs, **kw)


# ---------------------------------------------------------------------------
# Widgets
# ---------------------------------------------------------------------------

class FilterButton(Button):
    """A toggleable button representing a named grubber query."""

    def __init__(self, label: str, query: list[str], **kwargs) -> None:
        super().__init__(label, **kwargs)
        self.query = query  # list of grubber filter expressions for this button
        self.is_active = False

    def toggle(self) -> None:
        self.is_active = not self.is_active
        if self.is_active:
            self.add_class("filter-on")
        else:
            self.remove_class("filter-on")


class NoteItem(ListItem):
    """A ListItem that carries the full file path of the note."""

    def __init__(self, filename: str, full_path: str) -> None:
        super().__init__(Label(filename))
        self.full_path = full_path


class NoteListView(ListView):
    """ListView that highlights the first item automatically on focus."""

    def on_focus(self) -> None:
        if self.highlighted_child is None and len(self) > 0:
            self.index = 0


class MetaDataTable(DataTable):
    """Non-focusable DataTable for the metadata view.

    The table cursor follows the left-pane list selection, so Tab should
    never land here.
    """

    can_focus = False


# ---------------------------------------------------------------------------
# App
# ---------------------------------------------------------------------------

class MatterbaseApp(App):
    """Terminal note picker – browse, filter, and open Markdown notes."""

    CSS = """
    /* ── Layout ───────────────────────────────────────────────────── */
    Screen {
        layout: vertical;
    }

    #main {
        layout: horizontal;
        height: 1fr;
    }

    #left {
        width: 35%;
        height: 100%;
        border-right: solid $primary-darken-2;
        padding: 0 1;
        background: #2E3440;
    }

    #right {
        width: 65%;
        height: 100%;
        padding: 0 1;
    }

    /* ── Left pane ─────────────────────────────────────────────────── */
    #filter-row {
        layout: grid;
        grid-size: 3;
        height: auto;
        margin-bottom: 1;
    }

    FilterButton {
        background: $surface;
        border: none;
        width: 1fr;
    }

    FilterButton:hover {
        background: $primary-darken-1;
    }

    FilterButton.filter-on {
        background: $accent;
        color: $text;
        text-style: bold;
    }

    #search {
        height: 3;
        border: solid #D8DEE9;
        background: #2E3440;
    }

    #search:focus {
        border: solid $accent;
    }

    #file-list {
        height: 1fr;
        border: solid #D8DEE9;
        background: #2E3440;
    }

    #file-list:focus {
        border: solid $accent;
    }

    /* ── Right pane ────────────────────────────────────────────────── */
    #right.hidden {
        display: none;
    }

    #left.wide {
        width: 100%;
        border-right: none;
    }

    #preview-title {
        height: auto;
        color: $accent;
        text-style: bold;
        margin-bottom: 1;
    }

    #preview-scroll {
        height: 1fr;
        border: solid #D8DEE9;
        background: #2E3440;
    }

    #preview-scroll:focus {
        border: solid $accent;
    }

    #preview-scroll.hidden {
        display: none;
    }

    #preview {
        padding: 0 1;
    }

    #table-query {
        display: none;
        height: 3;
        border: solid #D8DEE9;
        background: #2E3440;
    }

    #table-query:focus {
        border: solid $accent;
    }

    #table-query.visible {
        display: block;
    }

    #meta-table {
        display: none;
        height: 1fr;
        border: solid #D8DEE9;
        background: #2E3440;
    }

    #meta-table.visible {
        display: block;
    }

    #meta-table > .datatable--cursor {
        background: transparent;
    }

    #meta-table > .datatable--fixed-cursor {
        background: transparent;
    }

    /* ── Footer ────────────────────────────────────────────────────── */
    #status {
        height: 1;
        background: $surface;
        color: $text-muted;
        padding: 0 1;
    }
    """

    BINDINGS = [
        Binding("q", "quit", "Quit"),
        Binding("escape", "quit", "Quit", show=False),
        Binding("space", "toggle_filter", "Toggle filter", show=False),
        Binding("p", "toggle_preview", "Preview"),
        Binding("t", "toggle_table", "Table"),
        Binding("ctrl+r", "run_table_query", "Run query", show=False, priority=True),
        Binding("y", "yank", "Yank cmd", show=False),
        Binding("Y", "yank_quit", "Yank cmd + quit", show=False),
        Binding("m", "toggle_compact", "Compact", show=False),
    ]

    FULLTEXT_MIN_CHARS = 3    # fewer chars match too broadly
    FULLTEXT_MAX_RESULTS = 25  # early exit; prompt user to refine

    def __init__(self, config: dict) -> None:
        super().__init__()
        self.config = config
        self.notes_dir: str = config["notes_dir"]
        self.editor: str = config.get("editor", "hx")
        self.filter_defs: list[dict] = config.get("filters", [])
        self.multi_select: bool = bool(config.get("multi_select", False))

        self._all_files: list[str] = []
        self._current_visible: list[str] = []
        self._active_queries: list[list[str]] = []  # one entry per active button
        self._search_term: str = ""
        self._preview_visible: bool = True
        self._table_mode: bool = False
        self._table_file_to_row_idx: dict[str, int] = {}
        self._apex_theme: str = config.get("apex_theme", "")
        self._apex_width: int = config.get("apex_width", 0)
        self._apex_code_highlight: str = config.get("apex_code_highlight", "")
        self._apex_code_highlight_theme: str = config.get("apex_code_highlight_theme", "")
        self._grubber_search_mode: str = config.get("grubber_search_mode", "all")
        self._grubber_mmd: bool = bool(config.get("grubber_mmd", False))
        self._grubber_array_fields: list[str] = config.get("array_fields", [])
        self._grubber_depth: int | None = config.get("depth")
        self._table_columns: list[str] = config.get("table_columns", [])
        self._table_default_query: str = config.get("table_query", "")
        self._table_raw_data: list[dict] = []   # grubber records before nu filtering
        self._table_nu_query: str = ""          # current live query in the input field
        self._fulltext_timer = None             # debounce handle for fulltext search
        self._compact_preview: bool = False     # show only frontmatter + YAML blocks + Tasks
        self._compact_tasks_heading: str = config.get("compact_tasks_heading", "Tasks")

    # ── Compose ──────────────────────────────────────────────────────

    def compose(self) -> ComposeResult:
        with Horizontal(id="main"):
            # Left pane
            with Vertical(id="left"):
                with Vertical(id="filter-row"):
                    for fd in self.filter_defs:
                        safe_id = "fb-" + re.sub(r"[^a-z0-9_-]", "-", fd["label"].lower())
                        query = fd.get("query", [])
                        yield FilterButton(fd["label"], query, id=safe_id)

                yield Input(placeholder="Search  (space + term = fulltext)", id="search")
                yield NoteListView(id="file-list")

            # Right pane
            with Vertical(id="right"):
                yield Static("", id="preview-title")
                with ScrollableContainer(id="preview-scroll", can_focus=False):
                    yield Static("", id="preview", expand=True)
                yield Input(placeholder="nu query  (ctrl+r to run)", id="table-query")
                yield MetaDataTable(id="meta-table", cursor_type="row", show_cursor=True)

        yield Static("", id="status")

    # ── Lifecycle ────────────────────────────────────────────────────

    async def on_mount(self) -> None:
        await self._refresh_files()

    # ── Grubber / file refresh ────────────────────────────────────────

    async def _refresh_files(self) -> None:
        """Re-run grubber and rebuild the list."""
        self._all_files = query_files(
            self.notes_dir,
            self._active_queries,
            self.multi_select,
            search_mode=self._grubber_search_mode,
            array_fields=self._grubber_array_fields,
            mmd=self._grubber_mmd,
            depth=self._grubber_depth,
        )
        self._apply_search()

    def _apply_search(self) -> None:
        """Filter _all_files by search term.

        Leading space triggers full-text search (filename + content) in a
        background thread so the UI stays responsive.
        No leading space: filename-only match (synchronous, fast).
        """
        raw = self._search_term
        if not raw:
            self._rebuild_list(self._all_files)
            return

        fulltext = raw.startswith(" ")
        term = raw.lstrip(" ").lower()
        if not term:
            self._rebuild_list(self._all_files)
            return

        if fulltext:
            if len(term) < self.FULLTEXT_MIN_CHARS:
                self._rebuild_list(self._all_files)
                self.query_one("#status", Static).update(
                    f" Volltext: mindestens {self.FULLTEXT_MIN_CHARS} Zeichen  │  {self.notes_dir}"
                )
                return

            files = list(self._all_files)
            max_results = self.FULLTEXT_MAX_RESULTS
            notes_dir = self.notes_dir

            def _do_search() -> None:
                visible = []
                truncated = False
                for path in files:
                    if term in os.path.basename(path).lower():
                        visible.append(path)
                    else:
                        try:
                            content = Path(path).read_text(encoding="utf-8", errors="replace").lower()
                            if term in content:
                                visible.append(path)
                        except OSError:
                            pass
                    if len(visible) >= max_results:
                        truncated = True
                        break
                self.call_from_thread(self._rebuild_list, visible)
                if truncated:
                    self.call_from_thread(
                        lambda: self.query_one("#status", Static).update(
                            f" {max_results}+ matches – refine search term  │  {notes_dir}"
                        )
                    )

            self.run_worker(_do_search, exclusive=True, thread=True, group="search")
        else:
            visible = [p for p in self._all_files if term in os.path.basename(p).lower()]
            self._rebuild_list(visible)

    def _rebuild_list(self, paths: list[str]) -> None:
        """Repopulate the ListView and update the status bar."""
        self._current_visible = paths
        lv = self.query_one("#file-list", NoteListView)
        lv.clear()

        for p in paths:
            lv.append(NoteItem(os.path.basename(p), p))

        # Status bar
        total = len(self._all_files)
        shown = len(paths)
        note_word = "note" if total == 1 else "notes"
        if self._active_queries or self._search_term:
            msg = f" {shown} of {total} {note_word}  │  {self.notes_dir}"
        else:
            msg = f" {total} {note_word}  │  {self.notes_dir}"
        self.query_one("#status", Static).update(msg)

        # Clear preview if nothing to show
        if not paths:
            self.query_one("#preview-title", Static).update("")
            self.query_one("#preview", Static).update("[dim]No notes found.[/dim]")

        # Keep table in sync with the current file list
        if self._table_mode:
            self._populate_table()

    # ── Event handlers ────────────────────────────────────────────────

    async def on_button_pressed(self, event: Button.Pressed) -> None:
        if not isinstance(event.button, FilterButton):
            return
        btn = event.button
        btn.toggle()

        if btn.is_active:
            if btn.query not in self._active_queries:
                self._active_queries.append(btn.query)
        else:
            self._active_queries = [q for q in self._active_queries if q is not btn.query]

        await self._refresh_files()

    async def on_input_changed(self, event: Input.Changed) -> None:
        if event.input.id == "search":
            self._search_term = event.value
            fulltext = event.value.startswith(" ")
            if self._fulltext_timer is not None:
                self._fulltext_timer.stop()
            delay = 0.2 if fulltext else 0.15
            self._fulltext_timer = self.set_timer(delay, self._apply_search)

    async def on_input_submitted(self, event: Input.Submitted) -> None:
        if event.input.id == "search":
            self.query_one("#file-list", NoteListView).focus()
        elif event.input.id == "table-query":
            self._table_nu_query = event.value
            self._apply_table_query()

    def on_list_view_highlighted(self, event: ListView.Highlighted) -> None:
        if not isinstance(event.item, NoteItem):
            return
        if self._table_mode:
            self._highlight_table_row(event.item.full_path)
        else:
            self._show_preview(event.item.full_path)

    # ── Preview ───────────────────────────────────────────────────────

    def _extract_compact_content(self, path: str) -> str:
        """Extract frontmatter + YAML code blocks (with preceding heading) + ## Tasks section."""
        try:
            content = Path(path).read_text(encoding="utf-8", errors="replace")
        except OSError:
            return ""

        parts: list[str] = []
        body = content

        # 1. Frontmatter (YAML) or MMD metadata headers
        if content.startswith("---"):
            sections = content.split("---", 2)
            if len(sections) >= 3:
                parts.append(f"---{sections[1]}---")
                body = sections[2]
        elif self._grubber_mmd:
            mmd_yaml, mmd_body = _split_mmd_header(content)
            if mmd_yaml:
                parts.append(f"---\n{mmd_yaml}\n---")
                body = mmd_body

        lines = body.splitlines()

        # 2. YAML code blocks, keeping the immediately preceding heading (if any)
        #    A non-blank, non-heading line between heading and fence resets the heading.
        i = 0
        pending_heading: str | None = None
        while i < len(lines):
            line = lines[i]
            if re.match(r"^#{1,6}\s", line):
                pending_heading = line
                i += 1
                continue
            if re.match(r"^```ya?ml\s*$", line):
                k = i + 1
                while k < len(lines) and not re.match(r"^```\s*$", lines[k]):
                    k += 1
                block: list[str] = []
                if pending_heading:
                    block.append(pending_heading)
                block.extend(lines[i : k + 1])
                parts.append("\n".join(block))
                pending_heading = None
                i = k + 1
                continue
            if line.strip():
                pending_heading = None
            i += 1

        # 3. ## Tasks section (up to next h2)
        tasks_lines: list[str] = []
        in_tasks = False
        for line in lines:
            if re.match(r"^## " + re.escape(self._compact_tasks_heading) + r"\b", line):
                in_tasks = True
                tasks_lines = [line]
            elif in_tasks:
                if re.match(r"^## ", line):
                    break
                tasks_lines.append(line)
        if tasks_lines:
            parts.append("\n".join(tasks_lines).rstrip())

        return "\n\n".join(parts)

    def _show_preview(self, path: str) -> None:
        if not self._preview_visible:
            return

        title = self.query_one("#preview-title", Static)
        preview = self.query_one("#preview", Static)
        title.update(os.path.basename(path))

        # Prepare tempfile for apex: compact mode, or MMD files (converted to YAML frontmatter)
        tmp_path: str | None = None
        if self._compact_preview:
            compact = self._extract_compact_content(path)
            if compact:
                try:
                    with tempfile.NamedTemporaryFile(
                        mode="w", suffix=".md", delete=False, encoding="utf-8"
                    ) as f:
                        f.write(compact)
                        tmp_path = f.name
                except OSError:
                    tmp_path = None
        render_path = tmp_path if tmp_path else path
        cmd = ["apex", render_path, "--plugins", "-t", "terminal256"]
        if self._apex_code_highlight:
            cmd += ["--code-highlight", self._apex_code_highlight]
        if self._apex_code_highlight_theme:
            cmd += ["--code-highlight-theme", self._apex_code_highlight_theme]
        if self._apex_theme:
            cmd += ["--theme", self._apex_theme]
        if self._apex_width:
            cmd += ["--width", str(self._apex_width)]

        try:
            env = os.environ.copy()
            env.setdefault("TERM", "xterm-256color")
            env.setdefault("COLORTERM", "truecolor")
            result = subprocess.run(cmd, capture_output=True, timeout=10, env=env)
            ansi_output = result.stdout.decode("utf-8", errors="replace") if result.returncode == 0 else ""
        except (FileNotFoundError, subprocess.TimeoutExpired):
            ansi_output = ""
        finally:
            if tmp_path:
                try:
                    os.unlink(tmp_path)
                except OSError:
                    pass

        if ansi_output:
            preview.update(Text.from_ansi(ansi_output))
        else:
            # Fallback: raw content with dimmed frontmatter
            try:
                content = Path(path).read_text(encoding="utf-8", errors="replace")
            except OSError as exc:
                preview.update(f"[red]Error reading file:[/red] {exc}")
                return
            if content.startswith("---"):
                parts = content.split("---", 2)
                if len(parts) >= 3:
                    fm = parts[1].rstrip("\n")
                    body = parts[2].lstrip("\n")
                    preview.update(f"[dim]---\n{fm}\n---[/dim]\n\n{body}")
                    return
            if self._grubber_mmd:
                mmd_yaml, mmd_body = _split_mmd_header(content)
                if mmd_yaml:
                    preview.update(f"[dim]---\n{mmd_yaml}\n---[/dim]\n\n{mmd_body}")
                    return
            preview.update(content)

    # ── Actions ───────────────────────────────────────────────────────

    def on_list_view_selected(self, event: ListView.Selected) -> None:
        """Open note when Enter is pressed on the ListView."""
        if not isinstance(event.item, NoteItem):
            return
        with self.suspend():
            subprocess.run([self.editor, event.item.full_path])

    async def action_toggle_filter(self) -> None:
        focused = self.focused
        if isinstance(focused, FilterButton):
            focused.press()

    async def action_quit(self) -> None:
        self.exit()

    def _yank_command_display(self) -> str:
        """Short display version: only active filters and nu query."""
        parts = []
        for query_list in self._active_queries:
            for expr in query_list:
                parts += ["-f", shlex.quote(expr)]
        if self._table_nu_query.strip():
            nu_query = self._table_nu_query.strip().replace("'", '"')
            parts += ["|", "nu -c", f"'from json | {nu_query}'"]
        return " ".join(parts) if parts else "(no filters)"

    def _copy_to_clipboard(self, text: str) -> None:
        """Copy text to the system clipboard (macOS, Wayland, X11)."""
        system = platform.system()
        if system == "Darwin":
            candidates = [["pbcopy"]]
        elif os.environ.get("WAYLAND_DISPLAY"):
            candidates = [["wl-copy"]]
        else:
            candidates = [["xclip", "-selection", "clipboard"], ["xsel", "--clipboard", "--input"]]
        for cmd in candidates:
            if shutil.which(cmd[0]):
                subprocess.run(cmd, input=text, text=True)
                return

    def _build_yank_command(self) -> str:
        """Reconstruct the current grubber [| nu] command from app state."""
        parts = [GRUBBER_BIN, "extract", self.notes_dir]
        if self._grubber_search_mode == "frontmatter":
            parts.append("--frontmatter-only")
        elif self._grubber_search_mode == "blocks_only":
            parts.append("--blocks-only")
        else:
            parts.append("--all")
        if self._grubber_depth is not None:
            parts += ["--depth", str(self._grubber_depth)]
        if self._grubber_mmd:
            parts.append("--mmd")
        for query_list in self._active_queries:
            for expr in query_list:
                parts += ["-f", shlex.quote(expr)]
        cmd = " ".join(parts)
        if self._grubber_array_fields:
            cmd = f"GRUBBER_ARRAY_FIELDS={','.join(self._grubber_array_fields)} {cmd}"
        if self._table_nu_query.strip():
            nu_query = self._table_nu_query.strip().replace("'", '"')
            cmd += f" | nu -c 'from json | {nu_query}'"
        return cmd

    def action_yank(self) -> None:
        """Copy the current grubber command to the clipboard."""
        cmd = self._build_yank_command()
        self._copy_to_clipboard(cmd)
        self.notify(f"Copied: {cmd}", timeout=4)

    def action_yank_quit(self) -> None:
        """Copy the current grubber command to clipboard and exit."""
        cmd = self._build_yank_command()
        self._copy_to_clipboard(cmd)
        self.exit(result=cmd)

    def action_toggle_compact(self) -> None:
        """Toggle compact preview mode (frontmatter + YAML blocks + Tasks only)."""
        self._compact_preview = not self._compact_preview
        if not self._table_mode:
            lv = self.query_one("#file-list", NoteListView)
            if isinstance(lv.highlighted_child, NoteItem):
                self._show_preview(lv.highlighted_child.full_path)

    def action_toggle_preview(self) -> None:
        self._preview_visible = not self._preview_visible
        right = self.query_one("#right")
        left = self.query_one("#left")
        if self._preview_visible:
            right.remove_class("hidden")
            left.remove_class("wide")
        else:
            right.add_class("hidden")
            left.add_class("wide")

    def action_run_table_query(self) -> None:
        """Run the nu query from the table-query input (ctrl+r)."""
        if not self._table_mode:
            return
        tq = self.query_one("#table-query", Input)
        self._table_nu_query = tq.value
        self._apply_table_query()

    def action_toggle_table(self) -> None:
        """Toggle between preview and metadata table in the right pane."""
        self._table_mode = not self._table_mode
        scroll = self.query_one("#preview-scroll")
        table = self.query_one("#meta-table", MetaDataTable)
        right = self.query_one("#right")
        left = self.query_one("#left")

        # Make sure right pane is visible
        right.remove_class("hidden")
        left.remove_class("wide")
        self._preview_visible = True

        tq = self.query_one("#table-query", Input)
        if self._table_mode:
            scroll.add_class("hidden")
            tq.add_class("visible")
            table.add_class("visible")
            # Set default query on first open
            if not tq.value and self._table_default_query:
                tq.value = self._table_default_query
            self._table_nu_query = tq.value
            self.query_one("#preview-title", Static).update(self._yank_command_display())
            self._populate_table()
        else:
            table.remove_class("visible")
            tq.remove_class("visible")
            scroll.remove_class("hidden")
            # Restore preview for current selection
            lv = self.query_one("#file-list", NoteListView)
            if isinstance(lv.highlighted_child, NoteItem):
                self._show_preview(lv.highlighted_child.full_path)

    def _populate_table(self) -> None:
        """Fetch grubber data for current visible files, then apply nu query."""
        if not self._current_visible:
            return

        af = self._grubber_array_fields

        def _table_cmd(flag: str | None = None) -> list[str]:
            cmd = [GRUBBER_BIN, "extract", self.notes_dir]
            if flag:
                cmd.append(flag)
            if self._grubber_mmd:
                cmd.append("--mmd")
            return cmd

        if self._grubber_search_mode == "frontmatter":
            all_data = _run_grubber_cmd(_table_cmd("--frontmatter-only"), af)
        elif self._grubber_search_mode == "blocks_only":
            all_data = _run_grubber_cmd(_table_cmd("--blocks-only"), af)
        else:
            all_data = _run_grubber_cmd(_table_cmd("--all"), af)

        # Keep only currently visible files, in list order
        visible_set = set(self._current_visible)
        order = {p: i for i, p in enumerate(self._current_visible)}
        self._table_raw_data = sorted(
            [r for r in all_data if r.get("_note_file", "") in visible_set],
            key=lambda r: order.get(r.get("_note_file", ""), 9999),
        )
        self._apply_table_query()

    def _apply_table_query(self) -> None:
        """Filter _table_raw_data through nushell (if query set) and render."""
        data = self._table_raw_data
        query = self._table_nu_query.strip()

        if query and data:
            # Locate nu binary
            nu_bin = shutil.which("nu")
            if not nu_bin:
                for candidate in ("/opt/homebrew/bin/nu", "/usr/local/bin/nu", "/usr/bin/nu"):
                    if Path(candidate).exists():
                        nu_bin = candidate
                        break

            if not nu_bin:
                self.query_one("#status", Static).update(
                    " [red]nu not found[/red] – install nushell or add it to PATH"
                )
                self._render_table(data)
                return

            # Normalize: give every record the same set of keys so nushell
            # builds a uniform table (missing keys → null).  Without this,
            # `where status == "active"` fails if any record lacks `status`.
            all_keys: set[str] = set()
            for record in data:
                all_keys.update(record.keys())
            normalized = [{k: record.get(k) for k in all_keys} for record in data]

            tmp = None
            try:
                with tempfile.NamedTemporaryFile(
                    mode="w", suffix=".json", delete=False
                ) as f:
                    json.dump(normalized, f)
                    tmp = f.name

                # Pass the path via env var to avoid any quoting/space issues
                env = os.environ.copy()
                env["_NP_DATA"] = tmp
                result = subprocess.run(
                    [nu_bin, "-c", f"open $env._NP_DATA | {query} | to json"],
                    env=env,
                    capture_output=True,
                    text=True,
                    timeout=10,
                )
                if result.returncode == 0:
                    parsed = json.loads(result.stdout)
                    if isinstance(parsed, dict):
                        parsed = [parsed]
                    if not isinstance(parsed, list):
                        # Scalar result — not renderable as table
                        self.query_one("#status", Static).update(
                            f" [yellow]Query result is not a table:[/yellow] {parsed}"
                        )
                        return
                    data = parsed
                else:
                    # Show first non-empty error line in status bar
                    stderr = result.stderr.strip()
                    lines = [l.strip() for l in stderr.splitlines() if l.strip()]
                    err = lines[0] if lines else "unknown error"
                    self.query_one("#status", Static).update(
                        f" [red]nu:[/red] {err}"
                    )
            except subprocess.TimeoutExpired:
                self.query_one("#status", Static).update(" [red]nu query timed out[/red]")
            except (json.JSONDecodeError, OSError):
                pass
            finally:
                if tmp:
                    try:
                        os.unlink(tmp)
                    except OSError:
                        pass

        self._render_table(data)

    def _render_table(self, data: list[dict]) -> None:
        """Populate the DataTable from a list of grubber records."""
        table = self.query_one("#meta-table", MetaDataTable)
        table.clear(columns=True)
        self._table_file_to_row_idx = {}

        if not isinstance(data, list) or not data:
            return

        # Collect field names, optionally restricted to table_columns
        seen_keys: set[str] = set()
        all_keys: list[str] = []
        for record in data:
            for k in record:
                if k not in seen_keys and not k.startswith("_"):
                    seen_keys.add(k)
                    all_keys.append(k)

        if self._table_columns:
            all_keys = [k for k in all_keys if k in self._table_columns]

        table.add_column("file", key="file")
        for k in all_keys:
            table.add_column(k, key=k)

        for idx, record in enumerate(data):
            file_path = record.get("_note_file", "")
            row = [os.path.basename(file_path)] + [str(record.get(k, "")) for k in all_keys]
            table.add_row(*row)
            self._table_file_to_row_idx[file_path] = idx

        # Highlight current list selection
        lv = self.query_one("#file-list", NoteListView)
        if isinstance(lv.highlighted_child, NoteItem):
            self._highlight_table_row(lv.highlighted_child.full_path)

    def _highlight_table_row(self, path: str) -> None:
        """Move the DataTable cursor to the row matching path."""
        if not self._table_mode:
            return
        self.query_one("#preview-title", Static).update(self._yank_command_display())
        idx = self._table_file_to_row_idx.get(path)
        if idx is not None:
            self.query_one("#meta-table", MetaDataTable).move_cursor(row=idx)


# ---------------------------------------------------------------------------
# CLI entry point
# ---------------------------------------------------------------------------

def main() -> None:
    parser = argparse.ArgumentParser(
        prog="matterbase",
        description="Keyboard-driven terminal UI for browsing and filtering Markdown notes.",
        formatter_class=argparse.RawDescriptionHelpFormatter,
        epilog=__doc__,
    )
    parser.add_argument(
        "--config",
        required=True,
        metavar="CONFIG.YML",
        help="Path to YAML config file (see --help for format)",
    )
    parser.add_argument(
        "path",
        nargs="?",
        metavar="PATH",
        help="File or directory to browse (overrides notes_dir from config)",
    )
    parser.add_argument(
        "--depth",
        type=int,
        metavar="N",
        default=None,
        help="Limit directory recursion depth (0 = root only)",
    )
    args = parser.parse_args()

    config = load_config(args.config)
    if args.path:
        p = Path(args.path).expanduser().resolve()
        if not p.exists():
            print(f"Error: path not found: {p}", file=sys.stderr)
            sys.exit(1)
        config["notes_dir"] = str(p)
    if args.depth is not None:
        config["depth"] = args.depth
    result = MatterbaseApp(config).run()
    if result:
        print(result)


if __name__ == "__main__":
    main()
