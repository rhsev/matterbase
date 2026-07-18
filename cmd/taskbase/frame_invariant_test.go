package main

import (
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// drainCmds feeds a command's messages back into the model until
// quiescent — the bubbletea loop in miniature, shared with matterbase-go's
// own frame-invariant test.
func drainCmds(model tea.Model, cmd tea.Cmd) tea.Model {
	queue := []tea.Cmd{cmd}
	for i := 0; len(queue) > 0 && i < 50; i++ {
		c := queue[0]
		queue = queue[1:]
		if c == nil {
			continue
		}
		msg := c()
		if msg == nil {
			continue
		}
		if batch, ok := msg.(tea.BatchMsg); ok {
			for _, bc := range batch {
				queue = append(queue, tea.Cmd(bc))
			}
			continue
		}
		var next tea.Cmd
		model, next = model.Update(msg)
		queue = append(queue, next)
	}
	return model
}

// TestFrameNeverExceedsTerminal cursors through every task at several
// terminal sizes and asserts each frame is exactly the terminal height and
// never wider — the same guard as matterbase-go's test, needed here for the
// same reason: bubbles/table's bordered header renders one line taller
// than SetHeight, and any per-cell content wider than its width math
// (soft hyphens, tabs) desyncs Bubble Tea's diff renderer.
func TestFrameNeverExceedsTerminal(t *testing.T) {
	dir := t.TempDir()
	long := strings.Repeat("wide task text without any break ", 20)
	records := []IndexRecord{
		{File: dir + "/a.md", Project: "alpha", Tasks: []RawTask{
			{Line: 3, Task: "short task", Tags: map[string]any{}},
		}},
		{File: dir + "/b.md", Project: "beta", Tasks: []RawTask{
			{Line: 12, Task: long, Tags: map[string]any{"today": true, "waiting": "client"}},
		}},
		// tab-separated task text: tabs count width 0 in the ANSI math but
		// expand to 8-column stops in a real terminal.
		{File: dir + "/c.md", Project: "gamma", Tasks: []RawTask{
			{Line: 1, Task: "call\tsomeone\tabout\tthe\tthing", Tags: map[string]any{}},
		}},
		// soft hyphen + zero-width marks, the web-clipping case that broke
		// matterbase-go's renderer once already.
		{File: dir + "/d.md", Project: "soft­hyphen", Tasks: []RawTask{
			{Line: 7, Task: "justified­ text with zero​width marks", Tags: map[string]any{"deferred": true}},
		}},
	}

	data, err := json.Marshal(records)
	if err != nil {
		t.Fatal(err)
	}
	indexPath := dir + "/index.json"
	if err := os.WriteFile(indexPath, data, 0o644); err != nil {
		t.Fatal(err)
	}

	for _, size := range [][2]int{{190, 45}, {120, 30}, {80, 24}} {
		W, H := size[0], size[1]
		t.Run(fmt.Sprintf("%dx%d", W, H), func(t *testing.T) {
			m := newAppModel(indexPath)
			var model tea.Model = m
			model, _ = model.Update(tea.WindowSizeMsg{Width: W, Height: H})
			model = drainCmds(model, model.Init())

			if model.(appModel).table.Len() == 0 {
				t.Fatal("no tasks loaded")
			}
			for i := 0; i < model.(appModel).table.Len(); i++ {
				view := model.(appModel).View()
				if h := lipgloss.Height(view); h != H {
					t.Fatalf("row=%d: frame height %d, terminal %d", i, h, H)
				}
				if w := lipgloss.Width(view); w > W {
					t.Fatalf("row=%d: frame width %d, terminal %d", i, w, W)
				}
				if loc := controlRe.FindStringIndex(view); loc != nil {
					t.Fatalf("row=%d: raw control char %q in frame — width math and terminal will disagree", i, view[loc[0]:loc[1]])
				}
				var cmd tea.Cmd
				model, cmd = model.Update(tea.KeyMsg{Type: tea.KeyDown})
				model = drainCmds(model, cmd)
			}
			// toggling the preview off/on re-lays out the frame too.
			var cmd tea.Cmd
			model, cmd = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'p'}})
			model = drainCmds(model, cmd)
			if h := lipgloss.Height(model.(appModel).View()); h != H {
				t.Fatalf("preview hidden: frame height %d, terminal %d", h, H)
			}
		})
	}
}

// controlRe matches characters that must never reach a frame: C0/C1
// controls (except newline and ESC — SGR sequences are fine) and the
// zero-width/format characters terminals render differently than the
// width math counts them (soft hyphen, zero-width spaces, bidi marks).
var controlRe = regexp.MustCompile(`[\x00-\x08\x0b-\x1a\x1c-\x1f\x7f]|\x{00AD}|[\x{200B}-\x{200F}]|[\x{2028}-\x{202E}]|[\x{2060}-\x{2064}]|\x{FEFF}`)
