"""Unit tests for matterbase.content — pure functions, no Textual required."""

import os
import textwrap

import pytest

from matterbase.content import (
    extract_compact_content,
    extract_markdown_section,
    extract_section_for_record,
    split_mmd_header,
)


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

    def test_header_no_blank_line(self):
        content = "Title: My Note\nDate: 2025-01-01"
        yaml_part, body = split_mmd_header(content)
        assert "Title: My Note" in yaml_part
        assert body == ""

    def test_stops_at_non_key_line(self):
        content = "Title: My Note\nThis is not a key line\n\nBody."
        yaml_part, body = split_mmd_header(content)
        assert yaml_part == "Title: My Note"
        assert "This is not a key line" in body

    def test_empty_string(self):
        yaml_part, body = split_mmd_header("")
        assert yaml_part == ""
        assert body == ""

    def test_key_with_hyphen_and_space(self):
        content = "X-Custom Key: value\n\nBody."
        yaml_part, body = split_mmd_header(content)
        assert "X-Custom Key: value" in yaml_part

    def test_body_preserves_leading_blank_line_stripped(self):
        content = "Title: Note\n\n\nBody after two blanks."
        yaml_part, body = split_mmd_header(content)
        assert yaml_part == "Title: Note"
        assert "Body after two blanks." in body


# ---------------------------------------------------------------------------
# extract_markdown_section
# ---------------------------------------------------------------------------

class TestExtractMarkdownSection:
    # target_block_start is the 0-based index of a line *inside* the section
    # (e.g. the opening fence of a yaml block). The function searches backward
    # from there to find the enclosing heading.

    def test_simple_h2_section(self):
        content = textwrap.dedent("""\
            ## Alpha
            Some text.
            ## Beta
            Other text.
        """)
        # target line 1 = "Some text." → finds ## Alpha, stops before ## Beta
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
        # target line 1 = "### Subsection" → backward finds ## Section (level 2)
        result = extract_markdown_section(content, 1)
        assert "### Subsection" in result
        assert "## Next Section" not in result

    def test_target_inside_section(self):
        content = textwrap.dedent("""\
            ## First
            line 1
            line 2
            ## Second
            line 3
        """)
        result = extract_markdown_section(content, 1)
        assert "## First" in result
        assert "## Second" not in result

    def test_no_heading_returns_full_content(self):
        content = "just some text\nno headings here"
        # no heading found → returns from line 0 to end
        result = extract_markdown_section(content, 1)
        assert "just some text" in result

    def test_h1_section_ends_at_next_h1(self):
        content = textwrap.dedent("""\
            # Top
            content
            ## Sub
            subcontent
            # Next Top
        """)
        # target line 1 = "content" → finds # Top (level 1), stops before # Next Top
        result = extract_markdown_section(content, 1)
        assert "## Sub" in result
        assert "# Next Top" not in result


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
        # 'title' is a frontmatter key — should not be used for block matching
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
        path = tmp_note("---\nstatus: active\n---\n\nBody text.\n")
        result = extract_compact_content(path)
        assert "status: active" in result
        assert "Body text." not in result

    def test_includes_yaml_block(self, tmp_note):
        path = tmp_note(textwrap.dedent("""\
            ---
            title: Note
            ---

            ## Section

            ```yaml
            id: block-1
            ```

            Some prose.
        """))
        result = extract_compact_content(path)
        assert "id: block-1" in result
        assert "Some prose." not in result

    def test_yaml_block_keeps_preceding_heading(self, tmp_note):
        path = tmp_note(textwrap.dedent("""\
            ## My Section

            ```yaml
            id: x
            ```
        """))
        result = extract_compact_content(path)
        assert "## My Section" in result
        assert "id: x" in result

    def test_heading_reset_by_non_heading_line(self, tmp_note):
        # A non-blank non-heading line between heading and fence → heading not included
        path = tmp_note(textwrap.dedent("""\
            ## My Section

            Some intervening prose.

            ```yaml
            id: x
            ```
        """))
        result = extract_compact_content(path)
        assert "## My Section" not in result
        assert "id: x" in result

    def test_includes_tasks_section(self, tmp_note):
        path = tmp_note(textwrap.dedent("""\
            # Note

            ## Tasks

            - [ ] Do something
            - [x] Done thing

            ## Notes

            Other stuff.
        """))
        result = extract_compact_content(path, compact_tasks_heading="Tasks")
        assert "## Tasks" in result
        assert "Do something" in result
        assert "## Notes" not in result
        assert "Other stuff." not in result

    def test_custom_tasks_heading(self, tmp_note):
        path = tmp_note(textwrap.dedent("""\
            ## Aufgaben

            - [ ] Etwas tun

            ## Notizen

            Anderes.
        """))
        result = extract_compact_content(path, compact_tasks_heading="Aufgaben")
        assert "Aufgaben" in result
        assert "Etwas tun" in result
        assert "Anderes." not in result

    def test_mmd_header(self, tmp_note):
        path = tmp_note("Title: My Note\nDate: 2025-01-01\n\nBody text.\n")
        result = extract_compact_content(path, mmd=True)
        assert "Title: My Note" in result
        assert "Body text." not in result

    def test_nonexistent_file_returns_empty(self):
        result = extract_compact_content("/nonexistent/path/note.md")
        assert result == ""

    def test_no_yaml_blocks_no_tasks_returns_frontmatter_only(self, tmp_note):
        path = tmp_note("---\nstatus: active\n---\n\nJust prose, no blocks.\n")
        result = extract_compact_content(path)
        assert "status: active" in result
        assert "Just prose" not in result
