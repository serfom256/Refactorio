package renamepkg

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/serfom256/refactorio/internal/edit"
	"github.com/serfom256/refactorio/internal/index"
	"github.com/serfom256/refactorio/internal/lang/java"
)

type Request struct {
	OldPackage string
	NewPackage string
	Index      *index.Index
}

func Plan(req Request) (*edit.Set, error) {
	if req.Index == nil {
		return nil, fmt.Errorf("nil index")
	}
	if !validPackageName(req.NewPackage) {
		return nil, fmt.Errorf("%q is not a valid Java package name", req.NewPackage)
	}
	if req.OldPackage == req.NewPackage {
		return nil, fmt.Errorf("packages are identical")
	}

	filesInPkg := append([]string(nil), req.Index.FilesInPackage(req.OldPackage)...)
	if len(filesInPkg) == 0 {
		return nil, fmt.Errorf("no files found in package %q", req.OldPackage)
	}
	sort.Strings(filesInPkg)

	var typesInPkg []*index.TypeDecl
	for _, path := range filesInPkg {
		fe := req.Index.File(path)
		if fe == nil || fe.Symbols == nil {
			continue
		}
		for i := range fe.Symbols.Types {
			if td := req.Index.LookupFQN(req.OldPackage + "." + fe.Symbols.Types[i].Name); td != nil {
				typesInPkg = append(typesInPkg, td)
			}
		}
	}

	set := edit.NewSet()

	movedFiles := map[string]bool{}
	for _, path := range filesInPkg {
		if movedFiles[path] {
			continue
		}
		movedFiles[path] = true

		fe := req.Index.File(path)
		if fe == nil || fe.Symbols == nil {
			continue
		}
		if fe.Symbols.PackageRange.End > 0 {
			set.Add(edit.Edit{
				File: path, Start: fe.Symbols.PackageRange.Start, End: fe.Symbols.PackageRange.End,
				NewText: req.NewPackage, Why: fmt.Sprintf("update package %s -> %s", req.OldPackage, req.NewPackage),
			})
		}
		srcRoot, ok := computeSourceRoot(path, req.OldPackage)
		if ok {
			newPath := filepath.Join(srcRoot, packageToPath(req.NewPackage), filepath.Base(path))
			set.AddMove(edit.Move{
				From: path, To: newPath,
				Why: fmt.Sprintf("move to package %s", req.NewPackage),
			})
		}
	}

	dependentFiles := map[string]bool{}
	for _, td := range typesInPkg {
		for _, path := range req.Index.FilesImporting(td.FQN) {
			if movedFiles[path] {
				continue
			}
			dependentFiles[path] = true
		}
	}

	for path := range dependentFiles {
		fe := req.Index.File(path)
		if fe == nil || fe.Symbols == nil {
			continue
		}
		for _, imp := range fe.Symbols.Imports {
			newPath, ok := rewriteImport(imp, req.OldPackage, req.NewPackage)
			if !ok {
				continue
			}
			set.Add(edit.Edit{
				File: fe.Path, Start: imp.PathRange.Start, End: imp.PathRange.End,
				NewText: newPath, Why: fmt.Sprintf("update import for package %s -> %s", req.OldPackage, req.NewPackage),
			})
		}
	}

	return set, nil
}

func rewriteImport(imp java.ImportSpec, oldPkg, newPkg string) (string, bool) {
	switch {
	case imp.IsWildcard && !imp.IsStatic:
		if imp.Path == oldPkg {
			return newPkg, true
		}
	case imp.IsStatic && imp.IsWildcard:
		if strings.HasPrefix(imp.Path, oldPkg+".") {
			return newPkg + imp.Path[len(oldPkg):], true
		}
	case imp.IsStatic:
		if strings.HasPrefix(imp.Path, oldPkg+".") {
			return newPkg + imp.Path[len(oldPkg):], true
		}
	default:
		if strings.HasPrefix(imp.Path, oldPkg+".") {
			return newPkg + imp.Path[len(oldPkg):], true
		}
	}
	return "", false
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
		if !java.IsValidIdentifier(seg) {
			return false
		}
	}
	return true
}
