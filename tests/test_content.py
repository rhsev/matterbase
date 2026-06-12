"""Unit tests for matterbase.content — pure functions, no Textual required."""

import textwrap

import pytest

from matterbase.content import (
    extract_compact_content,
    extract_markdown_section,
    extract_section_for_record,
    source_type,
    split_frontmatter,
    split_mmd_header,
)


# ---------------------------------------------------------------------------
# source_type
# ---------------------------------------------------------------------------

class TestSourceType:
    def test_markdown(self):
        assert source_type("/notes/a.md") == "markdown"
        assert source_type("/notes/a.markdown") == "markdown"

    def test_typst(self):
        assert source_type("/notes/a.typ") == "typst"
        assert source_type("/notes/a.typst") == "typst"

    def test_jsonl(self):
        assert source_type("/notes/collections/inbox.jsonl") == "jsonl"

    def test_other(self):
        assert source_type("/notes/a.pdf") == "other"

    def test_case_insensitive(self):
        assert source_type("/notes/A.MD") == "markdown"


# ---------------------------------------------------------------------------
# split_frontmatter
# ---------------------------------------------------------------------------

class TestSplitFrontmatter:
    def test_yaml_frontmatter(self):
        content = "---\ntitle: X\nstatus: active\n---\n\nBody."
        fm, body, keys = split_frontmatter(content)
        assert "title: X" in fm
        assert "Body." in body
        assert keys == {"title", "status"}

    def test_no_frontmatter(self):
        fm, body, keys = split_frontmatter("# Just a heading\n")
        assert fm == ""
        assert body.startswith("# Just")
        assert keys == set()

    def test_mmd_header(self):
        fm, body, keys = split_frontmatter("Title: X\n\nBody.", mmd=True)
        assert "Title: X" in fm
        assert "Title" in keys
        assert "Body." in body

    def test_mmd_disabled(self):
        fm, body, keys = split_frontmatter("Title: X\n\nBody.", mmd=False)
        assert fm == ""


# ---------------------------------------------------------------------------
# split_mmd_header
# ---------------------------------------------------------------------------

class TestSplitMmdHeader:
    def test_no_header(self):
        content = "# Heading\n\nSome body text."
        yaml_part, body = split_mmd_header(content)
        assert yaml_part == ""
        assert body == content

    def test_simple_header(self):
        content = "Title: My Note\nDate: 2025-01-01\n\nBody here."
        yaml_part, body = split_mmd_header(content)
        assert "Title: My Note" in yaml_part
        assert "Date: 2025-01-01" in yaml_part
        assert body == "Body here."

    def test_stops_at_non_key_line(self):
        content = "Title: My Note\nThis is not a key line\n\nBody."
        yaml_part, body = split_mmd_header(content)
        assert yaml_part == "Title: My Note"
        assert "This is not a key line" in body

    def test_empty_string(self):
        yaml_part, body = split_mmd_header("")
        assert yaml_part == ""
        assert body == ""


# ---------------------------------------------------------------------------
# extract_markdown_section
# ---------------------------------------------------------------------------

class TestExtractMarkdownSection:
    def test_simple_h2_section(self):
        content = textwrap.dedent("""\
            ## Alpha
            Some text.
            ## Beta
            Other text.
        """)
        result = extract_markdown_section(content, 1)
        assert "## Alpha" in result
        assert "Some text." in result
        assert "## Beta" not in result

    def test_nested_headings_stay_inside(self):
        content = textwrap.dedent("""\
            ## Section
            ### Subsection
            Detail.
            ## Next Section
        """)
        result = extract_markdown_section(content, 1)
        assert "### Subsection" in result
        assert "## Next Section" not in result

    def test_no_heading_returns_full_content(self):
        content = "just some text\nno headings here"
        result = extract_markdown_section(content, 1)
        assert "just some text" in result


# ---------------------------------------------------------------------------
# extract_section_for_record
# ---------------------------------------------------------------------------

SAMPLE_DOC = textwrap.dedent("""\
    ---
    title: Test Note
    ---

    ## Tasks

    ```yaml
    id: task-1
    status: active
    ```

    Some prose.

    ## Archive

    ```yaml
    id: task-2
    status: done
    ```
""")


class TestExtractSectionForRecord:
    def test_matches_by_id(self):
        body = SAMPLE_DOC.split("---", 2)[2]
        result = extract_section_for_record(body, {"id": "task-1", "status": "active"})
        assert "## Tasks" in result
        assert "task-1" in result
        assert "## Archive" not in result

    def test_matches_second_block(self):
        body = SAMPLE_DOC.split("---", 2)[2]
        result = extract_section_for_record(body, {"id": "task-2", "status": "done"})
        assert "## Archive" in result
        assert "task-2" in result

    def test_no_match_returns_empty(self):
        body = SAMPLE_DOC.split("---", 2)[2]
        result = extract_section_for_record(body, {"id": "nonexistent"})
        assert result == ""

    def test_frontmatter_keys_excluded(self):
        body = SAMPLE_DOC.split("---", 2)[2]
        result = extract_section_for_record(
            body,
            {"id": "task-1", "title": "Test Note"},
            frontmatter_keys={"title"},
        )
        assert "task-1" in result

    def test_empty_record_returns_empty(self):
        body = SAMPLE_DOC.split("---", 2)[2]
        result = extract_section_for_record(body, {})
        assert result == ""


# ---------------------------------------------------------------------------
# extract_compact_content
# ---------------------------------------------------------------------------

@pytest.fixture
def tmp_note(tmp_path):
    def _make(content: str) -> str:
        p = tmp_path / "note.md"
        p.write_text(content, encoding="utf-8")
        return str(p)
    return _make


class TestExtractCompactContent:
    def test_includes_yaml_frontmatter(self, tmp_note):
        path = tmp_note("---\ntitle: X\n---\n\nProse.\n")
        result = extract_compact_content(path)
        assert "title: X" in result
        assert "Prose." not in result

    def test_includes_yaml_block_with_heading(self, tmp_note):
        path = tmp_note(textwrap.dedent("""\
            ## Data

            ```yaml
            id: a
            ```

            Prose between.
        """))
        result = extract_compact_content(path)
        assert "## Data" in result
        assert "id: a" in result
        assert "Prose between." not in result

    def test_includes_tasks_section(self, tmp_note):
        path = tmp_note("## Tasks\n- [ ] one\n\n## Other\nx\n")
        result = extract_compact_content(path)
        assert "- [ ] one" in result
        assert "## Other" not in result

    def test_nonexistent_file_returns_empty(self):
        assert extract_compact_content("/no/such/file.md") == ""
