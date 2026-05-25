// Package rstrename implements Rust rename, move, and delete operations.
package rstrename

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/serfom256/refactorio/internal/edit"
	"github.com/serfom256/refactorio/internal/lang/rust"
)

type RenameRequest struct {
	File    string
	Symbol  string
	NewName string
	Index   *rust.Index
}

func PlanRename(req RenameRequest) (*edit.Set, error) {
	if !rust.IsValidIdentifier(req.NewName) {
		return nil, fmt.Errorf("%q is not a valid Rust identifier", req.NewName)
	}
	if req.Symbol == req.NewName {
		return nil, fmt.Errorf("symbol is already named %q", req.NewName)
	}

	fe := req.Index.File(req.File)
	if fe == nil || fe.Symbols == nil {
		return nil, fmt.Errorf("file %s not indexed", req.File)
	}

	// Find the declaration.
	var decl *rust.Declaration
	for i := range fe.Symbols.Declarations {
		if fe.Symbols.Declarations[i].Name == req.Symbol {
			decl = &fe.Symbols.Declarations[i]
			break
		}
	}
	if decl == nil {
		return nil, fmt.Errorf("no declaration %q in %s", req.Symbol, req.File)
	}

	modPath := req.Index.FileToModulePath(req.File)
	set := edit.NewSet()

	// 1. Rename the declaration.
	set.Add(edit.Edit{
		File: req.File, Start: decl.NameRange.Start, End: decl.NameRange.End,
		NewText: req.NewName, Why: fmt.Sprintf("rename declaration %s -> %s", req.Symbol, req.NewName),
	})

	// 2. Rename usages in the same module (same file + sibling files).
	// This covers impl blocks, type references, and other occurrences.
	for _, path := range req.Index.FilesInModule(modPath) {
		if err := renameInFile(path, req.Symbol, req.NewName, decl.NameRange, set); err != nil {
			return nil, err
		}
	}

	// 3. Rename in files that `use` this symbol.
	for _, path := range req.Index.FilesUsing(modPath, req.Symbol) {
		useFE := req.Index.File(path)
		if useFE == nil || useFE.Symbols == nil {
			continue
		}
		// Rewrite use paths.
		oldUsePath := modPath + "::" + req.Symbol
		newUsePath := modPath + "::" + req.NewName
		for _, u := range useFE.Symbols.Uses {
			if u.Path == oldUsePath {
				set.Add(edit.Edit{
					File: path, Start: u.PathRange.Start, End: u.PathRange.End,
					NewText: newUsePath, Why: "update use path",
				})
			}
		}
		// Rename identifier usages (the local name after `use`).
		localName := req.Symbol
		for _, u := range useFE.Symbols.Uses {
			if u.Path == oldUsePath && u.Alias != "" {
				localName = u.Alias // aliased — don't rename usages
			}
		}
		if localName == req.Symbol {
			if err := renameInFile(path, req.Symbol, req.NewName, rust.Range{}, set); err != nil {
				return nil, err
			}
		}
	}

	return set, nil
}

type MoveRequest struct {
	File    string
	NewPath string
	Index   *rust.Index
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
	if fe == nil {
		return nil, fmt.Errorf("file %s not indexed", req.File)
	}

	oldMod := req.Index.FileToModulePath(req.File)
	newMod := req.Index.FileToModulePath(req.NewPath)

	set := edit.NewSet()

	// Update `use` paths in all files that reference the old module.
	if oldMod != newMod {
		for _, path := range req.Index.AllFiles() {
			fileFE := req.Index.File(path)
			if fileFE == nil || fileFE.Symbols == nil || path == req.File {
				continue
			}
			for _, u := range fileFE.Symbols.Uses {
				if strings.HasPrefix(u.Path, oldMod+"::") {
					newPath := newMod + u.Path[len(oldMod):]
					set.Add(edit.Edit{
						File: path, Start: u.PathRange.Start, End: u.PathRange.End,
						NewText: newPath, Why: "update use path for moved module",
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
	Index *rust.Index
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

	modPath := req.Index.FileToModulePath(req.File)
	set := edit.NewSet()
	var refs []string

	// Remove `use` statements referencing this module's symbols.
	for _, path := range req.Index.AllFiles() {
		fileFE := req.Index.File(path)
		if fileFE == nil || fileFE.Symbols == nil || path == req.File {
			continue
		}
		removed := false
		for _, u := range fileFE.Symbols.Uses {
			if strings.HasPrefix(u.Path, modPath+"::") || u.Path == modPath {
				set.Add(edit.Edit{
					File: path, Start: u.Range.Start, End: u.Range.End,
					NewText: "", Why: "remove use for deleted module",
				})
				removed = true
			}
		}
		// Remove `mod` declarations for this module.
		for _, md := range fileFE.Symbols.ModDecls {
			baseName := strings.TrimSuffix(filepath.Base(req.File), ".rs")
			if baseName == "mod" {
				baseName = filepath.Base(filepath.Dir(req.File))
			}
			if md.Name == baseName {
				// Find the full `mod name;` range to remove.
				src, err := os.ReadFile(path)
				if err == nil {
					tokens := rust.Tokenize(src)
					for i, tok := range tokens {
						if tok.Kind == rust.TIdent && tok.Text == "mod" && i+1 < len(tokens) && tokens[i+1].Text == baseName {
							// Find the trailing ;
							end := tokens[i+1].End
							for j := i + 2; j < len(tokens); j++ {
								if tokens[j].Text == ";" {
									end = tokens[j].End
									break
								}
							}
							set.Add(edit.Edit{
								File: path, Start: tok.Start, End: end,
								NewText: "", Why: "remove mod declaration for deleted file",
							})
							break
						}
					}
				}
			}
		}
		if removed {
			refs = append(refs, path)
		}
	}

	set.AddDelete(req.File, "delete file")
	return &DeleteResult{Set: set, FilesWithReferences: refs}, nil
}

func renameInFile(path, oldName, newName string, skipRange rust.Range, set *edit.Set) error {
	src, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	tokens := rust.Tokenize(src)
	syms, _ := rust.ExtractSymbols(src)

	for i, tok := range tokens {
		if tok.Kind != rust.TIdent || tok.Text != oldName {
			continue
		}
		if tok.Start == skipRange.Start && tok.End == skipRange.End {
			continue
		}
		// Skip tokens inside use declarations.
		if syms != nil && inUseRange(tok, syms) {
			continue
		}
		// Skip local variable declarations: `let name`, `let mut name`.
		if isLocalDeclRs(tokens, i) {
			continue
		}
		set.Add(edit.Edit{
			File: path, Start: tok.Start, End: tok.End,
			NewText: newName, Why: fmt.Sprintf("rename %s -> %s", oldName, newName),
		})
	}
	return nil
}

func inUseRange(tok rust.Token, syms *rust.FileSymbols) bool {
	for _, u := range syms.Uses {
		if tok.Start >= u.Range.Start && tok.End <= u.Range.End {
			return true
		}
	}
	return false
}

func isLocalDeclRs(tokens []rust.Token, i int) bool {
	if i <= 0 {
		return false
	}
	prev := prevNonComment(tokens, i-1)
	if prev == nil {
		return false
	}
	// `let name` or `let mut name`
	if prev.Text == "let" || prev.Text == "mut" {
		return true
	}
	return false
}

func prevNonComment(tokens []rust.Token, from int) *rust.Token {
	for from >= 0 {
		if tokens[from].Kind != rust.TComment {
			return &tokens[from]
		}
		from--
	}
	return nil
}
