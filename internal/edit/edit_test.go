package edit

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestApplyToBytes_SingleEdit(t *testing.T) {
	src := []byte("hello world")
	out, err := applyToBytes(src, []Edit{{Start: 6, End: 11, NewText: "Go"}})
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != "hello Go" {
		t.Errorf("got %q", out)
	}
}

func TestApplyToBytes_MultipleNonOverlapping(t *testing.T) {
	// "foo bar foo" -> rename foo->qux at both positions
	src := []byte("foo bar foo")
	out, err := applyToBytes(src, []Edit{
		{Start: 0, End: 3, NewText: "qux"},
		{Start: 8, End: 11, NewText: "qux"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != "qux bar qux" {
		t.Errorf("got %q", out)
	}
}

func TestValidate_OverlapDetected(t *testing.T) {
	s := NewSet()
	s.Add(Edit{File: "a", Start: 0, End: 5, NewText: "x"})
	s.Add(Edit{File: "a", Start: 3, End: 7, NewText: "y"})
	if err := s.Validate(); err == nil {
		t.Fatalf("expected overlap error")
	}
}

func TestValidate_AdjacentOK(t *testing.T) {
	// End-exclusive: [0,3) and [3,6) touch but do not overlap.
	s := NewSet()
	s.Add(Edit{File: "a", Start: 0, End: 3, NewText: "x"})
	s.Add(Edit{File: "a", Start: 3, End: 6, NewText: "y"})
	if err := s.Validate(); err != nil {
		t.Fatalf("unexpected: %v", err)
	}
}

func TestValidate_NegativeRange(t *testing.T) {
	s := NewSet()
	s.Add(Edit{File: "a", Start: 5, End: 2, NewText: "x"})
	if err := s.Validate(); err == nil {
		t.Fatalf("expected invalid range error")
	}
}

func TestApply_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "src.go")
	if err := os.WriteFile(p, []byte("foo bar"), 0o644); err != nil {
		t.Fatal(err)
	}
	s := NewSet()
	s.Add(Edit{File: p, Start: 0, End: 3, NewText: "qux"})
	if err := s.Apply(); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(p)
	if string(got) != "qux bar" {
		t.Errorf("got %q", got)
	}
}

func TestApply_AtomicRollbackOnSecondFileFailure(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(dir, "a.go")
	b := filepath.Join(dir, "missing", "b.go") // dir does not exist
	if err := os.WriteFile(a, []byte("foo"), 0o644); err != nil {
		t.Fatal(err)
	}
	s := NewSet()
	s.Add(Edit{File: a, Start: 0, End: 3, NewText: "qux"})
	s.Add(Edit{File: b, Start: 0, End: 0, NewText: "x"})
	if err := s.Apply(); err == nil {
		t.Fatalf("expected error from missing file")
	}
	// a.go should be untouched.
	got, _ := os.ReadFile(a)
	if string(got) != "foo" {
		t.Errorf("a.go was modified: %q", got)
	}
}

func TestDryRun_DoesNotWrite(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "src.go")
	original := "foo bar"
	if err := os.WriteFile(p, []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}
	s := NewSet()
	s.Add(Edit{File: p, Start: 0, End: 3, NewText: "qux", Why: "rename foo -> qux"})
	out, err := s.DryRun()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "rename foo -> qux") {
		t.Errorf("dry-run output missing why: %q", out)
	}
	got, _ := os.ReadFile(p)
	if string(got) != original {
		t.Errorf("DryRun modified file: %q", got)
	}
}

func TestDryRun_Empty(t *testing.T) {
	s := NewSet()
	out, err := s.DryRun()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "no changes") {
		t.Errorf("got %q", out)
	}
}

func TestFiles_DistinctPreserveOrder(t *testing.T) {
	s := NewSet()
	s.Add(Edit{File: "b", Start: 0, End: 1, NewText: "x"})
	s.Add(Edit{File: "a", Start: 0, End: 1, NewText: "y"})
	s.Add(Edit{File: "b", Start: 2, End: 3, NewText: "z"})
	got := s.Files()
	if len(got) != 2 || got[0] != "b" || got[1] != "a" {
		t.Errorf("Files() = %v", got)
	}
}

func TestDryRun_GroupsRepeatedEditsOntoOneLine(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "src.go")
	// "foo" on lines 1, 3 and 4.
	src := "foo\nbar\nfoo\nfoo\n"
	if err := os.WriteFile(p, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	s := NewSet()
	for _, off := range []int{0, 8, 12} {
		s.Add(Edit{File: p, Start: off, End: off + 3, NewText: "qux", Why: "rename foo -> qux"})
	}
	out, err := s.DryRun()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, `1,3,4  "foo" -> "qux"  rename foo -> qux`) {
		t.Errorf("edits not collapsed onto one line: %q", out)
	}
	if strings.Contains(out, "3 edits\n    1 ") {
		t.Errorf("expected grouped output, got per-edit lines: %q", out)
	}
	// The file must be named once, not once per edit.
	if n := strings.Count(out, p); n != 1 {
		t.Errorf("path repeated %d times, want 1: %q", n, out)
	}
}

func TestDryRun_DistinctReasonsStaySeparate(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "src.go")
	if err := os.WriteFile(p, []byte("foo\nfoo\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	s := NewSet()
	s.Add(Edit{File: p, Start: 0, End: 3, NewText: "qux", Why: "rename declaration foo -> qux"})
	s.Add(Edit{File: p, Start: 4, End: 7, NewText: "qux", Why: "rename foo -> qux"})
	out, err := s.DryRun()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, `1  "foo" -> "qux"  rename declaration foo -> qux`) {
		t.Errorf("declaration line missing: %q", out)
	}
	if !strings.Contains(out, `2  "foo" -> "qux"  rename foo -> qux`) {
		t.Errorf("usage line missing: %q", out)
	}
}

func TestDryRun_PathsRelativeToWorkingDir(t *testing.T) {
	dir := t.TempDir()
	// t.TempDir may sit behind a symlink (/var -> /private/var on macOS);
	// resolve it so the path we compare against is the one Getwd reports.
	dir, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(dir, "pkg", "src.go")
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte("foo\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	prev, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(prev)

	s := NewSet()
	s.Add(Edit{File: p, Start: 0, End: 3, NewText: "qux", Why: "rename foo -> qux"})
	out, err := s.DryRun()
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join("pkg", "src.go")
	if !strings.Contains(out, want) {
		t.Errorf("want relative path %q in output: %q", want, out)
	}
	if strings.Contains(out, dir) {
		t.Errorf("absolute path leaked into output: %q", out)
	}
}

func TestRelTo_LeavesPathsOutsideBaseAlone(t *testing.T) {
	// A path that is not under base must stay absolute rather than turn
	// into a chain of "..", which would be unreadable and ambiguous.
	got := RelTo(filepath.FromSlash("/a/b"), filepath.FromSlash("/c/d/e.go"))
	if got != filepath.FromSlash("/c/d/e.go") {
		t.Errorf("got %q, want the absolute path unchanged", got)
	}
	if got := RelTo("", filepath.FromSlash("/c/d.go")); got != filepath.FromSlash("/c/d.go") {
		t.Errorf("empty base: got %q", got)
	}
	if got := RelTo(filepath.FromSlash("/a"), "rel/d.go"); got != "rel/d.go" {
		t.Errorf("already-relative path: got %q", got)
	}
}

func TestFormatLines_CollapsesRepeats(t *testing.T) {
	for _, tc := range []struct {
		in   []int
		want string
	}{
		{[]int{3}, "3"},
		{[]int{5, 5}, "5(2)"},
		{[]int{1, 5, 5, 9}, "1,5(2),9"},
		{[]int{7, 7, 7}, "7(3)"},
		{nil, ""},
	} {
		if got := formatLines(tc.in); got != tc.want {
			t.Errorf("formatLines(%v) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestMeasure_CountsDistinctLinesNotEdits(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "src.go")
	// Two edits on line 1, one on line 3.
	if err := os.WriteFile(p, []byte("foo foo\nbar\nfoo\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	s := NewSet()
	s.Add(Edit{File: p, Start: 0, End: 3, NewText: "qux", Why: "rename"})
	s.Add(Edit{File: p, Start: 4, End: 7, NewText: "qux", Why: "rename"})
	s.Add(Edit{File: p, Start: 12, End: 15, NewText: "qux", Why: "rename"})

	m := s.Measure()
	if m.Edits != 3 {
		t.Errorf("Edits = %d, want 3", m.Edits)
	}
	if m.LinesChanged != 2 {
		t.Errorf("LinesChanged = %d, want 2 (lines 1 and 3)", m.LinesChanged)
	}
	if m.Files != 1 {
		t.Errorf("Files = %d, want 1", m.Files)
	}
}

func TestMeasure_CountsMovedAndDeletedLines(t *testing.T) {
	dir := t.TempDir()
	moved := filepath.Join(dir, "moved.go")
	deleted := filepath.Join(dir, "deleted.go")
	if err := os.WriteFile(moved, []byte("a\nb\nc\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// No trailing newline: the last line must still count.
	if err := os.WriteFile(deleted, []byte("a\nb"), 0o644); err != nil {
		t.Fatal(err)
	}
	s := NewSet()
	s.AddMove(Move{From: moved, To: filepath.Join(dir, "sub", "moved.go"), Why: "move"})
	s.AddDelete(deleted, "delete")

	m := s.Measure()
	if m.LinesMoved != 3 {
		t.Errorf("LinesMoved = %d, want 3", m.LinesMoved)
	}
	if m.LinesDeleted != 2 {
		t.Errorf("LinesDeleted = %d, want 2", m.LinesDeleted)
	}
}

func TestMeasure_MissingFileIsSkippedNotFatal(t *testing.T) {
	s := NewSet()
	s.Add(Edit{File: filepath.Join(t.TempDir(), "gone.go"), Start: 0, End: 1, NewText: "x"})
	m := s.Measure() // must not panic
	if m.LinesChanged != 0 {
		t.Errorf("LinesChanged = %d, want 0", m.LinesChanged)
	}
}
