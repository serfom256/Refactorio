package edit

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

type Edit struct {
	File    string
	Start   int
	End     int
	NewText string

	Why string
}

type Move struct {
	From string
	To   string
	Why  string
}

type Delete struct {
	Path string
	Why  string
}

type Set struct {
	edits   []Edit
	moves   []Move
	deletes []Delete
}

func NewSet() *Set { return &Set{} }

func (s *Set) Add(e Edit) { s.edits = append(s.edits, e) }

func (s *Set) AddMove(m Move) { s.moves = append(s.moves, m) }

func (s *Set) AddDelete(path, why string) {
	s.deletes = append(s.deletes, Delete{Path: path, Why: why})
}

func (s *Set) Moves() []Move {
	out := make([]Move, len(s.moves))
	copy(out, s.moves)
	return out
}

func (s *Set) Deletes() []Delete {
	out := make([]Delete, len(s.deletes))
	copy(out, s.deletes)
	return out
}

func (s *Set) Len() int { return len(s.edits) }

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

func (s *Set) Edits() []Edit {
	out := make([]Edit, len(s.edits))
	copy(out, s.edits)
	return out
}

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

type editGroup struct {
	lines   []int
	old     string
	newText string
	why     string
}

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

func lineStarts(src []byte) []int {
	starts := []int{0}
	for i, c := range src {
		if c == '\n' {
			starts = append(starts, i+1)
		}
	}
	return starts
}

func lineAt(starts []int, off int) int {
	i := sort.SearchInts(starts, off)
	if i < len(starts) && starts[i] == off {
		return i + 1
	}
	return i
}

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

type Metrics struct {
	Files        int
	Edits        int
	LinesChanged int
	Moves        int
	LinesMoved   int
	Deletes      int
	LinesDeleted int
}

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
