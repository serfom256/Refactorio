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
	"strconv"
	"strings"
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
//
// The output is deliberately terse, because it is usually read by a tool or
// an agent rather than scrolled by a human: paths are shown relative to the
// working directory, each file is named once for its whole group instead of
// once per edit, and edits sharing a replacement and reason collapse onto a
// single line listing the line numbers they occur on.
func (s *Set) DryRun() (string, error) {
	if err := s.Validate(); err != nil {
		return "", err
	}
	groups := s.groupByFile()
	files := s.Files()
	if len(files) == 0 && len(s.moves) == 0 && len(s.deletes) == 0 {
		return "no changes\n", nil
	}
	cwd, _ := os.Getwd()
	var b []byte
	b = append(b, fmt.Sprintf("%d files, %d edits, %d moves, %d deletes\n",
		len(files), s.Len(), len(s.moves), len(s.deletes))...)
	for _, f := range files {
		es := groups[f]
		b = append(b, fmt.Sprintf("  %s (%s)\n", RelTo(cwd, f), plural(len(es), "edit"))...)
		src, err := os.ReadFile(f)
		if err != nil {
			return "", err
		}
		for _, g := range groupEdits(src, es) {
			b = append(b, fmt.Sprintf("    %s  %q -> %q  %s\n",
				formatLines(g.lines), g.old, g.newText, g.why)...)
		}
	}
	for _, m := range s.moves {
		b = append(b, fmt.Sprintf("  move %s -> %s  %s\n", RelTo(cwd, m.From), RelTo(cwd, m.To), m.Why)...)
	}
	for _, d := range s.deletes {
		b = append(b, fmt.Sprintf("  delete %s  %s\n", RelTo(cwd, d.Path), d.Why)...)
	}
	return string(b), nil
}

// editGroup is a run of edits within one file that share the same original
// text, replacement and reason, and so can be reported as one line list.
type editGroup struct {
	lines   []int
	old     string
	newText string
	why     string
}

// groupEdits collapses edits with an identical (old, new, why) triple into a
// single group, keeping the order in which each distinct triple first appears.
func groupEdits(src []byte, es []Edit) []editGroup {
	starts := lineStarts(src)
	var out []editGroup
	idx := map[string]int{}
	for _, e := range es {
		old := safeSlice(src, e.Start, e.End)
		key := old + "\x00" + e.NewText + "\x00" + e.Why
		i, ok := idx[key]
		if !ok {
			i = len(out)
			idx[key] = i
			out = append(out, editGroup{old: old, newText: e.NewText, why: e.Why})
		}
		out[i].lines = append(out[i].lines, lineAt(starts, e.Start))
	}
	for i := range out {
		sort.Ints(out[i].lines)
	}
	return out
}

// lineStarts records the byte offset at which each line begins, so that line
// numbers can be resolved by binary search rather than by rescanning the file
// from the top for every edit.
func lineStarts(src []byte) []int {
	starts := []int{0}
	for i, c := range src {
		if c == '\n' {
			starts = append(starts, i+1)
		}
	}
	return starts
}

// lineAt returns the 1-based number of the line containing off.
func lineAt(starts []int, off int) int {
	i := sort.SearchInts(starts, off)
	if i < len(starts) && starts[i] == off {
		return i + 1
	}
	return i
}

// formatLines renders a sorted line list, collapsing repeats so that two
// edits on the same line read as "5(2)" rather than an ambiguous "5,5".
func formatLines(ns []int) string {
	var b strings.Builder
	for i := 0; i < len(ns); {
		j := i
		for j < len(ns) && ns[j] == ns[i] {
			j++
		}
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(strconv.Itoa(ns[i]))
		if n := j - i; n > 1 {
			b.WriteByte('(')
			b.WriteString(strconv.Itoa(n))
			b.WriteByte(')')
		}
		i = j
	}
	return b.String()
}

// RelTo shortens p for display when it lies under base. Paths outside base
// are left alone, so the output is never ambiguous. Reporting commands share
// this so every path Refactorio prints is relative to the working directory.
func RelTo(base, p string) string {
	if base == "" || !filepath.IsAbs(p) {
		return p
	}
	rel, err := filepath.Rel(base, p)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return p
	}
	return rel
}

func plural(n int, word string) string {
	if n == 1 {
		return fmt.Sprintf("%d %s", n, word)
	}
	return fmt.Sprintf("%d %ss", n, word)
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

// Metrics summarises how much code a Set changes. LinesChanged is the
// headline number: how many distinct source lines the refactor rewrote,
// which is a fairer measure of work done than the raw edit count (a rename
// often lands several edits on one line).
type Metrics struct {
	Files        int
	Edits        int
	LinesChanged int
	Moves        int
	LinesMoved   int
	Deletes      int
	LinesDeleted int
}

// Measure computes the Metrics for this Set.
//
// It must be called BEFORE Apply: it reads files as they are on disk now, and
// Apply rewrites, moves and removes them. Files that cannot be read are
// skipped rather than reported as an error — metrics are for statistics, and
// must never be the reason a refactor fails.
func (s *Set) Measure() Metrics {
	m := Metrics{
		Edits:   s.Len(),
		Moves:   len(s.moves),
		Deletes: len(s.deletes),
	}
	for f, es := range s.groupByFile() {
		m.Files++
		src, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		starts := lineStarts(src)
		seen := map[int]bool{}
		for _, e := range es {
			seen[lineAt(starts, e.Start)] = true
		}
		m.LinesChanged += len(seen)
	}
	for _, mv := range s.moves {
		m.LinesMoved += countLines(mv.From)
	}
	for _, d := range s.deletes {
		m.LinesDeleted += countLines(d.Path)
	}
	return m
}

// countLines returns the number of lines in a file, or 0 if unreadable.
// A trailing newline does not count as an extra line.
func countLines(path string) int {
	src, err := os.ReadFile(path)
	if err != nil || len(src) == 0 {
		return 0
	}
	n := 0
	for _, c := range src {
		if c == '\n' {
			n++
		}
	}
	if src[len(src)-1] != '\n' {
		n++
	}
	return n
}
