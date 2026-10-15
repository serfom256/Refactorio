package usages

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"

	"github.com/serfom256/refactorio/internal/edit"
)

const probeName = "RefactorioUsageProbe"

type Category string

const (
	CatDeclaration Category = "declaration"
	CatUsage       Category = "usage"
	CatOverload    Category = "overload"
)

type Finding struct {
	File     string   `json:"file"`
	Line     int      `json:"line"`
	Column   int      `json:"column"`
	Category Category `json:"category"`
	Text     string   `json:"text"`
}

type Result struct {
	Symbol   string    `json:"symbol"`
	Total    int       `json:"total"`
	Findings []Finding `json:"findings"`
}

type RenameFn func(inFile, name, newName, root string) (*edit.Set, error)

func Find(inFile, symbol, root string, rename RenameFn) (*Result, error) {
	set, err := rename(inFile, symbol, probeName, root)
	if err != nil {
		return nil, err
	}
	res := &Result{Symbol: symbol}

	type fileCtx struct {
		src   []byte
		lines []int
	}
	cache := map[string]*fileCtx{}
	loadCtx := func(path string) *fileCtx {
		if c, ok := cache[path]; ok {
			return c
		}
		src, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		c := &fileCtx{src: src, lines: []int{0}}
		for i, b := range src {
			if b == '\n' {
				c.lines = append(c.lines, i+1)
			}
		}
		cache[path] = c
		return c
	}

	for _, e := range set.Edits() {
		if !isIdentifierEdit(e.Why) {
			continue
		}
		ctx := loadCtx(e.File)
		if ctx == nil {
			continue
		}
		line, col := positionAt(ctx.lines, e.Start)
		lineText := lineAt(ctx.src, ctx.lines, line)
		cat := CatUsage
		if strings.Contains(e.Why, "declaration") {
			cat = CatDeclaration
		}
		res.Findings = append(res.Findings, Finding{
			File:     e.File,
			Line:     line,
			Column:   col,
			Category: cat,
			Text:     strings.TrimRight(lineText, "\r\n"),
		})
	}

	res.Findings = orderFindings(res.Findings)
	res.Total = len(res.Findings)
	return res, nil
}

func orderFindings(fs []Finding) []Finding {
	var primary *Finding
	var usages, overloads []Finding
	for i := range fs {
		f := fs[i]
		switch f.Category {
		case CatDeclaration:
			if primary == nil {
				p := f
				primary = &p
			} else {
				f.Category = CatOverload
				overloads = append(overloads, f)
			}
		case CatOverload:
			overloads = append(overloads, f)
		default:
			usages = append(usages, f)
		}
	}
	sortByLoc := func(xs []Finding) {
		sort.SliceStable(xs, func(i, j int) bool {
			if xs[i].File != xs[j].File {
				return xs[i].File < xs[j].File
			}
			if xs[i].Line != xs[j].Line {
				return xs[i].Line < xs[j].Line
			}
			return xs[i].Column < xs[j].Column
		})
	}
	sortByLoc(usages)
	sortByLoc(overloads)
	var out []Finding
	if primary != nil {
		out = append(out, *primary)
	}
	out = append(out, usages...)
	out = append(out, overloads...)
	return out
}

func isIdentifierEdit(why string) bool {
	if strings.HasPrefix(why, "update import") ||
		strings.HasPrefix(why, "update use") ||
		strings.HasPrefix(why, "update static import") ||
		strings.HasPrefix(why, "update namespace") ||
		strings.HasPrefix(why, "update package") ||
		strings.HasPrefix(why, "add import") ||
		strings.HasPrefix(why, "remove import") ||
		strings.HasPrefix(why, "rename file") ||
		strings.HasPrefix(why, "rewrite inline FQN") {
		return false
	}
	return true
}

func positionAt(lineStarts []int, off int) (line, col int) {
	lo, hi := 0, len(lineStarts)-1
	for lo <= hi {
		mid := (lo + hi) / 2
		if lineStarts[mid] <= off {
			lo = mid + 1
		} else {
			hi = mid - 1
		}
	}
	if lo == 0 {
		return 1, off + 1
	}
	line = lo
	col = off - lineStarts[line-1] + 1
	return
}

func lineAt(src []byte, lineStarts []int, line int) string {
	if line < 1 || line > len(lineStarts) {
		return ""
	}
	start := lineStarts[line-1]
	end := len(src)
	if line < len(lineStarts) {
		end = lineStarts[line] - 1
	}
	if end > len(src) {
		end = len(src)
	}
	return string(src[start:end])
}

func FormatText(w io.Writer, r *Result) error {
	if len(r.Findings) == 0 {
		_, err := fmt.Fprintf(w, "no usages of %q found\n", r.Symbol)
		return err
	}
	cwd, _ := os.Getwd()
	locs := make([]string, len(r.Findings))
	maxLoc := 0
	for i, f := range r.Findings {
		locs[i] = fmt.Sprintf("%s:%d:%d", edit.RelTo(cwd, f.File), f.Line, f.Column)
		if len(locs[i]) > maxLoc {
			maxLoc = len(locs[i])
		}
	}
	for i, f := range r.Findings {
		line := strings.TrimSpace(f.Text)
		if f.Category != CatUsage {
			line = fmt.Sprintf("%s  (%s)", line, f.Category)
		}
		_, err := fmt.Fprintf(w, "%-*s  %s\n", maxLoc, locs[i], line)
		if err != nil {
			return err
		}
	}
	files := map[string]struct{}{}
	for _, f := range r.Findings {
		files[f.File] = struct{}{}
	}
	_, err := fmt.Fprintf(w, "\n%d usage(s) of %q across %d file(s).\n", r.Total, r.Symbol, len(files))
	return err
}

func FormatJSON(w io.Writer, r *Result) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(r)
}
