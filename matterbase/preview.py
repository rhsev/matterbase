"""Adaptive preview rendering — rich renderables, no Textual dependency.

One *global* mode — whole / compact / record — set once and applied to
whichever record is selected, by the record's source type (decision 3):

* markdown / typst → whole-file render, or compact (the YAML block + its
  surrounding markdown context), or the record's fields.
* jsonl → the record's fields, whatever the mode (there is no body to render).
"""

import os
from pathlib import Path

from rich.markup import escape as markup_escape
from rich.text import Text

from .content import (
    ApexConfig,
    extract_section_for_record,
    render_with_apex,
    render_with_bat,
    source_type,
    split_frontmatter,
)

PREVIEW_MODES = ("whole", "compact", "record")


def next_mode(mode: str) -> str:
    i = PREVIEW_MODES.index(mode) if mode in PREVIEW_MODES else 0
    return PREVIEW_MODES[(i + 1) % len(PREVIEW_MODES)]


def render_record_fields(record: dict) -> str:
    """The record as a field form (rich markup) — the jsonl/'record' view."""
    lines: list[str] = []
    for k, v in record.items():
        # None fields are replay-union artifacts (grubber fills the union
        # schema with nulls), not record content — skip them.
        if k.startswith("_") or v is None:
            continue
        if isinstance(v, list):
            v = ", ".join(str(x) for x in v)
        lines.append(
            f"[bold cyan]{markup_escape(str(k))}[/bold cyan]  "
            f"{markup_escape(str(v))}"
        )
    src = record.get("_note_file", "")
    if src:
        lines.append("")
        lines.append(f"[dim]source  {markup_escape(src)}[/dim]")
    return "\n".join(lines) if lines else "[dim](empty record)[/dim]"


def _raw_with_dimmed_frontmatter(content: str, mmd: bool) -> str:
    fm, body, _ = split_frontmatter(content, mmd=mmd)
    if fm:
        return (
            f"[dim]---\n{markup_escape(fm)}\n---[/dim]\n\n"
            f"{markup_escape(body.lstrip(chr(10)))}"
        )
    return markup_escape(content)


def _render_whole(path: str, apex_cfg: ApexConfig, mmd: bool):
    stype = source_type(path)
    if stype == "markdown":
        ansi = render_with_apex(path, apex_cfg)
        if ansi:
            return Text.from_ansi(ansi)
    bat = render_with_bat(path)
    if bat:
        return Text.from_ansi(bat)
    try:
        content = Path(path).read_text(encoding="utf-8", errors="replace")
    except OSError as exc:
        return f"[red]Error reading file:[/red] {markup_escape(str(exc))}"
    return _raw_with_dimmed_frontmatter(content, mmd)


def _render_compact(record: dict, path: str, apex_cfg: ApexConfig, mmd: bool):
    """The record's YAML block + its surrounding markdown context."""
    try:
        content = Path(path).read_text(encoding="utf-8", errors="replace")
    except OSError as exc:
        return f"[red]Error reading file:[/red] {markup_escape(str(exc))}"

    parts: list[str] = []
    fm, body, fm_keys = split_frontmatter(content, mmd=mmd)
    if fm:
        parts.append(f"---\n{fm}\n---")

    section = extract_section_for_record(body, record, frontmatter_keys=fm_keys)
    if section:
        parts.append(section)

    md_text = "\n\n".join(parts)
    if not md_text:
        # Frontmatter-only record or no matching block — fall back to fields.
        return render_record_fields(record)

    ansi = render_with_apex(path="", cfg=apex_cfg, tmp_content=md_text)
    if ansi:
        return Text.from_ansi(ansi)
    return markup_escape(md_text)


def render_preview(
    record: dict,
    mode: str,
    apex_cfg: ApexConfig,
    *,
    mmd: bool = False,
):
    """Render *record* in the global *mode*, adapted to its source type.

    Returns (title, renderable) — renderable is a rich Text or markup string.
    """
    path = record.get("_note_file", "")
    stype = source_type(path)
    title = os.path.basename(path) if path else "record"

    if stype == "jsonl" or mode == "record" or not path:
        return title, render_record_fields(record)

    if not Path(path).exists():
        return title, "[dim](file not found)[/dim]"

    if mode == "whole":
        return title, _render_whole(path, apex_cfg, mmd)

    return title, _render_compact(record, path, apex_cfg, mmd)
