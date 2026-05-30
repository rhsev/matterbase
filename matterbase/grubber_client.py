"""grubber subprocess integration — no Textual dependency."""

import json
import os
import re
import subprocess
from collections.abc import Iterator
from pathlib import Path


# ---------------------------------------------------------------------------
# Binary resolution
#
# grubber is matterbase's actively-developed core engine; it is expected to be
# installed once and on PATH (like git or grep), not vendored per-consumer.
# Bundling a fast-moving tool only guarantees version drift. Override with
# $GRUBBER if it lives somewhere non-standard.
# ---------------------------------------------------------------------------

# Minimum grubber matterbase relies on. 0.10.0 brings --from-ndjson, needed to
# read grubber-collection's NDJSON inbox.
MIN_GRUBBER_VERSION = (0, 10, 0)


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
            [GRUBBER_BIN, "--version"], capture_output=True, timeout=5
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
    on_error: "callable[[str], None] | None" = None,
) -> list[dict]:
    """Run a grubber command and return parsed JSON records.

    on_error: optional callback(message) called when output can't be parsed,
    so callers can surface the problem in a status bar.
    """
    env = os.environ.copy()
    if array_fields:
        env["GRUBBER_ARRAY_FIELDS"] = ",".join(array_fields)
    try:
        result = subprocess.run(cmd, capture_output=True, text=True, encoding="utf-8", timeout=15, env=env)
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
# High-level query helpers
# ---------------------------------------------------------------------------

def run_grubber(
    notes_dir: str,
    expressions: list[str] | None = None,
    search_mode: str = "all",
    array_fields: list[str] | None = None,
    mmd: bool = False,
    depth: int | None = None,
    on_error: "callable[[str], None] | None" = None,
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
        records = _run_grubber_cmd(base_cmd("--frontmatter-only"), af, on_error)
    elif search_mode == "blocks_only":
        records = _run_grubber_cmd(base_cmd("--blocks-only"), af, on_error)
    else:
        records = _run_grubber_cmd(base_cmd("-a"), af, on_error)

    seen: set[str] = set()
    paths: list[str] = []
    for record in records:
        file_path = record.get("_note_file", "")
        if file_path and file_path not in seen:
            seen.add(file_path)
            paths.append(file_path)
    return paths


# ---------------------------------------------------------------------------
# NDJSON streaming
# ---------------------------------------------------------------------------

_STREAM_MODE_FLAG = {
    "frontmatter": "--frontmatter-only",
    "blocks_only": "--blocks-only",
}

STREAM_BATCH_SIZE = 25


def _stream_grubber_records(
    cmd: list[str],
    array_fields: list[str] | None = None,
    on_error: "callable[[str], None] | None" = None,
) -> Iterator[dict]:
    """Yield grubber records one by one from an NDJSON stream."""
    env = os.environ.copy()
    if array_fields:
        env["GRUBBER_ARRAY_FIELDS"] = ",".join(array_fields)
    try:
        proc = subprocess.Popen(
            cmd,
            stdout=subprocess.PIPE,
            stderr=subprocess.PIPE,
            text=True,
            env=env,
        )
        for line in proc.stdout:
            line = line.strip()
            if not line:
                continue
            try:
                yield json.loads(line)
            except json.JSONDecodeError as exc:
                if on_error:
                    on_error(f"grubber: invalid NDJSON ({exc})")
        proc.wait()
        if proc.returncode != 0:
            stderr = proc.stderr.read().strip()
            if on_error and stderr:
                on_error(f"grubber: {stderr[:120]}")
    except FileNotFoundError:
        if on_error:
            on_error("grubber: binary not found")
    except OSError as exc:
        if on_error:
            on_error(f"grubber: {exc}")


def stream_grubber_paths(
    notes_dir: str,
    expressions: list[str] | None = None,
    search_mode: str = "all",
    array_fields: list[str] | None = None,
    mmd: bool = False,
    depth: int | None = None,
    on_error: "callable[[str], None] | None" = None,
) -> Iterator[str]:
    """Yield unique file paths from grubber via NDJSON as they are discovered."""
    mode_flag = _STREAM_MODE_FLAG.get(search_mode, "-a")
    cmd = [GRUBBER_BIN, "extract", notes_dir, mode_flag, "--format=ndjson"]
    if depth is not None:
        cmd += ["--depth", str(depth)]
    if mmd:
        cmd.append("--mmd")
    if expressions:
        for expr in expressions:
            cmd.extend(["-f", expr])

    seen: set[str] = set()
    for record in _stream_grubber_records(cmd, array_fields, on_error):
        path = record.get("_note_file", "")
        if path and path not in seen:
            seen.add(path)
            yield path


def query_files(
    notes_dir: str,
    active_queries: list[list[str]],
    multi_select: bool,
    search_mode: str = "all",
    array_fields: list[str] | None = None,
    mmd: bool = False,
    depth: int | None = None,
    on_error: "callable[[str], None] | None" = None,
) -> list[str]:
    """Return file list for active filter buttons; multiple buttons AND-intersect."""
    kw = dict(
        search_mode=search_mode,
        array_fields=array_fields,
        mmd=mmd,
        depth=depth,
        on_error=on_error,
    )

    if not active_queries:
        return run_grubber(notes_dir, **kw)

    if multi_select and len(active_queries) > 1:
        sets: list[set[str]] = [set(run_grubber(notes_dir, q, **kw)) for q in active_queries]
        combined = sets[0]
        for s in sets[1:]:
            combined &= s
        return sorted(combined)

    all_exprs = [expr for q in active_queries for expr in q]
    return run_grubber(notes_dir, all_exprs, **kw)


