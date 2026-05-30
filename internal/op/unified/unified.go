// Package unified provides language-agnostic rename, move, and delete.
// Language is detected from the file extension.
package unified

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/serfom256/refactorio/internal/edit"
	"github.com/serfom256/refactorio/internal/index"
	"github.com/serfom256/refactorio/internal/lang/golang"
	"github.com/serfom256/refactorio/internal/lang/java"
	"github.com/serfom256/refactorio/internal/lang/kotlin"
	"github.com/serfom256/refactorio/internal/lang/typescript"
	"github.com/serfom256/refactorio/internal/lang/rust"
	"github.com/serfom256/refactorio/internal/op/gorename"
	"github.com/serfom256/refactorio/internal/op/ktrename"
	"github.com/serfom256/refactorio/internal/op/renameclass"
	"github.com/serfom256/refactorio/internal/op/renamefield"
	"github.com/serfom256/refactorio/internal/op/renamemethod"
	"github.com/serfom256/refactorio/internal/op/renamepkg"
	"github.com/serfom256/refactorio/internal/op/rstrename"
	"github.com/serfom256/refactorio/internal/op/tsrename"
	"github.com/serfom256/refactorio/internal/project"
)

func detectLang(file string) string {
	switch strings.ToLower(filepath.Ext(file)) {
	case ".java":
		return "java"
	case ".kt", ".kts":
		return "kotlin"
	case ".go":
		return "go"
	case ".ts", ".tsx", ".js", ".jsx", ".mts", ".cts", ".mjs", ".cjs":
		return "ts"
	case ".rs":
		return "rust"
	}
	return ""
}

// Rename renames a symbol declared in `inFile` across the project.
func Rename(inFile, name, newName, root string) (*edit.Set, error) {
	inFile, _ = filepath.Abs(inFile)
	lang := detectLang(inFile)

	switch lang {
	case "java":
		return renameJava(inFile, name, newName, root)
	case "kotlin":
		return renameKotlin(inFile, name, newName, root)
	case "go":
		return renameGo(inFile, name, newName, root)
	case "ts":
		return renameTS(inFile, name, newName, root)
	case "rust":
		return renameRust(inFile, name, newName, root)
	}
	return nil, fmt.Errorf("unsupported file type: %s", filepath.Ext(inFile))
}

// Move moves a file and updates all imports. The returned warnings
// flag situations the move cannot safely handle (e.g. Java package-
// private access that breaks when crossing a package boundary).
func Move(inFile, toPath, root string) (*edit.Set, []string, error) {
	inFile, _ = filepath.Abs(inFile)
	toPath, _ = filepath.Abs(toPath)
	// If destination is a directory, append the source filename.
	if info, err := os.Stat(toPath); err == nil && info.IsDir() {
		toPath = filepath.Join(toPath, filepath.Base(inFile))
	} else if strings.HasSuffix(toPath, string(filepath.Separator)) {
		toPath = filepath.Join(toPath, filepath.Base(inFile))
	}
	lang := detectLang(inFile)

	switch lang {
	case "java":
		return moveJava(inFile, toPath, root)
	case "kotlin":
		set, err := moveKotlin(inFile, toPath, root)
		return set, nil, err
	case "go":
		idx, err := goIndex(root)
		if err != nil {
			return nil, nil, err
		}
		set, err := gorename.PlanMove(gorename.MoveRequest{File: inFile, NewPath: toPath, Index: idx})
		return set, nil, err
	case "ts":
		idx, err := tsIndex(root)
		if err != nil {
			return nil, nil, err
		}
		set, err := tsrename.PlanMove(tsrename.MoveRequest{File: inFile, NewPath: toPath, Index: idx})
		return set, nil, err
	case "rust":
		idx, err := rustIndex(root)
		if err != nil {
			return nil, nil, err
		}
		set, err := rstrename.PlanMove(rstrename.MoveRequest{File: inFile, NewPath: toPath, Index: idx})
		return set, nil, err
	}
	return nil, nil, fmt.Errorf("unsupported file type: %s", filepath.Ext(inFile))
}

// Delete removes a file and cleans imports. Returns the edit set and
// a list of files that still reference the deleted symbol.
func Delete(inFile, root string) (*edit.Set, []string, error) {
	inFile, _ = filepath.Abs(inFile)
	lang := detectLang(inFile)

	switch lang {
	case "java":
		return deleteJava(inFile, root)
	case "kotlin":
		return deleteKotlin(inFile, root)
	case "go":
		idx, err := goIndex(root)
		if err != nil {
			return nil, nil, err
		}
		r, err := gorename.PlanDelete(gorename.DeleteRequest{File: inFile, Index: idx})
		if err != nil {
			return nil, nil, err
		}
		return r.Set, r.FilesWithReferences, nil
	case "ts":
		idx, err := tsIndex(root)
		if err != nil {
			return nil, nil, err
		}
		r, err := tsrename.PlanDelete(tsrename.DeleteRequest{File: inFile, Index: idx})
		if err != nil {
			return nil, nil, err
		}
		return r.Set, r.FilesWithReferences, nil
	case "rust":
		idx, err := rustIndex(root)
		if err != nil {
			return nil, nil, err
		}
		r, err := rstrename.PlanDelete(rstrename.DeleteRequest{File: inFile, Index: idx})
		if err != nil {
			return nil, nil, err
		}
		return r.Set, r.FilesWithReferences, nil
	}
	return nil, nil, fmt.Errorf("unsupported file type: %s", filepath.Ext(inFile))
}

// --- Java ---

func renameJava(file, name, newName, root string) (*edit.Set, error) {
	_, idx, err := javaIndex(root, file)
	if err != nil {
		return nil, err
	}
	fe := idx.File(file)
	if fe == nil || fe.Symbols == nil {
		return nil, fmt.Errorf("file %s not indexed", file)
	}
	pkg := fe.Symbols.Package
	fqn := name
	if pkg != "" {
		fqn = pkg + "." + name
	}

	// 1. If it's a top-level type → use the precise renameclass op.
	if idx.LookupFQN(fqn) != nil {
		return renameclass.Plan(renameclass.Request{FQN: fqn, NewName: newName, Index: idx})
	}

	// 2. Check if it's a method or field inside a type in this file.
	for _, td := range fe.Symbols.Types {
		typeFQN := td.Name
		if pkg != "" {
			typeFQN = pkg + "." + td.Name
		}
		for _, m := range td.Methods {
			if m.Kind == "method" && m.Name == name {
				return renamemethod.Plan(renamemethod.Request{
					ClassFQN: typeFQN, Method: name, NewName: newName, Index: idx,
				})
			}
		}
		for _, f := range td.Fields {
			if f.Name == name {
				return renamefield.Plan(renamefield.Request{
					ClassFQN: typeFQN, Field: name, NewName: newName, Index: idx,
				})
			}
		}
	}

	return nil, fmt.Errorf("symbol %q not found in %s", name, file)
}

func moveJava(file, toFile, root string) (*edit.Set, []string, error) {
	// If destination is a directory, append the source filename.
	if info, err := os.Stat(toFile); err == nil && info.IsDir() {
		toFile = filepath.Join(toFile, filepath.Base(file))
	} else if strings.HasSuffix(toFile, string(filepath.Separator)) {
		toFile = filepath.Join(toFile, filepath.Base(file))
	}

	p, idx, err := javaIndex(root, file)
	if err != nil {
		return nil, nil, err
	}
	fe := idx.File(file)
	if fe == nil || fe.Symbols == nil {
		return nil, nil, fmt.Errorf("file %s not indexed", file)
	}
	newPkg := packageFromPath(toFile, p)
	if newPkg == fe.Symbols.Package {
		set := edit.NewSet()
		set.AddMove(edit.Move{From: file, To: toFile, Why: "move file"})
		return set, nil, nil
	}
	warnings := detectAccessWarnings(file, fe, idx)
	set := edit.NewSet()
	if fe.Symbols.PackageRange.End > 0 {
		set.Add(edit.Edit{
			File: file, Start: fe.Symbols.PackageRange.Start, End: fe.Symbols.PackageRange.End,
			NewText: newPkg, Why: fmt.Sprintf("update package to %s", newPkg),
		})
	}
	for _, td := range fe.Symbols.Types {
		oldFQN := td.Name
		if fe.Symbols.Package != "" {
			oldFQN = fe.Symbols.Package + "." + td.Name
		}
		newFQN := newPkg + "." + td.Name
		for _, path := range idx.FilesImporting(oldFQN) {
			if path == file {
				continue
			}
			dep := idx.File(path)
			if dep == nil || dep.Symbols == nil {
				continue
			}
			// Rewrite explicit imports.
			for _, imp := range dep.Symbols.Imports {
				if imp.Path == oldFQN {
					set.Add(edit.Edit{
						File: path, Start: imp.PathRange.Start, End: imp.PathRange.End,
						NewText: newFQN, Why: "update import for moved file",
					})
				}
			}
			// Same-package files that ACTUALLY USE the class need an
			// explicit import now. Only add if the file references the
			// class name in code (not just in the same package by chance).
			if dep.Symbols.Package == fe.Symbols.Package && fileUsesIdentifier(path, td.Name) {
				insertOff := javaInsertionPoint(dep)
				set.Add(edit.Edit{
					File: path, Start: insertOff, End: insertOff,
					NewText: "\nimport " + newFQN + ";", Why: "add import for moved class",
				})
			}
		}
	}
	// Scan ALL project files for inline FQN references (used without
	// an import, e.g. `com.foo.Bar x = ...`). Rewrite the full chain.
	for _, td := range fe.Symbols.Types {
		oldFQN := td.Name
		if fe.Symbols.Package != "" {
			oldFQN = fe.Symbols.Package + "." + td.Name
		}
		newFQN := newPkg + "." + td.Name
		for _, entry := range idx.Files() {
			if entry.Err != nil || entry.Symbols == nil || entry.Path == file {
				continue
			}
			src, err := os.ReadFile(entry.Path)
			if err != nil {
				continue
			}
			tokens := java.Tokenize(src)
			moveFQNReferences(entry.Path, tokens, oldFQN, newFQN, entry, set)
		}
	}

	set.AddMove(edit.Move{From: file, To: toFile, Why: "move file"})
	return set, warnings, nil
}

// detectAccessWarnings flags Java package-private access that will
// break when the file moves to a different package. Type-level only:
//
//   1. The moved file references a package-private type in its old
//      package — that type won't be importable from the new package.
//   2. The moved type itself is package-private and has dependents —
//      it must become public to be imported after the move.
func detectAccessWarnings(file string, fe *index.FileEntry, idx *index.Index) []string {
	var warnings []string
	oldPkg := fe.Symbols.Package

	// Collect package-private type names declared in the old package
	// (excluding the moved file's own types).
	movedNames := map[string]bool{}
	for _, td := range fe.Symbols.Types {
		movedNames[td.Name] = true
	}
	pkgPrivateTypes := map[string]bool{}
	for _, entry := range idx.Files() {
		if entry.Symbols == nil || entry.Path == file || entry.Symbols.Package != oldPkg {
			continue
		}
		for _, td := range entry.Symbols.Types {
			if !td.IsPublic && !movedNames[td.Name] {
				pkgPrivateTypes[td.Name] = true
			}
		}
	}

	// Case 1: moved file uses any of those package-private types.
	if len(pkgPrivateTypes) > 0 {
		src, err := os.ReadFile(file)
		if err == nil {
			used := map[string]bool{}
			for _, tok := range java.Tokenize(src) {
				if tok.Kind == java.TIdent && pkgPrivateTypes[tok.Text] {
					used[tok.Text] = true
				}
			}
			for name := range used {
				warnings = append(warnings, fmt.Sprintf(
					"%s is package-private in %s and will not be importable after the move; make it public or keep them in the same package",
					name, oldPkg))
			}
		}
	}

	// Case 2: the moved type is package-private and has dependents.
	for _, td := range fe.Symbols.Types {
		if td.IsPublic {
			continue
		}
		fqn := td.Name
		if oldPkg != "" {
			fqn = oldPkg + "." + td.Name
		}
		deps := idx.FilesImporting(fqn)
		hasDep := false
		for _, d := range deps {
			if d != file {
				hasDep = true
				break
			}
		}
		if hasDep {
			warnings = append(warnings, fmt.Sprintf(
				"%s is package-private but is referenced elsewhere; it must be made public to be imported after the move",
				td.Name))
		}
	}

	return warnings
}

// fileUsesIdentifier scans a Java file for any identifier token
// matching name, excluding import/package declarations.
func fileUsesIdentifier(path, name string) bool {
	src, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	tokens := java.Tokenize(src)
	// Quick parse to get import/package ranges to skip.
	syms, _ := java.ExtractSymbols(src)
	for _, tok := range tokens {
		if tok.Kind != java.TIdent || tok.Text != name {
			continue
		}
		if syms != nil && inJavaImport(tok, syms) {
			continue
		}
		if syms != nil && inJavaPackageDecl(tok, syms) {
			continue
		}
		return true
	}
	return false
}

func deleteJava(file, root string) (*edit.Set, []string, error) {
	_, idx, err := javaIndex(root, file)
	if err != nil {
		return nil, nil, err
	}
	fe := idx.File(file)
	if fe == nil || fe.Symbols == nil {
		return nil, nil, fmt.Errorf("file %s not indexed", file)
	}
	set := edit.NewSet()
	var refs []string
	for _, td := range fe.Symbols.Types {
		fqn := td.Name
		if fe.Symbols.Package != "" {
			fqn = fe.Symbols.Package + "." + td.Name
		}
		for _, path := range idx.FilesImporting(fqn) {
			if path == file {
				continue
			}
			dep := idx.File(path)
			if dep == nil || dep.Symbols == nil {
				continue
			}
			for _, imp := range dep.Symbols.Imports {
				if imp.Path == fqn && !imp.IsWildcard {
					set.Add(edit.Edit{
						File: path, Start: imp.Range.Start, End: imp.Range.End,
						NewText: "", Why: "remove import for deleted file",
					})
					refs = append(refs, path)
				}
			}
		}
	}
	set.AddDelete(file, "delete file")
	return set, refs, nil
}

// --- Kotlin ---

func renameKotlin(file, name, newName, root string) (*edit.Set, error) {
	p, err := detectProject(root, file)
	if err != nil {
		return nil, err
	}
	idx, err := ktrename.BuildIndex(p)
	if err != nil {
		return nil, err
	}
	fe := idx.File(file)
	if fe == nil || fe.Symbols == nil {
		return nil, fmt.Errorf("file %s not indexed", file)
	}
	fqn := name
	if fe.Symbols.Package != "" {
		fqn = fe.Symbols.Package + "." + name
	}
	return ktrename.PlanRename(ktrename.RenameRequest{FQN: fqn, NewName: newName, Index: idx})
}

func moveKotlin(file, toFile, root string) (*edit.Set, error) {
	p, err := detectProject(root, file)
	if err != nil {
		return nil, err
	}
	idx, err := ktrename.BuildIndex(p)
	if err != nil {
		return nil, err
	}
	fe := idx.File(file)
	if fe == nil || fe.Symbols == nil {
		return nil, fmt.Errorf("file %s not indexed", file)
	}
	fqn := ""
	for _, d := range fe.Symbols.Declarations {
		fqn = d.Name
		if fe.Symbols.Package != "" {
			fqn = fe.Symbols.Package + "." + fqn
		}
		break
	}
	if fqn == "" {
		return nil, fmt.Errorf("no declaration in %s", file)
	}
	newPkg := packageFromPath(toFile, p)
	return ktrename.PlanMove(ktrename.MoveRequest{FQN: fqn, NewPackage: newPkg, Index: idx})
}

func deleteKotlin(file, root string) (*edit.Set, []string, error) {
	p, err := detectProject(root, file)
	if err != nil {
		return nil, nil, err
	}
	idx, err := ktrename.BuildIndex(p)
	if err != nil {
		return nil, nil, err
	}
	fe := idx.File(file)
	if fe == nil || fe.Symbols == nil {
		set := edit.NewSet()
		set.AddDelete(file, "delete file")
		return set, nil, nil
	}
	for _, d := range fe.Symbols.Declarations {
		fqn := d.Name
		if fe.Symbols.Package != "" {
			fqn = fe.Symbols.Package + "." + fqn
		}
		r, err := ktrename.PlanDelete(ktrename.DeleteRequest{FQN: fqn, Index: idx})
		if err == nil {
			return r.Set, r.FilesWithReferences, nil
		}
	}
	set := edit.NewSet()
	set.AddDelete(file, "delete file")
	return set, nil, nil
}

// --- Go ---

func renameGo(file, name, newName, root string) (*edit.Set, error) {
	idx, err := goIndex(root)
	if err != nil {
		return nil, err
	}
	pkg := idx.FileToImportPath(file)
	return gorename.PlanRename(gorename.RenameRequest{Package: pkg, Symbol: name, NewName: newName, Index: idx})
}

// --- TS ---

func renameTS(file, name, newName, root string) (*edit.Set, error) {
	idx, err := tsIndex(root)
	if err != nil {
		return nil, err
	}
	return tsrename.PlanRename(tsrename.RenameRequest{File: file, Symbol: name, NewName: newName, Index: idx})
}

// --- shared helpers ---

func detectProject(root, file string) (*project.Project, error) {
	start := root
	if start == "" {
		start = filepath.Dir(file)
	}
	abs, _ := filepath.Abs(start)
	return project.DetectFrom(abs)
}

func javaIndex(root, file string) (*project.Project, *index.Index, error) {
	p, err := detectProject(root, file)
	if err != nil {
		return nil, nil, err
	}
	idx, err := index.Build(p, ".java")
	if err != nil {
		return nil, nil, err
	}
	return p, idx, nil
}

func goIndex(root string) (*golang.Index, error) {
	if root == "" {
		root, _ = os.Getwd()
	}
	abs, _ := filepath.Abs(root)
	return golang.BuildIndex(abs)
}

func tsIndex(root string) (*typescript.Index, error) {
	if root == "" {
		root, _ = os.Getwd()
	}
	abs, _ := filepath.Abs(root)
	return typescript.BuildIndex(abs)
}

func packageFromPath(file string, p *project.Project) string {
	dir := filepath.Dir(file)
	for _, m := range p.Modules {
		for _, sr := range m.SourceRoots {
			if strings.HasPrefix(dir, sr.Path) {
				rel, err := filepath.Rel(sr.Path, dir)
				if err != nil {
					continue
				}
				return strings.ReplaceAll(rel, string(filepath.Separator), ".")
			}
		}
	}
	return ""
}

func javaInsertionPoint(fe *index.FileEntry) int {
	if n := len(fe.Symbols.Imports); n > 0 {
		return fe.Symbols.Imports[n-1].Range.End
	}
	if fe.Symbols.PackageRange.End > 0 {
		src, err := os.ReadFile(fe.Path)
		if err != nil {
			return 0
		}
		for i := fe.Symbols.PackageRange.End; i < len(src); i++ {
			if src[i] == ';' {
				return i + 1
			}
		}
	}
	return 0
}

// renameByToken is a generic token-scan rename for Java/Kotlin.
//
// Critical safety rule: we only rename identifiers in files where we
// are CONFIDENT the name refers to the target, not to some other
// symbol with the same name (e.g. java.lang.Class vs our Class).
//
// Confidence tiers:
//   - Declaring file: rename all matches (we defined it here)
//   - Explicit import (`import com.foo.Bar`): rename all matches
//     (the import proves the name refers to our target)
//   - Same-package / wildcard import: SKIP identifier renames
//     (bare `Class` might be java.lang.Class, not ours)
//
// This is conservative — same-package references to unique names
// like `FooBarHelper` would be safe to rename but we skip them
// anyway. The user can do a second pass for those files.
func renameByToken(file, name, newName, fqn string, idx *index.Index, p *project.Project) (*edit.Set, error) {
	set := edit.NewSet()
	newFQN := strings.TrimSuffix(fqn, "."+name) + "." + newName
	declFE := idx.File(file)
	declPkg := ""
	if declFE != nil && declFE.Symbols != nil {
		declPkg = declFE.Symbols.Package
	}

	for _, path := range idx.FilesImporting(fqn) {
		src, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		fe := idx.File(path)
		if fe == nil || fe.Symbols == nil {
			continue
		}

		isDeclFile := path == file
		hasExplicitImport := fileHasExplicitImport(fe, fqn)
		isSamePackage := fe.Symbols.Package == declPkg && !isDeclFile

		// Rewrite explicit imports regardless.
		for _, imp := range fe.Symbols.Imports {
			if imp.Path == fqn && !imp.IsWildcard {
				set.Add(edit.Edit{
					File: path, Start: imp.PathRange.Start, End: imp.PathRange.End,
					NewText: newFQN, Why: "update import",
				})
			}
		}

		// Only scan identifiers in files where we're confident.
		if !isDeclFile && !hasExplicitImport {
			// Same-package and wildcard files: skip identifier renames
			// to avoid collisions with java.lang.* or other same-named
			// symbols. Log for dry-run awareness.
			if isSamePackage {
				continue
			}
			continue
		}

		tokens := java.Tokenize(src)
		for _, tok := range tokens {
			if tok.Kind != java.TIdent || tok.Text != name {
				continue
			}
			if inJavaImport(tok, fe.Symbols) {
				continue
			}
			if inJavaPackageDecl(tok, fe.Symbols) {
				continue
			}
			set.Add(edit.Edit{
				File: path, Start: tok.Start, End: tok.End,
				NewText: newName, Why: fmt.Sprintf("rename %s -> %s", name, newName),
			})
		}
	}

	// Scan ALL project files for inline fully-qualified references
	// like `com.foo.Bar` used without an import. These don't show up
	// in FilesImporting() because there's no import statement.
	for _, fe := range idx.Files() {
		if fe.Err != nil || fe.Symbols == nil {
			continue
		}
		src, err := os.ReadFile(fe.Path)
		if err != nil {
			continue
		}
		tokens := java.Tokenize(src)
		renameFQNReferences(fe.Path, tokens, fqn, newName, fe, set)
	}

	// Rename the file if it matches the old name.
	oldBase := filepath.Base(file)
	if oldBase == name+".java" {
		newFile := filepath.Join(filepath.Dir(file), newName+".java")
		set.AddMove(edit.Move{From: file, To: newFile, Why: "rename file"})
	}
	return set, nil
}

func fileHasExplicitImport(fe *index.FileEntry, fqn string) bool {
	for _, imp := range fe.Symbols.Imports {
		if !imp.IsWildcard && !imp.IsStatic && imp.Path == fqn {
			return true
		}
	}
	return false
}

func inJavaPackageDecl(tok java.Token, syms *java.FileSymbols) bool {
	r := syms.PackageRange
	return r.End > 0 && tok.Start >= r.Start && tok.End <= r.End
}

func inJavaImport(tok java.Token, syms *java.FileSymbols) bool {
	for _, imp := range syms.Imports {
		if tok.Start >= imp.Range.Start && tok.End <= imp.Range.End {
			return true
		}
	}
	return false
}

// RenamePackage renames a package across the project. Detects language
// from the package path format or from files in the directory.
func RenamePackage(from, to, root string) (*edit.Set, error) {
	// Try Java/Kotlin first (dotted package names like com.foo).
	if strings.Contains(from, ".") {
		// Could be Java or Kotlin. Try Java first.
		p, err := detectProjectFromRoot(root)
		if err == nil {
			// Try Java.
			jIdx, err := index.Build(p, ".java")
			if err == nil {
				files := jIdx.FilesInPackage(from)
				if len(files) > 0 {
					return javaRenamePackage(from, to, p)
				}
			}
			// Try Kotlin.
			kIdx, err := ktrename.BuildIndex(p)
			if err == nil {
				if len(kIdx.FilesInPkg(from)) > 0 {
					return ktrename.PlanRenamePackage(ktrename.RenamePackageRequest{
						OldPackage: from, NewPackage: to, Index: kIdx,
					})
				}
			}
		}
		return nil, fmt.Errorf("no files found in package %q", from)
	}

	// Go import path (contains /).
	if strings.Contains(from, "/") {
		idx, err := goIndex(root)
		if err != nil {
			return nil, err
		}
		return gorename.PlanRenamePackage(gorename.RenamePackageRequest{
			OldImportPath: from, NewImportPath: to, Index: idx,
		})
	}

	return nil, fmt.Errorf("could not determine language for package %q — use dotted (Java/Kotlin) or slash-separated (Go) format", from)
}

func detectProjectFromRoot(root string) (*project.Project, error) {
	if root == "" {
		root, _ = os.Getwd()
	}
	abs, _ := filepath.Abs(root)
	return project.DetectFrom(abs)
}

func javaRenamePackage(from, to string, p *project.Project) (*edit.Set, error) {
	idx, err := index.Build(p, ".java")
	if err != nil {
		return nil, err
	}
	return renamepkg.Plan(renamepkg.Request{OldPackage: from, NewPackage: to, Index: idx})
}

// --- Rust ---

func renameRust(file, name, newName, root string) (*edit.Set, error) {
	idx, err := rustIndex(root)
	if err != nil {
		return nil, err
	}
	return rstrename.PlanRename(rstrename.RenameRequest{File: file, Symbol: name, NewName: newName, Index: idx})
}

func rustIndex(root string) (*rust.Index, error) {
	if root == "" {
		root, _ = os.Getwd()
	}
	abs, _ := filepath.Abs(root)
	return rust.BuildIndex(abs)
}

// Keep kotlin and typescript imported.
var (
	_ = kotlin.Tokenize
	_ = typescript.Tokenize
)
