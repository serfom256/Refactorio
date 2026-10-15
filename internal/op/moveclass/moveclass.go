package moveclass

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/serfom256/refactorio/internal/edit"
	"github.com/serfom256/refactorio/internal/index"
	"github.com/serfom256/refactorio/internal/lang/java"
)

type Request struct {
	FQN        string
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
	target := req.Index.LookupFQN(req.FQN)
	if target == nil {
		return nil, fmt.Errorf("no class %q in project", req.FQN)
	}
	if target.Package == req.NewPackage {
		return nil, fmt.Errorf("class is already in package %q", req.NewPackage)
	}
	if target.Package == "" {
		return nil, fmt.Errorf("moving from the default package is not supported")
	}

	srcRoot, ok := computeSourceRoot(target.File, target.Package, target.Name)
	if !ok {
		return nil, fmt.Errorf("could not determine source root for %s (file %s, package %s)",
			target.FQN, target.File, target.Package)
	}
	newPath := filepath.Join(srcRoot, packageToPath(req.NewPackage), filepath.Base(target.File))

	set := edit.NewSet()

	fe := req.Index.File(target.File)
	if fe == nil || fe.Symbols == nil {
		return nil, fmt.Errorf("declaring file %s not indexed", target.File)
	}
	set.Add(edit.Edit{
		File:    target.File,
		Start:   fe.Symbols.PackageRange.Start,
		End:     fe.Symbols.PackageRange.End,
		NewText: req.NewPackage,
		Why:     fmt.Sprintf("update package declaration to %s", req.NewPackage),
	})

	set.AddMove(edit.Move{
		From: target.File,
		To:   newPath,
		Why:  fmt.Sprintf("move %s from %s to %s", target.Name, target.Package, req.NewPackage),
	})

	newFQN := req.NewPackage + "." + target.Name
	for _, path := range req.Index.FilesImporting(target.FQN) {
		if path == target.File {
			continue
		}
		dep := req.Index.File(path)
		if dep == nil || dep.Symbols == nil {
			continue
		}
		if err := planDependent(dep, target, req.NewPackage, newFQN, set); err != nil {
			return nil, err
		}
	}

	return set, nil
}

func planDependent(fe *index.FileEntry, target *index.TypeDecl, newPackage, newFQN string, set *edit.Set) error {
	var (
		hasExplicitTargetImport bool
		hasWildcardOldPkg       bool
		wasSamePackage          = fe.Symbols.Package == target.Package
	)

	for _, imp := range fe.Symbols.Imports {
		switch {
		case imp.IsStatic && imp.IsWildcard:
			if imp.Path == target.FQN {
				set.Add(edit.Edit{
					File:    fe.Path,
					Start:   imp.PathRange.Start,
					End:     imp.PathRange.End,
					NewText: newFQN,
					Why:     fmt.Sprintf("update static import for %s", target.FQN),
				})
			}
		case imp.IsStatic:
			fqn := strings.TrimSuffix(imp.Path, "."+simpleName(imp.Path))
			if fqn == target.FQN {
				member := simpleName(imp.Path)
				set.Add(edit.Edit{
					File:    fe.Path,
					Start:   imp.PathRange.Start,
					End:     imp.PathRange.End,
					NewText: newFQN + "." + member,
					Why:     fmt.Sprintf("update static import for %s", target.FQN),
				})
			}
		case imp.IsWildcard:
			if imp.Path == target.Package {
				hasWildcardOldPkg = true
			}
		default:
			if imp.Path == target.FQN {
				hasExplicitTargetImport = true
				set.Add(edit.Edit{
					File:    fe.Path,
					Start:   imp.PathRange.Start,
					End:     imp.PathRange.End,
					NewText: newFQN,
					Why:     fmt.Sprintf("update import for %s", target.FQN),
				})
			}
		}
	}

	needAdd := !hasExplicitTargetImport &&
		fe.Symbols.Package != newPackage &&
		(wasSamePackage || hasWildcardOldPkg)

	if needAdd {
		insertOff, err := computeImportInsertion(fe)
		if err != nil {
			return fmt.Errorf("%s: %w", fe.Path, err)
		}
		set.Add(edit.Edit{
			File:    fe.Path,
			Start:   insertOff,
			End:     insertOff,
			NewText: "\nimport " + newFQN + ";",
			Why:     fmt.Sprintf("add import for moved class %s", target.Name),
		})
	}
	return nil
}

func computeImportInsertion(fe *index.FileEntry) (int, error) {
	if n := len(fe.Symbols.Imports); n > 0 {
		return fe.Symbols.Imports[n-1].Range.End, nil
	}
	src, err := os.ReadFile(fe.Path)
	if err != nil {
		return 0, err
	}
	for i := fe.Symbols.PackageRange.End; i < len(src); i++ {
		if src[i] == ';' {
			return i + 1, nil
		}
	}
	return 0, fmt.Errorf("no semicolon after package declaration")
}

func computeSourceRoot(file, pkg, className string) (string, bool) {
	_ = className
	dir := filepath.Dir(file)
	if pkg == "" {
		return dir, true
	}
	pkgPath := packageToPath(pkg)
	if !strings.HasSuffix(dir, pkgPath) {
		return "", false
	}
	root := dir[:len(dir)-len(pkgPath)]
	root = strings.TrimRight(root, string(filepath.Separator))
	return root, true
}

func packageToPath(pkg string) string {
	if pkg == "" {
		return ""
	}
	return strings.ReplaceAll(pkg, ".", string(filepath.Separator))
}

func simpleName(fqn string) string {
	i := strings.LastIndexByte(fqn, '.')
	if i < 0 {
		return fqn
	}
	return fqn[i+1:]
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
