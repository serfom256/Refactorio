package index

import (
	"strings"
	"sync"

	"github.com/serfom256/refactorio/internal/lang/java"
	"github.com/serfom256/refactorio/internal/project"
)

type Index struct {
	project *project.Project

	files map[string]*FileEntry

	byFQN map[string]*TypeDecl

	byName map[string][]*TypeDecl

	explicitImports map[string][]string

	wildcardImports map[string][]string

	packageFiles map[string][]string
}

type FileEntry struct {
	Path    string
	Symbols *java.FileSymbols
	Err     error
}

type TypeDecl struct {
	*java.TypeDecl
	File    string
	Package string
	FQN     string
}

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
	type result struct {
		path  string
		entry *FileEntry
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
				idx.explicitImports[imp.Path] = append(idx.explicitImports[imp.Path], path)
			case imp.IsStatic:
				fqn := dropLastSegment(imp.Path)
				if fqn != "" {
					idx.explicitImports[fqn] = append(idx.explicitImports[fqn], path)
				}
			case imp.IsWildcard:
				idx.wildcardImports[imp.Path] = append(idx.wildcardImports[imp.Path], path)
			default:
				idx.explicitImports[imp.Path] = append(idx.explicitImports[imp.Path], path)
			}
		}
	}
}

func dropLastSegment(s string) string {
	i := strings.LastIndexByte(s, '.')
	if i < 0 {
		return ""
	}
	return s[:i]
}
