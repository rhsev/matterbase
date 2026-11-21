package exec

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestRunReturnsStdout(t *testing.T) {
	out, err := Run(context.Background(), 0, nil, "echo", "-n", "hello")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if string(out) != "hello" {
		t.Errorf("stdout = %q, want %q", out, "hello")
	}
}

func TestRunErrorIsFirstLineOfStderr(t *testing.T) {
	_, err := Run(context.Background(), 0, nil, "sh", "-c", "echo 'first line\nsecond line' >&2; exit 1")
	if err == nil {
		t.Fatal("expected an error from a non-zero exit")
	}
	if !strings.Contains(err.Error(), "first line") {
		t.Errorf("error = %q, want it to contain %q", err, "first line")
	}
	if strings.Contains(err.Error(), "second line") {
		t.Errorf("error = %q, must not include lines past the first", err)
	}
}

func TestRunErrorTruncatesLongStderr(t *testing.T) {
	long := strings.Repeat("x", 200)
	_, err := Run(context.Background(), 0, nil, "sh", "-c", "echo -n '"+long+"' >&2; exit 1")
	if err == nil {
		t.Fatal("expected an error")
	}
	// The message embeds "name: <truncated stderr>" — just check the
	// truncated stderr portion doesn't exceed maxErrLen runes.
	parts := strings.SplitN(err.Error(), ": ", 2)
	if len(parts) != 2 {
		t.Fatalf("unexpected error shape: %q", err)
	}
	if got := len([]rune(parts[1])); got > maxErrLen {
		t.Errorf("truncated stderr length = %d, want <= %d", got, maxErrLen)
	}
}

func TestRunTimeout(t *testing.T) {
	_, err := Run(context.Background(), 10*time.Millisecond, nil, "sleep", "1")
	if err == nil {
		t.Fatal("expected a timeout error")
	}
	if !strings.Contains(err.Error(), "timed out") {
		t.Errorf("error = %q, want it to mention timing out", err)
	}
}

func TestRunMissingBinary(t *testing.T) {
	_, err := Run(context.Background(), 0, nil, "definitely-not-a-real-binary-xyz")
	if err == nil {
		t.Fatal("expected an error for a missing binary")
	}
}

func TestCopyErrorsWhenNoToolFound(t *testing.T) {
	if runtime.GOOS == "darwin" {
		t.Skip("pbcopy is always present on darwin, can't force the no-tool path")
	}
	t.Setenv("PATH", "")
	t.Setenv("WAYLAND_DISPLAY", "")
	if err := Copy("test"); err == nil {
		t.Error("expected an error when no clipboard tool is on PATH")
	}
}

func TestEditorArgv(t *testing.T) {
	cases := []struct {
		editor string
		want   []string
	}{
		{"sh", []string{"sh"}},
		{"sh -c", []string{"sh", "-c"}},
		{"  definitely-not-a-real-binary-xyz  --wait ", []string{"definitely-not-a-real-binary-xyz", "--wait"}},
		{"", nil},
		{"   ", nil},
	}
	for _, c := range cases {
		got := editorArgv(c.editor)
		if strings.Join(got, "|") != strings.Join(c.want, "|") {
			t.Errorf("editorArgv(%q) = %q, want %q", c.editor, got, c.want)
		}
	}
}

// A program whose path holds a blank is one program, not two words.
func TestEditorArgvKeepsAPathWithBlanks(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "My Editor")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	prog := filepath.Join(dir, "edit")
	if err := os.WriteFile(prog, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if got := editorArgv(prog); len(got) != 1 || got[0] != prog {
		t.Errorf("editorArgv(%q) = %q, want it whole", prog, got)
	}
}

func TestOpenEditorWithoutEditorReportsIt(t *testing.T) {
	msg := OpenEditor(" ", "/tmp/x.md")()
	if closed, ok := msg.(EditorClosed); !ok || closed.Err == nil {
		t.Errorf("got %#v, want EditorClosed with an error", msg)
	}
}

// Inside tmux the editor's arguments reach the new window as words of
// their own, and a failing launch comes back as the error.
func TestOpenEditorInTmuxPassesArgumentsAndErrors(t *testing.T) {
	dir := t.TempDir()
	log := filepath.Join(dir, "args")
	script := "#!/bin/sh\nfor a in \"$@\"; do echo \"$a\" >> " + log + "; done\n"
	if err := os.WriteFile(filepath.Join(dir, "tmux"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
	t.Setenv("ZELLIJ", "")
	t.Setenv("TMUX", "1")

	if closed := OpenEditor("code -w", "/n/a b.md")().(EditorClosed); closed.Err != nil {
		t.Fatalf("Err = %v", closed.Err)
	}
	got, _ := os.ReadFile(log)
	if want := "new-window\ncode\n-w\n/n/a b.md\n"; string(got) != want {
		t.Errorf("tmux got %q, want %q", got, want)
	}

	if err := os.WriteFile(filepath.Join(dir, "tmux"), []byte("#!/bin/sh\necho 'no server' >&2\nexit 1\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	closed := OpenEditor("code -w", "/n/a.md")().(EditorClosed)
	if closed.Err == nil || !strings.Contains(closed.Err.Error(), "no server") {
		t.Errorf("Err = %v, want tmux's message", closed.Err)
	}
}
