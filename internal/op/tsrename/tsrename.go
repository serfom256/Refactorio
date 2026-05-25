// Package tsrename implements cross-file TS/JS symbol rename and
// file move with import-path updates.
//
// Subcommands:
//   ts-rename   file=<path> symbol=<name> to=<newName>
//   ts-move     file=<path> to=<newPath>
package tsrename

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/serfom256/refactorio/internal/edit"
	"github.com/serfom256/refactorio/internal/lang/typescript"
)

// RenameRequest renames a symbol across all files that import it.
type RenameRequest struct {
	File    string // absolute path of the file declaring the symbol
	Symbol  string // current name
	NewName string
	Index   *typescript.Index
}

// PlanRename computes the edit set for a cross-file symbol rename.
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

	// 1. Rename the declaration itself.
	set.Add(edit.Edit{
		File: declFE.Path, Start: decl.NameRange.Start, End: decl.NameRange.End,
		NewText: req.NewName, Why: fmt.Sprintf("rename declaration %s -> %s", req.Symbol, req.NewName),
	})

	// 2. Rename all identifier tokens matching the symbol in the
	//    declaring file (usages within the same file).
	if err := renameInFile(declFE.Path, req.Symbol, req.NewName, decl.NameRange, set); err != nil {
		return nil, err
	}

	// 3. For each file that imports this symbol, rename the import
	//    binding + all usages of the local name.
	for _, importerPath := range req.Index.FilesImporting(declFE.Path) {
		imps := req.Index.ImportsOf(importerPath, declFE.Path)
		for _, imp := range imps {
			for _, n := range imp.Names {
				// Match: the imported name must be the symbol we're renaming
				// (or "default" if the symbol is a default export).
				importedName := n.Name
				if importedName != req.Symbol && !(importedName == "default" && decl.IsDefault) {
					continue
				}
				localName := n.LocalName()
				// If the import uses an alias (`import { Foo as F }`),
				// we update the source name in the import specifier but
				// the local alias stays. If no alias, rename both the
				// import specifier and all local usages.
				if n.Alias != "" {
					// Has alias: update the imported name portion.
					// `import { Foo as F }` → `import { Bar as F }`
					// Only if it's a named (non-default) import.
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
					// No alias: the local name IS the symbol name.
					// Rename it in the import + all usages in the file.
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

// MoveRequest moves a file and updates all import paths that reference it.
type MoveRequest struct {
	File    string // absolute path of the file to move
	NewPath string // absolute destination path
	Index   *typescript.Index
}

// PlanMove computes the edit set for a file move + import path updates.
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

	// 1. Update import paths in files that import the moved file.
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

	// 2. Update relative imports INSIDE the moved file (they now
	//    resolve from a different directory).
	if fe.Symbols != nil {
		for _, imp := range fe.Symbols.Imports {
			if !strings.HasPrefix(imp.Path, ".") {
				continue
			}
			// Resolve the old import target.
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

	// 3. Queue the file move.
	set.AddMove(edit.Move{
		From: req.File, To: req.NewPath,
		Why: fmt.Sprintf("move %s -> %s", filepath.Base(req.File), filepath.Base(req.NewPath)),
	})

	return set, nil
}

// renameInFile scans all identifier tokens in a file and renames
// matches, skipping the declaration range (already handled) and
// tokens inside import/export statements.
func renameInFile(path, oldName, newName string, skipRange typescript.Range, set *edit.Set) error {
	src, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	tokens := typescript.Tokenize(src)
	fe := &typescript.FileEntry{Path: path}
	fsTmp, _ := typescript.ExtractSymbols(src)
	fe.Symbols = fsTmp

	// Build ranges to skip (import/export statements).
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
		// Skip local variable/parameter declarations.
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

// isLocalDeclTS detects local variable/parameter declarations.
func isLocalDeclTS(tokens []typescript.Token, i int) bool {
	if i <= 0 {
		return false
	}
	prev := prevNonCommentTS(tokens, i-1)
	if prev == nil {
		return false
	}
	// `const name`, `let name`, `var name` → local declaration.
	if prev.Text == "const" || prev.Text == "let" || prev.Text == "var" {
		return true
	}
	// Parameter: `name:` (TS type annotation) or `name,` or `name)`
	// preceded by `(` or `,` → parameter declaration.
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

// findImportNameRange locates the byte range of the imported name
// (not the alias) within a named import specifier. For `{ Foo as F }`,
// this is the range of `Foo`.
func findImportNameRange(imp typescript.ImportDecl, n typescript.ImportName, file string) typescript.Range {
	src, err := os.ReadFile(file)
	if err != nil {
		return typescript.Range{}
	}
	tokens := typescript.Tokenize(src)
	// Find the import name token that matches n.Name within the import range.
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
