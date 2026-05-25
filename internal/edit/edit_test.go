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
