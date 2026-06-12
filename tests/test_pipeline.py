"""Unit tests for matterbase.pipeline — SQL stage (real DuckDB) and the chain."""

from unittest.mock import patch

from matterbase.pipeline import apply_sql, run_pipeline
from matterbase.query import Preset, QueryState


RECORDS = [
    {"_note_file": "/n/a.md", "status": "active", "amount": 50},
    {"_note_file": "/n/b.md", "status": "done", "amount": 200},
    {"_note_file": "/n/collections/inbox.jsonl", "status": "active", "amount": 999},
]


# ---------------------------------------------------------------------------
# apply_sql
# ---------------------------------------------------------------------------

class TestApplySql:
    def test_empty_where_passthrough(self):
        records, err = apply_sql(RECORDS, "")
        assert records == RECORDS
        assert err is None

    def test_filters_by_field(self):
        records, err = apply_sql(RECORDS, "status = 'active'")
        assert err is None
        assert {r["_note_file"] for r in records} == {
            "/n/a.md", "/n/collections/inbox.jsonl"
        }

    def test_comparison(self):
        records, err = apply_sql(RECORDS, "amount > 100")
        assert err is None
        assert len(records) == 2

    def test_filename_like_clause(self):
        records, err = apply_sql(RECORDS, "_note_file LIKE '%a.md%'")
        assert err is None
        assert len(records) == 1

    def test_invalid_sql_returns_error_and_unfiltered(self):
        records, err = apply_sql(RECORDS, "no_such_column ===")
        assert err is not None and err.startswith("SQL:")
        assert records == RECORDS

    def test_empty_records_passthrough(self):
        records, err = apply_sql([], "status = 'x'")
        assert records == []
        assert err is None


# ---------------------------------------------------------------------------
# run_pipeline — replay → SQL → full-text
# ---------------------------------------------------------------------------

class TestRunPipeline:
    def _patch_replay(self, records):
        return patch(
            "matterbase.pipeline.query_cached_records", return_value=records
        )

    def test_plain_replay(self):
        with self._patch_replay(RECORDS) as mock:
            result = run_pipeline("/cache.jsonl", QueryState())
        assert result.records == RECORDS
        assert result.structured_count == 3
        assert result.error is None
        mock.assert_called_once()

    def test_preset_expressions_forwarded(self):
        state = QueryState(presets=[Preset("a", ["status=active"], active=True)])
        with self._patch_replay([]) as mock:
            run_pipeline("/cache.jsonl", state)
        assert mock.call_args[0][1] == ["status=active"]

    def test_sql_stage_applied(self):
        state = QueryState(sql_where="amount > 100")
        with self._patch_replay(RECORDS):
            result = run_pipeline("/cache.jsonl", state)
        assert len(result.records) == 2

    def test_fulltext_narrows_display_after_sql(self, tmp_path):
        md = tmp_path / "a.md"
        md.write_text("contains the needle", encoding="utf-8")
        records = [
            {"_note_file": str(md), "status": "active"},
            {"_note_file": "/n/inbox.jsonl", "status": "active"},
        ]
        state = QueryState(fulltext_term="needle")
        with self._patch_replay(records):
            result = run_pipeline("/cache.jsonl", state)
        # structured count is pre-full-text; display is narrowed; jsonl drops out
        assert result.structured_count == 2
        assert len(result.records) == 1
        assert result.records[0]["_note_file"] == str(md)

    def test_sql_error_surfaced(self):
        state = QueryState(sql_where="bogus ===")
        with self._patch_replay(RECORDS):
            result = run_pipeline("/cache.jsonl", state)
        assert result.error is not None
