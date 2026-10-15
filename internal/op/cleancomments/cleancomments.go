package cleancomments

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/serfom256/refactorio/internal/edit"
)

type Candidate struct {
	File string
	Line int
	Text string

	start, end int
	lines      int
}

type Kept struct {
	File   string
	Line   int
	Reason string
	Text   string
}

type Result struct {
	Candidates []Candidate
	Kept       []Kept
	Scanned    int
	Comments   int
}

func Plan(files []string) (*edit.Set, *Result, error) {
	res := &Result{}
	set := edit.NewSet()

	sorted := append([]string(nil), files...)
	sort.Strings(sorted)
	for _, path := range sorted {
		fr, err := Scan(path)
		if err != nil {
			return nil, nil, err
		}
		res.Scanned++
		res.Comments += fr.Comments
		res.Kept = append(res.Kept, fr.Kept...)
		res.Candidates = append(res.Candidates, fr.Candidates...)
		for _, c := range fr.Candidates {
			set.Add(edit.Edit{
				File: c.File, Start: c.start, End: c.end, NewText: "",
				Why: "remove comment",
			})
		}
	}
	return set, res, nil
}

func Scan(path string) (*Result, error) {
	src, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	toks, lang := tokenize(path, src)
	if lang == "" {
		return nil, fmt.Errorf("unsupported file type: %s", filepath.Ext(path))
	}
	res := &Result{Scanned: 1}
	starts := lineStarts(src)

	for _, blk := range blocks(toks, src, lang) {
		res.Comments++
		text := string(src[blk.start:blk.end])
		if reason, ok := Keep(text); ok {
			res.Kept = append(res.Kept, Kept{File: path, Line: blk.line, Reason: reason, Text: firstLine(text)})
			continue
		}
		if blk.docstring && onlyStatementInBlock(src, blk) {
			res.Kept = append(res.Kept, Kept{
				File: path, Line: blk.line,
				Reason: "removing it would empty a Python block",
				Text:   firstLine(text),
			})
			continue
		}
		delStart, delEnd := removalRange(src, blk.start, blk.end, starts)
		res.Candidates = append(res.Candidates, Candidate{
			File: path, Line: blk.line,
			Text:  firstLine(text),
			start: delStart, end: delEnd,
			lines: strings.Count(string(src[delStart:delEnd]), "\n"),
		})
	}
	return res, nil
}

func onlyStatementInBlock(src []byte, blk block) bool {
	i := blk.end
	for i < len(src) {
		for i < len(src) && src[i] != '\n' {
			i++
		}
		if i < len(src) {
			i++
		}
		lineEnd := i
		for lineEnd < len(src) && src[lineEnd] != '\n' {
			lineEnd++
		}
		raw := string(src[i:lineEnd])
		line := strings.TrimSpace(raw)
		i = lineEnd
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		return indentOf(raw) < blk.indent
	}
	return true
}

type block struct {
	start, end int
	line       int
	indent     int
	lineCount  int
	docstring  bool
	trailing   bool
}

func blocks(toks []tok, src []byte, lang string) []block {
	var out []block
	for i := 0; i < len(toks); i++ {
		t := toks[i]
		if !isDoc(toks, i, lang) {
			continue
		}
		if !ownLine(src, t.Start) {
			if lastOnLine(src, t.End) {
				out = append(out, block{
					start: t.Start, end: t.End, line: t.Line,
					trailing: true, lineCount: 1,
				})
			}
			continue
		}
		if isPinned(t.Text) {
			out = append(out, block{
				start: t.Start, end: t.End, line: t.Line,
				indent: indentBefore(src, t.Start), lineCount: 1,
			})
			continue
		}
		b := block{
			start: t.Start, end: t.End, line: t.Line,
			docstring: t.Class == classString,
			indent:    indentBefore(src, t.Start),
			lineCount: 1,
		}
		for t.Class == classComment {
			next := nextMeaningful(toks, i+1)
			if next < 0 {
				break
			}
			n := toks[next]
			if n.Class != classComment || !ownLine(src, n.Start) {
				break
			}
			if isPinned(n.Text) {
				break
			}
			if !onlyBlankBetween(src, b.end, n.Start) {
				break
			}
			b.end = n.End
			b.lineCount++
			i = next
		}
		out = append(out, b)
	}
	return out
}

func nextMeaningful(toks []tok, i int) int {
	for ; i < len(toks); i++ {
		if strings.TrimSpace(toks[i].Text) != "" {
			return i
		}
	}
	return -1
}

func isDoc(toks []tok, i int, lang string) bool {
	t := toks[i]
	if t.Class == classComment {
		return true
	}
	if lang != "python" || t.Class != classString {
		return false
	}
	if !strings.HasPrefix(t.Text, `"""`) && !strings.HasPrefix(t.Text, "'''") {
		return false
	}
	for j := i - 1; j >= 0; j-- {
		if toks[j].Class == classComment || strings.TrimSpace(toks[j].Text) == "" {
			continue
		}
		return toks[j].Text == ":"
	}
	return true
}

func indentBefore(src []byte, off int) int {
	n := 0
	for i := off - 1; i >= 0 && src[i] != '\n'; i-- {
		if src[i] != ' ' && src[i] != '\t' {
			return 0
		}
		n++
	}
	return n
}

func lastOnLine(src []byte, off int) bool {
	for i := off; i < len(src) && src[i] != '\n'; i++ {
		if src[i] != ' ' && src[i] != '\t' && src[i] != '\r' {
			return false
		}
	}
	return true
}

func ownLine(src []byte, start int) bool {
	for i := start - 1; i >= 0; i-- {
		if src[i] == '\n' {
			return true
		}
		if src[i] != ' ' && src[i] != '\t' {
			return false
		}
	}
	return true
}

func onlyBlankBetween(src []byte, from, to int) bool {
	if to < from {
		return false
	}
	newlines := 0
	for i := from; i < to; i++ {
		switch src[i] {
		case '\n':
			newlines++
			if newlines > 1 {
				return false
			}
		case ' ', '\t', '\r':
		default:
			return false
		}
	}
	return true
}

func removalRange(src []byte, start, end int, starts []int) (int, int) {
	s := start
	if !ownLine(src, start) {
		for s > 0 && (src[s-1] == ' ' || src[s-1] == '\t') {
			s--
		}
		return s, end
	}
	for s > 0 && src[s-1] != '\n' {
		s--
	}
	e := end
	for e < len(src) && src[e] != '\n' {
		e++
	}
	if e < len(src) {
		e++
	}
	if blankLineBefore(src, s) && blankLineAt(src, e) {
		for e < len(src) && src[e] != '\n' {
			e++
		}
		if e < len(src) {
			e++
		}
	}
	return s, e
}

func blankLineBefore(src []byte, off int) bool {
	if off == 0 {
		return false
	}
	start := off - 1
	for start > 0 && src[start-1] != '\n' {
		start--
	}
	return strings.TrimSpace(string(src[start:off])) == ""
}

func blankLineAt(src []byte, off int) bool {
	if off >= len(src) {
		return false
	}
	e := off
	for e < len(src) && src[e] != '\n' {
		e++
	}
	return strings.TrimSpace(string(src[off:e])) == ""
}

func indentOf(raw string) int {
	n := 0
	for _, r := range raw {
		if r != ' ' && r != '\t' {
			break
		}
		n++
	}
	return n
}

func isCommentStart(line string) bool {
	for _, p := range []string{"//", "/*", "*", "#"} {
		if strings.HasPrefix(line, p) {
			return true
		}
	}
	return false
}

func lineStarts(src []byte) []int {
	out := []int{0}
	for i, b := range src {
		if b == '\n' {
			out = append(out, i+1)
		}
	}
	return out
}

func firstLine(text string) string {
	if i := strings.IndexByte(text, '\n'); i >= 0 {
		return strings.TrimSpace(text[:i]) + " …"
	}
	return strings.TrimSpace(text)
}

func (c Candidate) Lines() int { return c.lines }
