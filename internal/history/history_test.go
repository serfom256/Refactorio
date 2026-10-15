package history

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/serfom256/refactorio/internal/edit"
)

func useTempHome(t *testing.T) string {
	t.Helper()
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	return tmp
}

func TestFindProjectRoot_Marker(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module x"), 0o644); err != nil {
		t.Fatal(err)
	}
	sub := filepath.Join(root, "pkg", "deep")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	got := FindProjectRoot(sub)
	if got != root {
		t.Errorf("FindProjectRoot(%q) = %q, want %q", sub, got, root)
	}
}

func TestFindProjectRoot_NoMarker(t *testing.T) {
	dir := t.TempDir()
	got := FindProjectRoot(dir)
	if got != dir {
		t.Errorf("got %q, want %q (fallback)", got, dir)
	}
}

func TestSnapshotSaveRollback_PerProject(t *testing.T) {
	useTempHome(t)

	projA := t.TempDir()
	projB := t.TempDir()

	fileA := filepath.Join(projA, "a.txt")
	if err := os.WriteFile(fileA, []byte("original A"), 0o644); err != nil {
		t.Fatal(err)
	}
	fileB := filepath.Join(projB, "b.txt")
	if err := os.WriteFile(fileB, []byte("original B"), 0o644); err != nil {
		t.Fatal(err)
	}

	setA := edit.NewSet()
	setA.Add(edit.Edit{File: fileA, Start: 0, End: 8, NewText: "MODIFIED"})
	csA, err := Snapshot(setA, "rename A", projA)
	if err != nil {
		t.Fatal(err)
	}
	if err := setA.Apply(); err != nil {
		t.Fatal(err)
	}
	if err := Save(csA); err != nil {
		t.Fatal(err)
	}

	setB := edit.NewSet()
	setB.Add(edit.Edit{File: fileB, Start: 0, End: 8, NewText: "MODIFIED"})
	csB, err := Snapshot(setB, "rename B", projB)
	if err != nil {
		t.Fatal(err)
	}
	if err := setB.Apply(); err != nil {
		t.Fatal(err)
	}
	if err := Save(csB); err != nil {
		t.Fatal(err)
	}

	entriesA, _ := List(projA)
	entriesB, _ := List(projB)
	if len(entriesA) != 1 {
		t.Errorf("projA entries = %d, want 1: %v", len(entriesA), entriesA)
	}
	if len(entriesB) != 1 {
		t.Errorf("projB entries = %d, want 1: %v", len(entriesB), entriesB)
	}

	if _, err := Rollback(projA); err != nil {
		t.Fatal(err)
	}
	gotA, _ := os.ReadFile(fileA)
	if !strings.HasPrefix(string(gotA), "original A") {
		t.Errorf("projA rollback did not restore: %q", gotA)
	}
	gotB, _ := os.ReadFile(fileB)
	if !strings.HasPrefix(string(gotB), "MODIFIED") {
		t.Errorf("projB was wrongly affected by projA rollback: %q", gotB)
	}

	entriesA, _ = List(projA)
	entriesB, _ = List(projB)
	if len(entriesA) != 0 {
		t.Errorf("projA entries after rollback = %d, want 0", len(entriesA))
	}
	if len(entriesB) != 1 {
		t.Errorf("projB entries = %d, want 1 (untouched by projA rollback)", len(entriesB))
	}
}

func TestRollback_NoHistory(t *testing.T) {
	useTempHome(t)
	proj := t.TempDir()
	if _, err := Rollback(proj); err == nil {
		t.Fatal("expected error when no history exists")
	}
}

func TestSave_PrunesOldEntries(t *testing.T) {
	useTempHome(t)
	proj := t.TempDir()
	file := filepath.Join(proj, "f.txt")
	if err := os.WriteFile(file, []byte("v0"), 0o644); err != nil {
		t.Fatal(err)
	}

	for i := 0; i < MaxEntries+3; i++ {
		set := edit.NewSet()
		set.Add(edit.Edit{File: file, Start: 0, End: 2, NewText: "vX"})
		cs, _ := Snapshot(set, "op", proj)
		if err := Save(cs); err != nil {
			t.Fatal(err)
		}
	}
	entries, _ := List(proj)
	if len(entries) != MaxEntries {
		t.Errorf("entries = %d, want %d", len(entries), MaxEntries)
	}
}
