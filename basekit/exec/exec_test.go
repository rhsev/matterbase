package exec

import (
	"context"
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
