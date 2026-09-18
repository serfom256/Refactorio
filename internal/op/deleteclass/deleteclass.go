// Package deleteclass removes a Java class file and cleans up import
// statements in dependent files.
//
// Code references to the deleted class (variable types, method calls,
// etc.) are NOT removed — removing them would require understanding
// what to replace them with, which is a human decision. The dry-run
// output lists all files that still reference the class so the user
// knows where manual cleanup is needed.
package deleteclass

import (
	"fmt"
	"os"
	"strings"

	"github.com/serfom256/refactorio/internal/edit"
	"github.com/serfom256/refactorio/internal/index"
	"github.com/serfom256/refactorio/internal/lang/java"
)

type Request struct {
	FQN   string
	Index *index.Index
}

type Result struct {
	Set *edit.Set
	// FilesWithReferences lists dependent files that still contain code
	// references (type usages, method calls) after import removal. The
	// user should inspect these manually.
	FilesWithReferences []string
}

func Plan(req Request) (*Result, error) {
	if req.Index == nil {
		return nil, fmt.Errorf("nil index")
	}
	target := req.Index.LookupFQN(req.FQN)
	if target == nil {
		return nil, fmt.Errorf("no class %q in project", req.FQN)
	}

	set := edit.NewSet()
	var refs []string

	for _, path := range req.Index.FilesImporting(req.FQN) {
		if path == target.File {
			continue
		}
		fe := req.Index.File(path)
		if fe == nil || fe.Symbols == nil {
			continue
		}
		// Removing the import is the cleanup, not evidence that cleanup is
		// still needed, so its result deliberately does not feed into refs.
		removeImports(fe, target.FQN, set)
		if hasCodeReferences(fe, target.Name) {
			refs = append(refs, path)
		}
	}

	// Queue file deletion via a remove (os.Remove happens in Apply).
	set.AddDelete(target.File, fmt.Sprintf("delete %s", req.FQN))

	return &Result{Set: set, FilesWithReferences: refs}, nil
}

// removeImports adds edits to remove any import line that names the
// target class. Returns true if any import was removed.
func removeImports(fe *index.FileEntry, targetFQN string, set *edit.Set) bool {
	removed := false
	for _, imp := range fe.Symbols.Imports {
		shouldRemove := false
		switch {
		case imp.IsStatic && imp.IsWildcard:
			shouldRemove = imp.Path == targetFQN
		case imp.IsStatic:
			fqn := strings.TrimSuffix(imp.Path, "."+simpleName(imp.Path))
			shouldRemove = fqn == targetFQN
		case imp.IsWildcard:
			// Wildcard imports cover an entire package, not just this class.
			// Don't remove them — other classes from the package may still
			// be used.
		default:
			shouldRemove = imp.Path == targetFQN
		}
		if shouldRemove {
			set.Add(edit.Edit{
				File: fe.Path, Start: imp.Range.Start, End: imp.Range.End,
				NewText: "", Why: fmt.Sprintf("remove import for deleted class %s", simpleName(targetFQN)),
			})
			removed = true
		}
	}
	return removed
}

// hasCodeReferences reports whether the file still references className in
// code once the imports naming it have been removed. A rough check — it is
// name-based, not type-aware.
//
// Identifiers inside import statements are skipped. Those ranges are already
// queued for deletion, so counting them reported every single file whose
// import had just been cleaned as still needing manual cleanup.
func hasCodeReferences(fe *index.FileEntry, className string) bool {
	src, err := os.ReadFile(fe.Path)
	if err != nil {
		return false
	}
	for _, tok := range java.Tokenize(src) {
		if tok.Kind != java.TIdent || tok.Text != className {
			continue
		}
		if withinImport(fe.Symbols.Imports, tok.Start) {
			continue
		}
		return true
	}
	return false
}

// withinImport reports whether byte offset off falls inside any import
// statement in the file.
func withinImport(imports []java.ImportSpec, off int) bool {
	for _, imp := range imports {
		if off >= imp.Range.Start && off < imp.Range.End {
			return true
		}
	}
	return false
}

func simpleName(fqn string) string {
	i := strings.LastIndexByte(fqn, '.')
	if i < 0 {
		return fqn
	}
	return fqn[i+1:]
}
