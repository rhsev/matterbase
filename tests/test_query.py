"""Unit tests for matterbase.query — the constructed query and its channels."""

from matterbase.query import (
    Preset,
    QueryState,
    append_clause,
    build_clause,
    filter_records_fulltext,
    remove_last_clause,
)


def _state(**kwargs) -> QueryState:
    return QueryState(**kwargs)


# ---------------------------------------------------------------------------
# Preset channel
# ---------------------------------------------------------------------------

class TestActiveExpressions:
    def test_inactive_presets_ignored(self):
        s = _state(presets=[Preset("a", ["x=1"]), Preset("b", ["y=2"])])
        assert s.active_expressions() == []

    def test_active_presets_flattened_in_order(self):
        s = _state(presets=[
            Preset("a", ["x=1", "x2=1"], active=True),
            Preset("b", ["y=2"], active=False),
            Preset("c", ["z=3"], active=True),
        ])
        assert s.active_expressions() == ["x=1", "x2=1", "z=3"]


# ---------------------------------------------------------------------------
# SQL channel — filename folded in as SQL
# ---------------------------------------------------------------------------

class TestEffectiveSql:
    def test_empty(self):
        assert _state().effective_sql() == ""

    def test_user_sql_only(self):
        s = _state(sql_where="status != 'done'")
        assert s.effective_sql() == "status != 'done'"

    def test_filename_only_becomes_like_clause(self):
        s = _state(filename_term="2025")
        assert s.effective_sql() == "_note_file LIKE '%2025%'"

    def test_filename_and_sql_combined_with_and(self):
        s = _state(sql_where="status = 'x'", filename_term="rep")
        assert s.effective_sql() == "(status = 'x') AND _note_file LIKE '%rep%'"

    def test_filename_quote_escaped(self):
        s = _state(filename_term="o'brien")
        assert "o''brien" in s.effective_sql()

    def test_whitespace_only_filename_inactive(self):
        assert _state(filename_term="   ").effective_sql() == ""


# ---------------------------------------------------------------------------
# The constructed (yankable) command
# ---------------------------------------------------------------------------

class TestBuildCommand:
    def test_baseline_command(self):
        cmd = _state().build_command("/notes")
        assert cmd.startswith("grubber extract /notes -a")
        assert "duckdb" not in cmd

    def test_presets_become_f_flags(self):
        s = _state(presets=[Preset("a", ["status=active"], active=True)])
        cmd = s.build_command("/notes")
        assert "-f status=active" in cmd

    def test_sql_appends_duckdb_stage(self):
        s = _state(sql_where="amount > 100")
        cmd = s.build_command("/notes")
        assert "| duckdb -json -c" in cmd
        assert "WHERE amount > 100" in cmd
        assert "read_json_auto('/dev/stdin')" in cmd

    def test_filename_search_is_in_the_yank_as_sql(self):
        s = _state(filename_term="2025")
        cmd = s.build_command("/notes")
        assert "WHERE _note_file LIKE '%2025%'" in cmd

    def test_fulltext_never_in_yank(self):
        """Decision 1: full-text narrows the display only."""
        s = _state(fulltext_term="needle", sql_where="x = 1")
        cmd = s.build_command("/notes")
        assert "needle" not in cmd

    def test_collection_dir_included_with_merge_on(self):
        cmd = _state().build_command("/notes", collection_dir="/notes/collections")
        assert "--from-jsonl /notes/collections" in cmd
        assert "--merge-on id,binder" in cmd

    def test_options_included(self):
        cmd = _state().build_command(
            "/notes", search_mode="frontmatter", mmd=True, depth=2,
            array_fields=["tags"],
        )
        assert "--frontmatter-only" in cmd
        assert "--mmd" in cmd
        assert "--depth 2" in cmd
        assert cmd.startswith("GRUBBER_ARRAY_FIELDS=tags ")

    def test_notes_dir_with_space_quoted(self):
        cmd = _state().build_command("/my notes")
        assert "'/my notes'" in cmd

    def test_grubber_set_shrinks_command(self):
        """With a config set, the database definition stays in grubber's
        config — no path, no --from-jsonl/--merge-on in the yank."""
        s = _state(
            presets=[Preset("a", ["binder=x"], active=True)],
            sql_where="kind = 'pdf'",
        )
        cmd = s.build_command(
            "/notes", collection_dir="/notes/collections", grubber_set="notes",
        )
        assert cmd.startswith("grubber extract --set notes -a")
        assert "/notes/collections" not in cmd
        assert "--merge-on" not in cmd
        assert "-f binder=x" in cmd
        assert "WHERE kind = 'pdf'" in cmd

    def test_grubber_set_keeps_explicit_overrides(self):
        """depth/mmd are CLI-wins options — kept so yank ≡ display."""
        cmd = _state().build_command("/notes", mmd=True, depth=2, grubber_set="notes")
        assert "--mmd" in cmd and "--depth 2" in cmd


# ---------------------------------------------------------------------------
# Full-text display filter (decision 2: markdown/typst only)
# ---------------------------------------------------------------------------

class TestFilterRecordsFulltext:
    def _files(self, tmp_path):
        md = tmp_path / "a.md"
        md.write_text("# Title\n\nThe needle is here.\n", encoding="utf-8")
        typ = tmp_path / "b.typ"
        typ.write_text("= Doc\nNothing relevant.\n", encoding="utf-8")
        return str(md), str(typ)

    def test_matching_markdown_kept(self, tmp_path):
        md, typ = self._files(tmp_path)
        records = [{"_note_file": md}, {"_note_file": typ}]
        result = filter_records_fulltext(records, "needle")
        assert result == [{"_note_file": md}]

    def test_jsonl_records_drop_out(self, tmp_path):
        md, _ = self._files(tmp_path)
        records = [
            {"_note_file": md},
            {"_note_file": str(tmp_path / "inbox.jsonl"), "filename": "needle.pdf"},
        ]
        result = filter_records_fulltext(records, "needle")
        assert len(result) == 1
        assert result[0]["_note_file"] == md

    def test_case_insensitive(self, tmp_path):
        md, _ = self._files(tmp_path)
        assert filter_records_fulltext([{"_note_file": md}], "NEEDLE")

    def test_empty_term_passthrough(self):
        records = [{"_note_file": "/x.jsonl"}]
        assert filter_records_fulltext(records, "  ") == records

    def test_cache_prevents_rereads(self, tmp_path):
        md, _ = self._files(tmp_path)
        cache: dict[str, str] = {}
        filter_records_fulltext([{"_note_file": md}], "needle", cache)
        assert md in cache
        # poison the file; the cached content must be used
        (tmp_path / "a.md").write_text("changed", encoding="utf-8")
        result = filter_records_fulltext([{"_note_file": md}], "needle", cache)
        assert result

    def test_unreadable_file_dropped(self, tmp_path):
        records = [{"_note_file": str(tmp_path / "missing.md")}]
        assert not filter_records_fulltext(records, "needle")


# ---------------------------------------------------------------------------
# SQL form: build_clause / append_clause
# ---------------------------------------------------------------------------

class TestBuildClause:
    def test_equals_string_quoted(self):
        assert build_clause("status", "=", "active") == "status = 'active'"

    def test_equals_number_bare(self):
        assert build_clause("amount", "=", "450") == "amount = 450"

    def test_comparison_number(self):
        assert build_clause("amount", ">", "100") == "amount > 100"

    def test_like_wraps_wildcards(self):
        assert build_clause("title", "LIKE", "report") == "title LIKE '%report%'"

    def test_like_keeps_explicit_wildcards(self):
        assert build_clause("title", "LIKE", "re%t") == "title LIKE 're%t'"

    def test_is_null_needs_no_value(self):
        assert build_clause("binder", "IS NULL") == "binder IS NULL"
        assert build_clause("binder", "IS NOT NULL", "ignored") == "binder IS NOT NULL"

    def test_in_splits_and_quotes(self):
        assert build_clause("binder", "IN", "a, b, 3") == "binder IN ('a', 'b', 3)"

    def test_quote_escaping(self):
        assert build_clause("name", "=", "o'brien") == "name = 'o''brien'"

    def test_odd_field_name_quoted(self):
        assert build_clause("my field", "=", "x") == '"my field" = \'x\''

    def test_incomplete_returns_empty(self):
        assert build_clause("", "=", "x") == ""
        assert build_clause("status", "=", "") == ""
        assert build_clause("status", "bogus", "x") == ""

    def test_op_case_insensitive(self):
        assert build_clause("title", "like", "x") == "title LIKE '%x%'"


class TestAppendClause:
    def test_starts_fresh(self):
        assert append_clause("", "a = 1") == "a = 1"

    def test_ands_onto_existing(self):
        assert append_clause("a = 1", "b = 2") == "a = 1 AND b = 2"

    def test_empty_clause_keeps_sql(self):
        assert append_clause("a = 1", "") == "a = 1"


class TestRemoveLastClause:
    def test_single_clause_clears(self):
        assert remove_last_clause("status = 'open'") == ""

    def test_removes_last_of_two(self):
        assert remove_last_clause("a = 1 AND b = 2") == "a = 1"

    def test_removes_only_last_of_three(self):
        assert remove_last_clause("a = 1 AND b = 2 AND c = 3") == "a = 1 AND b = 2"

    def test_and_inside_string_ignored(self):
        sql = "title = 'salt AND pepper' AND b = 2"
        assert remove_last_clause(sql) == "title = 'salt AND pepper'"

    def test_and_inside_in_parens_ignored(self):
        # nobody writes AND inside IN, but parens must not split either way
        sql = "binder IN ('a', 'b') AND c = 3"
        assert remove_last_clause(sql) == "binder IN ('a', 'b')"

    def test_lowercase_and(self):
        assert remove_last_clause("a = 1 and b = 2") == "a = 1"

    def test_escaped_quote_stays_in_string(self):
        sql = "name = 'o''brien AND co' AND b = 2"
        assert remove_last_clause(sql) == "name = 'o''brien AND co'"

    def test_empty_returns_empty(self):
        assert remove_last_clause("  ") == ""
