"""Unit tests for matterbase.grubber_client — on_error callback and query helpers."""

import json
import subprocess
from unittest.mock import MagicMock, patch

import pytest

from matterbase.grubber_client import _run_grubber_cmd, query_files, run_grubber


# ---------------------------------------------------------------------------
# _run_grubber_cmd
# ---------------------------------------------------------------------------

def _fake_run(stdout: str, returncode: int = 0):
    """Return a mock subprocess.CompletedProcess."""
    result = MagicMock()
    result.stdout = stdout
    result.stderr = ""
    result.returncode = returncode
    return result


class TestRunGrubberCmd:
    def test_returns_parsed_json(self):
        records = [{"_note_file": "/a.md", "status": "active"}]
        with patch("subprocess.run", return_value=_fake_run(json.dumps(records))):
            result = _run_grubber_cmd(["grubber", "extract", "/notes", "--all"])
        assert result == records

    def test_nonzero_returncode_returns_empty(self):
        with patch("subprocess.run", return_value=_fake_run("", returncode=1)):
            result = _run_grubber_cmd(["grubber", "extract", "/notes", "--all"])
        assert result == []

    def test_nonzero_calls_on_error_with_stderr(self):
        err_result = MagicMock()
        err_result.stdout = ""
        err_result.stderr = "some grubber error"
        err_result.returncode = 1
        errors = []
        with patch("subprocess.run", return_value=err_result):
            _run_grubber_cmd(["grubber"], on_error=errors.append)
        assert errors and "grubber" in errors[0]

    def test_invalid_json_calls_on_error(self):
        errors = []
        with patch("subprocess.run", return_value=_fake_run("not valid json")):
            result = _run_grubber_cmd(["grubber"], on_error=errors.append)
        assert result == []
        assert errors

    def test_invalid_json_without_on_error_returns_empty(self):
        with patch("subprocess.run", return_value=_fake_run("not valid json")):
            result = _run_grubber_cmd(["grubber"])
        assert result == []

    def test_timeout_calls_on_error(self):
        errors = []
        with patch("subprocess.run", side_effect=subprocess.TimeoutExpired("grubber", 15)):
            result = _run_grubber_cmd(["grubber"], on_error=errors.append)
        assert result == []
        assert errors

    def test_file_not_found_calls_on_error(self):
        errors = []
        with patch("subprocess.run", side_effect=FileNotFoundError("grubber not found")):
            result = _run_grubber_cmd(["grubber"], on_error=errors.append)
        assert result == []
        assert errors

    def test_array_fields_set_in_env(self):
        records = [{"_note_file": "/a.md"}]
        captured_env = {}

        def fake_run(cmd, **kwargs):
            captured_env.update(kwargs.get("env", {}))
            return _fake_run(json.dumps(records))

        with patch("subprocess.run", side_effect=fake_run):
            _run_grubber_cmd(["grubber"], array_fields=["tags", "keywords"])

        assert captured_env.get("GRUBBER_ARRAY_FIELDS") == "tags,keywords"

    def test_empty_array_does_not_set_env(self):
        records = [{"_note_file": "/a.md"}]
        captured_env = {}

        def fake_run(cmd, **kwargs):
            captured_env.update(kwargs.get("env", {}))
            return _fake_run(json.dumps(records))

        with patch("subprocess.run", side_effect=fake_run):
            _run_grubber_cmd(["grubber"], array_fields=[])

        assert "GRUBBER_ARRAY_FIELDS" not in captured_env


# ---------------------------------------------------------------------------
# run_grubber — deduplication and search_mode flags
# ---------------------------------------------------------------------------

class TestRunGrubber:
    def _patch(self, records):
        return patch(
            "matterbase.grubber_client._run_grubber_cmd",
            return_value=records,
        )

    def test_returns_unique_paths(self):
        records = [
            {"_note_file": "/a.md"},
            {"_note_file": "/a.md"},  # duplicate
            {"_note_file": "/b.md"},
        ]
        with self._patch(records):
            result = run_grubber("/notes")
        assert result == ["/a.md", "/b.md"]

    def test_skips_records_without_note_file(self):
        records = [{"status": "active"}, {"_note_file": "/a.md"}]
        with self._patch(records):
            result = run_grubber("/notes")
        assert result == ["/a.md"]

    def test_frontmatter_mode_passes_flag(self):
        calls = []

        def fake_cmd(cmd, *args, **kwargs):
            calls.append(cmd)
            return []

        with patch("matterbase.grubber_client._run_grubber_cmd", side_effect=fake_cmd):
            run_grubber("/notes", search_mode="frontmatter")

        assert any("--frontmatter-only" in c for c in calls)

    def test_blocks_only_mode_passes_flag(self):
        calls = []

        def fake_cmd(cmd, *args, **kwargs):
            calls.append(cmd)
            return []

        with patch("matterbase.grubber_client._run_grubber_cmd", side_effect=fake_cmd):
            run_grubber("/notes", search_mode="blocks_only")

        assert any("--blocks-only" in c for c in calls)

    def test_expressions_added_as_f_flags(self):
        calls = []

        def fake_cmd(cmd, *args, **kwargs):
            calls.append(cmd)
            return []

        with patch("matterbase.grubber_client._run_grubber_cmd", side_effect=fake_cmd):
            run_grubber("/notes", expressions=["status=active", "project=x"])

        flat = [item for c in calls for item in c]
        assert flat.count("-f") == 2
        assert "status=active" in flat
        assert "project=x" in flat

    def test_depth_flag_included(self):
        calls = []

        def fake_cmd(cmd, *args, **kwargs):
            calls.append(cmd)
            return []

        with patch("matterbase.grubber_client._run_grubber_cmd", side_effect=fake_cmd):
            run_grubber("/notes", depth=2)

        flat = [item for c in calls for item in c]
        assert "--depth" in flat
        assert "2" in flat

    def test_mmd_flag_included(self):
        calls = []

        def fake_cmd(cmd, *args, **kwargs):
            calls.append(cmd)
            return []

        with patch("matterbase.grubber_client._run_grubber_cmd", side_effect=fake_cmd):
            run_grubber("/notes", mmd=True)

        flat = [item for c in calls for item in c]
        assert "--mmd" in flat


# ---------------------------------------------------------------------------
# query_files — multi-select intersection logic
# ---------------------------------------------------------------------------

class TestQueryFiles:
    def _patch(self, mapping: dict):
        """mapping: expressions tuple → list of paths returned."""

        def fake_run_grubber(notes_dir, expressions=None, **kwargs):
            key = tuple(expressions or [])
            return mapping.get(key, [])

        return patch("matterbase.grubber_client.run_grubber", side_effect=fake_run_grubber)

    def test_no_active_queries_returns_all(self):
        with patch(
            "matterbase.grubber_client.run_grubber", return_value=["/a.md", "/b.md"]
        ):
            result = query_files("/notes", [], multi_select=False)
        assert result == ["/a.md", "/b.md"]

    def test_single_button_passes_expressions(self):
        with patch(
            "matterbase.grubber_client.run_grubber", return_value=["/a.md"]
        ) as mock:
            query_files("/notes", [["status=active"]], multi_select=False)
        mock.assert_called_once()
        _, kwargs = mock.call_args
        # expressions passed as positional or keyword
        args = mock.call_args[0]
        assert "status=active" in (args[1] if len(args) > 1 else kwargs.get("expressions", []))

    def test_multi_select_intersects(self):
        mapping = {
            ("status=active",): ["/a.md", "/b.md"],
            ("project=x",): ["/b.md", "/c.md"],
        }
        with self._patch(mapping):
            result = query_files(
                "/notes",
                [["status=active"], ["project=x"]],
                multi_select=True,
            )
        assert result == ["/b.md"]

    def test_multi_select_false_flattens_expressions(self):
        calls = []

        def fake_run_grubber(notes_dir, expressions=None, **kwargs):
            calls.append(expressions)
            return []

        with patch("matterbase.grubber_client.run_grubber", side_effect=fake_run_grubber):
            query_files(
                "/notes",
                [["status=active"], ["project=x"]],
                multi_select=False,
            )

        assert calls
        flat = calls[0]
        assert "status=active" in flat
        assert "project=x" in flat
