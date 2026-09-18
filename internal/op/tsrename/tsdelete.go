package tsrename

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/serfom256/refactorio/internal/edit"
	"github.com/serfom256/refactorio/internal/lang/typescript"
	"github.com/serfom256/refactorio/internal/op/refs"
)

// DeleteRequest removes a TS/JS file and cleans import statements
// in all importing files.
type DeleteRequest struct {
	File  string
	Index *typescript.Index
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

	set := edit.NewSet()
	var filesWithRefs []string

	for _, path := range req.Index.FilesImporting(req.File) {
		impFE := req.Index.File(path)
		if impFE == nil || impFE.Symbols == nil {
			continue
		}
		var spans []refs.Span
		var bound, stillBound []string
		for _, imp := range impFE.Symbols.Imports {
			resolved := typescript.ResolveImportPath(imp.Path, path)
			if resolved != req.File {
				// A binding from a module we are keeping, so it stays live
				// even if the deleted module bound the same local name.
				for _, n := range imp.Names {
					stillBound = append(stillBound, n.LocalName())
				}
				continue
			}
			set.Add(edit.Edit{
				File: path, Start: imp.Range.Start, End: imp.Range.End,
				NewText: "", Why: "remove import for deleted file",
			})
			spans = append(spans, refs.Span{Start: imp.Range.Start, End: imp.Range.End})
			for _, n := range imp.Names {
				bound = append(bound, n.LocalName())
			}
		}
		if danglingTS(path, bound, stillBound, spans) {
			filesWithRefs = append(filesWithRefs, path)
		}
	}

	set.AddDelete(req.File, fmt.Sprintf("delete %s", filepath.Base(req.File)))
	return &DeleteResult{Set: set, FilesWithReferences: filesWithRefs}, nil
}

// danglingTS reports whether path still uses a binding that the removed
// imports provided.
func danglingTS(path string, bound, stillBound []string, spans []refs.Span) bool {
	if len(bound) == 0 {
		return false
	}
	src, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	var idents []refs.Ident
	for _, tok := range typescript.Tokenize(src) {
		if tok.Kind == typescript.TIdent {
			idents = append(idents, refs.Ident{Name: tok.Text, Start: tok.Start})
		}
	}
	return refs.Dangling(idents, bound, stillBound, spans)
}
