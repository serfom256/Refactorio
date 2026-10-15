package unified

import (
	"fmt"
	"os"
	"strings"

	"github.com/serfom256/refactorio/internal/edit"
	"github.com/serfom256/refactorio/internal/lang/common"
	"github.com/serfom256/refactorio/internal/lang/java"
	"github.com/serfom256/refactorio/internal/lang/kotlin"
	"github.com/serfom256/refactorio/internal/project"
)

func planExternalPackageRename(from, to string, p *project.Project) (*edit.Set, int, error) {
	set := edit.NewSet()
	refs := 0

	javaFiles, err := p.SourceFiles(".java")
	if err == nil {
		for _, path := range javaFiles {
			n, err := rewriteJavaExternal(path, from, to, set)
			if err != nil {
				continue
			}
			refs += n
		}
	}

	ktFiles, err := p.SourceFiles(".kt", ".kts")
	if err == nil {
		for _, path := range ktFiles {
			n, err := rewriteKotlinExternal(path, from, to, set)
			if err != nil {
				continue
			}
			refs += n
		}
	}

	return set, refs, nil
}

func inPackage(path, pkg string) bool {
	return path == pkg || strings.HasPrefix(path, pkg+".")
}

func rewriteJavaExternal(path, from, to string, set *edit.Set) (int, error) {
	src, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	syms, err := java.ExtractSymbols(src)
	if err != nil || syms == nil {
		return 0, err
	}
	n := 0
	skip := []common.Range{syms.PackageRange}
	for _, imp := range syms.Imports {
		skip = append(skip, imp.Range)
		if !inPackage(imp.Path, from) {
			continue
		}
		if e, ok := prefixEdit(path, src, imp.PathRange.Start, from, to); ok {
			set.Add(e)
			n++
		}
	}
	tokens := java.Tokenize(src)
	for _, c := range javaChainsWithPrefix(tokens, from) {
		if withinAny(skip, c.start) {
			continue
		}
		set.Add(edit.Edit{
			File: path, Start: c.start, End: c.prefixEnd, NewText: to,
			Why: fmt.Sprintf("rewrite external package %s -> %s", from, to),
		})
		n++
	}
	return n, nil
}

func rewriteKotlinExternal(path, from, to string, set *edit.Set) (int, error) {
	src, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	syms, err := kotlin.ExtractSymbols(src)
	if err != nil || syms == nil {
		return 0, err
	}
	n := 0
	skip := []common.Range{syms.PackageRange}
	for _, imp := range syms.Imports {
		skip = append(skip, imp.Range)
		if !inPackage(imp.Path, from) {
			continue
		}
		if e, ok := prefixEdit(path, src, imp.PathRange.Start, from, to); ok {
			set.Add(e)
			n++
		}
	}
	tokens := kotlin.Tokenize(src)
	for _, c := range kotlinChainsWithPrefix(tokens, from) {
		if withinAny(skip, c.start) {
			continue
		}
		set.Add(edit.Edit{
			File: path, Start: c.start, End: c.prefixEnd, NewText: to,
			Why: fmt.Sprintf("rewrite external package %s -> %s", from, to),
		})
		n++
	}
	return n, nil
}

func prefixEdit(path string, src []byte, start int, from, to string) (edit.Edit, bool) {
	end := start + len(from)
	if start < 0 || end > len(src) || string(src[start:end]) != from {
		return edit.Edit{}, false
	}
	return edit.Edit{
		File: path, Start: start, End: end, NewText: to,
		Why: fmt.Sprintf("rewrite external package %s -> %s", from, to),
	}, true
}

type prefixChain struct {
	start     int
	prefixEnd int
}

func javaChainsWithPrefix(tokens []java.Token, pkg string) []prefixChain {
	segs := strings.Split(pkg, ".")
	var out []prefixChain
	for i := 0; i < len(tokens); {
		if tokens[i].Kind != java.TIdent {
			i++
			continue
		}
		matched, endTok := 0, i
		j := i
		for matched < len(segs) {
			if j >= len(tokens) || tokens[j].Kind != java.TIdent || tokens[j].Text != segs[matched] {
				break
			}
			endTok = j
			matched++
			if matched == len(segs) {
				break
			}
			if j+1 >= len(tokens) || tokens[j+1].Kind != java.TPunct || tokens[j+1].Text != "." {
				break
			}
			j += 2
		}
		if matched == len(segs) && endTok+1 < len(tokens) &&
			tokens[endTok+1].Kind == java.TPunct && tokens[endTok+1].Text == "." {
			out = append(out, prefixChain{start: tokens[i].Start, prefixEnd: tokens[endTok].End})
			i = endTok + 1
			continue
		}
		i++
	}
	return out
}

func kotlinChainsWithPrefix(tokens []kotlin.Token, pkg string) []prefixChain {
	segs := strings.Split(pkg, ".")
	var out []prefixChain
	for i := 0; i < len(tokens); {
		if tokens[i].Kind != kotlin.TIdent {
			i++
			continue
		}
		matched, endTok := 0, i
		j := i
		for matched < len(segs) {
			if j >= len(tokens) || tokens[j].Kind != kotlin.TIdent || tokens[j].Text != segs[matched] {
				break
			}
			endTok = j
			matched++
			if matched == len(segs) {
				break
			}
			if j+1 >= len(tokens) || tokens[j+1].Kind != kotlin.TPunct || tokens[j+1].Text != "." {
				break
			}
			j += 2
		}
		if matched == len(segs) && endTok+1 < len(tokens) &&
			tokens[endTok+1].Kind == kotlin.TPunct && tokens[endTok+1].Text == "." {
			out = append(out, prefixChain{start: tokens[i].Start, prefixEnd: tokens[endTok].End})
			i = endTok + 1
			continue
		}
		i++
	}
	return out
}

func withinAny(ranges []common.Range, off int) bool {
	for _, r := range ranges {
		if off >= r.Start && off < r.End {
			return true
		}
	}
	return false
}
