package gorename

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/serfom256/refactorio/internal/edit"
	"github.com/serfom256/refactorio/internal/lang/golang"
	"github.com/serfom256/refactorio/internal/op/refs"
)

type DeleteRequest struct {
	File  string
	Index *golang.Index
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

	pkg := req.Index.FileToImportPath(req.File)
	set := edit.NewSet()
	var filesWithRefs []string

	pkgFiles := req.Index.FilesInPackage(pkg)
	isLastInPkg := len(pkgFiles) <= 1

	if isLastInPkg {
		for _, path := range req.Index.FilesImporting(pkg) {
			impFE := req.Index.File(path)
			if impFE == nil || impFE.Symbols == nil {
				continue
			}
			var spans []refs.Span
			var bound []string
			for _, imp := range impFE.Symbols.Imports {
				if imp.Path != pkg {
					continue
				}
				set.Add(edit.Edit{
					File: path, Start: imp.Range.Start, End: imp.Range.End,
					NewText: "", Why: fmt.Sprintf("remove import for deleted package %s", pkg),
				})
				spans = append(spans, refs.Span{Start: imp.Range.Start, End: imp.Range.End})
				bound = append(bound, imp.PackageName())
			}
			if danglingGo(path, bound, spans) {
				filesWithRefs = append(filesWithRefs, path)
			}
		}
	}

	set.AddDelete(req.File, fmt.Sprintf("delete %s", filepath.Base(req.File)))
	return &DeleteResult{Set: set, FilesWithReferences: filesWithRefs}, nil
}

type RenamePackageRequest struct {
	OldImportPath string
	NewImportPath string
	Index         *golang.Index
}

func PlanRenamePackage(req RenamePackageRequest) (*edit.Set, error) {
	if req.OldImportPath == req.NewImportPath {
		return nil, fmt.Errorf("import paths are identical")
	}

	oldDir := req.Index.ImportPathToDir(req.OldImportPath)
	if oldDir == "" {
		return nil, fmt.Errorf("package %s not found in module", req.OldImportPath)
	}
	files := req.Index.FilesInPackage(req.OldImportPath)
	if len(files) == 0 {
		return nil, fmt.Errorf("no files in package %s", req.OldImportPath)
	}

	newDir := req.Index.ImportPathToDir(req.NewImportPath)
	if newDir == "" {
		rel := req.NewImportPath
		if strings.HasPrefix(rel, req.Index.ModulePath+"/") {
			rel = rel[len(req.Index.ModulePath)+1:]
		}
		newDir = filepath.Join(req.Index.Root, filepath.FromSlash(rel))
	}
	newPkgName := filepath.Base(newDir)

	set := edit.NewSet()

	for _, path := range files {
		fe := req.Index.File(path)
		if fe == nil || fe.Symbols == nil {
			continue
		}
		if fe.Symbols.PackageRange.End > 0 {
			set.Add(edit.Edit{
				File: path, Start: fe.Symbols.PackageRange.Start, End: fe.Symbols.PackageRange.End,
				NewText: newPkgName, Why: fmt.Sprintf("update package %s -> %s", fe.Symbols.Package, newPkgName),
			})
		}
		newPath := filepath.Join(newDir, filepath.Base(path))
		set.AddMove(edit.Move{From: path, To: newPath, Why: "move to new package"})
	}

	for _, path := range req.Index.FilesImporting(req.OldImportPath) {
		fe := req.Index.File(path)
		if fe == nil || fe.Symbols == nil {
			continue
		}
		for _, imp := range fe.Symbols.Imports {
			if imp.Path == req.OldImportPath {
				set.Add(edit.Edit{
					File: path, Start: imp.PathRange.Start, End: imp.PathRange.End,
					NewText: req.NewImportPath, Why: fmt.Sprintf("update import %s -> %s", req.OldImportPath, req.NewImportPath),
				})
			}
		}
	}

	return set, nil
}

func danglingGo(path string, bound []string, spans []refs.Span) bool {
	if len(bound) == 0 {
		return false
	}
	src, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	var idents []refs.Ident
	for _, tok := range golang.Tokenize(src) {
		if tok.Kind == golang.TIdent {
			idents = append(idents, refs.Ident{Name: tok.Text, Start: tok.Start})
		}
	}
	return refs.Dangling(idents, bound, nil, spans)
}
