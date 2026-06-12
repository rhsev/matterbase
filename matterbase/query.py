"""The constructed query — matterbase's actual purpose. No Textual dependency.

QueryState holds the three channels of the query builder:

* grubber presets (the "sets") — become ``-f`` expressions,
* SQL WHERE — including filename search folded in as SQL (``_note_file LIKE``),
* full-text — a *display filter* with special status: it narrows what is shown
  but is never part of the yankable command (grubber | duckdb cannot express it).
"""

import re
import shlex
from dataclasses import dataclass, field
from pathlib import Path

from .content import source_type


@dataclass
class Preset:
    """A named grubber query ("set") from the config."""
    label: str
    exprs: list[str]
    active: bool = False


@dataclass
class QueryState:
    presets: list[Preset] = field(default_factory=list)
    sql_where: str = ""
    filename_term: str = ""
    fulltext_term: str = ""

    # ── grubber channel ──────────────────────────────────────────────

    def active_expressions(self) -> list[str]:
        """All active presets' expressions, flattened into one grubber call
        (record-level AND) — identical semantics in display and yank."""
        return [e for p in self.presets if p.active for e in p.exprs]

    # ── SQL channel (filename folded in) ─────────────────────────────

    def filename_clause(self) -> str:
        """The filename search expressed as SQL, or "" when inactive."""
        term = self.filename_term.strip()
        if not term:
            return ""
        escaped = term.replace("'", "''")
        return f"_note_file LIKE '%{escaped}%'"

    def effective_sql(self) -> str:
        """User SQL AND the filename clause — the WHERE the pipeline runs."""
        user = self.sql_where.strip()
        fname = self.filename_clause()
        if user and fname:
            return f"({user}) AND {fname}"
        return user or fname

    # ── full-text channel (display only, never yanked) ───────────────

    def fulltext_active(self) -> bool:
        return bool(self.fulltext_term.strip())

    # ── the constructed command ──────────────────────────────────────

    def build_command(
        self,
        notes_dir: str,
        *,
        search_mode: str = "all",
        mmd: bool = False,
        depth: int | None = None,
        collection_dir: str | None = None,
        array_fields: list[str] | None = None,
        grubber_set: str = "",
    ) -> str:
        """The yankable `grubber … | duckdb 'SQL'` pipeline.

        Reproduces the *structured* query only — full-text is deliberately
        absent (decision 1: yank ≠ displayed set when full-text is active).

        With *grubber_set*, the database definition (path, JSONL sources,
        merge keys) is assumed to live in the grubber config set of that
        name, and the command shrinks to `grubber extract --set NAME …`.
        The set must describe the same database as this session's config.
        """
        if grubber_set:
            parts = [
                shlex.quote(get_grubber_bin_name()), "extract",
                "--set", shlex.quote(grubber_set),
            ]
        else:
            parts = [shlex.quote(get_grubber_bin_name()), "extract", shlex.quote(notes_dir)]
        if search_mode == "frontmatter":
            parts.append("--frontmatter-only")
        elif search_mode == "blocks_only":
            parts.append("--blocks-only")
        else:
            parts.append("-a")
        if depth is not None:
            parts += ["--depth", str(depth)]
        if mmd:
            parts.append("--mmd")
        if collection_dir and not grubber_set:
            from .grubber_client import COLLECTION_MERGE_KEYS
            parts += ["--from-jsonl", shlex.quote(collection_dir)]
            parts += ["--merge-on", COLLECTION_MERGE_KEYS]
        for expr in self.active_expressions():
            parts += ["-f", shlex.quote(expr)]
        cmd = " ".join(parts)
        if array_fields:
            cmd = f"GRUBBER_ARRAY_FIELDS={','.join(array_fields)} {cmd}"
        sql = self.effective_sql()
        if sql:
            escaped_sql = sql.replace('"', '\\"')
            cmd += (
                f' | duckdb -json -c "SELECT * FROM'
                f" read_json_auto('/dev/stdin') WHERE {escaped_sql}\""
            )
        return cmd


def get_grubber_bin_name() -> str:
    # Late import so tests can monkeypatch grubber_client.GRUBBER_BIN.
    from . import grubber_client
    return grubber_client.GRUBBER_BIN


# ---------------------------------------------------------------------------
# SQL form helper
#
# The form (field → operator → value) is comfort only: it *generates* a WHERE
# clause into the SQL input, which stays the single source of truth and is
# freely editable afterwards.
# ---------------------------------------------------------------------------

SQL_FORM_OPERATORS = (
    "=", "!=", "LIKE", ">", "<", ">=", "<=", "IS NULL", "IS NOT NULL", "IN",
)

_SIMPLE_IDENT = re.compile(r"^[A-Za-z_][A-Za-z0-9_]*$")


def _sql_ident(field: str) -> str:
    """Quote a field name unless it is a plain SQL identifier."""
    if _SIMPLE_IDENT.match(field):
        return field
    return '"' + field.replace('"', '""') + '"'


def _sql_value(value: str) -> str:
    """Render a scalar: numbers stay bare, everything else single-quoted."""
    v = value.strip()
    try:
        float(v)
        return v
    except ValueError:
        return "'" + v.replace("'", "''") + "'"


def build_clause(field: str, op: str, value: str = "") -> str:
    """Build one WHERE clause from the form inputs. Returns "" when the
    combination is incomplete (no field, or a missing required value)."""
    field = field.strip()
    op = op.strip().upper()
    if not field or op not in SQL_FORM_OPERATORS:
        return ""
    ident = _sql_ident(field)

    if op in ("IS NULL", "IS NOT NULL"):
        return f"{ident} {op}"

    value = value.strip()
    if not value:
        return ""

    if op == "LIKE":
        # Substring search by default; explicit wildcards are kept as-is.
        pattern = value if "%" in value else f"%{value}%"
        return f"{ident} LIKE '" + pattern.replace("'", "''") + "'"

    if op == "IN":
        items = [v.strip() for v in value.split(",") if v.strip()]
        if not items:
            return ""
        return f"{ident} IN (" + ", ".join(_sql_value(v) for v in items) + ")"

    return f"{ident} {op} {_sql_value(value)}"


def append_clause(sql: str, clause: str) -> str:
    """AND a generated clause onto existing SQL (or start with it)."""
    sql = sql.strip()
    if not clause:
        return sql
    return f"{sql} AND {clause}" if sql else clause


def remove_last_clause(sql: str) -> str:
    """Strip the last top-level AND-clause; a single clause clears to "".

    Quote- and paren-aware so an ``AND`` inside a string literal or inside
    ``IN (…)`` does not count as a split point.
    """
    s = sql.strip()
    in_str = False
    depth = 0
    last = -1
    i = 0
    while i < len(s):
        c = s[i]
        if in_str:
            if c == "'":
                if i + 1 < len(s) and s[i + 1] == "'":
                    i += 1  # escaped '' stays inside the literal
                else:
                    in_str = False
        elif c == "'":
            in_str = True
        elif c == "(":
            depth += 1
        elif c == ")":
            depth -= 1
        elif depth == 0 and s[i : i + 5].upper() == " AND ":
            last = i
        i += 1
    if last < 0:
        return ""
    return s[:last].rstrip()


# ---------------------------------------------------------------------------
# Full-text display filter
#
# Searches prose + YAML-block body — what grubber | duckdb cannot express.
# Only markdown/typst records have a body; jsonl records drop out while
# full-text is active (decision 2). File contents are cached per refresh.
# ---------------------------------------------------------------------------

def filter_records_fulltext(
    records: list[dict],
    term: str,
    file_cache: dict[str, str] | None = None,
) -> list[dict]:
    """Keep only markdown/typst records whose source file contains *term*.

    file_cache maps path → lowercased content; populated lazily so each
    source file is read at most once per cache lifetime.
    """
    needle = term.strip().lower()
    if not needle:
        return records
    cache = file_cache if file_cache is not None else {}

    result: list[dict] = []
    for rec in records:
        path = rec.get("_note_file", "")
        if source_type(path) not in ("markdown", "typst"):
            continue
        content = cache.get(path)
        if content is None:
            try:
                content = Path(path).read_text(
                    encoding="utf-8", errors="replace"
                ).lower()
            except OSError:
                content = ""
            cache[path] = content
        if needle in content:
            result.append(rec)
    return result
