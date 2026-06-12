"""Reusable Textual widgets for matterbase."""

from textual.binding import Binding
from textual.message import Message
from textual.widgets import DataTable, Static


class PresetItem(Static):
    """A toggleable text line representing a named grubber query (a "set")."""

    can_focus = True

    BINDINGS = [
        Binding("space", "toggle", "Toggle", show=False),
        Binding("enter", "toggle", "Toggle", show=False),
        Binding("up", "focus_previous", "Previous", show=False),
        Binding("down", "focus_next", "Next", show=False),
        Binding("tab", "skip_tab", "Skip Tab", show=False),
        Binding("shift+tab", "skip_shift_tab", "Skip Shift Tab", show=False),
    ]

    class Pressed(Message):
        def __init__(self, item: "PresetItem") -> None:
            self.item = item
            super().__init__()

    DEFAULT_CSS = """
    PresetItem {
        width: 1fr;
        height: 1;
        padding: 0 1;
        background: transparent;
        color: #D8DEE9;
    }
    PresetItem:hover, PresetItem:focus {
        background: #434C5E;
    }
    PresetItem.preset-on {
        color: $accent;
        text-style: bold;
    }
    """

    def __init__(self, label: str, exprs: list[str], **kwargs) -> None:
        super().__init__(f"  {label}", **kwargs)
        self.preset_label = label
        self.exprs = exprs
        self.is_active = False

    def toggle_active(self) -> None:
        self.is_active = not self.is_active
        if self.is_active:
            self.add_class("preset-on")
            self.update(f"■ {self.preset_label}")
        else:
            self.remove_class("preset-on")
            self.update(f"  {self.preset_label}")

    async def on_click(self) -> None:
        self.post_message(self.Pressed(self))

    def action_toggle(self) -> None:
        self.post_message(self.Pressed(self))

    def action_focus_previous(self) -> None:
        self.screen.focus_previous()

    def action_focus_next(self) -> None:
        self.screen.focus_next()

    def action_skip_tab(self) -> None:
        self.screen.focus_next()
        while isinstance(self.screen.focused, PresetItem):
            self.screen.focus_next()

    def action_skip_shift_tab(self) -> None:
        self.screen.focus_previous()
        while isinstance(self.screen.focused, PresetItem):
            self.screen.focus_previous()


class RecordTable(DataTable):
    """The spine: every passing record across all sources, source as a column."""

    can_focus = True
