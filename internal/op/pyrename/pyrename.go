// Package pyrename implements Python 3 rename, move, and delete operations.
package pyrename

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/serfom256/refactorio/internal/edit"
	"github.com/serfom256/refactorio/internal/lang/python"
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

	// Find the declaration: top-level declaration in the file, or
	// a class member.
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

	// Declaration edit.
	set.Add(edit.Edit{
		File: req.File, Start: declRange.Start, End: declRange.End,
		NewText: req.NewName, Why: fmt.Sprintf("rename declaration %s -> %s", req.Symbol, req.NewName),
	})

	// Rename in the declaring file, skipping the declaration itself.
	if err := renameInFile(req.File, req.Symbol, req.NewName, declRange, set); err != nil {
		return nil, err
	}

	// For top-level symbols: rename across importing files.
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

// topLevelIdx returns the index of the top-level declaration with the
// given name, or -1 if it's a class member (not top-level).
func topLevelIdx(fs *python.FileSymbols, name string) int {
	for i, d := range fs.Declarations {
		if d.Name == name {
			return i
		}
	}
	return 0 // fallback — caller uses range comparison anyway
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
	// Compute new module path from where the file will land.
	newRel, _ := filepath.Rel(req.Index.Root, req.NewPath)
	newRel = filepath.ToSlash(newRel)
	newRel = strings.TrimSuffix(newRel, ".py")
	newRel = strings.TrimSuffix(newRel, "/__init__")
	newMod := strings.ReplaceAll(newRel, "/", ".")

	set := edit.NewSet()

	// Update imports in all files that referenced the old module path.
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
	var refs []string

	// Remove imports referencing this module.
	for _, path := range req.Index.AllFiles() {
		if path == req.File {
			continue
		}
		pfe := req.Index.File(path)
		if pfe == nil || pfe.Symbols == nil {
			continue
		}
		removed := false
		for _, imp := range pfe.Symbols.Imports {
			if imp.Path == modPath || strings.HasPrefix(imp.Path, modPath+".") {
				set.Add(edit.Edit{
					File: path, Start: imp.Range.Start, End: imp.Range.End,
					NewText: "", Why: "remove import for deleted module",
				})
				removed = true
			}
		}
		if removed {
			refs = append(refs, path)
		}
	}

	set.AddDelete(req.File, "delete file")
	return &DeleteResult{Set: set, FilesWithReferences: refs}, nil
}

// renameInFile scans tokens, renaming identifier matches while skipping
// the declaration itself and tokens inside import statements / strings.
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

// renameInImportingFile handles a file that imports the symbol from
// the module we're renaming a symbol IN. We rewrite:
//
//   from <module> import <oldName>           → from <module> import <newName>
//   from <module> import <oldName> as <X>    → from <module> import <newName> as <X>
//   <module>.<oldName>                       → <module>.<newName>
//
// Plus bare references to the local binding (when no alias is used).
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

	// Rewrite `from X import name [as alias]` clauses.
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
		// User code uses the alias; no further usage rewrites needed.
		return nil
	}

	// Rewrite `module.oldName` chains and bare `oldName` references
	// (the latter only if a non-aliased `from X import oldName` exists).
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
		// Detect `module.oldName` form.
		prev := prevNonComment(tokens, i-1)
		if prev != nil && prev.Text == "." {
			// Heuristic: rename when the receiver is the module's last
			// segment.
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

// isLocalAssign reports whether the identifier is the target of a
// local assignment (preceded by indent, followed by `=` but not `==`).
// We treat any assignment that's not at column 1 as local.
func isLocalAssign(tokens []python.Token, i int) bool {
	if i < 0 || i >= len(tokens) {
		return false
	}
	tok := tokens[i]
	if tok.Column == 1 {
		return false
	}
	// Walk forward past optional `: type` annotation.
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
	// `=` followed by something other than `=` is assignment.
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
