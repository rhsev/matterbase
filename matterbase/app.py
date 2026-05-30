"""MatterbaseApp — the Textual application, config loader, and CLI entry point."""

__version__ = "0.5.0"

import argparse
import datetime
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

import duckdb
import yaml
from rich.markup import escape as markup_escape
from rich.text import Text
from textual.app import App, ComposeResult
from textual.binding import Binding
from textual.containers import Horizontal, ScrollableContainer, Vertical
from textual.reactive import reactive
from textual.widgets import DataTable, Footer, Input, ListView, Static

from .content import (
    ApexConfig,
    extract_compact_content,
    extract_docx_text,
    extract_pdf_text,
    extract_section_for_record,
    render_with_apex,
    render_with_bat,
    split_mmd_header,
)
from .grubber_client import (
    GRUBBER_BIN,
    MIN_GRUBBER_VERSION,
    _run_grubber_cmd,
    check_grubber_version,
    extract_to_ndjson,
    query_cached_files,
    query_files,
)
from .widgets import FilterButton, MetaDataTable, NoteItem, NoteListView


# ---------------------------------------------------------------------------
# Config
# ---------------------------------------------------------------------------

def load_config(config_path: str) -> dict:
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
        print(
            f"Error: notes_dir does not exist or is not a directory: {notes_dir}",
            file=sys.stderr,
        )
        sys.exit(1)
    config["notes_dir"] = str(notes_dir)
    return config


# ---------------------------------------------------------------------------
# App
# ---------------------------------------------------------------------------

class MatterbaseApp(App):
    """Terminal note picker - browse, filter, and open Markdown notes."""

    CSS = """
    /* ── Layout ───────────────────────────────────────────────────── */
    Screen {
        layout: vertical;
    }

    #main {
        layout: horizontal;
        height: 1fr;
    }

    /* ── Left pane (file list) ─────────────────────────────────────── */
    #left {
        width: 35%;
        height: 100%;
        border-right: solid $primary-darken-2;
        padding: 0 1;
        background: #2E3440;
    }

    #left.narrow {
        width: 22%;
    }

    #left.wide {
        width: 100%;
        border-right: none;
    }

    #filter-row {
        layout: grid;
        grid-size: 3;
        grid-gutter: 1;
        height: auto;
        margin-bottom: 1;
        background: #2E3440;
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

    /* ── Middle pane (table - toggleable with t) ───────────────────── */
    #middle {
        display: none;
        width: 43%;
        height: 100%;
        border-right: solid $primary-darken-2;
        padding: 0 1;
        background: #2E3440;
    }

    #middle.visible {
        display: block;
    }

    #middle.expand {
        width: 78%;
    }

    #middle-label {
        height: 1;
        color: $accent;
        text-style: bold;
        margin-bottom: 1;
    }

    #table-query {
        height: 3;
        border: solid #D8DEE9;
        background: #2E3440;
    }

    #table-query:focus {
        border: solid $accent;
    }

    #meta-table {
        height: 1fr;
        border: solid #D8DEE9;
        background: #2E3440;
    }

    #meta-table:focus {
        border: solid $accent;
    }

    #meta-table > .datatable--cursor {
        background: $accent;
        color: $text;
    }

    #meta-table > .datatable--fixed-cursor {
        background: $accent;
        color: $text;
    }

    /* ── Right pane (preview - always visible, toggle with p) ─────── */
    #right {
        width: 1fr;
        height: 100%;
        padding: 0 1;
        background: #2E3440;
    }

    #right.hidden {
        display: none;
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

    #preview {
        padding: 0 1;
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
        Binding("escape", "navigate_mode", "Navigate", show=False),
        Binding("space", "toggle_filter", "Toggle filter", show=False),
        Binding("p", "toggle_preview", "Preview"),
        Binding("t", "toggle_table", "Table"),
        Binding("a", "toggle_all_select", "All / single", show=False),
        Binding("f", "toggle_ref_preview", "File preview", show=False),
        Binding("o", "open_ref_file", "Open ref file", show=False),
        Binding("y", "yank", "Yank cmd", show=False),
        Binding("Y", "yank_quit", "Yank cmd + quit", show=False),
        Binding("m", "toggle_compact", "Compact", show=False),
        Binding("r", "refresh", "Refresh", show=False),
        Binding("l", "quicklook", "QuickLook", show=False),
    ]

    FULLTEXT_MIN_CHARS = 3
    FULLTEXT_MAX_RESULTS = 25

    def __init__(self, config: dict) -> None:
        super().__init__()
        self.config = config
        self.notes_dir: str = config["notes_dir"]
        self.editor: str = config.get("editor", "hx")
        self.filter_defs: list[dict] = config.get("filters", [])
        self.multi_select: bool = bool(config.get("multi_select", False))

        self._all_files: list[str] = []
        self._current_visible: list[str] = []
        self._active_queries: list[list[str]] = []
        # In-session NDJSON cache: the notes dir is scanned once per refresh into
        # this file; filter changes replay it via grubber --from-ndjson instead
        # of re-scanning Markdown. Rebuilt on refresh, removed on exit.
        self._cache_path: str | None = None
        self._search_term: str = ""
        self._preview_visible: bool = True
        self._table_mode: bool = False
        self._table_file_to_row_idx: dict[str, int] = {}
        self._grubber_search_mode: str = config.get("grubber_search_mode", "all")
        self._grubber_mmd: bool = bool(config.get("grubber_mmd", False))
        self._grubber_array_fields: list[str] = config.get("array_fields", [])
        self._grubber_depth: int | None = config.get("depth")
        self._table_columns: list[str] = config.get("table_columns", [])
        self._table_default_query: str = config.get("table_query", "")
        self._table_raw_data: list[dict] = []
        self._table_nu_query: str = ""
        self._table_display_records: list[dict] = []
        self._all_mode: bool = True
        self._ref_preview_mode: bool = False
        self._bookmark_path_cache: dict[str, str] = {}
        self._fulltext_timer = None
        self._compact_preview: bool = False
        self._compact_tasks_heading: str = config.get("compact_tasks_heading", "Tasks")
        self._input_focused: bool = False
        self._apex = ApexConfig(
            theme=config.get("apex_theme", ""),
            width=config.get("apex_width", 0),
            code_highlight=config.get("apex_code_highlight", ""),
            code_highlight_theme=config.get("apex_code_highlight_theme", ""),
        )

    # ── Compose ──────────────────────────────────────────────────────

    def compose(self) -> ComposeResult:
        with Horizontal(id="main"):
            with Vertical(id="left"):
                with Vertical(id="filter-row"):
                    for fd in self.filter_defs:
                        safe_id = "fb-" + re.sub(r"[^a-z0-9_-]", "-", fd["label"].lower())
                        query = fd.get("query", [])
                        yield FilterButton(fd["label"], query, id=safe_id)

                yield Input(placeholder="Search  (⎵ term = fulltext)", id="search")
                yield NoteListView(id="file-list")

            with Vertical(id="middle"):
                yield Static("", id="middle-label")
                yield Input(placeholder="SQL WHERE  (Enter to run)", id="table-query")
                yield MetaDataTable(id="meta-table", cursor_type="row", show_cursor=True)

            with Vertical(id="right"):
                yield Static("", id="preview-title")
                with ScrollableContainer(id="preview-scroll", can_focus=False):
                    yield Static("", id="preview", expand=True)

        yield Static("", id="status")

    # ── Lifecycle ────────────────────────────────────────────────────

    async def on_mount(self) -> None:
        await self._refresh_files()

    def on_unmount(self) -> None:
        # Remove the per-session NDJSON cache file on teardown.
        if self._cache_path and os.path.exists(self._cache_path):
            try:
                os.unlink(self._cache_path)
            except OSError:
                pass

    # ── Grubber / file refresh ────────────────────────────────────────

    def _ensure_cache_path(self) -> str:
        """Lazily allocate the per-session NDJSON cache file path."""
        if self._cache_path is None:
            fd, path = tempfile.mkstemp(prefix="matterbase-", suffix=".ndjson")
            os.close(fd)
            self._cache_path = path
        return self._cache_path

    def _status_on_error(self):
        return lambda msg: self.call_from_thread(
            self.query_one("#status", Static).update, f" [red]{msg}[/red]"
        )

    async def _refresh_files(self) -> None:
        """Re-scan the notes dir once into the NDJSON cache, then filter it.

        This is the only path that touches Markdown on disk; it runs on refresh
        (`r`) and at startup. Filter changes use _apply_filters (cache replay).
        """
        cache_path = self._ensure_cache_path()
        on_error = self._status_on_error()

        def _rebuild_and_filter() -> None:
            ok = extract_to_ndjson(
                self.notes_dir,
                cache_path,
                search_mode=self._grubber_search_mode,
                mmd=self._grubber_mmd,
                depth=self._grubber_depth,
                on_error=on_error,
            )
            paths = query_cached_files(
                cache_path,
                self._active_queries,
                self.multi_select,
                array_fields=self._grubber_array_fields,
                on_error=on_error,
            ) if ok else []
            self.call_from_thread(self._finish_refresh, paths)

        self.run_worker(_rebuild_and_filter, exclusive=True, thread=True, group="refresh")

    async def _apply_filters(self) -> None:
        """Re-filter the existing in-session cache without re-scanning Markdown.

        The fast path for filter-button changes: the data hasn't changed, only
        the filter has, so replay the cache instead of re-running the dir scan.
        Falls back to a full refresh if the cache isn't built yet.
        """
        if self._cache_path is None or not os.path.exists(self._cache_path):
            await self._refresh_files()
            return
        cache_path = self._cache_path
        on_error = self._status_on_error()

        def _filter() -> None:
            paths = query_cached_files(
                cache_path,
                self._active_queries,
                self.multi_select,
                array_fields=self._grubber_array_fields,
                on_error=on_error,
            )
            self.call_from_thread(self._finish_refresh, paths)

        self.run_worker(_filter, exclusive=True, thread=True, group="refresh")

    def _finish_refresh(self, paths: list[str]) -> None:
        self._all_files = paths
        self._apply_search()

    def _apply_search(self) -> None:
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
                    f" Fulltext: {self.FULLTEXT_MIN_CHARS}+ chars required  │  {self.notes_dir}"
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
                            f" {max_results}+ matches - refine search term  │  {notes_dir}"
                        )
                    )

            self.run_worker(_do_search, exclusive=True, thread=True, group="search")
        else:
            visible = [p for p in self._all_files if term in os.path.basename(p).lower()]
            self._rebuild_list(visible)

    def _rebuild_list(self, paths: list[str]) -> None:
        self._current_visible = paths
        lv = self.query_one("#file-list", NoteListView)
        lv.clear()

        for p in paths:
            lv.append(NoteItem(os.path.basename(p), p))

        total = len(self._all_files)
        shown = len(paths)
        note_word = "note" if total == 1 else "notes"
        if self._active_queries or self._search_term:
            msg = f" {shown} of {total} {note_word}  │  {self.notes_dir}"
        else:
            msg = f" {total} {note_word}  │  {self.notes_dir}"
        self.query_one("#status", Static).update(msg)

        if not paths:
            self.query_one("#preview-title", Static).update("")
            self.query_one("#preview", Static).update("[dim]No notes found.[/dim]")

        if self._table_mode:
            self._populate_table()

    # ── Event handlers ────────────────────────────────────────────────

    async def on_button_pressed(self, event) -> None:
        if not isinstance(event.button, FilterButton):
            return
        btn = event.button
        btn.toggle()

        if btn.is_active:
            if btn.query not in self._active_queries:
                self._active_queries.append(btn.query)
        else:
            self._active_queries = [q for q in self._active_queries if q is not btn.query]

        # Filter changed, not the data — replay the cache, don't re-scan Markdown.
        await self._apply_filters()

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
        path = event.item.full_path
        self._show_preview(path)
        if self._table_mode:
            if self._all_mode:
                self._highlight_table_row(path)
            else:
                self._populate_table(single_file=path)

    # ── Preview ───────────────────────────────────────────────────────

    def _show_preview(self, path: str) -> None:
        if not self._preview_visible:
            return

        title = self.query_one("#preview-title", Static)
        preview = self.query_one("#preview", Static)
        title.update(os.path.basename(path))

        suffix = Path(path).suffix.lower()
        if suffix == ".pdf":
            preview.update(extract_pdf_text(path))
            return
        if suffix == ".docx":
            preview.update(extract_docx_text(path))
            return

        tmp_content: str | None = None
        if self._compact_preview:
            compact = extract_compact_content(
                path,
                compact_tasks_heading=self._compact_tasks_heading,
                mmd=self._grubber_mmd,
            )
            if compact:
                tmp_content = compact

        ansi = render_with_apex(path, self._apex, tmp_content=tmp_content)
        if ansi:
            preview.update(Text.from_ansi(ansi))
            return

        if suffix not in ("", ".md", ".markdown"):
            bat = render_with_bat(path)
            if bat:
                preview.update(Text.from_ansi(bat))
                return

        # Fallback: raw content with dimmed frontmatter
        try:
            content = Path(path).read_text(encoding="utf-8", errors="replace")
        except OSError as exc:
            preview.update(f"[red]Error reading file:[/red] {exc}")
            return
        if content.startswith("---"):
            parts = content.split("---", 2)
            if len(parts) >= 3:
                fm = parts[1].strip("\n")
                body = parts[2].lstrip("\n")
                preview.update(f"[dim]---\n{markup_escape(fm)}\n---[/dim]\n\n{markup_escape(body)}")
                return
        if self._grubber_mmd:
            mmd_yaml, mmd_body = split_mmd_header(content)
            if mmd_yaml:
                preview.update(f"[dim]---\n{markup_escape(mmd_yaml)}\n---[/dim]\n\n{markup_escape(mmd_body)}")
                return
        preview.update(markup_escape(content))

    # ── Table focus: record → right pane ──────────────────────────────

    def on_data_table_row_highlighted(self, event: DataTable.RowHighlighted) -> None:
        table = self.query_one("#meta-table", MetaDataTable)
        if not table.has_focus:
            return
        idx = event.cursor_row
        if idx >= len(self._table_display_records):
            return
        self._show_record_right(self._table_display_records[idx])

    def on_data_table_row_selected(self, event: DataTable.RowSelected) -> None:
        idx = event.cursor_row
        if idx < len(self._table_display_records):
            record = self._table_display_records[idx]
            file_path = record.get("_note_file", "")
            if file_path and Path(file_path).exists():
                if Path(file_path).suffix.lower() in (".md", ".markdown"):
                    editor = self.config.get("editor", "hx")
                    if os.environ.get("ZELLIJ"):
                        subprocess.run(["zellij", "run", "-f", "--", editor, file_path])
                    elif os.environ.get("TMUX"):
                        subprocess.run(["tmux", "new-window", editor, file_path])
                    else:
                        with self.suspend():
                            subprocess.run([editor, file_path])
                else:
                    subprocess.run(["open", file_path])

    def _current_table_record(self) -> dict | None:
        table = self.query_one("#meta-table", MetaDataTable)
        idx = table.cursor_row
        if idx < len(self._table_display_records):
            return self._table_display_records[idx]
        return None

    def _current_file_path(self) -> str | None:
        if self._table_mode:
            record = self._current_table_record()
            return record.get("_note_file") if record else None
        lv = self.query_one("#file-list", NoteListView)
        if isinstance(lv.highlighted_child, NoteItem):
            return lv.highlighted_child.full_path
        return None

    def _show_record_right(self, record: dict) -> None:
        is_ref = str(record.get("type", "")).strip() == "ref"
        if is_ref and self._ref_preview_mode:
            lookup = str(record.get("id") or record.get("alias") or "").strip()
            if lookup:
                self._show_bookmarked_preview(lookup)
                return
        self._show_record_context(record)

    def _show_record_context(self, record: dict) -> None:
        if not self._preview_visible:
            return

        file_path = record.get("_note_file", "")
        title_widget = self.query_one("#preview-title", Static)
        preview_widget = self.query_one("#preview", Static)
        title_widget.update(os.path.basename(file_path) if file_path else "Record")

        if not file_path or not Path(file_path).exists():
            preview_widget.update("[dim](file not found)[/dim]")
            return

        try:
            content = Path(file_path).read_text(encoding="utf-8", errors="replace")
        except OSError as exc:
            preview_widget.update(f"[red]Error:[/red] {exc}")
            return

        parts: list[str] = []
        body = content
        fm_keys: set[str] = set()
        if content.startswith("---"):
            sections = content.split("---", 2)
            if len(sections) >= 3:
                parts.append(f"---{sections[1]}---")
                body = sections[2]
                try:
                    fm_data = yaml.safe_load(sections[1]) or {}
                    fm_keys = set(fm_data.keys())
                except yaml.YAMLError:
                    pass
        elif self._grubber_mmd:
            mmd_yaml, mmd_body = split_mmd_header(content)
            if mmd_yaml:
                parts.append(f"---\n{mmd_yaml}\n---")
                body = mmd_body
                fm_keys = {
                    line.split(":", 1)[0].strip()
                    for line in mmd_yaml.splitlines()
                    if ":" in line
                }

        section = extract_section_for_record(body, record, frontmatter_keys=fm_keys)
        if section:
            parts.append(section)

        md_text = "\n\n".join(parts)
        if not md_text:
            preview_widget.update("[dim](no content)[/dim]")
            return

        ansi = render_with_apex(path="", cfg=self._apex, tmp_content=md_text)
        if ansi:
            preview_widget.update(Text.from_ansi(ansi))
        else:
            preview_widget.update(md_text)

    def _show_bookmarked_preview(self, ref_id: str) -> None:
        if ref_id in self._bookmark_path_cache:
            path = self._bookmark_path_cache[ref_id]
            if path:
                self._show_preview(path)
            return

        def _resolve() -> None:
            try:
                result = subprocess.run(
                    ["bookmarker", "get", ref_id],
                    capture_output=True, text=True, timeout=5,
                )
                if result.returncode == 0:
                    resolved = result.stdout.strip()
                    self._bookmark_path_cache[ref_id] = resolved
                    if resolved and Path(resolved).exists():
                        self.call_from_thread(self._show_preview, resolved)
                else:
                    self._bookmark_path_cache[ref_id] = ""
            except (FileNotFoundError, subprocess.TimeoutExpired):
                self._bookmark_path_cache[ref_id] = ""

        self.run_worker(_resolve, exclusive=True, thread=True, group="bookmark-resolve")

    def action_toggle_ref_preview(self) -> None:
        if not self._table_mode:
            return
        self._ref_preview_mode = not self._ref_preview_mode
        record = self._current_table_record()
        if record:
            self._show_record_right(record)

    def action_open_ref_file(self) -> None:
        if not self._table_mode:
            return
        record = self._current_table_record()
        if not record:
            return
        path = self._resolve_ref_path(record)
        if path and Path(path).exists():
            subprocess.run(["open", path])

    def watch_focused(self, focused) -> None:
        if not isinstance(focused, MetaDataTable):
            lv = self.query_one("#file-list", NoteListView)
            if isinstance(lv.highlighted_child, NoteItem):
                self._show_preview(lv.highlighted_child.full_path)

    # ── Layout ────────────────────────────────────────────────────────

    def _update_layout(self) -> None:
        left = self.query_one("#left")
        middle = self.query_one("#middle")
        right = self.query_one("#right")

        middle_visible = self._table_mode

        if middle_visible:
            left.add_class("narrow")
            left.remove_class("wide")
            middle.add_class("visible")
        else:
            left.remove_class("narrow")
            middle.remove_class("visible")
            middle.remove_class("expand")

        if self._preview_visible:
            right.remove_class("hidden")
            if middle_visible:
                middle.remove_class("expand")
            else:
                left.remove_class("wide")
        else:
            right.add_class("hidden")
            if middle_visible:
                middle.add_class("expand")
            else:
                left.add_class("wide")

    # ── Actions ───────────────────────────────────────────────────────

    def on_list_view_selected(self, event: ListView.Selected) -> None:
        if not isinstance(event.item, NoteItem):
            return
        path = event.item.full_path
        editor = self.config.get("editor", "hx")
        if os.environ.get("ZELLIJ"):
            subprocess.run(["zellij", "run", "-f", "--", editor, path])
        elif os.environ.get("TMUX"):
            subprocess.run(["tmux", "new-window", editor, path])
        else:
            with self.suspend():
                subprocess.run([editor, path])

    async def action_toggle_filter(self) -> None:
        focused = self.focused
        if isinstance(focused, FilterButton):
            focused.press()

    # ── Mode handling ─────────────────────────────────────────────────

    def _navigate_status(self) -> str:
        if self._table_mode:
            return " q quit  ·  y yank  ·  / search  ·  t table  ·  p preview"
        else:
            return " q quit  ·  y yank  ·  / search  ·  m compact  ·  t table"

    def _search_status(self) -> str:
        return ""

    def action_navigate_mode(self) -> None:
        self._input_focused = False
        if self._table_mode:
            self.query_one("#meta-table", MetaDataTable).focus()
        else:
            self.query_one("#file-list", NoteListView).focus()
        self.query_one("#status", Static).update(self._navigate_status())

    def on_input_focus(self, event) -> None:
        self._input_focused = True
        self.query_one("#status", Static).update(self._search_status())

    def on_input_blur(self, event) -> None:
        self._input_focused = False

    async def on_key(self, event) -> None:
        if self._input_focused:
            return
        if event.key == "q":
            event.stop()
            self.exit()
        elif event.key == "slash":
            event.stop()
            self.query_one("#search", Input).focus()

    async def action_quit(self) -> None:
        self.exit()

    async def action_refresh(self) -> None:
        await self._refresh_files()
        self.notify("Refreshed", timeout=2)

    def _resolve_ref_path(self, record: dict) -> str:
        lookup = str(record.get("id") or record.get("alias") or "").strip()
        if not lookup:
            return ""
        if lookup in self._bookmark_path_cache:
            return self._bookmark_path_cache[lookup]
        try:
            result = subprocess.run(
                ["bookmarker", "get", lookup],
                capture_output=True, text=True, timeout=5,
            )
            path = result.stdout.strip() if result.returncode == 0 else ""
        except (FileNotFoundError, subprocess.TimeoutExpired):
            path = ""
        self._bookmark_path_cache[lookup] = path
        return path

    def action_quicklook(self) -> None:
        if not self._table_mode:
            return
        record = self._current_table_record()
        if not record or str(record.get("type", "")).strip() != "ref":
            return
        path = self._resolve_ref_path(record)
        if path and Path(path).exists():
            subprocess.Popen(
                ["qlmanage", "-p", path],
                stdout=subprocess.DEVNULL,
                stderr=subprocess.DEVNULL,
            )

    def _yank_command_display(self) -> str:
        parts = []
        for query_list in self._active_queries:
            for expr in query_list:
                parts += ["-f", shlex.quote(expr)]
        if self._table_nu_query.strip():
            user_query = self._table_nu_query.strip()
            parts += ["|", "duckdb -json -c", f'"...WHERE {user_query}"']
        return " ".join(parts) if parts else "(no filters)"

    def _copy_to_clipboard(self, text: str) -> None:
        import platform as _platform
        system = _platform.system()
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
        parts = [shlex.quote(GRUBBER_BIN), "extract", shlex.quote(self.notes_dir)]
        if self._table_mode:
            parts.append("-a")
        elif self._grubber_search_mode == "frontmatter":
            parts.append("--frontmatter-only")
        elif self._grubber_search_mode == "blocks_only":
            parts.append("--blocks-only")
        else:
            parts.append("-a")
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
            user_query = self._table_nu_query.strip().replace('"', '\\"')
            cmd += f' | duckdb -json -c "SELECT * FROM read_json_auto(\'/dev/stdin\') WHERE {user_query}"'
        return cmd

    def action_yank(self) -> None:
        cmd = self._build_yank_command()
        self._copy_to_clipboard(cmd)
        self.notify(f"Copied: {cmd}", timeout=4)

    def action_yank_quit(self) -> None:
        cmd = self._build_yank_command()
        self._copy_to_clipboard(cmd)
        self.exit(result=cmd)

    def action_toggle_compact(self) -> None:
        table = self.query_one("#meta-table", MetaDataTable)
        if self._table_mode and table.has_focus:
            return
        self._compact_preview = not self._compact_preview
        lv = self.query_one("#file-list", NoteListView)
        if isinstance(lv.highlighted_child, NoteItem):
            self._show_preview(lv.highlighted_child.full_path)

    def action_toggle_preview(self) -> None:
        self._preview_visible = not self._preview_visible
        self._update_layout()
        if self._preview_visible:
            table = self.query_one("#meta-table", MetaDataTable)
            if self._table_mode and table.has_focus:
                record = self._current_table_record()
                if record:
                    self._show_record_right(record)
                    return
            lv = self.query_one("#file-list", NoteListView)
            if isinstance(lv.highlighted_child, NoteItem):
                self._show_preview(lv.highlighted_child.full_path)

    def action_toggle_all_select(self) -> None:
        if not self._table_mode:
            return
        self._all_mode = not self._all_mode
        lv = self.query_one("#file-list", NoteListView)
        cursor_path = lv.highlighted_child.full_path if isinstance(lv.highlighted_child, NoteItem) else None
        self._update_middle_label()
        if self._all_mode:
            self._populate_table()
        elif cursor_path:
            self._populate_table(single_file=cursor_path)

    def _update_middle_label(self) -> None:
        mode = "all" if self._all_mode else "single"
        query = self._yank_command_display()
        self.query_one("#middle-label", Static).update(f"[reverse] {mode} [/reverse]   {query}")

    def action_toggle_table(self) -> None:
        self._table_mode = not self._table_mode
        self._update_layout()

        tq = self.query_one("#table-query", Input)
        if self._table_mode:
            if not tq.value and self._table_default_query:
                tq.value = self._table_default_query
            self._table_nu_query = tq.value
            self._update_middle_label()
            lv = self.query_one("#file-list", NoteListView)
            cursor_path = lv.highlighted_child.full_path if isinstance(lv.highlighted_child, NoteItem) else None
            if self._all_mode:
                self._populate_table()
            elif cursor_path:
                self._populate_table(single_file=cursor_path)
            else:
                self._populate_table()
            self.query_one("#meta-table", MetaDataTable).focus()
        else:
            self.query_one("#file-list", NoteListView).focus()
            lv = self.query_one("#file-list", NoteListView)
            if isinstance(lv.highlighted_child, NoteItem):
                self._show_preview(lv.highlighted_child.full_path)

    def _populate_table(self, single_file: str | None = None) -> None:
        if not self._current_visible and not single_file:
            return

        cmd = [GRUBBER_BIN, "extract", self.notes_dir, "-a"]
        if self._grubber_depth is not None:
            cmd += ["--depth", str(self._grubber_depth)]
        if self._grubber_mmd:
            cmd.append("--mmd")
        all_data = _run_grubber_cmd(
            cmd,
            self._grubber_array_fields,
            on_error=lambda msg: self.query_one("#status", Static).update(f" [red]{msg}[/red]"),
        )

        if single_file:
            self._table_raw_data = [r for r in all_data if r.get("_note_file") == single_file]
        else:
            visible_set = set(self._current_visible)
            order = {p: i for i, p in enumerate(self._current_visible)}
            self._table_raw_data = sorted(
                [r for r in all_data if r.get("_note_file", "") in visible_set],
                key=lambda r: order.get(r.get("_note_file", ""), 9999),
            )
        self._apply_table_query()

    def _apply_table_query(self) -> None:
        data = self._table_raw_data
        query = self._table_nu_query.strip()

        if query and data:
            tmp = None
            try:
                with tempfile.NamedTemporaryFile(
                    mode="w", suffix=".json", delete=False
                ) as f:
                    json.dump(data, f)
                    tmp = f.name
                safe_path = tmp.replace("'", "''")
                con = duckdb.connect()
                result = con.execute(
                    f"SELECT * FROM read_json_auto('{safe_path}') WHERE {query}"
                )
                cols = [d[0] for d in result.description]
                data = [dict(zip(cols, row)) for row in result.fetchall()]
                self.query_one("#status", Static).update("")
            except duckdb.Error as e:
                self.query_one("#status", Static).update(
                    f" [red]SQL:[/red] {str(e).splitlines()[0]}"
                )
            finally:
                if tmp:
                    try:
                        os.unlink(tmp)
                    except OSError:
                        pass

        self._render_table(data)

    def _render_table(self, data: list[dict]) -> None:
        table = self.query_one("#meta-table", MetaDataTable)
        table.clear(columns=True)
        self._table_file_to_row_idx = {}
        self._table_display_records = list(data)

        if not isinstance(data, list) or not data:
            return

        seen_keys: set[str] = set()
        all_keys: list[str] = []
        for record in data:
            for k in record:
                if k not in seen_keys and not k.startswith("_"):
                    seen_keys.add(k)
                    all_keys.append(k)

        if self._table_columns:
            all_keys = [k for k in all_keys if k in self._table_columns]

        all_keys = [k for k in all_keys if any(record.get(k) is not None for record in data)]

        table.add_column("_note_file", key="file")
        for k in all_keys:
            table.add_column(k, key=k)

        for idx, record in enumerate(data):
            file_path = record.get("_note_file", "")
            row = [os.path.basename(file_path)] + [str(record.get(k, "")) for k in all_keys]
            table.add_row(*row)
            self._table_file_to_row_idx[file_path] = idx

        self.query_one("#status", Static).update(f" {len(data)} records")

        lv = self.query_one("#file-list", NoteListView)
        if isinstance(lv.highlighted_child, NoteItem):
            self._highlight_table_row(lv.highlighted_child.full_path)

    def _highlight_table_row(self, path: str) -> None:
        if not self._table_mode:
            return
        self._update_middle_label()
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
    )
    parser.add_argument(
        "--version",
        action="version",
        version=f"matterbase {__version__}",
    )
    parser.add_argument(
        "--init",
        action="store_true",
        help="Interactively create a config file",
    )
    parser.add_argument(
        "--config",
        metavar="CONFIG.YML",
        help="Path to YAML config file",
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

    if args.init:
        from .init_config import run_init
        run_init()
        return

    if not args.config:
        parser.error("--config is required (or use --init to create one)")

    # Verify grubber is actually executable before launching the TUI.
    try:
        subprocess.run([GRUBBER_BIN, "--version"], capture_output=True, timeout=5)
    except FileNotFoundError:
        print(f"Error: grubber binary not found: {GRUBBER_BIN}", file=sys.stderr)
        print("Install grubber or make sure it is on PATH.", file=sys.stderr)
        sys.exit(1)
    except subprocess.TimeoutExpired:
        print(f"Error: grubber binary timed out: {GRUBBER_BIN}", file=sys.stderr)
        sys.exit(1)
    except OSError as e:
        print(f"Error: could not run grubber ({GRUBBER_BIN}): {e}", file=sys.stderr)
        sys.exit(1)

    # Require a grubber new enough for the features matterbase relies on.
    ok, found_version = check_grubber_version()
    if not ok:
        want = ".".join(map(str, MIN_GRUBBER_VERSION))
        print(
            f"Error: grubber {want} or newer is required (found: {found_version}).",
            file=sys.stderr,
        )
        print(f"Update grubber: {GRUBBER_BIN}", file=sys.stderr)
        sys.exit(1)

    config = load_config(args.config)
    if args.path:
        p = Path(args.path).expanduser().resolve()
        if not p.exists():
            print(f"Error: path not found: {p}", file=sys.stderr)
            sys.exit(1)
        config["notes_dir"] = str(p)
    if args.depth is not None:
        config["depth"] = args.depth

    # Write startup log (overwritten on each run)
    import textual as _textual
    probe = subprocess.run([GRUBBER_BIN, "--version"], capture_output=True, timeout=5)
    grubber_version = probe.stdout.decode().strip() if probe.returncode == 0 else "unknown"
    log_errors: list[str] = []
    initial_files = query_files(
        config["notes_dir"],
        [],
        False,
        search_mode=config.get("grubber_search_mode", "all"),
        array_fields=config.get("array_fields") or [],
        mmd=bool(config.get("grubber_mmd", False)),
        depth=config.get("depth"),
        on_error=log_errors.append,
    )
    try:
        with open(Path.home() / ".matterbase.log", "w") as lf:
            lf.write(f"timestamp: {datetime.datetime.now().isoformat()}\n")
            lf.write(f"matterbase version: {__version__}\n")
            lf.write(f"python: {sys.version}\n")
            lf.write(f"platform: {platform.platform()}\n")
            lf.write(f"textual: {_textual.__version__}\n")
            lf.write(f"config: {args.config}\n")
            lf.write(f"notes_dir: {config['notes_dir']}\n")
            lf.write(f"grubber: {GRUBBER_BIN}\n")
            lf.write(f"grubber version: {grubber_version}\n")
            lf.write(f"files found: {len(initial_files)}\n")
            for err in log_errors:
                lf.write(f"grubber error: {err}\n")
    except OSError:
        pass

    result = MatterbaseApp(config).run()
    if result:
        print(result)


if __name__ == "__main__":
    main()
