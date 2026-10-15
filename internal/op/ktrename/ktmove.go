package ktrename

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/serfom256/refactorio/internal/edit"
	"github.com/serfom256/refactorio/internal/lang/kotlin"
)

type MoveRequest struct {
	FQN        string
	NewPackage string
	Index      *KtIndex
}

func PlanMove(req MoveRequest) (*edit.Set, error) {
	if !validPackageName(req.NewPackage) {
		return nil, fmt.Errorf("%q is not a valid Kotlin package name", req.NewPackage)
	}
	target := req.Index.byFQN[req.FQN]
	if target == nil {
		return nil, fmt.Errorf("no declaration %q in project", req.FQN)
	}
	if target.Package == req.NewPackage {
		return nil, fmt.Errorf("already in package %q", req.NewPackage)
	}

	fe := req.Index.files[target.File]
	if fe == nil || fe.Symbols == nil {
		return nil, fmt.Errorf("file %s not indexed", target.File)
	}

	srcRoot, ok := computeSourceRoot(target.File, target.Package)
	if !ok {
		return nil, fmt.Errorf("could not determine source root for %s", target.File)
	}
	newPath := filepath.Join(srcRoot, packageToPath(req.NewPackage), filepath.Base(target.File))
	newFQN := req.NewPackage + "." + target.Name

	set := edit.NewSet()

	if fe.Symbols.PackageRange.End > 0 {
		set.Add(edit.Edit{
			File: target.File, Start: fe.Symbols.PackageRange.Start, End: fe.Symbols.PackageRange.End,
			NewText: req.NewPackage, Why: fmt.Sprintf("update package to %s", req.NewPackage),
		})
	}

	for _, path := range req.Index.FilesImporting(req.FQN) {
		if path == target.File {
			continue
		}
		depFE := req.Index.files[path]
		if depFE == nil || depFE.Symbols == nil {
			continue
		}
		for _, imp := range depFE.Symbols.Imports {
			if imp.Path == req.FQN {
				set.Add(edit.Edit{
					File: path, Start: imp.PathRange.Start, End: imp.PathRange.End,
					NewText: newFQN, Why: "update import for moved class",
				})
			}
		}
		if depFE.Symbols.Package == target.Package {
			insertOff := computeInsertionPoint(depFE)
			set.Add(edit.Edit{
				File: path, Start: insertOff, End: insertOff,
				NewText: "\nimport " + newFQN, Why: "add import for moved class",
			})
		}
	}

	set.AddMove(edit.Move{From: target.File, To: newPath, Why: fmt.Sprintf("move to %s", req.NewPackage)})

	return set, nil
}

type DeleteRequest struct {
	FQN   string
	Index *KtIndex
}

type DeleteResult struct {
	Set                 *edit.Set
	FilesWithReferences []string
}

func PlanDelete(req DeleteRequest) (*DeleteResult, error) {
	target := req.Index.byFQN[req.FQN]
	if target == nil {
		return nil, fmt.Errorf("no declaration %q in project", req.FQN)
	}

	set := edit.NewSet()
	var refs []string

	for _, path := range req.Index.FilesImporting(req.FQN) {
		if path == target.File {
			continue
		}
		depFE := req.Index.files[path]
		if depFE == nil || depFE.Symbols == nil {
			continue
		}
		for _, imp := range depFE.Symbols.Imports {
			if imp.Path == req.FQN {
				set.Add(edit.Edit{
					File: path, Start: imp.Range.Start, End: imp.Range.End,
					NewText: "", Why: fmt.Sprintf("remove import for deleted %s", target.Name),
				})
			}
		}
		if hasCodeRef(path, depFE.Symbols.Imports, target.Name) {
			refs = append(refs, path)
		}
	}

	set.AddDelete(target.File, fmt.Sprintf("delete %s", req.FQN))
	return &DeleteResult{Set: set, FilesWithReferences: refs}, nil
}

type RenamePackageRequest struct {
	OldPackage string
	NewPackage string
	Index      *KtIndex
}

func PlanRenamePackage(req RenamePackageRequest) (*edit.Set, error) {
	if !validPackageName(req.NewPackage) {
		return nil, fmt.Errorf("%q is not a valid Kotlin package name", req.NewPackage)
	}
	if req.OldPackage == req.NewPackage {
		return nil, fmt.Errorf("packages are identical")
	}

	filesInPkg := req.Index.byPkg[req.OldPackage]
	if len(filesInPkg) == 0 {
		return nil, fmt.Errorf("no files in package %q", req.OldPackage)
	}

	set := edit.NewSet()
	movedFiles := map[string]bool{}

	for _, path := range filesInPkg {
		if movedFiles[path] {
			continue
		}
		movedFiles[path] = true
		fe := req.Index.files[path]
		if fe == nil || fe.Symbols == nil {
			continue
		}
		if fe.Symbols.PackageRange.End > 0 {
			set.Add(edit.Edit{
				File: path, Start: fe.Symbols.PackageRange.Start, End: fe.Symbols.PackageRange.End,
				NewText: req.NewPackage, Why: fmt.Sprintf("update package %s -> %s", req.OldPackage, req.NewPackage),
			})
		}
		if srcRoot, ok := computeSourceRoot(path, req.OldPackage); ok {
			newPath := filepath.Join(srcRoot, packageToPath(req.NewPackage), filepath.Base(path))
			set.AddMove(edit.Move{From: path, To: newPath, Why: "move to new package"})
		}
	}

	seen := map[string]bool{}
	for _, path := range filesInPkg {
		fe := req.Index.files[path]
		if fe == nil || fe.Symbols == nil {
			continue
		}
		for _, d := range fe.Symbols.Declarations {
			fqn := req.OldPackage + "." + d.Name
			for _, depPath := range req.Index.FilesImporting(fqn) {
				if movedFiles[depPath] || seen[depPath+fqn] {
					continue
				}
				seen[depPath+fqn] = true
				depFE := req.Index.files[depPath]
				if depFE == nil || depFE.Symbols == nil {
					continue
				}
				for _, imp := range depFE.Symbols.Imports {
					if imp.Path == fqn {
						newFQN := req.NewPackage + "." + d.Name
						set.Add(edit.Edit{
							File: depPath, Start: imp.PathRange.Start, End: imp.PathRange.End,
							NewText: newFQN, Why: "update import for renamed package",
						})
					}
					if strings.HasSuffix(imp.Path, ".*") && strings.TrimSuffix(imp.Path, ".*") == req.OldPackage {
						newWild := req.NewPackage + ".*"
						set.Add(edit.Edit{
							File: depPath, Start: imp.PathRange.Start, End: imp.PathRange.End,
							NewText: newWild, Why: "update wildcard import",
						})
					}
				}
			}
		}
	}

	return set, nil
}

func hasCodeRef(path string, imports []kotlin.ImportSpec, name string) bool {
	src, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	for _, tok := range kotlin.Tokenize(src) {
		if tok.Kind != kotlin.TIdent || tok.Text != name {
			continue
		}
		inImport := false
		for _, imp := range imports {
			if tok.Start >= imp.Range.Start && tok.Start < imp.Range.End {
				inImport = true
				break
			}
		}
		if !inImport {
			return true
		}
	}
	return false
}

func computeSourceRoot(file, pkg string) (string, bool) {
	dir := filepath.Dir(file)
	pkgPath := packageToPath(pkg)
	if !strings.HasSuffix(dir, pkgPath) {
		return "", false
	}
	root := dir[:len(dir)-len(pkgPath)]
	root = strings.TrimRight(root, string(filepath.Separator))
	return root, true
}

func packageToPath(pkg string) string {
	return strings.ReplaceAll(pkg, ".", string(filepath.Separator))
}

func validPackageName(pkg string) bool {
	if pkg == "" {
		return false
	}
	for _, seg := range strings.Split(pkg, ".") {
		if !kotlin.IsValidIdentifier(seg) {
			return false
		}
	}
	return true
}

func computeInsertionPoint(fe *KtFileEntry) int {
	if n := len(fe.Symbols.Imports); n > 0 {
		return fe.Symbols.Imports[n-1].Range.End
	}
	if fe.Symbols.PackageRange.End > 0 {
		src, err := os.ReadFile(fe.Path)
		if err != nil {
			return 0
		}
		for i := fe.Symbols.PackageRange.End; i < len(src); i++ {
			if src[i] == '\n' {
				return i
			}
		}
	}
	return 0
}
