"""Unit tests for matterbase.grubber_client — runner, cache, merge, replay."""

import json
import subprocess
from unittest.mock import MagicMock, patch

from matterbase.grubber_client import (
    _run_grubber_cmd,
    extract_to_jsonl,
    find_collection_dir,
    query_cached_records,
)


def _fake_run(stdout: str, returncode: int = 0):
    result = MagicMock()
    result.stdout = stdout
    result.stderr = ""
    result.returncode = returncode
    return result


# ---------------------------------------------------------------------------
# _run_grubber_cmd
# ---------------------------------------------------------------------------

class TestRunGrubberCmd:
    def test_returns_parsed_json(self):
        records = [{"_note_file": "/a.md", "status": "active"}]
        with patch("subprocess.run", return_value=_fake_run(json.dumps(records))):
            result = _run_grubber_cmd(["grubber", "extract", "/notes", "-a"])
        assert result == records

    def test_nonzero_returncode_returns_empty(self):
        with patch("subprocess.run", return_value=_fake_run("", returncode=1)):
            assert _run_grubber_cmd(["grubber"]) == []

    def test_nonzero_calls_on_error_with_stderr(self):
        err = MagicMock(stdout="", stderr="some grubber error", returncode=1)
        errors = []
        with patch("subprocess.run", return_value=err):
            _run_grubber_cmd(["grubber"], on_error=errors.append)
        assert errors and "grubber" in errors[0]

    def test_invalid_json_calls_on_error(self):
        errors = []
        with patch("subprocess.run", return_value=_fake_run("not valid json")):
            result = _run_grubber_cmd(["grubber"], on_error=errors.append)
        assert result == []
        assert errors

    def test_timeout_calls_on_error(self):
        errors = []
        with patch("subprocess.run", side_effect=subprocess.TimeoutExpired("grubber", 15)):
            result = _run_grubber_cmd(["grubber"], on_error=errors.append)
        assert result == []
        assert errors

    def test_file_not_found_calls_on_error(self):
        errors = []
        with patch("subprocess.run", side_effect=FileNotFoundError("nope")):
            result = _run_grubber_cmd(["grubber"], on_error=errors.append)
        assert result == []
        assert errors

    def test_array_fields_set_in_env(self):
        captured_env = {}

        def fake_run(cmd, **kwargs):
            captured_env.update(kwargs.get("env", {}))
            return _fake_run("[]")

        with patch("subprocess.run", side_effect=fake_run):
            _run_grubber_cmd(["grubber"], array_fields=["tags", "keywords"])
        assert captured_env.get("GRUBBER_ARRAY_FIELDS") == "tags,keywords"


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

    def test_nonzero_returns_false(self, tmp_path):
        out = tmp_path / "cache.jsonl"
        errors = []
        err = MagicMock(stdout="", stderr="boom", returncode=1)
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
        assert cmd[1:3] == ["extract", "/notes"]
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

    def test_collection_dir_adds_from_jsonl_and_merge_on(self, tmp_path):
        """The (id, binder) merge happens inside grubber via --merge-on."""
        captured = {}

        def fake_run(cmd, **kwargs):
            captured["cmd"] = cmd
            return _fake_run('{"id":"a","binder":"T","_note_file":"/n/b.md"}\n')

        with patch("subprocess.run", side_effect=fake_run):
            extract_to_jsonl("/notes", str(tmp_path / "c.jsonl"),
                             collection_dir="/notes/collections")
        cmd = captured["cmd"]
        assert "--from-jsonl" in cmd and "/notes/collections" in cmd
        assert "--merge-on" in cmd and "id,binder" in cmd

    def test_without_collection_dir_no_merge_flags(self, tmp_path):
        captured = {}

        def fake_run(cmd, **kwargs):
            captured["cmd"] = cmd
            return _fake_run("")

        with patch("subprocess.run", side_effect=fake_run):
            extract_to_jsonl("/notes", str(tmp_path / "c.jsonl"))
        assert "--from-jsonl" not in captured["cmd"]
        assert "--merge-on" not in captured["cmd"]


# ---------------------------------------------------------------------------
# query_cached_records — record-level replay
# ---------------------------------------------------------------------------

class TestQueryCachedRecords:
    def test_no_expressions_replays_all(self):
        calls = []

        def fake_cmd(cmd, *args, **kwargs):
            calls.append(cmd)
            return [{"_note_file": "/a.md"}, {"_note_file": "/a.md", "x": 1}]

        with patch("matterbase.grubber_client._run_grubber_cmd", side_effect=fake_cmd):
            result = query_cached_records("/cache.jsonl")

        # records, not deduplicated paths — two records from one file survive
        assert len(result) == 2
        cmd = calls[0]
        assert "--from-jsonl" in cmd and "/cache.jsonl" in cmd
        assert "-f" not in cmd

    def test_expressions_flattened_into_one_call(self):
        calls = []

        def fake_cmd(cmd, *args, **kwargs):
            calls.append(cmd)
            return []

        with patch("matterbase.grubber_client._run_grubber_cmd", side_effect=fake_cmd):
            query_cached_records("/c.jsonl", ["binder=alpha", "kind=pdf"])

        assert len(calls) == 1
        cmd = calls[0]
        assert cmd.count("-f") == 2
        assert "binder=alpha" in cmd and "kind=pdf" in cmd

    def test_array_fields_forwarded(self):
        captured = {}

        def fake_cmd(cmd, array_fields=None, on_error=None):
            captured["array_fields"] = array_fields
            return []

        with patch("matterbase.grubber_client._run_grubber_cmd", side_effect=fake_cmd):
            query_cached_records("/c.jsonl", ["x=1"], array_fields=["tags"])
        assert captured["array_fields"] == ["tags"]


# ---------------------------------------------------------------------------
# find_collection_dir
# ---------------------------------------------------------------------------

class TestFindCollectionDir:
    def test_returns_path_when_jsonl_exists(self, tmp_path):
        col = tmp_path / "collections"
        col.mkdir()
        (col / "inbox.jsonl").write_text('{"type":"ref"}\n')
        assert find_collection_dir(str(tmp_path)) == str(col)

    def test_returns_none_when_no_collections_dir(self, tmp_path):
        assert find_collection_dir(str(tmp_path)) is None

    def test_returns_none_when_collections_dir_has_no_jsonl(self, tmp_path):
        col = tmp_path / "collections"
        col.mkdir()
        (col / "readme.txt").write_text("nothing")
        assert find_collection_dir(str(tmp_path)) is None


# The (id, binder) merge semantics themselves are grubber's responsibility
# (--merge-on) and are tested in the grubber repo (merge_test.go).
