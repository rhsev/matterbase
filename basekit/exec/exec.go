// Package exec provides the subprocess plumbing basekit apps share:
// a timeout-bounded runner with stderr-first-line error extraction (port
// of grubber_client._run_grubber_cmd), clipboard copy, and opening a
// path in $EDITOR — as a zellij/tmux pane when available, otherwise by
// suspending the Bubble Tea program (port of app.py's
// on_data_table_row_selected).
package exec

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	osexec "os/exec"
	"runtime"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// DefaultTimeout matches grubber_client's subprocess.run(timeout=15).
const DefaultTimeout = 15 * time.Second

// maxErrLen matches the [:120] truncation on grubber's stderr in the
// Python original.
const maxErrLen = 120

// Run executes name with args and returns stdout on success. On a
// non-zero exit, timeout, or launch failure, the error message is the
// first line of stderr (or the exec error, if stderr was empty),
// truncated to 120 runes — the same shape status bars across the Python
// originals expect.
func Run(ctx context.Context, timeout time.Duration, env []string, name string, args ...string) ([]byte, error) {
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := osexec.CommandContext(ctx, name, args...)
	if env != nil {
		cmd.Env = env
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	if ctx.Err() == context.DeadlineExceeded {
		return nil, fmt.Errorf("%s: timed out", name)
	}
	if err != nil {
		msg := firstLine(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return nil, fmt.Errorf("%s: %s", name, truncate(msg, maxErrLen))
	}
	return stdout.Bytes(), nil
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	return s
}

func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}

// Copy copies text to the system clipboard using the first available
// tool for the platform: pbcopy on macOS, wl-copy under Wayland,
// otherwise xclip or xsel. Returns an error if none is found — the
// Python original silently does nothing instead, but a Go caller can
// choose to surface or ignore it.
func Copy(text string) error {
	var candidates [][]string
	switch {
	case runtime.GOOS == "darwin":
		candidates = [][]string{{"pbcopy"}}
	case os.Getenv("WAYLAND_DISPLAY") != "":
		candidates = [][]string{{"wl-copy"}}
	default:
		candidates = [][]string{
			{"xclip", "-selection", "clipboard"},
			{"xsel", "--clipboard", "--input"},
		}
	}
	for _, c := range candidates {
		path, err := osexec.LookPath(c[0])
		if err != nil {
			continue
		}
		cmd := osexec.Command(path, c[1:]...)
		cmd.Stdin = strings.NewReader(text)
		return cmd.Run()
	}
	return errors.New("no clipboard tool found")
}

// EditorClosed is emitted once the editor opened via OpenEditor returns
// control — via a suspended Bubble Tea program, or a fire-and-forget
// zellij/tmux launch (Err is always nil in the latter case, matching the
// Python original's check=False).
type EditorClosed struct{ Err error }

// OpenEditor returns a tea.Cmd that opens path in editor. Inside zellij
// or tmux it spawns a new pane/window and returns immediately, so the
// TUI keeps running underneath. Otherwise it suspends the program via
// tea.ExecProcess so the editor gets the terminal — port of app.py's
// on_data_table_row_selected.
func OpenEditor(editor, path string) tea.Cmd {
	switch {
	case os.Getenv("ZELLIJ") != "":
		return func() tea.Msg {
			_, _ = Run(context.Background(), DefaultTimeout, nil, "zellij", "run", "-f", "--", editor, path)
			return EditorClosed{}
		}
	case os.Getenv("TMUX") != "":
		return func() tea.Msg {
			_, _ = Run(context.Background(), DefaultTimeout, nil, "tmux", "new-window", editor, path)
			return EditorClosed{}
		}
	default:
		cmd := osexec.Command(editor, path)
		return tea.ExecProcess(cmd, func(err error) tea.Msg {
			return EditorClosed{Err: err}
		})
	}
}
