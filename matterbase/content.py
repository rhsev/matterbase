"""Content extraction and rendering helpers — no Textual dependency."""

import os
import re
import subprocess
import tempfile
from dataclasses import dataclass, field
from pathlib import Path

import yaml
from docx import Document as DocxDocument
from pypdf import PdfReader


# ---------------------------------------------------------------------------
# MMD header parsing
# ---------------------------------------------------------------------------

_MMD_KEY_RE = re.compile(r'^[\w. -]+\s*:')


def split_mmd_header(content: str) -> tuple[str, str]:
    """If content starts with MMD key: value pairs, return (yaml_lines, body).

    Returns ("", content) if no MMD header found.
    """
    lines = content.split("\n")
    mmd_lines: list[str] = []
    for line in lines:
        if not line.strip():
            break
        if _MMD_KEY_RE.match(line):
            mmd_lines.append(line)
        else:
            break
    if not mmd_lines:
        return "", content
    rest_start = len(mmd_lines)
    if rest_start < len(lines) and not lines[rest_start].strip():
        rest_start += 1
    return "\n".join(mmd_lines), "\n".join(lines[rest_start:])


# Keep the private alias used in app.py for backward compat during transition
_split_mmd_header = split_mmd_header


# ---------------------------------------------------------------------------
# Binary file extractors
# ---------------------------------------------------------------------------

def extract_pdf_text(path: str, max_pages: int = 10) -> str:
    try:
        reader = PdfReader(path)
        pages = reader.pages[:max_pages]
        text = "\n\n".join(p.extract_text() or "" for p in pages).strip()
        total = len(reader.pages)
        if total > max_pages:
            text += f"\n\n[dim]… {total - max_pages} weitere Seiten[/dim]"
        return text or "[dim](kein Text extrahierbar)[/dim]"
    except Exception as e:
        return f"[red]PDF-Fehler: {e}[/red]"


def extract_docx_text(path: str) -> str:
    try:
        doc = DocxDocument(path)
        lines = []
        for para in doc.paragraphs:
            text = para.text.strip()
            if not text:
                lines.append("")
                continue
            style = para.style.name if para.style else ""
            if style.startswith("Heading 1"):
                lines.append(f"# {text}")
            elif style.startswith("Heading 2"):
                lines.append(f"## {text}")
            elif style.startswith("Heading 3"):
                lines.append(f"### {text}")
            else:
                lines.append(text)
        return "\n".join(lines).strip() or "[dim](kein Text extrahierbar)[/dim]"
    except Exception as e:
        return f"[red]DOCX-Fehler: {e}[/red]"


# ---------------------------------------------------------------------------
# Markdown section extraction
# ---------------------------------------------------------------------------

def extract_markdown_section(content: str, target_block_start: int) -> str:
    """Return the markdown section (nearest heading → next same-level heading)
    that contains the line at *target_block_start*."""
    lines = content.splitlines()

    heading_line_idx = 0
    heading_level = 0
    for i in range(target_block_start - 1, -1, -1):
        m = re.match(r"^(#{1,6})\s", lines[i])
        if m:
            heading_line_idx = i
            heading_level = len(m.group(1))
            break

    section_end_idx = len(lines)
    if heading_level > 0:
        stop_pattern = re.compile(r"^#{1," + str(heading_level) + r"}\s")
        for i in range(heading_line_idx + 1, len(lines)):
            if stop_pattern.match(lines[i]):
                section_end_idx = i
                break

    return "\n".join(lines[heading_line_idx:section_end_idx]).rstrip()


def extract_section_for_record(
    body: str,
    record: dict,
    frontmatter_keys: set[str] | None = None,
) -> str:
    """Return the markdown section containing the YAML block matching *record*.

    frontmatter_keys: excluded when building the match fingerprint so that
    frontmatter-only fields don't prevent matching.
    """
    lines = body.splitlines()
    fm_keys = frontmatter_keys or set()

    match_fields: dict[str, str] = {}
    if record.get("id") is not None and "id" not in fm_keys:
        match_fields["id"] = str(record["id"])
    else:
        for k, v in record.items():
            if not k.startswith("_") and k not in fm_keys and v is not None:
                match_fields[k] = str(v)

    if not match_fields:
        return ""

    block_start = None
    i = 0
    while i < len(lines):
        if re.match(r"^```ya?ml\s*$", lines[i]):
            j = i + 1
            while j < len(lines) and not re.match(r"^```\s*$", lines[j]):
                j += 1
            try:
                block_data = yaml.safe_load("\n".join(lines[i + 1 : j])) or {}
                if all(str(block_data.get(k, "")) == v for k, v in match_fields.items()):
                    block_start = i
                    break
            except yaml.YAMLError:
                pass
            i = j + 1
        else:
            i += 1

    if block_start is None:
        return ""

    return extract_markdown_section(body, block_start)


# Private alias for backward compat during transition
_extract_section_for_record = extract_section_for_record


# ---------------------------------------------------------------------------
# Compact content extraction
# ---------------------------------------------------------------------------

def extract_compact_content(
    path: str,
    compact_tasks_heading: str = "Tasks",
    mmd: bool = False,
) -> str:
    """Extract frontmatter + YAML code blocks (with preceding heading) + Tasks section."""
    try:
        content = Path(path).read_text(encoding="utf-8", errors="replace")
    except OSError:
        return ""

    parts: list[str] = []
    body = content

    if content.startswith("---"):
        sections = content.split("---", 2)
        if len(sections) >= 3:
            parts.append(f"---{sections[1]}---")
            body = sections[2]
    elif mmd:
        mmd_yaml, mmd_body = split_mmd_header(content)
        if mmd_yaml:
            parts.append(f"---\n{mmd_yaml}\n---")
            body = mmd_body

    lines = body.splitlines()

    i = 0
    pending_heading: str | None = None
    while i < len(lines):
        line = lines[i]
        if re.match(r"^#{1,6}\s", line):
            pending_heading = line
            i += 1
            continue
        if re.match(r"^```ya?ml\s*$", line):
            k = i + 1
            while k < len(lines) and not re.match(r"^```\s*$", lines[k]):
                k += 1
            block: list[str] = []
            if pending_heading:
                block.append(pending_heading)
            block.extend(lines[i : k + 1])
            parts.append("\n".join(block))
            pending_heading = None
            i = k + 1
            continue
        if line.strip():
            pending_heading = None
        i += 1

    tasks_lines: list[str] = []
    in_tasks = False
    for line in lines:
        if re.match(r"^## " + re.escape(compact_tasks_heading) + r"\b", line):
            in_tasks = True
            tasks_lines = [line]
        elif in_tasks:
            if re.match(r"^## ", line):
                break
            tasks_lines.append(line)
    if tasks_lines:
        parts.append("\n".join(tasks_lines).rstrip())

    return "\n\n".join(parts)


# ---------------------------------------------------------------------------
# apex rendering
# ---------------------------------------------------------------------------

@dataclass
class ApexConfig:
    theme: str = ""
    width: int = 0
    code_highlight: str = ""
    code_highlight_theme: str = ""


def render_with_apex(
    path: str,
    cfg: ApexConfig,
    tmp_content: str | None = None,
) -> str:
    """Render *path* (or *tmp_content* written to a tempfile) via apex.

    Returns ANSI-escaped string, or "" if apex is unavailable or fails.
    tmp_content: if given, write to a .md tempfile and pass that to apex instead.
    """
    tmp_path: str | None = None
    try:
        if tmp_content is not None:
            with tempfile.NamedTemporaryFile(
                mode="w", suffix=".md", delete=False, encoding="utf-8"
            ) as f:
                f.write(tmp_content)
                tmp_path = f.name
        render_path = tmp_path if tmp_path else path
        cmd = ["apex", render_path, "--plugins", "-t", "terminal256"]
        if cfg.code_highlight:
            cmd += ["--code-highlight", cfg.code_highlight]
        if cfg.code_highlight_theme:
            cmd += ["--code-highlight-theme", cfg.code_highlight_theme]
        if cfg.theme:
            cmd += ["--theme", cfg.theme]
        if cfg.width:
            cmd += ["--width", str(cfg.width)]
        env = os.environ.copy()
        env.setdefault("TERM", "xterm-256color")
        env.setdefault("COLORTERM", "truecolor")
        result = subprocess.run(cmd, capture_output=True, timeout=10, env=env)
        if result.returncode == 0:
            return result.stdout.decode("utf-8", errors="replace")
        return ""
    except (FileNotFoundError, subprocess.TimeoutExpired):
        return ""
    finally:
        if tmp_path:
            try:
                os.unlink(tmp_path)
            except OSError:
                pass


def render_with_bat(path: str) -> str:
    """Syntax-highlight *path* via bat. Returns ANSI string or ""."""
    try:
        env = os.environ.copy()
        env.setdefault("TERM", "xterm-256color")
        env.setdefault("COLORTERM", "truecolor")
        result = subprocess.run(
            ["bat", "--color=always", "--style=plain", "--paging=never", path],
            capture_output=True,
            timeout=5,
            env=env,
        )
        if result.returncode == 0:
            return result.stdout.decode("utf-8", errors="replace")
        return ""
    except (FileNotFoundError, subprocess.TimeoutExpired):
        return ""
