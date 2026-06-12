"""The record pipeline: cache replay → DuckDB SQL → full-text display filter.

No Textual dependency; the app runs this in a background worker.
"""

import json
import os
import tempfile
from dataclasses import dataclass
from typing import Callable

import duckdb

from .grubber_client import query_cached_records
from .query import QueryState, filter_records_fulltext


@dataclass
class PipelineResult:
    records: list[dict]
    structured_count: int  # count before the full-text display filter
    error: str | None = None


def apply_sql(records: list[dict], where: str) -> tuple[list[dict], str | None]:
    """Filter *records* with a DuckDB WHERE clause. Returns (records, error)."""
    where = where.strip()
    if not where or not records:
        return records, None
    tmp = None
    try:
        with tempfile.NamedTemporaryFile(
            mode="w", suffix=".json", delete=False, encoding="utf-8"
        ) as f:
            json.dump(records, f, ensure_ascii=False, default=str)
            tmp = f.name
        safe_path = tmp.replace("'", "''")
        con = duckdb.connect()
        result = con.execute(
            f"SELECT * FROM read_json_auto('{safe_path}') WHERE {where}"
        )
        cols = [d[0] for d in result.description]
        return [dict(zip(cols, row)) for row in result.fetchall()], None
    except duckdb.Error as e:
        return records, f"SQL: {str(e).splitlines()[0]}"
    finally:
        if tmp:
            try:
                os.unlink(tmp)
            except OSError:
                pass


def run_pipeline(
    cache_path: str,
    state: QueryState,
    *,
    array_fields: list[str] | None = None,
    fulltext_cache: dict[str, str] | None = None,
    on_error: Callable[[str], None] | None = None,
) -> PipelineResult:
    """Run the full chain over the in-session cache.

    1. grubber replay with the active preset expressions (record-level AND),
    2. DuckDB WHERE (user SQL with the filename clause folded in),
    3. full-text display filter (markdown/typst body search; not yankable).
    """
    records = query_cached_records(
        cache_path,
        state.active_expressions(),
        array_fields=array_fields,
        on_error=on_error,
    )

    error: str | None = None
    sql = state.effective_sql()
    if sql:
        records, error = apply_sql(records, sql)

    structured_count = len(records)
    if state.fulltext_active():
        records = filter_records_fulltext(
            records, state.fulltext_term, fulltext_cache
        )

    return PipelineResult(records, structured_count, error)
