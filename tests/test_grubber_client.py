"""Unit tests for matterbase.grubber_client — on_error callback and query helpers."""

import json
import subprocess
from unittest.mock import MagicMock, patch

import pytest

from matterbase.grubber_client import (
    _run_grubber_cmd,
    extract_to_jsonl,
    query_cached_files,
    query_files,
    run_grubber,
)


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


# ---------------------------------------------------------------------------
# extract_to_jsonl — build the in-session cache
# ---------------------------------------------------------------------------

class TestExtractToJsonl:
    def test_writes_stdout_to_file_and_returns_true(self, tmp_path):
        out = tmp_path / "cache.jsonl"
        body = '{"_note_file":"/a.md","binder":"x"}\n'
        with patch("subprocess.run", return_value=_fake_run(body)):
            ok = extract_to_jsonl("/notes", str(out))
        assert ok is True
        assert out.read_text() == body

    def test_nonzero_returns_false_and_no_file(self, tmp_path):
        out = tmp_path / "cache.jsonl"
        errors = []
        err = MagicMock()
        err.stdout = ""
        err.stderr = "boom"
        err.returncode = 1
        with patch("subprocess.run", return_value=err):
            ok = extract_to_jsonl("/notes", str(out), on_error=errors.append)
        assert ok is False
        assert not out.exists()
        assert errors

    def test_command_shape_default_mode(self, tmp_path):
        captured = {}

        def fake_run(cmd, **kwargs):
            captured["cmd"] = cmd
            return _fake_run("")

        with patch("subprocess.run", side_effect=fake_run):
            extract_to_jsonl("/notes", str(tmp_path / "c.jsonl"))

        cmd = captured["cmd"]
        assert cmd[:3] == ["grubber", "extract", "/notes"]
        assert "-a" in cmd
        assert "--format=jsonl" in cmd

    def test_command_shape_mode_depth_mmd(self, tmp_path):
        captured = {}

        def fake_run(cmd, **kwargs):
            captured["cmd"] = cmd
            return _fake_run("")

        with patch("subprocess.run", side_effect=fake_run):
            extract_to_jsonl(
                "/notes", str(tmp_path / "c.jsonl"),
                search_mode="frontmatter", mmd=True, depth=3,
            )

        cmd = captured["cmd"]
        assert "--frontmatter-only" in cmd
        assert "--mmd" in cmd
        assert "--depth" in cmd and "3" in cmd

    def test_timeout_returns_false(self, tmp_path):
        errors = []
        with patch("subprocess.run", side_effect=subprocess.TimeoutExpired("grubber", 30)):
            ok = extract_to_jsonl("/notes", str(tmp_path / "c.jsonl"), on_error=errors.append)
        assert ok is False
        assert errors


# ---------------------------------------------------------------------------
# query_cached_files — replay the cache via --from-jsonl
# ---------------------------------------------------------------------------

class TestQueryCachedFiles:
    def test_no_queries_replays_all(self):
        calls = []

        def fake_cmd(cmd, *args, **kwargs):
            calls.append(cmd)
            return [{"_note_file": "/a.md"}, {"_note_file": "/b.md"}]

        with patch("matterbase.grubber_client._run_grubber_cmd", side_effect=fake_cmd):
            result = query_cached_files("/cache.jsonl", [], multi_select=False)

        assert result == ["/a.md", "/b.md"]
        cmd = calls[0]
        # sources from the cache, never re-scans a notes dir
        assert "--from-jsonl" in cmd and "/cache.jsonl" in cmd
        assert "-f" not in cmd

    def test_single_query_passes_f_and_uses_cache(self):
        calls = []

        def fake_cmd(cmd, *args, **kwargs):
            calls.append(cmd)
            return [{"_note_file": "/a.md"}]

        with patch("matterbase.grubber_client._run_grubber_cmd", side_effect=fake_cmd):
            query_cached_files("/cache.jsonl", [["binder=alpha"]], multi_select=False)

        cmd = calls[0]
        assert cmd[:4] == ["grubber", "extract", "--from-jsonl", "/cache.jsonl"]
        assert "-f" in cmd and "binder=alpha" in cmd

    def test_dedups_paths(self):
        records = [{"_note_file": "/a.md"}, {"_note_file": "/a.md"}, {"_note_file": "/b.md"}]
        with patch("matterbase.grubber_client._run_grubber_cmd", return_value=records):
            result = query_cached_files("/cache.jsonl", [], multi_select=False)
        assert result == ["/a.md", "/b.md"]

    def test_multi_select_intersects(self):
        def fake_cmd(cmd, *args, **kwargs):
            if "binder=alpha" in cmd:
                return [{"_note_file": "/a.md"}, {"_note_file": "/b.md"}]
            if "kind=pdf" in cmd:
                return [{"_note_file": "/b.md"}, {"_note_file": "/c.md"}]
            return []

        with patch("matterbase.grubber_client._run_grubber_cmd", side_effect=fake_cmd):
            result = query_cached_files(
                "/cache.jsonl",
                [["binder=alpha"], ["kind=pdf"]],
                multi_select=True,
            )
        assert result == ["/b.md"]

    def test_array_fields_forwarded(self):
        captured = {}

        def fake_cmd(cmd, array_fields=None, on_error=None):
            captured["array_fields"] = array_fields
            return [{"_note_file": "/a.md"}]

        with patch("matterbase.grubber_client._run_grubber_cmd", side_effect=fake_cmd):
            query_cached_files(
                "/cache.jsonl", [["binder=alpha"]], multi_select=False,
                array_fields=["tags"],
            )
        assert captured["array_fields"] == ["tags"]
