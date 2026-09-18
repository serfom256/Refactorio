// Package usages reports where a symbol is declared and referenced
// across the project. Read-only — no edits are written.
//
// Implementation: we ask the rename layer "what would you edit if I
// renamed this symbol?", then convert each edit into a finding. This
// guarantees the usages report and a subsequent rename touch exactly
// the same locations.
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

// Category classifies a finding.
type Category string

const (
	CatDeclaration Category = "declaration"
	CatUsage       Category = "usage"
	CatOverload    Category = "overload" // additional declaration (e.g. method overload)
)

// Finding is one location where the symbol appears.
type Finding struct {
	File     string   `json:"file"`
	Line     int      `json:"line"`
	Column   int      `json:"column"`
	Category Category `json:"category"`
	Text     string   `json:"text"`
}

// Result is the full report for a symbol.
type Result struct {
	Symbol   string    `json:"symbol"`
	Total    int       `json:"total"`
	Findings []Finding `json:"findings"`
}

// RenameFn is the rename dispatcher we ask "what would you change?".
// Callers (the CLI layer) wire this to unified.Rename to avoid a
// circular import.
type RenameFn func(inFile, name, newName, root string) (*edit.Set, error)

// Find runs the rename probe and converts edits to findings.
func Find(inFile, symbol, root string, rename RenameFn) (*Result, error) {
	set, err := rename(inFile, symbol, probeName, root)
	if err != nil {
		return nil, err
	}
	res := &Result{Symbol: symbol}

	// Cache file contents so we can compute line text and column once
	// per file.
	type fileCtx struct {
		src   []byte
		lines []int // byte offset at the start of each line (1-based via index+1)
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
		// Skip non-identifier edits: import rewrites, package decl
		// updates, file renames, etc.
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

// orderFindings groups by category:
//
//  1. Primary declaration (first one seen)
//  2. Usages, sorted by file then line
//  3. Additional declarations (overloads, etc.) at the bottom
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
	// Filter out edits that aren't on the identifier itself.
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

// FormatText writes a human-readable report to w.
func FormatText(w io.Writer, r *Result) error {
	if len(r.Findings) == 0 {
		_, err := fmt.Fprintf(w, "no usages of %q found\n", r.Symbol)
		return err
	}
	// Locations are shown relative to the working directory, and the column
	// is padded only to the widest of those — padding to absolute paths cost
	// far more than it bought in readability.
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
		// The category tag trails the source line, so plain usages — the
		// overwhelming majority — pay nothing for the column.
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

// FormatJSON writes the result as JSON to w.
func FormatJSON(w io.Writer, r *Result) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(r)
}
