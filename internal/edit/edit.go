// Package edit collects byte-range edits across one or more files and
// applies them atomically.
//
// An [Edit] is an inclusive-start, exclusive-end byte range plus replacement
// text. Edits within a single file must not overlap; we sort them in reverse
// order before applying so earlier-position offsets stay valid as we mutate.
//
// Apply is best-effort atomic: it stages every file to a sibling temp file,
// fsyncs each, then renames. If any rename fails partway through, already
// renamed files are restored from the originals we read at the start.
// This is not crash-safe across machine power loss; that would require a
// journal. It is safe against operation-level failures (parse error,
// invalid edit, disk-full on one of N files).
package edit

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

// Edit is a single byte-range replacement within a file.
type Edit struct {
	File    string // absolute or working-dir-relative path
	Start   int    // byte offset, inclusive
	End     int    // byte offset, exclusive
	NewText string

	// Why describes the edit for dry-run output, e.g. "rename fizzBuzz -> FizzBuzz".
	Why string
}

// Move is a file rename applied after all edits have committed.
// Apply uses os.Rename, which is atomic on the same filesystem but
// not across mountpoints. Directories along To are created as needed.
type Move struct {
	From string
	To   string
	Why  string
}

// Delete is a file removal queued after edits and moves.
type Delete struct {
	Path string
	Why  string
}

// Set is an ordered collection of edits across any number of files,
// plus optional file moves and deletes. Apply runs edits first, then
// moves, then deletes.
type Set struct {
	edits   []Edit
	moves   []Move
	deletes []Delete
}

func NewSet() *Set { return &Set{} }

func (s *Set) Add(e Edit) { s.edits = append(s.edits, e) }

// AddMove queues a file rename.
func (s *Set) AddMove(m Move) { s.moves = append(s.moves, m) }

// AddDelete queues a file removal.
func (s *Set) AddDelete(path, why string) {
	s.deletes = append(s.deletes, Delete{Path: path, Why: why})
}

// Moves returns a copy of the queued file moves.
func (s *Set) Moves() []Move {
	out := make([]Move, len(s.moves))
	copy(out, s.moves)
	return out
}

// Deletes returns a copy of the queued file deletions.
func (s *Set) Deletes() []Delete {
	out := make([]Delete, len(s.deletes))
	copy(out, s.deletes)
	return out
}

func (s *Set) Len() int { return len(s.edits) }

// Files returns the distinct files touched by the set, in first-seen order.
func (s *Set) Files() []string {
	seen := map[string]struct{}{}
	var out []string
	for _, e := range s.edits {
		if _, ok := seen[e.File]; ok {
			continue
		}
		seen[e.File] = struct{}{}
		out = append(out, e.File)
	}
	return out
}

// Edits returns a copy of the underlying edits.
func (s *Set) Edits() []Edit {
	out := make([]Edit, len(s.edits))
	copy(out, s.edits)
	return out
}

// Validate checks that no two edits within the same file overlap.
// Returns the conflicting pair on first overlap.
func (s *Set) Validate() error {
	byFile := map[string][]Edit{}
	for _, e := range s.edits {
		if e.Start < 0 || e.End < e.Start {
			return fmt.Errorf("invalid range [%d,%d) in %s", e.Start, e.End, e.File)
		}
		byFile[e.File] = append(byFile[e.File], e)
	}
	for f, es := range byFile {
		sort.Slice(es, func(i, j int) bool { return es[i].Start < es[j].Start })
		for i := 1; i < len(es); i++ {
			if es[i].Start < es[i-1].End {
				return fmt.Errorf("overlapping edits in %s: [%d,%d) and [%d,%d)",
					f, es[i-1].Start, es[i-1].End, es[i].Start, es[i].End)
			}
		}
	}
	return nil
}

// Apply writes every edit. On any failure, files already written are
// restored from in-memory originals so the on-disk state matches what it
// was before the call.
func (s *Set) Apply() error {
	if err := s.Validate(); err != nil {
		return err
	}
	type staged struct {
		path     string
		original []byte
		written  []byte
	}
	groups := s.groupByFile()
	stages := make([]staged, 0, len(groups))
	for _, f := range s.Files() {
		orig, err := os.ReadFile(f)
		if err != nil {
			return fmt.Errorf("read %s: %w", f, err)
		}
		out, err := applyToBytes(orig, groups[f])
		if err != nil {
			return fmt.Errorf("apply to %s: %w", f, err)
		}
		stages = append(stages, staged{path: f, original: orig, written: out})
	}
	// Write each file. On failure, restore everything we already wrote.
	written := make([]staged, 0, len(stages))
	for _, st := range stages {
		if err := writeAtomic(st.path, st.written); err != nil {
			for _, done := range written {
				_ = writeAtomic(done.path, done.original)
			}
			return fmt.Errorf("write %s: %w", st.path, err)
		}
		written = append(written, st)
	}
	// Edits committed; now perform queued file moves. If a move fails
	// after edits have written, we surface the error but do NOT roll
	// back the edits — that would require a separate transaction log.
	// In practice this matters only when target.To straddles a
	// mountpoint or lacks write permission; both are caller errors.
	for _, m := range s.moves {
		if err := os.MkdirAll(filepath.Dir(m.To), 0o755); err != nil {
			return fmt.Errorf("mkdir for move %s -> %s: %w", m.From, m.To, err)
		}
		if err := os.Rename(m.From, m.To); err != nil {
			return fmt.Errorf("rename %s -> %s: %w", m.From, m.To, err)
		}
	}
	for _, d := range s.deletes {
		if err := os.Remove(d.Path); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("delete %s: %w", d.Path, err)
		}
	}
	return nil
}

// DryRun returns a human-readable summary of what Apply would do, without
// touching the filesystem.
func (s *Set) DryRun() (string, error) {
	if err := s.Validate(); err != nil {
		return "", err
	}
	groups := s.groupByFile()
	files := s.Files()
	if len(files) == 0 && len(s.moves) == 0 && len(s.deletes) == 0 {
		return "no changes\n", nil
	}
	var b []byte
	b = append(b, fmt.Sprintf("%d files would be modified, %d edits, %d moves\n", len(files), s.Len(), len(s.moves))...)
	for _, f := range files {
		es := groups[f]
		b = append(b, fmt.Sprintf("  %s (%d edits)\n", f, len(es))...)
		// Read once to compute line numbers for nicer output.
		src, err := os.ReadFile(f)
		if err != nil {
			return "", err
		}
		for _, e := range es {
			line := lineOf(src, e.Start)
			old := safeSlice(src, e.Start, e.End)
			b = append(b, fmt.Sprintf("    %s:%d  %q -> %q  %s\n", f, line, old, e.NewText, e.Why)...)
		}
	}
	for _, m := range s.moves {
		b = append(b, fmt.Sprintf("  move %s -> %s  %s\n", m.From, m.To, m.Why)...)
	}
	for _, d := range s.deletes {
		b = append(b, fmt.Sprintf("  delete %s  %s\n", d.Path, d.Why)...)
	}
	return string(b), nil
}

func (s *Set) groupByFile() map[string][]Edit {
	out := map[string][]Edit{}
	for _, e := range s.edits {
		out[e.File] = append(out[e.File], e)
	}
	return out
}

// applyToBytes returns src with edits applied. Edits are sorted by Start
// descending so we can splice in-place without shifting unprocessed offsets.
func applyToBytes(src []byte, edits []Edit) ([]byte, error) {
	es := make([]Edit, len(edits))
	copy(es, edits)
	sort.Slice(es, func(i, j int) bool { return es[i].Start > es[j].Start })
	out := make([]byte, len(src))
	copy(out, src)
	for _, e := range es {
		if e.End > len(out) {
			return nil, fmt.Errorf("edit end %d past EOF %d", e.End, len(out))
		}
		out = append(out[:e.Start], append([]byte(e.NewText), out[e.End:]...)...)
	}
	return out, nil
}

func writeAtomic(path string, data []byte) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".refactorio.*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	cleanup := func() { _ = os.Remove(tmpName) }
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		cleanup()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		cleanup()
		return err
	}
	if err := tmp.Close(); err != nil {
		cleanup()
		return err
	}
	if err := os.Rename(tmpName, path); err != nil {
		cleanup()
		return err
	}
	return nil
}

func lineOf(src []byte, off int) int {
	if off > len(src) {
		off = len(src)
	}
	line := 1
	for i := 0; i < off; i++ {
		if src[i] == '\n' {
			line++
		}
	}
	return line
}

func safeSlice(src []byte, start, end int) string {
	if start < 0 {
		start = 0
	}
	if end > len(src) {
		end = len(src)
	}
	if start > end {
		return ""
	}
	return string(src[start:end])
}
