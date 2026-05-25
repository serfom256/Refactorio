// Package renamepkg renames an entire Java package: updates the
// package declaration in every file, moves files to the new directory,
// and rewrites imports in all dependent files.
package renamepkg

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/serfom256/refactorio/internal/edit"
	"github.com/serfom256/refactorio/internal/index"
	"github.com/serfom256/refactorio/internal/lang/java"
)

type Request struct {
	OldPackage string       // e.g. "com.foo"
	NewPackage string       // e.g. "com.bar"
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

	// Find all types in the old package.
	var typesInPkg []*index.TypeDecl
	for _, fe := range req.Index.Files() {
		if fe.Symbols == nil || fe.Symbols.Package != req.OldPackage {
			continue
		}
		for i := range fe.Symbols.Types {
			td := req.Index.LookupFQN(req.OldPackage + "." + fe.Symbols.Types[i].Name)
			if td != nil {
				typesInPkg = append(typesInPkg, td)
			}
		}
	}
	if len(typesInPkg) == 0 {
		return nil, fmt.Errorf("no types found in package %q", req.OldPackage)
	}

	set := edit.NewSet()

	// 1. For each file in the old package: rewrite package declaration + queue file move.
	movedFiles := map[string]bool{}
	for _, td := range typesInPkg {
		if movedFiles[td.File] {
			continue
		}
		movedFiles[td.File] = true

		fe := req.Index.File(td.File)
		if fe == nil || fe.Symbols == nil {
			continue
		}
		// Rewrite package declaration.
		if fe.Symbols.PackageRange.End > 0 {
			set.Add(edit.Edit{
				File: td.File, Start: fe.Symbols.PackageRange.Start, End: fe.Symbols.PackageRange.End,
				NewText: req.NewPackage, Why: fmt.Sprintf("update package %s -> %s", req.OldPackage, req.NewPackage),
			})
		}
		// Queue file move.
		srcRoot, ok := computeSourceRoot(td.File, req.OldPackage)
		if ok {
			newPath := filepath.Join(srcRoot, packageToPath(req.NewPackage), filepath.Base(td.File))
			set.AddMove(edit.Move{
				From: td.File, To: newPath,
				Why: fmt.Sprintf("move to package %s", req.NewPackage),
			})
		}
	}

	// 2. Rewrite imports in all dependent files.
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
		// import com.foo.* → import com.bar.*
		if imp.Path == oldPkg {
			return newPkg, true
		}
	case imp.IsStatic && imp.IsWildcard:
		// import static com.foo.Bar.* — Path = "com.foo.Bar"
		if strings.HasPrefix(imp.Path, oldPkg+".") {
			return newPkg + imp.Path[len(oldPkg):], true
		}
	case imp.IsStatic:
		// import static com.foo.Bar.member — Path = "com.foo.Bar.member"
		if strings.HasPrefix(imp.Path, oldPkg+".") {
			return newPkg + imp.Path[len(oldPkg):], true
		}
	default:
		// import com.foo.Bar
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
