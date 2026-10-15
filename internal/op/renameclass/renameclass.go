package renameclass

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/serfom256/refactorio/internal/edit"
	"github.com/serfom256/refactorio/internal/index"
	"github.com/serfom256/refactorio/internal/lang/java"
)

type Request struct {
	FQN     string
	NewName string
	Index   *index.Index
}

func Plan(req Request) (*edit.Set, error) {
	if req.Index == nil {
		return nil, fmt.Errorf("nil index")
	}
	if !java.IsValidIdentifier(req.NewName) {
		return nil, fmt.Errorf("%q is not a valid Java identifier", req.NewName)
	}
	target := req.Index.LookupFQN(req.FQN)
	if target == nil {
		simple := simpleName(req.FQN)
		candidates := req.Index.LookupType(simple)
		if len(candidates) == 0 {
			return nil, fmt.Errorf("no class %q in project", req.FQN)
		}
		var fqns []string
		for _, c := range candidates {
			fqns = append(fqns, c.FQN)
		}
		return nil, fmt.Errorf("no class %q; did you mean: %s",
			req.FQN, strings.Join(fqns, ", "))
	}
	if target.Name == req.NewName {
		return nil, fmt.Errorf("class is already named %q", req.NewName)
	}

	set := edit.NewSet()

	for _, path := range req.Index.FilesImporting(req.FQN) {
		fe := req.Index.File(path)
		if fe == nil || fe.Symbols == nil {
			continue
		}
		if !resolvesTarget(fe, target) {
			continue
		}
		if err := planFile(fe, target, req.NewName, set); err != nil {
			return nil, fmt.Errorf("plan %s: %w", path, err)
		}
	}

	newFQN := strings.TrimSuffix(req.FQN, "."+target.Name) + "." + req.NewName
	for _, fe := range req.Index.Files() {
		if fe.Err != nil || fe.Symbols == nil {
			continue
		}
		src, err := os.ReadFile(fe.Path)
		if err != nil {
			continue
		}
		tokens := java.Tokenize(src)
		scanAndRenameFQN(fe.Path, tokens, req.FQN, req.NewName, fe.Symbols, set)
	}

	if set.Len() == 0 {
		return nil, fmt.Errorf("no references to %s found", req.FQN)
	}
	_ = newFQN

	oldBase := filepath.Base(target.File)
	if oldBase == target.Name+".java" {
		newFile := filepath.Join(filepath.Dir(target.File), req.NewName+".java")
		set.AddMove(edit.Move{
			From: target.File,
			To:   newFile,
			Why:  fmt.Sprintf("rename file %s -> %s.java", oldBase, req.NewName),
		})
	}

	return set, nil
}

func planFile(fe *index.FileEntry, target *index.TypeDecl, newName string, set *edit.Set) error {
	src, err := os.ReadFile(fe.Path)
	if err != nil {
		return err
	}
	tokens := java.Tokenize(src)

	if fe.Path == target.File {
		set.Add(edit.Edit{
			File: fe.Path, Start: target.NameRange.Start, End: target.NameRange.End,
			NewText: newName, Why: fmt.Sprintf("rename type %s -> %s (declaration)", target.Name, newName),
		})
		for _, m := range target.Methods {
			if m.Kind != "constructor" {
				continue
			}
			set.Add(edit.Edit{
				File: fe.Path, Start: m.NameRange.Start, End: m.NameRange.End,
				NewText: newName, Why: fmt.Sprintf("rename constructor %s -> %s", target.Name, newName),
			})
		}
	}

	for i, tok := range tokens {
		if tok.Kind != java.TIdent || tok.Text != target.Name {
			continue
		}
		if fe.Path == target.File && overlapsDecl(tok, target) {
			continue
		}
		if inImportRange(tok, fe.Symbols) {
			continue
		}
		if inPackageRange(tok, fe.Symbols) {
			continue
		}
		if isLocalDeclJava(tokens, i) {
			continue
		}
		set.Add(edit.Edit{
			File: fe.Path, Start: tok.Start, End: tok.End,
			NewText: newName, Why: fmt.Sprintf("rename type %s -> %s", target.Name, newName),
		})
	}

	for _, imp := range fe.Symbols.Imports {
		newPath, ok := rewriteImport(imp, target.FQN, target.Package, newName)
		if !ok {
			continue
		}
		set.Add(edit.Edit{
			File: fe.Path, Start: imp.PathRange.Start, End: imp.PathRange.End,
			NewText: newPath, Why: fmt.Sprintf("update import for %s", target.FQN),
		})
	}
	return nil
}

func overlapsDecl(tok java.Token, target *index.TypeDecl) bool {
	if tok.Start == target.NameRange.Start && tok.End == target.NameRange.End {
		return true
	}
	for _, m := range target.Methods {
		if m.Kind == "constructor" && tok.Start == m.NameRange.Start && tok.End == m.NameRange.End {
			return true
		}
	}
	return false
}

func isLocalDeclJava(tokens []java.Token, i int) bool {
	if i <= 0 || i+1 >= len(tokens) {
		return false
	}
	prev := prevNonCommentJava(tokens, i-1)
	next := nextNonCommentJava(tokens, i+1)
	if prev != nil && prev.Kind == java.TIdent && !isJavaModifier(prev.Text) &&
		next != nil && (next.Text == "=" || next.Text == ";" || next.Text == "," || next.Text == ")") {
		return true
	}
	return false
}

func prevNonCommentJava(tokens []java.Token, from int) *java.Token {
	for from >= 0 {
		if tokens[from].Kind != java.TComment {
			return &tokens[from]
		}
		from--
	}
	return nil
}

func nextNonCommentJava(tokens []java.Token, from int) *java.Token {
	for from < len(tokens) {
		if tokens[from].Kind != java.TComment {
			return &tokens[from]
		}
		from++
	}
	return nil
}

func isJavaModifier(s string) bool {
	switch s {
	case "public", "private", "protected", "static", "final", "abstract",
		"native", "synchronized", "transient", "volatile":
		return true
	}
	return false
}

func scanAndRenameFQN(path string, tokens []java.Token, targetFQN, newName string, syms *java.FileSymbols, set *edit.Set) {
	i := 0
	for i < len(tokens) {
		if tokens[i].Kind != java.TIdent {
			i++
			continue
		}
		chain, endIdx := buildDottedChain(tokens, i)
		if chain == targetFQN && endIdx > i {
			if inImportRange(tokens[i], syms) || inPackageRange(tokens[i], syms) {
				i = endIdx
				continue
			}
			lastTok := tokens[endIdx-1]
			if lastTok.Kind == java.TIdent {
				set.Add(edit.Edit{
					File: path, Start: lastTok.Start, End: lastTok.End,
					NewText: newName, Why: "rename inline FQN reference",
				})
			}
			i = endIdx
			continue
		}
		i++
	}
}

func buildDottedChain(tokens []java.Token, start int) (string, int) {
	if start >= len(tokens) || tokens[start].Kind != java.TIdent {
		return "", start
	}
	result := tokens[start].Text
	i := start + 1
	for i+1 < len(tokens) {
		if tokens[i].Kind == java.TPunct && tokens[i].Text == "." &&
			tokens[i+1].Kind == java.TIdent {
			result += "." + tokens[i+1].Text
			i += 2
			continue
		}
		break
	}
	return result, i
}

func rewriteImport(imp java.ImportSpec, targetFQN, targetPkg, newSimple string) (string, bool) {
	newFQN := targetPkg
	if newFQN != "" {
		newFQN += "."
	}
	newFQN += newSimple

	switch {
	case imp.IsStatic && imp.IsWildcard:
		if imp.Path == targetFQN {
			return newFQN, true
		}
	case imp.IsStatic:
		fqn := strings.TrimSuffix(imp.Path, "."+simpleName(imp.Path))
		if fqn == targetFQN {
			member := simpleName(imp.Path)
			return newFQN + "." + member, true
		}
	case imp.IsWildcard:
		return "", false
	default:
		if imp.Path == targetFQN {
			return newFQN, true
		}
	}
	return "", false
}

func resolvesTarget(fe *index.FileEntry, target *index.TypeDecl) bool {
	simple := target.Name
	for _, imp := range fe.Symbols.Imports {
		if imp.IsStatic || imp.IsWildcard {
			continue
		}
		if endsWithDotName(imp.Path, simple) {
			return imp.Path == target.FQN
		}
	}
	if fe.Symbols.Package == target.Package {
		return true
	}
	for _, imp := range fe.Symbols.Imports {
		if imp.IsStatic || !imp.IsWildcard {
			continue
		}
		if imp.Path == target.Package {
			return true
		}
	}
	for _, imp := range fe.Symbols.Imports {
		if !imp.IsStatic {
			continue
		}
		var typeFQN string
		if imp.IsWildcard {
			typeFQN = imp.Path
		} else {
			typeFQN = strings.TrimSuffix(imp.Path, "."+simpleName(imp.Path))
		}
		if typeFQN == target.FQN {
			return true
		}
	}
	return false
}

func endsWithDotName(path, name string) bool {
	if path == name {
		return true
	}
	return len(path) > len(name)+1 &&
		path[len(path)-len(name)-1] == '.' &&
		path[len(path)-len(name):] == name
}

func inImportRange(tok java.Token, syms *java.FileSymbols) bool {
	for _, imp := range syms.Imports {
		if tok.Start >= imp.Range.Start && tok.End <= imp.Range.End {
			return true
		}
	}
	return false
}

func inPackageRange(tok java.Token, syms *java.FileSymbols) bool {
	r := syms.PackageRange
	return r.End > 0 && tok.Start >= r.Start && tok.End <= r.End
}

func simpleName(fqn string) string {
	i := strings.LastIndexByte(fqn, '.')
	if i < 0 {
		return fqn
	}
	return fqn[i+1:]
}
