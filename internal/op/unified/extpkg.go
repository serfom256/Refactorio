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

// planExternalPackageRename rewrites references to a package that has no
// sources in the project: javax.* -> jakarta.*, or swapping one dependency's
// API for another.
//
// Nothing is created, moved or deleted — the project does not own the package,
// so only the references to it change.
//
// Matching is on dot boundaries, which is the reason to do this with a lexer
// rather than a search-and-replace. Rewriting javax.persistence must not touch
// javax.sql.DataSource, a JDK type that has to stay exactly where it is, nor a
// hypothetical javax.persistencex. Sub-packages, on the other hand, must move:
// javax.persistence.criteria.Root becomes jakarta.persistence.criteria.Root,
// because a migration that silently skipped them would be worse than useless.
// Strings and comments are never touched, since the lexers tag them apart from
// identifiers.
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

// inPackage reports whether a dotted path lies inside pkg — the package itself,
// or any member or sub-package of it. The trailing dot is what makes
// "javax.persistence" reject "javax.sql.DataSource" and "javax.persistencex".
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

// prefixEdit replaces the leading `from` of a dotted path with `to`, leaving
// the rest of the path alone. It verifies the bytes really are `from` first:
// an import written with whitespace around its dots would otherwise be
// corrupted by a blind length-based splice.
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

// prefixChain is a dotted identifier chain in code whose leading segments spell
// the package being rewritten.
type prefixChain struct {
	start     int // byte offset of the first identifier
	prefixEnd int // byte offset past the last identifier of the prefix
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
		// Require a following dot: the chain must continue into the package,
		// otherwise this is some unrelated identifier that merely matches.
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
