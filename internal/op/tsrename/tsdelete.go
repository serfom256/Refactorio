package tsrename

import (
	"fmt"
	"path/filepath"

	"github.com/serfom256/refactorio/internal/edit"
	"github.com/serfom256/refactorio/internal/lang/typescript"
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
	var refs []string

	for _, path := range req.Index.FilesImporting(req.File) {
		impFE := req.Index.File(path)
		if impFE == nil || impFE.Symbols == nil {
			continue
		}
		removed := false
		for _, imp := range impFE.Symbols.Imports {
			resolved := typescript.ResolveImportPath(imp.Path, path)
			if resolved == req.File {
				set.Add(edit.Edit{
					File: path, Start: imp.Range.Start, End: imp.Range.End,
					NewText: "", Why: fmt.Sprintf("remove import for deleted file"),
				})
				removed = true
			}
		}
		if removed {
			refs = append(refs, path)
		}
	}

	set.AddDelete(req.File, fmt.Sprintf("delete %s", filepath.Base(req.File)))
	return &DeleteResult{Set: set, FilesWithReferences: refs}, nil
}

