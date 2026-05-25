"""Reusable Textual widgets for matterbase (and other apps)."""

from textual.widgets import Button, DataTable, Label, ListItem, ListView


class FilterButton(Button):
    """A toggleable button representing a named query."""

    DEFAULT_CSS = """
    FilterButton {
        background: $surface;
        border: none;
        width: 1fr;
        padding: 0 1;
    }
    FilterButton:hover {
        background: $primary-darken-1;
    }
    FilterButton.filter-on {
        background: $accent;
        color: $text;
        text-style: bold;
    }
    """

    def __init__(self, label: str, query: list[str], **kwargs) -> None:
        super().__init__(label, **kwargs)
        self.query = query
        self.is_active = False

    def toggle(self) -> None:
        self.is_active = not self.is_active
        if self.is_active:
            self.add_class("filter-on")
        else:
            self.remove_class("filter-on")


class NoteItem(ListItem):
    """A ListItem that carries the full file path of the note."""

    def __init__(self, filename: str, full_path: str) -> None:
        super().__init__(Label(filename))
        self.full_path = full_path


class NoteListView(ListView):
    """ListView that highlights the first item automatically on focus."""

    def on_focus(self) -> None:
        if self.highlighted_child is None and len(self) > 0:
            self.index = 0


class MetaDataTable(DataTable):
    """DataTable for the metadata / collection-refs view.

    Focusable so the user can Tab into it and navigate records with arrow keys.
    In regular table mode the cursor follows the left pane; in collection mode
    the cursor drives the right-pane preview.
    """

    can_focus = True
