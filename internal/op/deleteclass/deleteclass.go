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
		removed := removeImports(fe, target.FQN, set)
		if hasCodeReferences(fe.Path, target.Name) || removed {
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

// hasCodeReferences scans the file for identifier tokens matching the
// class name outside of import statements. A rough check for whether
// the file still references the deleted class in code.
func hasCodeReferences(path, className string) bool {
	src, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	tokens := java.Tokenize(src)
	for _, tok := range tokens {
		if tok.Kind == java.TIdent && tok.Text == className {
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
