// Package moveclass moves a Java class to a different package.
//
// Operations performed:
//
//  1. Rename the package declaration in the moved file
//     (`package com.foo;` → `package com.qux;`).
//  2. Move the file on disk from the old package path to the new one
//     under the SAME source root.
//  3. Rewrite imports in dependent files:
//     - explicit  `import com.foo.Bar` → `import com.qux.Bar`
//     - static    `import static com.foo.Bar.X` → `import static com.qux.Bar.X`
//  4. Add explicit `import com.qux.Bar;` to dependents that previously
//     reached Bar without naming the new package — namely:
//     - files in the old package (had same-package access, now lost)
//     - files with `import com.foo.*` (wildcard no longer covers Bar)
//     unless the file is now in the new package itself.
//
// Known limits:
//
//   - The new package's directory under the source root is created
//     during Apply if missing. No validation that the directory is
//     part of a known module — the move can land in a directory that
//     gradle/maven do not include as a source root. The user is
//     trusted to specify a sensible new package.
//   - When the file's package declaration is absent (default package),
//     we error out rather than inserting one; that case is rare and
//     adding a declaration cleanly needs a different code path.
//   - Wildcard imports in the new package's own files (`import com.qux.*`)
//     are not detected; they'd cover the moved class for free, but if
//     such a file ALSO has an explicit `import com.foo.Bar`, we rewrite
//     it to `import com.qux.Bar` instead of removing it.
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

// Request is the input.
type Request struct {
	FQN        string // e.g. "com.foo.Bar"
	NewPackage string // e.g. "com.qux"
	Index      *index.Index
}

// Plan returns an EditSet describing every change including the file
// move. Apply runs edits first (so the package-decl rewrite happens at
// the OLD path) then the os.Rename.
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
	// Use the source filename as-is (it may not match the class name
	// after a prior rename-class without file rename).
	newPath := filepath.Join(srcRoot, packageToPath(req.NewPackage), filepath.Base(target.File))

	set := edit.NewSet()

	// 1. Package decl rewrite (at the OLD path; the move happens after).
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

	// 2. Queue the file move.
	set.AddMove(edit.Move{
		From: target.File,
		To:   newPath,
		Why:  fmt.Sprintf("move %s from %s to %s", target.Name, target.Package, req.NewPackage),
	})

	// 3. Import rewrites + insertions in dependents.
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

	// Need to add an explicit import when the file's only path to the
	// target was same-package access or an old-package wildcard, AND
	// the file is not now in the new package.
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

// computeImportInsertion returns the byte offset where a new import
// line should be inserted (caller prepends the newline). Prefers
// "after the last existing import"; falls back to "after the
// package-decl semicolon".
func computeImportInsertion(fe *index.FileEntry) (int, error) {
	if n := len(fe.Symbols.Imports); n > 0 {
		return fe.Symbols.Imports[n-1].Range.End, nil
	}
	// Read source to locate the package-decl trailing semicolon.
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

// computeSourceRoot strips the package's directory mirror from the
// file path. We deliberately do NOT require the filename to match the
// class name — after a rename-class without a file rename, the class
// `Box` may still live in `Bar.java`, and we still want move-class to
// work. The required invariant is that the parent directory chain
// matches the package path. The className arg is unused but kept on
// the signature for callsite clarity.
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

// validPackageName checks that each dot-separated segment is a valid
// Java identifier. Uses the java package's identifier rule.
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
