// Package gorename implements cross-file Go symbol rename and
// file move with import-path updates.
package gorename

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/serfom256/refactorio/internal/edit"
	"github.com/serfom256/refactorio/internal/lang/golang"
)

// RenameRequest renames an exported symbol across all files that
// import its package.
type RenameRequest struct {
	// Package is the full import path, e.g. "github.com/foo/bar/pkg".
	Package string
	Symbol  string
	NewName string
	Index   *golang.Index
}

func PlanRename(req RenameRequest) (*edit.Set, error) {
	if !golang.IsValidIdentifier(req.NewName) {
		return nil, fmt.Errorf("%q is not a valid Go identifier", req.NewName)
	}
	if req.Symbol == req.NewName {
		return nil, fmt.Errorf("symbol is already named %q", req.NewName)
	}

	declFE, decl := req.Index.FindDeclaration(req.Package, req.Symbol)
	if decl == nil {
		return nil, fmt.Errorf("no declaration %q in package %s", req.Symbol, req.Package)
	}

	set := edit.NewSet()

	// 1. Rename the declaration.
	set.Add(edit.Edit{
		File: declFE.Path, Start: decl.NameRange.Start, End: decl.NameRange.End,
		NewText: req.NewName, Why: fmt.Sprintf("rename declaration %s -> %s", req.Symbol, req.NewName),
	})

	// 2. Rename all usages in the same package (all files with same
	//    package declaration share a namespace — no import needed).
	for _, path := range req.Index.FilesInPackage(req.Package) {
		if err := renameIdentInFile(path, req.Symbol, req.NewName, decl.NameRange, set); err != nil {
			return nil, err
		}
	}

	// 3. Rename qualified usages in importing files: `pkg.Symbol`.
	for _, path := range req.Index.FilesImporting(req.Package) {
		fe := req.Index.File(path)
		if fe == nil || fe.Symbols == nil {
			continue
		}
		localPkg := importLocalName(fe, req.Package)
		if localPkg == "" || localPkg == "_" {
			continue
		}
		if localPkg == "." {
			// Dot import: bare name in scope.
			if err := renameIdentInFile(path, req.Symbol, req.NewName, golang.Range{}, set); err != nil {
				return nil, err
			}
			continue
		}
		// Regular import: find `pkg.Symbol` pattern.
		if err := renameQualifiedInFile(path, localPkg, req.Symbol, req.NewName, set); err != nil {
			return nil, err
		}
	}

	return set, nil
}

// MoveRequest moves a Go file and updates import paths in all files
// that import its package.
type MoveRequest struct {
	File    string // absolute path
	NewPath string // absolute destination
	Index   *golang.Index
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

	oldPkg := req.Index.FileToImportPath(req.File)
	newPkg := computeImportPath(req.Index, req.NewPath)

	set := edit.NewSet()

	// If the file moves to a different package, update the package
	// declaration and imports in dependents.
	if oldPkg != newPkg && fe.Symbols != nil && newPkg != "" {
		// Update package decl to match new directory.
		newPkgName := filepath.Base(filepath.Dir(req.NewPath))
		if fe.Symbols.PackageRange.End > 0 {
			set.Add(edit.Edit{
				File: req.File, Start: fe.Symbols.PackageRange.Start, End: fe.Symbols.PackageRange.End,
				NewText: newPkgName, Why: fmt.Sprintf("update package to %s", newPkgName),
			})
		}

		// Update imports in files that imported the old package.
		for _, path := range req.Index.FilesImporting(oldPkg) {
			impFE := req.Index.File(path)
			if impFE == nil || impFE.Symbols == nil {
				continue
			}
			for _, imp := range impFE.Symbols.Imports {
				if imp.Path == oldPkg {
					set.Add(edit.Edit{
						File: path, Start: imp.PathRange.Start, End: imp.PathRange.End,
						NewText: newPkg, Why: fmt.Sprintf("update import path %s -> %s", oldPkg, newPkg),
					})
				}
			}
		}

		// Update relative imports inside the moved file.
		for _, imp := range fe.Symbols.Imports {
			if !strings.HasPrefix(imp.Path, req.Index.ModulePath) {
				continue
			}
			// Internal import — path stays the same (it's a module path,
			// not a relative path). No rewrite needed.
		}
	}

	set.AddMove(edit.Move{
		From: req.File, To: req.NewPath,
		Why: fmt.Sprintf("move %s", filepath.Base(req.File)),
	})

	return set, nil
}

func importLocalName(fe *golang.FileEntry, importPath string) string {
	for _, imp := range fe.Symbols.Imports {
		if imp.Path == importPath {
			return imp.PackageName()
		}
	}
	return ""
}

func renameIdentInFile(path, oldName, newName string, skipRange golang.Range, set *edit.Set) error {
	src, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	tokens := golang.Tokenize(src)
	for i, tok := range tokens {
		if tok.Kind != golang.TIdent || tok.Text != oldName {
			continue
		}
		if tok.Start == skipRange.Start && tok.End == skipRange.End {
			continue
		}
		if inImportBlock(tok, tokens) {
			continue
		}
		// Skip local variable declarations: `name :=` or `var name`
		// or parameter declarations: `name Type` after `(`
		if isLocalDeclGo(tokens, i) {
			continue
		}
		set.Add(edit.Edit{
			File: path, Start: tok.Start, End: tok.End,
			NewText: newName, Why: fmt.Sprintf("rename %s -> %s", oldName, newName),
		})
	}
	return nil
}

// isLocalDeclGo detects if the identifier is a local variable/parameter
// declaration rather than a reference to the symbol being renamed.
func isLocalDeclGo(tokens []golang.Token, i int) bool {
	// `name :=` → short var declaration.
	if i+1 < len(tokens) && tokens[i+1].Text == ":=" {
		return true
	}
	// `var name` or `const name` → explicit declaration.
	if i > 0 {
		prev := prevNonCommentGo(tokens, i-1)
		if prev != nil && (prev.Text == "var" || prev.Text == "const") {
			return true
		}
	}
	return false
}

func prevNonCommentGo(tokens []golang.Token, from int) *golang.Token {
	for from >= 0 {
		if tokens[from].Kind != golang.TComment {
			return &tokens[from]
		}
		from--
	}
	return nil
}

func renameQualifiedInFile(path, pkgName, oldName, newName string, set *edit.Set) error {
	src, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	tokens := golang.Tokenize(src)
	// Find pattern: <pkgName> "." <oldName>
	for i := 0; i+2 < len(tokens); i++ {
		if tokens[i].Kind == golang.TIdent && tokens[i].Text == pkgName &&
			tokens[i+1].Kind == golang.TPunct && tokens[i+1].Text == "." &&
			tokens[i+2].Kind == golang.TIdent && tokens[i+2].Text == oldName {
			set.Add(edit.Edit{
				File: path, Start: tokens[i+2].Start, End: tokens[i+2].End,
				NewText: newName, Why: fmt.Sprintf("rename %s.%s -> %s.%s", pkgName, oldName, pkgName, newName),
			})
		}
	}
	return nil
}

func inImportBlock(tok golang.Token, tokens []golang.Token) bool {
	// Rough check: find enclosing `import (...)` or `import "..."`.
	// We look backward for `import` keyword.
	for i := range tokens {
		if &tokens[i] == &tok {
			// Walk backward.
			for j := i - 1; j >= 0; j-- {
				if tokens[j].Kind == golang.TIdent && tokens[j].Text == "import" {
					return true
				}
				if tokens[j].Kind == golang.TIdent && isGoTopLevel(tokens[j].Text) && tokens[j].Text != "import" {
					return false
				}
			}
			return false
		}
	}
	return false
}

func computeImportPath(idx *golang.Index, file string) string {
	dir := filepath.Dir(file)
	rel, err := filepath.Rel(idx.Root, dir)
	if err != nil {
		return ""
	}
	if rel == "." {
		return idx.ModulePath
	}
	return idx.ModulePath + "/" + filepath.ToSlash(rel)
}

func isGoTopLevel(s string) bool {
	switch s {
	case "func", "type", "var", "const", "package", "import":
		return true
	}
	return false
}
