package csrename

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/serfom256/refactorio/internal/edit"
	"github.com/serfom256/refactorio/internal/lang/csharp"
)

type RenameRequest struct {
	File    string
	Symbol  string
	NewName string
	Index   *csharp.Index
}

func PlanRename(req RenameRequest) (*edit.Set, error) {
	if !csharp.IsValidIdentifier(req.NewName) {
		return nil, fmt.Errorf("%q is not a valid C# identifier", req.NewName)
	}
	if req.Symbol == req.NewName {
		return nil, fmt.Errorf("symbol is already named %q", req.NewName)
	}

	fe := req.Index.File(req.File)
	if fe == nil || fe.Symbols == nil {
		return nil, fmt.Errorf("file %s not indexed", req.File)
	}
	ns := fe.Symbols.Namespace

	var declRange csharp.Range
	var symbolKind string
	for _, td := range fe.Symbols.Types {
		if td.Name == req.Symbol {
			declRange = td.NameRange
			symbolKind = "type"
			break
		}
		for _, m := range td.Methods {
			if m.Name == req.Symbol && m.Kind == "method" {
				declRange = m.NameRange
				symbolKind = "method"
				break
			}
		}
		if symbolKind != "" {
			break
		}
		for _, f := range td.Fields {
			if f.Name == req.Symbol {
				declRange = f.NameRange
				symbolKind = "field"
				break
			}
		}
		if symbolKind != "" {
			break
		}
		for _, prop := range td.Properties {
			if prop.Name == req.Symbol {
				declRange = prop.NameRange
				symbolKind = "property"
				break
			}
		}
		if symbolKind != "" {
			break
		}
	}
	if symbolKind == "" {
		return nil, fmt.Errorf("no declaration %q in %s", req.Symbol, req.File)
	}

	set := edit.NewSet()

	set.Add(edit.Edit{
		File: req.File, Start: declRange.Start, End: declRange.End,
		NewText: req.NewName, Why: fmt.Sprintf("rename declaration %s -> %s", req.Symbol, req.NewName),
	})

	if err := renameInFile(req.File, req.Symbol, req.NewName, declRange, set); err != nil {
		return nil, err
	}

	if symbolKind == "type" {
		fqn := req.Symbol
		if ns != "" {
			fqn = ns + "." + req.Symbol
		}
		_ = fqn
		for _, path := range req.Index.FilesUsing(ns) {
			if path == req.File {
				continue
			}
			useFE := req.Index.File(path)
			if useFE == nil || useFE.Symbols == nil {
				continue
			}
			if err := renameInFile(path, req.Symbol, req.NewName, csharp.Range{}, set); err != nil {
				return nil, err
			}
		}

		base := filepath.Base(req.File)
		if base == req.Symbol+".cs" {
			newFile := filepath.Join(filepath.Dir(req.File), req.NewName+".cs")
			set.AddMove(edit.Move{From: req.File, To: newFile, Why: "rename file"})
		}
	}

	return set, nil
}

type MoveRequest struct {
	File    string
	NewPath string
	Index   *csharp.Index
}

func PlanMove(req MoveRequest) (*edit.Set, error) {
	abs, _ := filepath.Abs(req.File)
	newAbs, _ := filepath.Abs(req.NewPath)
	req.File = abs
	req.NewPath = newAbs

	if req.File == req.NewPath {
		return nil, fmt.Errorf("source and destination are the same")
	}

	fe := req.Index.File(req.File)
	if fe == nil || fe.Symbols == nil {
		return nil, fmt.Errorf("file %s not indexed", req.File)
	}

	set := edit.NewSet()

	newNS := namespaceFromPath(req.NewPath, req.Index.Root)
	oldNS := fe.Symbols.Namespace

	if newNS != "" && newNS != oldNS && fe.Symbols.NamespaceRange.End > 0 {
		set.Add(edit.Edit{
			File: req.File, Start: fe.Symbols.NamespaceRange.Start, End: fe.Symbols.NamespaceRange.End,
			NewText: newNS, Why: fmt.Sprintf("update namespace to %s", newNS),
		})

		for _, path := range req.Index.FilesUsing(oldNS) {
			if path == req.File {
				continue
			}
			useFE := req.Index.File(path)
			if useFE == nil || useFE.Symbols == nil {
				continue
			}
			for _, u := range useFE.Symbols.Usings {
				if u.Path == oldNS {
					set.Add(edit.Edit{
						File: path, Start: u.PathRange.Start, End: u.PathRange.End,
						NewText: newNS, Why: "update using for moved namespace",
					})
				}
			}
		}
	}

	set.AddMove(edit.Move{From: req.File, To: req.NewPath, Why: "move file"})
	return set, nil
}

type DeleteRequest struct {
	File  string
	Index *csharp.Index
}

type DeleteResult struct {
	Set                 *edit.Set
	FilesWithReferences []string
}

func PlanDelete(req DeleteRequest) (*DeleteResult, error) {
	abs, _ := filepath.Abs(req.File)
	req.File = abs
	fe := req.Index.File(req.File)
	if fe == nil {
		return nil, fmt.Errorf("file %s not indexed", req.File)
	}
	set := edit.NewSet()
	var refs []string
	for _, td := range fe.Symbols.Types {
		for _, path := range req.Index.AllFiles() {
			if path == req.File {
				continue
			}
			depFE := req.Index.File(path)
			if depFE == nil || depFE.Symbols == nil {
				continue
			}
			if fileHasIdentifier(path, td.Name) {
				refs = append(refs, path)
			}
		}
	}
	set.AddDelete(req.File, "delete file")
	return &DeleteResult{Set: set, FilesWithReferences: refs}, nil
}

func renameInFile(path, oldName, newName string, skipRange csharp.Range, set *edit.Set) error {
	src, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	tokens := csharp.Tokenize(src)
	syms, _ := csharp.ExtractSymbols(src)
	for i, tok := range tokens {
		if tok.Kind != csharp.TIdent || tok.Text != oldName {
			continue
		}
		if tok.Start == skipRange.Start && tok.End == skipRange.End {
			continue
		}
		if syms != nil && inUsingOrNamespace(tok, syms) {
			continue
		}
		if isLocalDeclCS(tokens, i) {
			continue
		}
		set.Add(edit.Edit{
			File: path, Start: tok.Start, End: tok.End,
			NewText: newName, Why: fmt.Sprintf("rename %s -> %s", oldName, newName),
		})
	}
	return nil
}

func inUsingOrNamespace(tok csharp.Token, syms *csharp.FileSymbols) bool {
	for _, u := range syms.Usings {
		if tok.Start >= u.Range.Start && tok.End <= u.Range.End {
			return true
		}
	}
	r := syms.NamespaceRange
	if r.End > 0 && tok.Start >= r.Start && tok.End <= r.End {
		return true
	}
	return false
}

func isLocalDeclCS(tokens []csharp.Token, i int) bool {
	if i <= 0 {
		return false
	}
	prev := prevNonComment(tokens, i-1)
	if prev == nil {
		return false
	}
	if prev.Kind == csharp.TIdent && (prev.Text == "var" || isPrimitiveType(prev.Text)) {
		next := nextNonComment(tokens, i+1)
		if next != nil && (next.Text == "=" || next.Text == ";" || next.Text == "," || next.Text == ")") {
			return true
		}
	}
	return false
}

func isPrimitiveType(s string) bool {
	switch s {
	case "int", "long", "short", "byte", "sbyte", "uint", "ulong", "ushort",
		"float", "double", "decimal", "bool", "char", "string", "object":
		return true
	}
	return false
}

func prevNonComment(tokens []csharp.Token, from int) *csharp.Token {
	for from >= 0 {
		if tokens[from].Kind != csharp.TComment {
			return &tokens[from]
		}
		from--
	}
	return nil
}

func nextNonComment(tokens []csharp.Token, from int) *csharp.Token {
	for from < len(tokens) {
		if tokens[from].Kind != csharp.TComment {
			return &tokens[from]
		}
		from++
	}
	return nil
}

func fileHasIdentifier(path, name string) bool {
	src, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	for _, tok := range csharp.Tokenize(src) {
		if tok.Kind == csharp.TIdent && tok.Text == name {
			return true
		}
	}
	return false
}

func namespaceFromPath(file, root string) string {
	dir := filepath.Dir(file)
	rel, err := filepath.Rel(root, dir)
	if err != nil || rel == "." {
		return ""
	}
	rel = strings.ReplaceAll(rel, string(filepath.Separator), ".")
	return rel
}
