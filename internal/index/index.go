// Package index builds an in-memory map of every type declaration in a
// Java project plus the per-file import data needed to find dependents.
//
// Scope is currently Java-only because [java.ExtractSymbols] is the
// only extractor wired up. When a second language follows the same
// data shape we can promote the Java types to a language-agnostic
// surface; for now keeping the Java dependency direct avoids guessing
// at the abstraction.
//
// What the index answers:
//   - File(path) → the parsed symbols for one file
//   - LookupFQN(fqn) → the single declaration of a fully-qualified type
//   - LookupType(name) → every declaration whose simple name matches
//     (used to surface "ambiguous; specify FQN" errors)
//   - FilesImporting(fqn) → every file that could reference this type
//     because it imports it explicitly, statically, by wildcard, or
//     lives in the same package
//
// Index does NOT do reference resolution. It is structural data only.
// The rules layer that consumes this index is where the heuristic
// "this `.foo()` call is probably a method of Bar" logic lives.
package index

import (
	"strings"
	"sync"

	"github.com/serfom256/refactorio/internal/lang/java"
	"github.com/serfom256/refactorio/internal/project"
)

// Index holds the project-wide symbol view.
type Index struct {
	project *project.Project

	files map[string]*FileEntry // path → entry (parsed or errored)

	// byFQN maps "com.foo.Bar" → its top-level declaration. Nested types
	// are not represented here; their parent's FQN is what callers ask
	// for in practice.
	byFQN map[string]*TypeDecl

	// byName maps "Bar" → all top-level declarations with that simple
	// name, possibly across packages.
	byName map[string][]*TypeDecl

	// explicitImports[fqn] = files that name this FQN in `import` or
	// `import static fqn.member`. Static-wildcard `import static fqn.*`
	// is treated as importing fqn.
	explicitImports map[string][]string

	// wildcardImports[pkg] = files with `import pkg.*`.
	wildcardImports map[string][]string

	// packageFiles[pkg] = files declared `package pkg;`.
	packageFiles map[string][]string
}

// FileEntry is one parsed source file (or the error that prevented
// parsing). Symbols is nil iff Err is non-nil.
type FileEntry struct {
	Path    string
	Symbols *java.FileSymbols
	Err     error
}

// TypeDecl is a top-level type declaration with the project context
// resolved: file it lives in, package, and computed FQN.
type TypeDecl struct {
	*java.TypeDecl
	File    string
	Package string
	FQN     string
}

// Build scans every file in the project whose extension is in exts
// (use ".java"), parses each, and computes the lookup tables.
// Per-file extraction errors are recorded on the FileEntry and do not
// abort the build.
func Build(p *project.Project, exts ...string) (*Index, error) {
	idx := &Index{
		project:         p,
		files:           map[string]*FileEntry{},
		byFQN:           map[string]*TypeDecl{},
		byName:          map[string][]*TypeDecl{},
		explicitImports: map[string][]string{},
		wildcardImports: map[string][]string{},
		packageFiles:    map[string][]string{},
	}
	files, err := p.SourceFiles(exts...)
	if err != nil {
		return nil, err
	}
	// Extraction is CPU-bound and per-file independent; parallelise.
	type result struct {
		path    string
		entry   *FileEntry
	}
	in := make(chan string, len(files))
	out := make(chan result, len(files))
	var wg sync.WaitGroup
	workers := 8
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for path := range in {
				entry := extractOne(path)
				out <- result{path, entry}
			}
		}()
	}
	for _, f := range files {
		in <- f
	}
	close(in)
	go func() { wg.Wait(); close(out) }()
	for r := range out {
		idx.files[r.path] = r.entry
	}
	idx.buildLookups()
	return idx, nil
}

func extractOne(path string) *FileEntry {
	entry := &FileEntry{Path: path}
	src, err := readFile(path)
	if err != nil {
		entry.Err = err
		return entry
	}
	fs, err := java.ExtractSymbols(src)
	if err != nil {
		entry.Err = err
		return entry
	}
	entry.Symbols = fs
	return entry
}

// buildLookups populates the lookup tables from idx.files. Called once
// at the end of Build; the index is then read-only.
func (idx *Index) buildLookups() {
	for path, fe := range idx.files {
		if fe.Symbols == nil {
			continue
		}
		pkg := fe.Symbols.Package
		if pkg != "" {
			idx.packageFiles[pkg] = append(idx.packageFiles[pkg], path)
		}
		for i := range fe.Symbols.Types {
			t := &fe.Symbols.Types[i]
			fqn := t.Name
			if pkg != "" {
				fqn = pkg + "." + t.Name
			}
			td := &TypeDecl{TypeDecl: t, File: path, Package: pkg, FQN: fqn}
			idx.byFQN[fqn] = td
			idx.byName[t.Name] = append(idx.byName[t.Name], td)
		}
		for _, imp := range fe.Symbols.Imports {
			switch {
			case imp.IsStatic && imp.IsWildcard:
				// import static com.foo.Bar.*  → references com.foo.Bar
				idx.explicitImports[imp.Path] = append(idx.explicitImports[imp.Path], path)
			case imp.IsStatic:
				// import static com.foo.Bar.member  → references com.foo.Bar
				fqn := dropLastSegment(imp.Path)
				if fqn != "" {
					idx.explicitImports[fqn] = append(idx.explicitImports[fqn], path)
				}
			case imp.IsWildcard:
				// import com.foo.*
				idx.wildcardImports[imp.Path] = append(idx.wildcardImports[imp.Path], path)
			default:
				// import com.foo.Bar
				idx.explicitImports[imp.Path] = append(idx.explicitImports[imp.Path], path)
			}
		}
	}
}

// dropLastSegment returns "a.b.c" from "a.b.c.d", or "" from a single
// segment with no dot.
func dropLastSegment(s string) string {
	i := strings.LastIndexByte(s, '.')
	if i < 0 {
		return ""
	}
	return s[:i]
}
