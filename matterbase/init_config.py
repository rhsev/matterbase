"""Interactive config generator — no TUI, plain CLI prompts.

Draft: covers the essential fields to get started. Invoked via
    matterbase --init
"""

import sys
from pathlib import Path

import yaml


# ---------------------------------------------------------------------------
# Helpers
# ---------------------------------------------------------------------------

def _ask(prompt: str, default: str = "") -> str:
    display = f"{prompt} [{default}]: " if default else f"{prompt}: "
    try:
        value = input(display).strip()
    except (KeyboardInterrupt, EOFError):
        print()
        sys.exit(0)
    return value or default


def _ask_path(prompt: str, default: str = "") -> str:
    while True:
        raw = _ask(prompt, default)
        p = Path(raw).expanduser()
        if p.is_dir():
            return str(p)
        print(f"  Directory not found: {p}")


def _ask_bool(prompt: str, default: bool = True) -> bool:
    hint = "Y/n" if default else "y/N"
    raw = _ask(f"{prompt} ({hint})", "").lower()
    if not raw:
        return default
    return raw.startswith("y")


def _ask_filters() -> list[dict]:
    filters = []
    print()
    print("Filter buttons (grubber expressions per button).")
    print("Operators: = equals  ~ contains  ^ starts-with  ! not-equals")
    print("Leave label empty to finish.")
    while True:
        print()
        label = _ask("  Button label", "")
        if not label:
            break
        expressions = []
        print(f"  Expressions for '{label}' (leave empty to finish):")
        while True:
            expr = _ask("    Expression", "")
            if not expr:
                break
            expressions.append(expr)
        if expressions:
            filters.append({"label": label, "query": expressions})
    return filters


# ---------------------------------------------------------------------------
# Main entry point
# ---------------------------------------------------------------------------

def run_init() -> None:
    """Interactively create a matterbase config file."""
    print("matterbase — config setup")
    print("─" * 40)
    print()

    # Notes directory
    notes_dir = _ask_path(
        "Notes directory",
        str(Path("~/Notes").expanduser()),
    )

    # Editor
    editor = _ask("Editor command", "hx")

    # apex (optional)
    use_apex = _ask_bool("Use apex for Markdown preview?", default=True)
    apex_theme = ""
    apex_width = 0
    if use_apex:
        apex_theme = _ask("  apex theme (leave empty for default)", "")
        width_raw = _ask("  apex width (0 = auto)", "0")
        try:
            apex_width = int(width_raw)
        except ValueError:
            apex_width = 0

    # Filters
    add_filters = _ask_bool("Add filter buttons?", default=True)
    filters = _ask_filters() if add_filters else []

    # Multi-select
    multi_select = _ask_bool("Multi-select (AND-intersect multiple buttons)?", default=True)

    # Output path
    default_out = str(Path("~/.config/matterbase/config.yml").expanduser())
    out_raw = _ask("Write config to", default_out)
    out_path = Path(out_raw).expanduser()

    # Build config dict
    config: dict = {"notes_dir": notes_dir, "editor": editor}
    if apex_theme:
        config["apex_theme"] = apex_theme
    if apex_width:
        config["apex_width"] = apex_width
    if filters:
        config["filters"] = filters
        config["multi_select"] = multi_select

    # Write
    out_path.parent.mkdir(parents=True, exist_ok=True)
    with open(out_path, "w") as f:
        yaml.dump(config, f, default_flow_style=False, allow_unicode=True, sort_keys=False)

    print()
    print(f"Config written to {out_path}")
    print()
    print(f"Start with:  matterbase --config {out_path}")
