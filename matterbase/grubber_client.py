"""grubber subprocess integration — no Textual dependency.

Ported from matterbase-next; reshaped for the record-centric model: the
replay helpers return *records* (dicts), never deduplicated file paths.
"""

import json
import os
import re
import subprocess
from pathlib import Path
from typing import Callable


# ---------------------------------------------------------------------------
# Binary resolution
#
# grubber is matterbase's actively-developed core engine; it is expected to be
# installed once and on PATH (like git or grep), not vendored per-consumer.
# Override with $GRUBBER if it lives somewhere non-standard.
# ---------------------------------------------------------------------------

# Minimum grubber matterbase relies on. 0.12.0 is the release with --merge-on
# (annotation + collection-index records collapse inside grubber, so the
# yankable command reproduces exactly what the table shows).
MIN_GRUBBER_VERSION = (0, 12, 0)


def _grubber_binary() -> str:
    """Resolve grubber: $GRUBBER override, else `grubber` on PATH."""
    return os.environ.get("GRUBBER") or "grubber"


GRUBBER_BIN = _grubber_binary()


def _parse_version(text: str) -> tuple[int, int, int] | None:
    """Pull an x.y[.z] version tuple out of `grubber --version` output."""
    m = re.search(r"(\d+)\.(\d+)(?:\.(\d+))?", text)
    if not m:
        return None
    return (int(m.group(1)), int(m.group(2)), int(m.group(3) or 0))


def check_grubber_version() -> tuple[bool, str]:
    """Probe grubber --version. Returns (meets_minimum, version_or_reason)."""
    try:
        probe = subprocess.run(
            [GRUBBER_BIN, "--version"], capture_output=True, timeout=2, check=False
        )
    except (FileNotFoundError, subprocess.TimeoutExpired, OSError):
        return (False, "not found")
    text = probe.stdout.decode().strip()
    parsed = _parse_version(text)
    if parsed is None:
        return (False, text or "unknown")
    return (parsed >= MIN_GRUBBER_VERSION, text)


# ---------------------------------------------------------------------------
# Core runner
# ---------------------------------------------------------------------------

def _run_grubber_cmd(
    cmd: list[str],
    array_fields: list[str] | None = None,
    on_error: Callable[[str], None] | None = None,
) -> list[dict]:
    """Run a grubber command and return parsed JSON records.

    on_error: optional callback(message) called when output can't be parsed,
    so callers can surface the problem in a status bar.
    """
    env = os.environ.copy()
    if array_fields:
        env["GRUBBER_ARRAY_FIELDS"] = ",".join(array_fields)
    try:
        result = subprocess.run(
            cmd, capture_output=True, text=True, encoding="utf-8",
            timeout=15, env=env, check=False,
        )
        if result.returncode != 0:
            if on_error and result.stderr.strip():
                on_error(f"grubber: {result.stderr.strip()[:120]}")
            return []
        return json.loads(result.stdout)
    except subprocess.TimeoutExpired:
        if on_error:
            on_error("grubber: timed out")
        return []
    except json.JSONDecodeError as exc:
        if on_error:
            on_error(f"grubber: invalid JSON output ({exc})")
        return []
    except (FileNotFoundError, OSError) as exc:
        if on_error:
            on_error(f"grubber: {exc}")
        return []


# ---------------------------------------------------------------------------
# Search-mode flags (shared by the cache builder)
# ---------------------------------------------------------------------------

_STREAM_MODE_FLAG = {
    "frontmatter": "--frontmatter-only",
    "blocks_only": "--blocks-only",
}


# ---------------------------------------------------------------------------
# In-session JSONL cache (extract once, filter by replay)
#
# The notes dir is scanned once per refresh into a JSONL file; filter changes
# replay it through `grubber --from-jsonl`. grubber stays the single filter
# authority (its -f operators are not reimplemented here). The session's
# search_mode/mmd/depth are baked into the cache at build time; replay only
# varies the -f filters. array_fields are applied at *replay* time.
#
# When a markbinder collection index is present (`<notes_dir>/collections/`),
# grubber is given `--from-jsonl=<collections_dir> --merge-on=id,binder`:
# index records are unioned into the scan, and records that appear in both the
# index and a Markdown annotation (same id + binder) collapse into one — the
# Markdown record wins, index-only fields (filename, kind) are back-filled.
# The merge lives in grubber itself so the yanked command reproduces it.
# ---------------------------------------------------------------------------

# The (id, binder) identity of markbinder collection records.
COLLECTION_MERGE_KEYS = "id,binder"


def find_collection_dir(notes_dir: str) -> str | None:
    """Return <notes_dir>/collections/ if it contains at least one *.jsonl file.

    Returns None when the collection index is absent so callers can skip the
    merge step entirely and fall back to the plain markdown-only path.
    """
    col_dir = Path(notes_dir) / "collections"
    if col_dir.is_dir() and any(col_dir.glob("*.jsonl")):
        return str(col_dir)
    return None


def extract_to_jsonl(
    notes_dir: str,
    out_path: str,
    *,
    search_mode: str = "all",
    mmd: bool = False,
    depth: int | None = None,
    collection_dir: str | None = None,
    on_error: Callable[[str], None] | None = None,
) -> bool:
    """Scan notes_dir once and write the full record set to out_path as JSONL.

    Returns True on success. The search_mode is baked in (fixed per session).

    When *collection_dir* is given (a directory containing ``*.jsonl`` index
    files), grubber unions those records into the scan via ``--from-jsonl``
    and collapses annotation + index entries for the same (id, binder) pair
    via ``--merge-on``.
    """
    mode_flag = _STREAM_MODE_FLAG.get(search_mode, "-a")
    cmd = [GRUBBER_BIN, "extract", notes_dir, mode_flag, "--format=jsonl"]
    if depth is not None:
        cmd += ["--depth", str(depth)]
    if mmd:
        cmd.append("--mmd")
    if collection_dir:
        cmd += ["--from-jsonl", collection_dir, "--merge-on", COLLECTION_MERGE_KEYS]
    try:
        result = subprocess.run(
            cmd, capture_output=True, text=True, encoding="utf-8",
            timeout=30, env=os.environ.copy(), check=False,
        )
        if result.returncode != 0:
            if on_error and result.stderr.strip():
                on_error(f"grubber: {result.stderr.strip()[:120]}")
            return False
        with open(out_path, "w", encoding="utf-8") as fh:
            fh.write(result.stdout)
        return True
    except subprocess.TimeoutExpired:
        if on_error:
            on_error("grubber: timed out building cache")
        return False
    except (FileNotFoundError, OSError) as exc:
        if on_error:
            on_error(f"grubber: {exc}")
        return False


def query_cached_records(
    cache_path: str,
    expressions: list[str] | None = None,
    *,
    array_fields: list[str] | None = None,
    on_error: Callable[[str], None] | None = None,
) -> list[dict]:
    """Replay the in-session cache through grubber with optional -f filters.

    Returns the matching *records* — the atomic unit of the unified view.
    All expressions go into a single grubber call (record-level AND), which is
    exactly what the yankable command reproduces. No notes_dir/search_mode/
    mmd/depth — those are baked into the cache at build time.
    """
    cmd = [GRUBBER_BIN, "extract", "--from-jsonl", cache_path]
    for expr in expressions or []:
        cmd.extend(["-f", expr])
    records = _run_grubber_cmd(cmd, array_fields, on_error)
    return records if isinstance(records, list) else []
