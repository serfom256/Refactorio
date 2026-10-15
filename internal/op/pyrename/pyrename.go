package pyrename

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/serfom256/refactorio/internal/edit"
	"github.com/serfom256/refactorio/internal/lang/python"
	"github.com/serfom256/refactorio/internal/op/refs"
)

type RenameRequest struct {
	File    string
	Symbol  string
	NewName string
	Index   *python.Index
}

func PlanRename(req RenameRequest) (*edit.Set, error) {
	if !python.IsValidIdentifier(req.NewName) {
		return nil, fmt.Errorf("%q is not a valid Python identifier", req.NewName)
	}
	if req.Symbol == req.NewName {
		return nil, fmt.Errorf("symbol is already named %q", req.NewName)
	}

	fe := req.Index.File(req.File)
	if fe == nil || fe.Symbols == nil {
		return nil, fmt.Errorf("file %s not indexed", req.File)
	}

	var declRange python.Range
	var kind string
	for _, d := range fe.Symbols.Declarations {
		if d.Name == req.Symbol {
			declRange = d.NameRange
			kind = d.Kind
			break
		}
		for _, m := range d.Members {
			if m.Name == req.Symbol {
				declRange = m.NameRange
				kind = m.Kind
				break
			}
		}
		if kind != "" {
			break
		}
	}
	if kind == "" {
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

	if declRange == fe.Symbols.Declarations[topLevelIdx(fe.Symbols, req.Symbol)].NameRange {
		modPath := req.Index.FileToModulePath(req.File)
		for _, path := range req.Index.FilesUsing(modPath, req.Symbol) {
			if path == req.File {
				continue
			}
			if err := renameInImportingFile(path, modPath, req.Symbol, req.NewName, req.Index, set); err != nil {
				return nil, err
			}
		}
	}

	return set, nil
}

func topLevelIdx(fs *python.FileSymbols, name string) int {
	for i, d := range fs.Declarations {
		if d.Name == name {
			return i
		}
	}
	return 0
}

type MoveRequest struct {
	File    string
	NewPath string
	Index   *python.Index
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
	newRel, _ := filepath.Rel(req.Index.Root, req.NewPath)
	newRel = filepath.ToSlash(newRel)
	newRel = strings.TrimSuffix(newRel, ".py")
	newRel = strings.TrimSuffix(newRel, "/__init__")
	newMod := strings.ReplaceAll(newRel, "/", ".")

	set := edit.NewSet()

	if oldMod != newMod && oldMod != "" {
		for _, path := range req.Index.AllFiles() {
			if path == req.File {
				continue
			}
			pfe := req.Index.File(path)
			if pfe == nil || pfe.Symbols == nil {
				continue
			}
			for _, imp := range pfe.Symbols.Imports {
				if imp.Path == oldMod || strings.HasPrefix(imp.Path, oldMod+".") {
					newPath := newMod + imp.Path[len(oldMod):]
					set.Add(edit.Edit{
						File: path, Start: imp.PathRange.Start, End: imp.PathRange.End,
						NewText: newPath, Why: "update import for moved module",
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
	Index *python.Index
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
		if path == req.File {
			continue
		}
		pfe := req.Index.File(path)
		if pfe == nil || pfe.Symbols == nil {
			continue
		}
		var spans []refs.Span
		var bound, stillBound []string
		for _, imp := range pfe.Symbols.Imports {
			hit := imp.Path == modPath || strings.HasPrefix(imp.Path, modPath+".")
			names := pyBindings(imp)
			if !hit {
				stillBound = append(stillBound, names...)
				continue
			}
			set.Add(edit.Edit{
				File: path, Start: imp.Range.Start, End: imp.Range.End,
				NewText: "", Why: "remove import for deleted module",
			})
			spans = append(spans, refs.Span{Start: imp.Range.Start, End: imp.Range.End})
			bound = append(bound, names...)
		}
		if danglingPy(path, bound, stillBound, spans) {
			filesWithRefs = append(filesWithRefs, path)
		}
	}

	set.AddDelete(req.File, "delete file")
	return &DeleteResult{Set: set, FilesWithReferences: filesWithRefs}, nil
}

func renameInFile(path, oldName, newName string, skipRange python.Range, set *edit.Set) error {
	src, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	tokens := python.Tokenize(src)
	syms, _ := python.ExtractSymbols(src)

	for i, tok := range tokens {
		if tok.Kind != python.TIdent || tok.Text != oldName {
			continue
		}
		if tok.Start == skipRange.Start && tok.End == skipRange.End {
			continue
		}
		if syms != nil && inImportRange(tok, syms) {
			continue
		}
		if isLocalAssign(tokens, i) {
			continue
		}
		set.Add(edit.Edit{
			File: path, Start: tok.Start, End: tok.End,
			NewText: newName, Why: fmt.Sprintf("rename %s -> %s", oldName, newName),
		})
	}
	return nil
}

func renameInImportingFile(path, modulePath, oldName, newName string, idx *python.Index, set *edit.Set) error {
	src, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	tokens := python.Tokenize(src)
	syms, _ := python.ExtractSymbols(src)
	if syms == nil {
		return nil
	}

	aliased := false
	for _, imp := range syms.Imports {
		if imp.IsFrom && imp.Path == modulePath {
			for _, n := range imp.Names {
				if n.Name == oldName {
					set.Add(edit.Edit{
						File: path, Start: n.Range.Start, End: n.Range.End,
						NewText: newName, Why: "update from-import specifier",
					})
					if n.Alias != "" {
						aliased = true
					}
				}
			}
		}
	}
	if aliased {
		return nil
	}

	hasBareImport := false
	for _, imp := range syms.Imports {
		if imp.IsFrom && imp.Path == modulePath {
			for _, n := range imp.Names {
				if n.Name == oldName && n.Alias == "" {
					hasBareImport = true
				}
			}
		}
	}

	for i, tok := range tokens {
		if tok.Kind != python.TIdent || tok.Text != oldName {
			continue
		}
		if inImportRange(tok, syms) {
			continue
		}
		prev := prevNonComment(tokens, i-1)
		if prev != nil && prev.Text == "." {
			receiver := prevNonComment(tokens, indexOf(tokens, prev)-1)
			if receiver != nil && receiver.Kind == python.TIdent {
				modParts := strings.Split(modulePath, ".")
				lastSeg := modParts[len(modParts)-1]
				if receiver.Text == lastSeg {
					set.Add(edit.Edit{
						File: path, Start: tok.Start, End: tok.End,
						NewText: newName, Why: "rename module-qualified usage",
					})
					continue
				}
			}
			continue
		}
		if hasBareImport && !isLocalAssign(tokens, i) {
			set.Add(edit.Edit{
				File: path, Start: tok.Start, End: tok.End,
				NewText: newName, Why: "rename imported usage",
			})
		}
	}
	return nil
}

func inImportRange(tok python.Token, syms *python.FileSymbols) bool {
	for _, imp := range syms.Imports {
		if tok.Start >= imp.Range.Start && tok.End <= imp.Range.End {
			return true
		}
	}
	return false
}

func isLocalAssign(tokens []python.Token, i int) bool {
	if i < 0 || i >= len(tokens) {
		return false
	}
	tok := tokens[i]
	if tok.Column == 1 {
		return false
	}
	j := i + 1
	if j < len(tokens) && tokens[j].Text == ":" {
		j++
		depth := 0
		for j < len(tokens) {
			t := tokens[j]
			if depth == 0 && (t.Text == "=" || t.Kind == python.TNewline) {
				break
			}
			switch t.Text {
			case "(", "[", "{":
				depth++
			case ")", "]", "}":
				depth--
			}
			j++
		}
	}
	if j >= len(tokens) {
		return false
	}
	if tokens[j].Text == "=" {
		return true
	}
	return false
}

func prevNonComment(tokens []python.Token, from int) *python.Token {
	for from >= 0 {
		k := tokens[from].Kind
		if k != python.TComment && k != python.TNewline {
			return &tokens[from]
		}
		from--
	}
	return nil
}

func indexOf(tokens []python.Token, t *python.Token) int {
	for i := range tokens {
		if &tokens[i] == t {
			return i
		}
	}
	return -1
}

func pyBindings(imp python.ImportDecl) []string {
	if imp.IsFrom {
		names := make([]string, 0, len(imp.Names))
		for _, n := range imp.Names {
			names = append(names, n.LocalName())
		}
		return names
	}
	for _, n := range imp.Names {
		if n.Alias != "" {
			return []string{n.Alias}
		}
	}
	path := strings.TrimLeft(imp.Path, ".")
	if i := strings.IndexByte(path, '.'); i >= 0 {
		path = path[:i]
	}
	if path == "" {
		return nil
	}
	return []string{path}
}

func danglingPy(path string, bound, stillBound []string, spans []refs.Span) bool {
	if len(bound) == 0 {
		return false
	}
	src, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	var idents []refs.Ident
	for _, tok := range python.Tokenize(src) {
		if tok.Kind == python.TIdent {
			idents = append(idents, refs.Ident{Name: tok.Text, Start: tok.Start})
		}
	}
	return refs.Dangling(idents, bound, stillBound, spans)
}
