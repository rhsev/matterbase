package frame

import (
	"strings"
	"testing"
)

func TestComputeBareTable(t *testing.T) {
	s := Compute(Config{}, 100, 40)
	if s.TopbarHeight != 0 {
		t.Errorf("topbar height = %d, want 0 (hidden)", s.TopbarHeight)
	}
	if s.StatusHeight != 1 {
		t.Errorf("status height = %d, want 1 (default)", s.StatusHeight)
	}
	if s.BuilderWidth != 0 || s.PreviewWidth != 0 {
		t.Errorf("builder/preview = %d/%d, want 0/0 (hidden)", s.BuilderWidth, s.PreviewWidth)
	}
	if s.TableWidth != 100 {
		t.Errorf("table width = %d, want 100 (full width)", s.TableWidth)
	}
	if s.MainHeight != 39 {
		t.Errorf("main height = %d, want 39 (40 - status 1)", s.MainHeight)
	}
}

func TestComputeThreeZone(t *testing.T) {
	s := Compute(Config{
		ShowTopbar: true, ShowBuilder: true, ShowPreview: true,
	}, 200, 50)

	if s.TopbarHeight != 1 {
		t.Errorf("topbar height = %d, want 1 (default)", s.TopbarHeight)
	}
	if s.BuilderWidth != 30 {
		t.Errorf("builder width = %d, want 30 (default)", s.BuilderWidth)
	}
	wantPreview := 80 // 40% of 200 = 80, at the default max
	if s.PreviewWidth != wantPreview {
		t.Errorf("preview width = %d, want %d", s.PreviewWidth, wantPreview)
	}
	if got, want := s.BuilderWidth+s.TableWidth+s.PreviewWidth, 200; got != want {
		t.Errorf("zone widths sum to %d, want %d", got, want)
	}
	if s.MainHeight != 48 {
		t.Errorf("main height = %d, want 48 (50 - topbar 1 - status 1)", s.MainHeight)
	}
}

func TestComputePreviewCapsAtMax(t *testing.T) {
	// 40% of 300 is 120, above the 80-column max — must clamp.
	s := Compute(Config{ShowPreview: true}, 300, 40)
	if s.PreviewWidth != 80 {
		t.Errorf("preview width = %d, want 80 (clamped to PreviewMax)", s.PreviewWidth)
	}
}

func TestComputeCustomPreviewSize(t *testing.T) {
	s := Compute(Config{ShowPreview: true, PreviewPct: 0.5, PreviewMax: 40}, 100, 40)
	if s.PreviewWidth != 40 {
		t.Errorf("preview width = %d, want 40 (50%% of 100 clamped to custom max 40)", s.PreviewWidth)
	}
}

func TestComputeNarrowTerminalShrinksPreviewFirst(t *testing.T) {
	// width 60, builder 30 + naive preview (40% = 24) would leave the
	// table under minTable (20). Preview must give way before builder.
	s := Compute(Config{ShowBuilder: true, ShowPreview: true}, 60, 40)

	if s.BuilderWidth != 30 {
		t.Errorf("builder width = %d, want 30 (defended; preview shrinks first)", s.BuilderWidth)
	}
	if s.TableWidth < minTable {
		t.Errorf("table width = %d, below the defended minimum %d", s.TableWidth, minTable)
	}
	if got, want := s.BuilderWidth+s.TableWidth+s.PreviewWidth, 60; got != want {
		t.Errorf("zone widths sum to %d, want %d", got, want)
	}
}

func TestComputeExtremelyNarrowShrinksBuilderToo(t *testing.T) {
	// Not enough room even with preview at 0: builder must give way too.
	s := Compute(Config{ShowBuilder: true, ShowPreview: true}, 25, 40)

	if s.PreviewWidth != 0 {
		t.Errorf("preview width = %d, want 0 (shrunk away first)", s.PreviewWidth)
	}
	if s.BuilderWidth+s.TableWidth+s.PreviewWidth != 25 {
		t.Errorf("zone widths sum to %d, want 25", s.BuilderWidth+s.TableWidth+s.PreviewWidth)
	}
	if s.TableWidth < 0 {
		t.Errorf("table width = %d, must never go negative", s.TableWidth)
	}
}

func TestInner(t *testing.T) {
	cases := map[int]int{0: 0, 1: 0, 2: 0, 3: 1, 30: 28}
	for n, want := range cases {
		if got := Inner(n); got != want {
			t.Errorf("Inner(%d) = %d, want %d", n, got, want)
		}
	}
}

func TestJoinRowOmitsHiddenZones(t *testing.T) {
	got := JoinRow("", "table", "")
	if got != "table" {
		t.Errorf("JoinRow with hidden builder/preview = %q, want %q", got, "table")
	}
}

func TestJoinColumnOmitsHiddenTopbar(t *testing.T) {
	got := strings.Split(JoinColumn("", "main", "status"), "\n")
	want := []string{"main", "status"}
	assertTrimmedLines(t, got, want)
}

func TestJoinColumnIncludesTopbar(t *testing.T) {
	got := strings.Split(JoinColumn("top", "main", "status"), "\n")
	want := []string{"top", "main", "status"}
	assertTrimmedLines(t, got, want)
}

// assertTrimmedLines compares line content, ignoring the trailing padding
// lipgloss.JoinVertical adds to make every line the block's full width.
func assertTrimmedLines(t *testing.T, got, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("got %d lines, want %d: %q", len(got), len(want), got)
	}
	for i := range want {
		if strings.TrimRight(got[i], " ") != want[i] {
			t.Errorf("line %d = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestInputFocus(t *testing.T) {
	var f InputFocus
	if f.Active() {
		t.Error("zero value must start blurred")
	}
	f.Focus()
	if !f.Active() {
		t.Error("Focus() must set Active()")
	}
	f.Blur()
	if f.Active() {
		t.Error("Blur() must clear Active()")
	}
}
