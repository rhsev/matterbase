"""MatterbaseApp — the unified record-query view, config loader, CLI entry point."""

__version__ = "0.6.0"

import argparse
import os
import platform
import re
import shutil
import subprocess
import sys
import tempfile
from pathlib import Path

import yaml
from textual.app import App, ComposeResult
from textual.binding import Binding
from textual.containers import Horizontal, ScrollableContainer, Vertical
from textual.timer import Timer
from textual.widgets import DataTable, Input, Select, Static

from .content import ApexConfig
from .grubber_client import (
    GRUBBER_BIN,
    MIN_GRUBBER_VERSION,
    check_grubber_version,
    extract_to_jsonl,
    find_collection_dir,
)
from .pipeline import PipelineResult, run_pipeline
from .preview import next_mode, render_preview
from .query import (
    SQL_FORM_OPERATORS,
    Preset,
    QueryState,
    append_clause,
    build_clause,
    remove_last_clause,
)
from .widgets import PresetItem, RecordTable


# ---------------------------------------------------------------------------
# Config
# ---------------------------------------------------------------------------

def read_config(config_path: str) -> tuple[dict | None, str]:
    """Read and validate the config. Returns (config, "") or (None, error) —
    non-exiting so the app can reload in-session and keep the old config."""
    path = Path(config_path).expanduser()
    if not path.exists():
        return None, f"Config file not found: {config_path}"

    try:
        with open(path, encoding="utf-8") as f:
            config = yaml.safe_load(f)
    except yaml.YAMLError as exc:
        return None, f"Invalid YAML: {str(exc).splitlines()[0]}"

    if not isinstance(config, dict):
        return None, f"Config file is empty or not a YAML mapping: {config_path}"

    if "notes_dir" not in config:
        return None, "Config missing required key: notes_dir"

    notes_dir = Path(config["notes_dir"]).expanduser()
    if not notes_dir.is_dir():
        return None, f"notes_dir does not exist or is not a directory: {notes_dir}"
    config["notes_dir"] = str(notes_dir)
    config["_config_path"] = str(path)
    return config, ""


def load_config(config_path: str) -> dict:
    config, err = read_config(config_path)
    if config is None:
        print(f"Error: {err}", file=sys.stderr)
        print("Run `matterbase --help` for config format.", file=sys.stderr)
        sys.exit(1)
    return config


def presets_from_config(config: dict) -> list[Preset]:
    # "presets" is the native key; "filters" accepted for matterbase-next configs.
    defs = config.get("presets") or config.get("filters") or []
    return [Preset(d["label"], d.get("query", d.get("exprs", []))) for d in defs]


# ---------------------------------------------------------------------------
# App
# ---------------------------------------------------------------------------

class MatterbaseApp(App):
    """The unified record-query view: builder | records | adaptive preview."""

    CSS = """
    /* Palette carried over from matterbase-next: Nord background #2D3440,
       quiet #D8DEE9 widget borders, $accent only for focus and titles. */
    Screen {
        layout: vertical;
        background: #2D3440;
    }

    /* ── TOP: the constructed (yankable) query ─────────────────────── */
    #query-bar {
        height: auto;
        max-height: 4;
        padding: 0 1;
        background: #2D3440;
        color: $text;
        border-bottom: solid $primary-darken-2;
    }

    #main {
        layout: horizontal;
        height: 1fr;
    }

    /* ── LEFT: the query builder ───────────────────────────────────── */
    #builder {
        width: 30;
        height: 100%;
        border-right: solid $primary-darken-2;
        padding: 0 1;
        background: #2D3440;
    }

    .builder-label {
        height: 1;
        color: $text-muted;
        margin-top: 1;
    }

    #presets {
        height: auto;
        background: #2D3440;
    }

    #sql, #filename, #fulltext {
        height: 3;
        border: solid #D8DEE9;
        background: #2D3440;
    }

    #sql:focus, #filename:focus {
        border: solid $accent;
    }

    /* The SQL form: generates a clause into #sql, which stays the truth.
       One row each: field, operator, value. */
    #form-field {
        margin-top: 1;
    }

    #form-field, #form-op {
        width: 1fr;
        background: #2D3440;
    }

    #form-field > SelectCurrent, #form-op > SelectCurrent {
        border: solid #D8DEE9;
        background: #2D3440;
    }

    #form-field:focus > SelectCurrent, #form-op:focus > SelectCurrent {
        border: solid $accent;
    }

    #form-value {
        width: 1fr;
        height: 3;
        border: solid #D8DEE9;
        background: #2D3440;
    }

    #form-value:focus {
        border: solid $accent;
    }

    #fulltext:focus {
        border: solid $warning;
    }

    /* ── CENTER: the records ───────────────────────────────────────── */
    #records {
        width: 1fr;
        height: 100%;
        border: solid #D8DEE9;
        background: #2D3440;
        /* Textual 8 tints focused widgets 5% lighter; the accent border
           already marks focus, keep the pane background uniform. */
        background-tint: transparent;
    }

    #records:focus {
        border: solid $accent;
    }

    #records > .datatable--cursor {
        background: $accent;
        color: $text;
    }

    /* Rows on the pane background, not Textual's default surface tint. */
    #records > .datatable--odd-row,
    #records > .datatable--even-row {
        background: #2D3440;
    }

    #records > .datatable--header {
        background: #2D3440;
        color: $accent;
    }

    /* ── RIGHT: adaptive preview ───────────────────────────────────── */
    #preview-pane {
        width: 40%;
        max-width: 80;
        height: 100%;
        border-left: solid $primary-darken-2;
        padding: 0 1;
        background: #2D3440;
    }

    #preview-pane.hidden {
        display: none;
    }

    #preview-title {
        height: 1;
        color: $accent;
        text-style: bold;
        margin-bottom: 1;
    }

    #preview-scroll {
        height: 1fr;
        border: solid #D8DEE9;
        background: #2D3440;
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
        Binding("escape", "focus_records", "Records", show=False),
        Binding("y", "yank", "Yank"),
        Binding("Y", "yank_quit", "Yank+quit", show=False),
        Binding("m", "cycle_preview_mode", "Preview mode"),
        Binding("p", "toggle_preview", "Preview", show=False),
        Binding("r", "refresh", "Refresh", show=False),
        Binding("R", "reload_config", "Reload config", show=False),
        Binding("minus", "remove_clause", "− clause", show=False),
    ]

    def __init__(self, config: dict) -> None:
        super().__init__()
        self.state = QueryState(presets=presets_from_config(config))
        if config.get("sql"):
            self.state.sql_where = str(config["sql"])
        self._apply_config(config)

        self._cache_path: str | None = None
        self._collection_dir: str | None = None
        self._records: list[dict] = []
        self._fulltext_cache: dict[str, str] = {}
        self._preview_mode: str = "whole"
        self._preview_visible: bool = True
        self._input_focused: bool = False
        self._debounce_timer: Timer | None = None
        self._form_keys: list[str] = []

    def _apply_config(self, config: dict) -> None:
        """Take over all settings that come straight from the config dict.

        Used at startup and by the in-session reload (R). Deliberately does
        not touch the query state — the built query survives a reload.
        """
        self.config = config
        self.notes_dir: str = config["notes_dir"]
        self.editor: str = config.get("editor", "hx")
        self._config_path: str = config.get("_config_path", "")
        self._search_mode: str = config.get("grubber_search_mode", "all")
        # Name of a grubber config set holding the database definition
        # (path, from_jsonl, merge_on) — shortens the yanked command.
        self._grubber_set: str = config.get("grubber_set", "")
        self._mmd: bool = bool(config.get("grubber_mmd", False))
        self._array_fields: list[str] = config.get("array_fields", [])
        self._depth: int | None = config.get("depth")
        self._table_columns: list[str] = config.get("table_columns", [])
        self._column_widths: dict[str, int] = config.get("column_widths", {})
        self._apex = ApexConfig(
            theme=config.get("apex_theme", ""),
            width=config.get("apex_width", 0),
            code_highlight=config.get("apex_code_highlight", ""),
            code_highlight_theme=config.get("apex_code_highlight_theme", ""),
        )

    def _build_preset_items(self) -> list[PresetItem]:
        """PresetItem widgets for the current state, active flags applied."""
        seen_ids: set[str] = set()
        items: list[PresetItem] = []
        for preset in self.state.presets:
            base_id = "pb-" + re.sub(r"[^a-z0-9_-]", "-", preset.label.lower())
            safe_id, n = base_id, 2
            while safe_id in seen_ids:
                safe_id = f"{base_id}-{n}"
                n += 1
            seen_ids.add(safe_id)
            item = PresetItem(preset.label, preset.exprs, id=safe_id)
            if preset.active:
                item.toggle_active()
            items.append(item)
        return items

    # ── Compose ──────────────────────────────────────────────────────

    def compose(self) -> ComposeResult:
        yield Static("", id="query-bar")

        with Horizontal(id="main"):
            with Vertical(id="builder"):
                yield Static("presets", classes="builder-label")
                with Vertical(id="presets"):
                    yield from self._build_preset_items()
                yield Static("sql where", classes="builder-label")
                yield Input(
                    placeholder="status != 'done'",
                    value=self.state.sql_where,
                    id="sql",
                )
                yield Select([], prompt="field", id="form-field")
                yield Select(
                    [(op.lower(), op) for op in SQL_FORM_OPERATORS],
                    prompt="op", id="form-op",
                )
                yield Input(placeholder="value ⏎", id="form-value")
                yield Static("filename (as sql)", classes="builder-label")
                yield Input(placeholder="_note_file LIKE …", id="filename")
                yield Static("full-text (display only)", classes="builder-label")
                yield Input(placeholder="not in yank", id="fulltext")

            yield RecordTable(id="records", cursor_type="row", show_cursor=True)

            with Vertical(id="preview-pane"):
                yield Static("", id="preview-title")
                with ScrollableContainer(id="preview-scroll", can_focus=False):
                    yield Static("", id="preview", expand=True)

        yield Static("", id="status")

    # ── Lifecycle ────────────────────────────────────────────────────

    async def on_mount(self) -> None:
        self._collection_dir = find_collection_dir(self.notes_dir)
        self._update_query_bar()
        self.query_one("#records", RecordTable).focus()
        self._refresh_data()

    def on_unmount(self) -> None:
        if self._cache_path and os.path.exists(self._cache_path):
            try:
                os.unlink(self._cache_path)
            except OSError:
                pass

    # ── Pipeline plumbing ────────────────────────────────────────────

    def _ensure_cache_path(self) -> str:
        if self._cache_path is None:
            fd, path = tempfile.mkstemp(prefix="matterbase-", suffix=".jsonl")
            os.close(fd)
            self._cache_path = path
        return self._cache_path

    def _status_on_error(self):
        return lambda msg: self.call_from_thread(self._set_status, f"[red]{msg}[/red]")

    def _set_status(self, msg: str) -> None:
        self.query_one("#status", Static).update(f" {msg}")

    def _refresh_data(self) -> None:
        """Scan the notes dir once into the JSONL cache, then run the pipeline.

        The only path that touches files on disk; runs at startup and on `r`.
        Query changes use _run_query (cache replay)."""
        cache_path = self._ensure_cache_path()
        on_error = self._status_on_error()
        state = self.state
        self._fulltext_cache = {}
        fulltext_cache = self._fulltext_cache

        def _rebuild() -> None:
            ok = extract_to_jsonl(
                self.notes_dir,
                cache_path,
                search_mode=self._search_mode,
                mmd=self._mmd,
                depth=self._depth,
                collection_dir=self._collection_dir,
                on_error=on_error,
            )
            result = run_pipeline(
                cache_path, state,
                array_fields=self._array_fields,
                fulltext_cache=fulltext_cache,
                on_error=on_error,
            ) if ok else PipelineResult([], 0)
            self.call_from_thread(self._finish_query, result)

        self.run_worker(_rebuild, exclusive=True, thread=True, group="query")

    def _run_query(self) -> None:
        """Re-run the pipeline over the existing cache — the fast path."""
        if self._cache_path is None or not os.path.exists(self._cache_path):
            self._refresh_data()
            return
        cache_path = self._cache_path
        on_error = self._status_on_error()
        state = self.state
        fulltext_cache = self._fulltext_cache

        def _query() -> None:
            result = run_pipeline(
                cache_path, state,
                array_fields=self._array_fields,
                fulltext_cache=fulltext_cache,
                on_error=on_error,
            )
            self.call_from_thread(self._finish_query, result)

        self.run_worker(_query, exclusive=True, thread=True, group="query")

    def _finish_query(self, result: PipelineResult) -> None:
        self._records = result.records
        self._render_table()
        self._update_form_fields()
        if result.error:
            self._set_status(f"[red]{result.error}[/red]")
        else:
            shown = len(result.records)
            word = "record" if shown == 1 else "records"
            if self.state.fulltext_active():
                msg = f"{shown} of {result.structured_count} {word}  │  {self.notes_dir}"
            else:
                msg = f"{shown} {word}  │  {self.notes_dir}"
            self._set_status(msg)

    # ── The constructed query (TOP) ──────────────────────────────────

    def _build_command(self) -> str:
        return self.state.build_command(
            self.notes_dir,
            search_mode=self._search_mode,
            mmd=self._mmd,
            depth=self._depth,
            collection_dir=self._collection_dir,
            array_fields=self._array_fields,
            grubber_set=self._grubber_set,
        )

    def _update_query_bar(self) -> None:
        from rich.markup import escape as markup_escape
        self.query_one("#query-bar", Static).update(
            f"[dim]y ⇒[/dim] {markup_escape(self._build_command())}"
        )

    # ── CENTER: the records table ────────────────────────────────────

    def _render_table(self) -> None:
        table = self.query_one("#records", RecordTable)
        prev_row = table.cursor_row
        table.clear(columns=True)

        data = self._records
        if not data:
            self.query_one("#preview-title", Static).update("")
            self.query_one("#preview", Static).update("[dim]No records.[/dim]")
            return

        seen_keys: set[str] = set()
        all_keys: list[str] = []
        for record in data:
            for k in record:
                if k not in seen_keys and not k.startswith("_"):
                    seen_keys.add(k)
                    all_keys.append(k)

        if self._table_columns:
            # config order wins
            all_keys = [k for k in self._table_columns if k in seen_keys]
        all_keys = [k for k in all_keys if any(r.get(k) is not None for r in data)]

        cw = self._column_widths
        table.add_column("source", key="source", width=cw.get("source", 30))
        for k in all_keys:
            table.add_column(k, key=k, width=cw.get(k, 20))

        for record in data:
            source = os.path.basename(record.get("_note_file", ""))
            row = [source] + [
                "" if record.get(k) is None else str(record.get(k))
                for k in all_keys
            ]
            table.add_row(*row)

        table.move_cursor(row=min(prev_row, len(data) - 1) if prev_row >= 0 else 0)
        self._preview_current()

    def _current_record(self) -> dict | None:
        table = self.query_one("#records", RecordTable)
        idx = table.cursor_row
        if 0 <= idx < len(self._records):
            return self._records[idx]
        return None

    def on_data_table_row_highlighted(self, event: DataTable.RowHighlighted) -> None:
        idx = event.cursor_row
        if 0 <= idx < len(self._records):
            self._show_preview(self._records[idx])

    def on_data_table_row_selected(self, event: DataTable.RowSelected) -> None:
        record = self._current_record()
        if not record:
            return
        path = record.get("_note_file", "")
        if not path or not Path(path).exists():
            return
        if Path(path).suffix.lower() == ".jsonl":
            return  # a jsonl line has no useful editor target
        if "ZELLIJ" in os.environ:
            subprocess.run(["zellij", "run", "-f", "--", self.editor, path], check=False)
        elif "TMUX" in os.environ:
            subprocess.run(["tmux", "new-window", self.editor, path], check=False)
        else:
            with self.suspend():
                subprocess.run([self.editor, path], check=False)

    # ── RIGHT: adaptive preview ──────────────────────────────────────

    def _preview_current(self) -> None:
        record = self._current_record()
        if record:
            self._show_preview(record)

    def _show_preview(self, record: dict) -> None:
        if not self._preview_visible:
            return
        title, renderable = render_preview(
            record, self._preview_mode, self._apex, mmd=self._mmd
        )
        self.query_one("#preview-title", Static).update(
            f"{title}  [dim]·  {self._preview_mode}[/dim]"
        )
        self.query_one("#preview", Static).update(renderable)

    def action_cycle_preview_mode(self) -> None:
        if self._input_focused:
            return
        self._preview_mode = next_mode(self._preview_mode)
        self._preview_current()

    def action_toggle_preview(self) -> None:
        if self._input_focused:
            return
        self._preview_visible = not self._preview_visible
        pane = self.query_one("#preview-pane")
        if self._preview_visible:
            pane.remove_class("hidden")
            self._preview_current()
        else:
            pane.add_class("hidden")

    # ── LEFT: the query builder ──────────────────────────────────────

    async def on_preset_item_pressed(self, event: PresetItem.Pressed) -> None:
        item = event.item
        item.toggle_active()
        for preset in self.state.presets:
            if preset.label == item.preset_label:
                preset.active = item.is_active
        self._update_query_bar()
        self._run_query()

    async def on_input_changed(self, event: Input.Changed) -> None:
        if event.input.id == "filename":
            self.state.filename_term = event.value
        elif event.input.id == "fulltext":
            self.state.fulltext_term = event.value
        else:
            return
        self._update_query_bar()
        if self._debounce_timer is not None:
            self._debounce_timer.stop()
        self._debounce_timer = self.set_timer(0.25, self._run_query)

    async def on_input_submitted(self, event: Input.Submitted) -> None:
        if event.input.id == "sql":
            self.state.sql_where = event.value
            self._update_query_bar()
            self._run_query()
        elif event.input.id == "form-value":
            self._apply_sql_form()
            return
        self.query_one("#records", RecordTable).focus()

    # ── SQL form: field → operator → value ⇒ clause into #sql ────────

    def _update_form_fields(self) -> None:
        """Feed the field select from the current result set's keys."""
        keys: list[str] = sorted(
            {k for rec in self._records for k in rec if not k.startswith("_")}
        )
        if keys == self._form_keys:
            return
        self._form_keys = keys
        sel = self.query_one("#form-field", Select)
        current = sel.value
        from rich.text import Text
        sel.set_options(
            (Text(k, no_wrap=True, overflow="ellipsis"), k) for k in keys
        )
        if current is not Select.BLANK and current in keys:
            sel.value = current

    def _apply_sql_form(self) -> None:
        field_sel = self.query_one("#form-field", Select)
        op_sel = self.query_one("#form-op", Select)
        value_input = self.query_one("#form-value", Input)

        field = "" if field_sel.value is Select.BLANK else str(field_sel.value)
        op = "" if op_sel.value is Select.BLANK else str(op_sel.value)
        clause = build_clause(field, op, value_input.value)
        if not clause:
            self._set_status("[red]sql form: field, operator and value needed[/red]")
            return

        sql_input = self.query_one("#sql", Input)
        sql_input.value = append_clause(sql_input.value, clause)
        value_input.value = ""
        self.state.sql_where = sql_input.value
        self._update_query_bar()
        self._run_query()
        self.query_one("#records", RecordTable).focus()

    def on_descendant_focus(self, event) -> None:
        self._input_focused = isinstance(event.widget, (Input, Select))

    def on_descendant_blur(self, event) -> None:
        if isinstance(event.widget, (Input, Select)):
            self._input_focused = False

    # ── Keys / actions ───────────────────────────────────────────────

    async def on_key(self, event) -> None:
        if self._input_focused:
            return
        if event.key == "q":
            event.stop()
            self.exit()
        elif event.key == "slash":
            event.stop()
            self.query_one("#sql", Input).focus()
        elif event.key == "f":
            event.stop()
            self.query_one("#filename", Input).focus()
        elif event.key == "t":
            event.stop()
            self.query_one("#fulltext", Input).focus()


    def action_focus_records(self) -> None:
        self._input_focused = False
        self.query_one("#records", RecordTable).focus()

    def action_remove_clause(self) -> None:
        """Strip the last AND-clause from the SQL input (key: -)."""
        if self._input_focused:
            return
        sql_input = self.query_one("#sql", Input)
        if not sql_input.value.strip():
            return
        sql_input.value = remove_last_clause(sql_input.value)
        self.state.sql_where = sql_input.value
        self._update_query_bar()
        self._run_query()

    def _copy_to_clipboard(self, text: str) -> None:
        system = platform.system()
        if system == "Darwin":
            candidates = [["pbcopy"]]
        elif os.environ.get("WAYLAND_DISPLAY"):
            candidates = [["wl-copy"]]
        else:
            candidates = [["xclip", "-selection", "clipboard"], ["xsel", "--clipboard", "--input"]]
        for cmd in candidates:
            if shutil.which(cmd[0]):
                subprocess.run(cmd, input=text, text=True, check=False)
                return

    def action_yank(self) -> None:
        if self._input_focused:
            return
        cmd = self._build_command()
        self._copy_to_clipboard(cmd)
        self.notify(f"Copied: {cmd}", timeout=4)

    def action_yank_quit(self) -> None:
        if self._input_focused:
            return
        cmd = self._build_command()
        self._copy_to_clipboard(cmd)
        self.exit(result=cmd)

    async def action_refresh(self) -> None:
        if self._input_focused:
            return
        self._refresh_data()
        self.notify("Refreshed", timeout=2)

    async def action_reload_config(self) -> None:
        """Re-read the config file and reapply it (R) — columns, presets,
        apex settings etc. The built query (presets' active state, SQL,
        filename, full-text) survives."""
        if self._input_focused:
            return
        if not self._config_path:
            self._set_status("[red]config: no file path known, cannot reload[/red]")
            return
        config, err = read_config(self._config_path)
        if config is None:
            self._set_status(f"[red]config: {err}[/red]")
            return

        self._apply_config(config)
        self._collection_dir = find_collection_dir(self.notes_dir)

        # Rebuild the preset list, carrying active selections over by label.
        active = {p.label for p in self.state.presets if p.active}
        self.state.presets = presets_from_config(config)
        for preset in self.state.presets:
            preset.active = preset.label in active
        container = self.query_one("#presets", Vertical)
        await container.remove_children()
        await container.mount_all(self._build_preset_items())

        self._update_query_bar()
        self._refresh_data()
        self.notify("Config reloaded", timeout=2)


# ---------------------------------------------------------------------------
# CLI entry point
# ---------------------------------------------------------------------------

def main() -> None:
    parser = argparse.ArgumentParser(
        prog="matterbase",
        description="Query-construction TUI over record sources (markdown / typst / jsonl).",
        formatter_class=argparse.RawDescriptionHelpFormatter,
        epilog=(
            "config (YAML):\n"
            "  notes_dir: ~/notes          # required\n"
            "  presets:                    # grubber query sets\n"
            "    - label: active\n"
            "      query: [\"status=active\"]\n"
            "  sql: \"...\"                  # default SQL WHERE\n"
            "  editor: hx\n"
            "  array_fields: [tags]\n"
        ),
    )
    parser.add_argument("--version", action="version", version=f"matterbase {__version__}")
    parser.add_argument("--config", metavar="CONFIG.YML", help="Path to YAML config file")
    parser.add_argument(
        "path", nargs="?", metavar="PATH",
        help="Directory to query (overrides notes_dir from config)",
    )
    parser.add_argument(
        "--depth", type=int, metavar="N", default=None,
        help="Limit directory recursion depth (0 = root only)",
    )
    args = parser.parse_args()

    if not args.config:
        parser.error("--config is required")

    ok, found_version = check_grubber_version()
    if not ok:
        if found_version == "not found":
            print(f"Error: grubber binary not found or not runnable: {GRUBBER_BIN}", file=sys.stderr)
            print("Install grubber or make sure it is on PATH.", file=sys.stderr)
        else:
            want = ".".join(map(str, MIN_GRUBBER_VERSION))
            print(f"Error: grubber {want} or newer is required (found: {found_version}).", file=sys.stderr)
        sys.exit(1)

    config = load_config(args.config)
    if args.path:
        p = Path(args.path).expanduser().resolve()
        if not p.is_dir():
            print(f"Error: not a directory: {p}", file=sys.stderr)
            sys.exit(1)
        config["notes_dir"] = str(p)
    if args.depth is not None:
        config["depth"] = args.depth

    result = MatterbaseApp(config).run()
    if result:
        print(result)


if __name__ == "__main__":
    main()
