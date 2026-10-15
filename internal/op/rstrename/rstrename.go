package rstrename

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/serfom256/refactorio/internal/edit"
	"github.com/serfom256/refactorio/internal/lang/rust"
	"github.com/serfom256/refactorio/internal/op/refs"
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

	set.Add(edit.Edit{
		File: req.File, Start: decl.NameRange.Start, End: decl.NameRange.End,
		NewText: req.NewName, Why: fmt.Sprintf("rename declaration %s -> %s", req.Symbol, req.NewName),
	})

	for _, path := range req.Index.FilesInModule(modPath) {
		if err := renameInFile(path, req.Symbol, req.NewName, decl.NameRange, set); err != nil {
			return nil, err
		}
	}

	for _, path := range req.Index.FilesUsing(modPath, req.Symbol) {
		useFE := req.Index.File(path)
		if useFE == nil || useFE.Symbols == nil {
			continue
		}
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
		localName := req.Symbol
		for _, u := range useFE.Symbols.Uses {
			if u.Path == oldUsePath && u.Alias != "" {
				localName = u.Alias
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
	var filesWithRefs []string

	for _, path := range req.Index.AllFiles() {
		fileFE := req.Index.File(path)
		if fileFE == nil || fileFE.Symbols == nil || path == req.File {
			continue
		}
		var spans []refs.Span
		var bound, stillBound []string
		for _, u := range fileFE.Symbols.Uses {
			if !strings.HasPrefix(u.Path, modPath+"::") && u.Path != modPath {
				stillBound = append(stillBound, useBinding(u))
				continue
			}
			set.Add(edit.Edit{
				File: path, Start: u.Range.Start, End: u.Range.End,
				NewText: "", Why: "remove use for deleted module",
			})
			spans = append(spans, refs.Span{Start: u.Range.Start, End: u.Range.End})
			bound = append(bound, useBinding(u))
		}
		for _, md := range fileFE.Symbols.ModDecls {
			baseName := strings.TrimSuffix(filepath.Base(req.File), ".rs")
			if baseName == "mod" {
				baseName = filepath.Base(filepath.Dir(req.File))
			}
			if md.Name == baseName {
				src, err := os.ReadFile(path)
				if err == nil {
					tokens := rust.Tokenize(src)
					for i, tok := range tokens {
						if tok.Kind == rust.TIdent && tok.Text == "mod" && i+1 < len(tokens) && tokens[i+1].Text == baseName {
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
		if danglingRs(path, bound, stillBound, spans) {
			filesWithRefs = append(filesWithRefs, path)
		}
	}

	set.AddDelete(req.File, "delete file")
	return &DeleteResult{Set: set, FilesWithReferences: filesWithRefs}, nil
}

func useBinding(u rust.UseDecl) string {
	if u.Alias != "" {
		return u.Alias
	}
	if i := strings.LastIndex(u.Path, "::"); i >= 0 {
		return u.Path[i+2:]
	}
	return u.Path
}

func danglingRs(path string, bound, stillBound []string, spans []refs.Span) bool {
	if len(bound) == 0 {
		return false
	}
	src, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	var idents []refs.Ident
	for _, tok := range rust.Tokenize(src) {
		if tok.Kind == rust.TIdent {
			idents = append(idents, refs.Ident{Name: tok.Text, Start: tok.Start})
		}
	}
	return refs.Dangling(idents, bound, stillBound, spans)
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
		if syms != nil && inUseRange(tok, syms) {
			continue
		}
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
