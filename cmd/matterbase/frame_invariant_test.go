package main

import (
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// drainCmds feeds a command's messages back into the model until
// quiescent — the bubbletea loop in miniature, shared by the frame
// invariant test below.
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

// TestFrameNeverExceedsTerminal cursors through records in every preview
// mode at several terminal sizes and asserts each frame is exactly the
// terminal height and never wider. A frame even one line over budget
// makes the terminal scroll and desyncs Bubble Tea's renderer —
// duplicated table headers, lost header underline (the bug this guards:
// bubbles/table's SetHeight renders one line taller with a bordered
// header; recordtable.SetSize self-calibrates against it).
func TestFrameNeverExceedsTerminal(t *testing.T) {
	if _, err := exec.LookPath("grubber"); err != nil {
		t.Skip("grubber not on PATH")
	}
	dir := t.TempDir()
	long := strings.Repeat("wide content without any break ", 40)
	notes := map[string]string{
		"a.md": "---\ntitle: alpha\nstatus: active\n---\n\nshort body\n",
		"b.md": "---\ntitle: " + long + "\nstatus: inbox\n---\n\n" + long + "\n",
		"c.md": "---\ntitle: gamma\namount: 12\n---\n\n" + strings.Repeat("line\n", 200),
		// the Halit-Akgül case: tab-separated memo lines. Tabs count as
		// width 0 in the ANSI width math but expand to 8-column stops in
		// the real terminal — one such line wraps and desyncs the
		// renderer (stale highlighted rows, duplicated content lines).
		"d.md": "---\ntitle: delta\nstatus: edit\n---\n\n### Memo\n2024-09-27\tcalled him, he answers later\n2024-05-03\twanted the car\r\nweird line\n",
		// the Streitkultur case: web-clipped notes carry soft hyphens
		// (U+00AD) and zero-width characters. The width math counts them
		// 0, terminals draw the soft hyphen 1 cell wide — one such rune
		// per line is enough to wrap it and desync the renderer.
		"e.md": "---\ntitle: soft\u00adhyphen clip\nstatus: active\n---\n\nA justified\u00ad paragraph with zero\u200bwidth and \u200e bidi marks.\n\u00ad=== Datum ===\n",
	}
	for name, content := range notes {
		if err := os.WriteFile(dir+"/"+name, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	for _, size := range [][2]int{{190, 45}, {120, 30}, {80, 24}} {
		W, H := size[0], size[1]
		t.Run(fmt.Sprintf("%dx%d", W, H), func(t *testing.T) {
			m := newAppModel(&Config{NotesDir: dir})
			defer os.Remove(m.cachePath)
			var model tea.Model = m
			model, _ = model.Update(tea.WindowSizeMsg{Width: W, Height: H})
			model = drainCmds(model, func() tea.Msg { return refreshRequestMsg{} })

			if model.(appModel).table.Len() == 0 {
				t.Fatal("no records")
			}
			for _, mode := range []string{"whole", "compact", "record"} {
				for i := 0; i < model.(appModel).table.Len(); i++ {
					view := model.(appModel).View()
					if h := lipgloss.Height(view); h != H {
						t.Fatalf("mode=%s row=%d: frame height %d, terminal %d", mode, i, h, H)
					}
					if w := lipgloss.Width(view); w > W {
						t.Fatalf("mode=%s row=%d: frame width %d, terminal %d", mode, i, w, W)
					}
					if loc := controlRe.FindStringIndex(view); loc != nil {
						t.Fatalf("mode=%s row=%d: raw control char %q in frame — width math and terminal will disagree", mode, i, view[loc[0]:loc[1]])
					}
					var cmd tea.Cmd
					model, cmd = model.Update(tea.KeyMsg{Type: tea.KeyDown})
					model = drainCmds(model, cmd)
				}
				var cmd tea.Cmd
				model, cmd = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'m'}})
				model = drainCmds(model, cmd)
			}
		})
	}
}

// controlRe matches characters that must never reach a frame: C0/C1
// controls (except newline and ESC — SGR sequences are fine) and the
// zero-width/format characters terminals render differently than the
// width math counts them (soft hyphen, zero-width spaces, bidi marks).
var controlRe = regexp.MustCompile(`[\x00-\x08\x0b-\x1a\x1c-\x1f\x7f]|\x{00AD}|[\x{200B}-\x{200F}]|[\x{2028}-\x{202E}]|[\x{2060}-\x{2064}]|\x{FEFF}`)
