package main

import (
	"strings"
	"testing"
)

func TestClampWidthTruncatesInsteadOfWrapping(t *testing.T) {
	long := strings.Repeat("x", 200)
	got := clampWidth(long, 60)
	if strings.Contains(got, "\n") {
		t.Fatalf("clampWidth must not introduce line breaks, got %d lines", strings.Count(got, "\n")+1)
	}
	if len(got) != 60 {
		t.Fatalf("clampWidth(long, 60) length = %d, want 60", len(got))
	}
}

func TestClampWidthLeavesShortContentUntouched(t *testing.T) {
	short := "short line"
	if got := clampWidth(short, 60); got != short {
		t.Errorf("clampWidth(%q, 60) = %q, want unchanged", short, got)
	}
}

func TestClampWidthTruncatesEveryLine(t *testing.T) {
	multi := strings.Repeat("a", 100) + "\n" + strings.Repeat("b", 100)
	got := clampWidth(multi, 60)
	for i, line := range strings.Split(got, "\n") {
		if len(line) > 60 {
			t.Errorf("line %d has length %d, want <= 60", i, len(line))
		}
	}
	if strings.Count(got, "\n") != 1 {
		t.Errorf("clampWidth must not add or remove lines, got %d newlines, want 1", strings.Count(got, "\n"))
	}
}
