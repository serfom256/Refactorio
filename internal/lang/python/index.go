package python

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// Index holds the project-wide view of Python source.
type Index struct {
	Root       string
	files      map[string]*FileEntry
	byModule   map[string][]string // module path → files
	bySymbol   map[string][]*SymbolRef
}

type FileEntry struct {
	Path    string
	Symbols *FileSymbols
	Err     error
}

type SymbolRef struct {
	Declaration
	File       string
	ModulePath string
}

// BuildIndex walks root for .py files, excluding venv directories,
// __pycache__, .git, build/, dist/, and similar non-source paths.
func BuildIndex(root string) (*Index, error) {
	root, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	idx := &Index{
		Root:     root,
		files:    map[string]*FileEntry{},
		byModule: map[string][]string{},
		bySymbol: map[string][]*SymbolRef{},
	}

	var paths []string
	_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			base := d.Name()
			switch base {
			case ".venv", "venv", "__pycache__", ".git", ".tox",
				"dist", "build", ".eggs", "node_modules", ".pytest_cache",
				".mypy_cache", ".ruff_cache":
				return filepath.SkipDir
			}
			return nil
		}
		if filepath.Ext(path) == ".py" {
			paths = append(paths, path)
		}
		return nil
	})

	type result struct {
		path  string
		entry *FileEntry
	}
	in := make(chan string, len(paths))
	out := make(chan result, len(paths))
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for p := range in {
				e := &FileEntry{Path: p}
				src, err := os.ReadFile(p)
				if err != nil {
					e.Err = err
				} else {
					fs, err := ExtractSymbols(src)
					if err != nil {
						e.Err = err
					} else {
						e.Symbols = fs
					}
				}
				out <- result{p, e}
			}
		}()
	}
	for _, p := range paths {
		in <- p
	}
	close(in)
	go func() { wg.Wait(); close(out) }()
	for r := range out {
		idx.files[r.path] = r.entry
	}
	idx.buildLookups()
	return idx, nil
}

func (idx *Index) buildLookups() {
	for path, fe := range idx.files {
		if fe.Symbols == nil {
			continue
		}
		modPath := idx.FileToModulePath(path)
		if modPath != "" {
			idx.byModule[modPath] = append(idx.byModule[modPath], path)
		}
		for i := range fe.Symbols.Declarations {
			d := &fe.Symbols.Declarations[i]
			ref := &SymbolRef{Declaration: *d, File: path, ModulePath: modPath}
			idx.bySymbol[d.Name] = append(idx.bySymbol[d.Name], ref)
		}
	}
}

// FileToModulePath computes the Python module path for a file based
// on its directory layout relative to the index root.
//
// Conventions:
//   foo.py                → "foo"
//   foo/__init__.py       → "foo"
//   foo/bar.py            → "foo.bar"
//   foo/bar/__init__.py   → "foo.bar"
func (idx *Index) FileToModulePath(file string) string {
	rel, err := filepath.Rel(idx.Root, file)
	if err != nil {
		return ""
	}
	rel = filepath.ToSlash(rel)
	rel = strings.TrimSuffix(rel, ".py")
	rel = strings.TrimSuffix(rel, "/__init__")
	rel = strings.TrimSuffix(rel, "__init__")
	if rel == "" || rel == "." {
		return ""
	}
	return strings.ReplaceAll(rel, "/", ".")
}

// ModulePathToFile resolves a Python module path to a file path
// within the index. Tries `<root>/<path>.py` then
// `<root>/<path>/__init__.py`.
func (idx *Index) ModulePathToFile(modulePath string) string {
	rel := strings.ReplaceAll(modulePath, ".", "/")
	candidates := []string{
		filepath.Join(idx.Root, rel+".py"),
		filepath.Join(idx.Root, rel, "__init__.py"),
	}
	for _, c := range candidates {
		if _, ok := idx.files[c]; ok {
			return c
		}
	}
	return ""
}

func (idx *Index) File(path string) *FileEntry { return idx.files[path] }

func (idx *Index) AllFiles() []string {
	out := make([]string, 0, len(idx.files))
	for p := range idx.files {
		out = append(out, p)
	}
	return out
}

// FilesUsing returns files whose imports could resolve the symbol
// declared in (modulePath, symbolName). Covers:
//   import modulePath               → uses modulePath.symbolName
//   from modulePath import symbolName
//   from modulePath import *
func (idx *Index) FilesUsing(modulePath, symbolName string) []string {
	seen := map[string]struct{}{}
	for path, fe := range idx.files {
		if fe.Symbols == nil {
			continue
		}
		for _, imp := range fe.Symbols.Imports {
			if imp.IsFrom && imp.Path == modulePath {
				// from modulePath import X / X as Y / *
				for _, n := range imp.Names {
					if n.Name == symbolName || n.Name == "*" {
						seen[path] = struct{}{}
						break
					}
				}
				continue
			}
			if !imp.IsFrom && imp.Path == modulePath {
				// import modulePath  → accessed as modulePath.symbolName
				seen[path] = struct{}{}
				continue
			}
		}
	}
	out := make([]string, 0, len(seen))
	for p := range seen {
		out = append(out, p)
	}
	return out
}

// SameModuleFiles returns other files in the same module path
// (i.e. the same package directory).
func (idx *Index) SameModuleFiles(modulePath string) []string {
	return idx.byModule[modulePath]
}

// FindDeclaration returns symbols matching name across the project.
func (idx *Index) FindDeclaration(name string) []*SymbolRef {
	return idx.bySymbol[name]
}
