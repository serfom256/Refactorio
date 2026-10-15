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
	Set                 *edit.Set
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
		removeImports(fe, target.FQN, set)
		if hasCodeReferences(fe, target.Name) {
			refs = append(refs, path)
		}
	}

	set.AddDelete(target.File, fmt.Sprintf("delete %s", req.FQN))

	return &Result{Set: set, FilesWithReferences: refs}, nil
}

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
