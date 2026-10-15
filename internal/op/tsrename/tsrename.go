package tsrename

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/serfom256/refactorio/internal/edit"
	"github.com/serfom256/refactorio/internal/lang/typescript"
)

type RenameRequest struct {
	File    string
	Symbol  string
	NewName string
	Index   *typescript.Index
}

func PlanRename(req RenameRequest) (*edit.Set, error) {
	if !typescript.IsValidIdentifier(req.NewName) {
		return nil, fmt.Errorf("%q is not a valid identifier", req.NewName)
	}
	if req.Symbol == req.NewName {
		return nil, fmt.Errorf("symbol is already named %q", req.NewName)
	}

	abs, err := filepath.Abs(req.File)
	if err != nil {
		return nil, err
	}
	req.File = abs

	declFE, decl := req.Index.FindDeclaration(req.Symbol, req.File)
	if decl == nil {
		return nil, fmt.Errorf("no exported declaration %q in %s", req.Symbol, req.File)
	}

	set := edit.NewSet()

	set.Add(edit.Edit{
		File: declFE.Path, Start: decl.NameRange.Start, End: decl.NameRange.End,
		NewText: req.NewName, Why: fmt.Sprintf("rename declaration %s -> %s", req.Symbol, req.NewName),
	})

	if err := renameInFile(declFE.Path, req.Symbol, req.NewName, decl.NameRange, set); err != nil {
		return nil, err
	}

	for _, importerPath := range req.Index.FilesImporting(declFE.Path) {
		imps := req.Index.ImportsOf(importerPath, declFE.Path)
		for _, imp := range imps {
			for _, n := range imp.Names {
				importedName := n.Name
				if importedName != req.Symbol && !(importedName == "default" && decl.IsDefault) {
					continue
				}
				localName := n.LocalName()
				if n.Alias != "" {
					if importedName != "default" {
						nameRange := findImportNameRange(imp, n, importerPath)
						if nameRange.End > 0 {
							set.Add(edit.Edit{
								File: importerPath, Start: nameRange.Start, End: nameRange.End,
								NewText: req.NewName, Why: fmt.Sprintf("update import specifier %s -> %s", req.Symbol, req.NewName),
							})
						}
					}
				} else {
					set.Add(edit.Edit{
						File: importerPath, Start: n.Range.Start, End: n.Range.End,
						NewText: req.NewName, Why: fmt.Sprintf("rename import binding %s -> %s", req.Symbol, req.NewName),
					})
					if err := renameInFile(importerPath, localName, req.NewName, n.Range, set); err != nil {
						return nil, err
					}
				}
			}
		}
	}

	return set, nil
}

type MoveRequest struct {
	File    string
	NewPath string
	Index   *typescript.Index
}

func PlanMove(req MoveRequest) (*edit.Set, error) {
	abs, err := filepath.Abs(req.File)
	if err != nil {
		return nil, err
	}
	req.File = abs
	newAbs, err := filepath.Abs(req.NewPath)
	if err != nil {
		return nil, err
	}
	req.NewPath = newAbs

	if req.File == req.NewPath {
		return nil, fmt.Errorf("source and destination are the same")
	}

	fe := req.Index.File(req.File)
	if fe == nil {
		return nil, fmt.Errorf("file %s not indexed", req.File)
	}

	set := edit.NewSet()

	for _, importerPath := range req.Index.FilesImporting(req.File) {
		imps := req.Index.ImportsOf(importerPath, req.File)
		for _, imp := range imps {
			newRelPath := typescript.ComputeNewImportPath(importerPath, req.NewPath)
			set.Add(edit.Edit{
				File: importerPath, Start: imp.PathRange.Start, End: imp.PathRange.End,
				NewText: newRelPath, Why: fmt.Sprintf("update import path for moved file"),
			})
		}
	}

	if fe.Symbols != nil {
		for _, imp := range fe.Symbols.Imports {
			if !strings.HasPrefix(imp.Path, ".") {
				continue
			}
			oldTarget := typescript.ResolveImportPath(imp.Path, req.File)
			if oldTarget == "" {
				continue
			}
			newRelPath := typescript.ComputeNewImportPath(req.NewPath, oldTarget)
			if newRelPath != imp.Path {
				set.Add(edit.Edit{
					File: req.File, Start: imp.PathRange.Start, End: imp.PathRange.End,
					NewText: newRelPath, Why: fmt.Sprintf("update relative import from moved file"),
				})
			}
		}
	}

	set.AddMove(edit.Move{
		From: req.File, To: req.NewPath,
		Why: fmt.Sprintf("move %s -> %s", filepath.Base(req.File), filepath.Base(req.NewPath)),
	})

	return set, nil
}

func renameInFile(path, oldName, newName string, skipRange typescript.Range, set *edit.Set) error {
	src, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	tokens := typescript.Tokenize(src)
	fe := &typescript.FileEntry{Path: path}
	fsTmp, _ := typescript.ExtractSymbols(src)
	fe.Symbols = fsTmp

	var skipRanges [][2]int
	if fe.Symbols != nil {
		for _, imp := range fe.Symbols.Imports {
			skipRanges = append(skipRanges, [2]int{imp.Range.Start, imp.Range.End})
		}
		for _, exp := range fe.Symbols.Exports {
			skipRanges = append(skipRanges, [2]int{exp.Range.Start, exp.Range.End})
		}
	}

	for i, tok := range tokens {
		if tok.Kind != typescript.TIdent || tok.Text != oldName {
			continue
		}
		if tok.Start == skipRange.Start && tok.End == skipRange.End {
			continue
		}
		if inAnyRange(tok, skipRanges) {
			continue
		}
		if isLocalDeclTS(tokens, i) {
			continue
		}
		set.Add(edit.Edit{
			File: path, Start: tok.Start, End: tok.End,
			NewText: newName, Why: fmt.Sprintf("rename usage %s -> %s", oldName, newName),
		})
	}
	return nil
}

func isLocalDeclTS(tokens []typescript.Token, i int) bool {
	if i <= 0 {
		return false
	}
	prev := prevNonCommentTS(tokens, i-1)
	if prev == nil {
		return false
	}
	if prev.Text == "const" || prev.Text == "let" || prev.Text == "var" {
		return true
	}
	next := nextNonCommentTS(tokens, i+1)
	if next != nil && (next.Text == ":" || next.Text == "," || next.Text == ")") {
		if prev.Text == "(" || prev.Text == "," {
			return true
		}
	}
	return false
}

func prevNonCommentTS(tokens []typescript.Token, from int) *typescript.Token {
	for from >= 0 {
		if tokens[from].Kind != typescript.TComment {
			return &tokens[from]
		}
		from--
	}
	return nil
}

func nextNonCommentTS(tokens []typescript.Token, from int) *typescript.Token {
	for from < len(tokens) {
		if tokens[from].Kind != typescript.TComment {
			return &tokens[from]
		}
		from++
	}
	return nil
}

func inAnyRange(tok typescript.Token, ranges [][2]int) bool {
	for _, r := range ranges {
		if tok.Start >= r[0] && tok.End <= r[1] {
			return true
		}
	}
	return false
}

func findImportNameRange(imp typescript.ImportDecl, n typescript.ImportName, file string) typescript.Range {
	src, err := os.ReadFile(file)
	if err != nil {
		return typescript.Range{}
	}
	tokens := typescript.Tokenize(src)
	for _, tok := range tokens {
		if tok.Kind != typescript.TIdent || tok.Text != n.Name {
			continue
		}
		if tok.Start >= imp.Range.Start && tok.End <= imp.Range.End {
			return typescript.Range{Start: tok.Start, End: tok.End, Line: tok.Line}
		}
	}
	return typescript.Range{}
}
