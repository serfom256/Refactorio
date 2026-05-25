package golang

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// Index holds the project-wide view of Go source files.
type Index struct {
	ModulePath string // from go.mod, e.g. "github.com/foo/bar"
	Root       string // absolute path of go.mod's directory
	files      map[string]*FileEntry
	// byPkg maps import path → list of files in that package.
	byPkg map[string][]string
}

type FileEntry struct {
	Path    string
	Symbols *FileSymbols
	Err     error
}

// BuildIndex reads go.mod from root, walks for .go files (excluding
// vendor, testdata, .git), and builds the lookup tables.
func BuildIndex(root string) (*Index, error) {
	root, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	modPath, err := readModulePath(root)
	if err != nil {
		return nil, err
	}

	idx := &Index{
		ModulePath: modPath,
		Root:       root,
		files:      map[string]*FileEntry{},
		byPkg:      map[string][]string{},
	}

	var paths []string
	_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			base := d.Name()
			if base == "vendor" || base == "testdata" || base == ".git" || base == "node_modules" {
				return filepath.SkipDir
			}
			return nil
		}
		if filepath.Ext(path) == ".go" {
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
	workers := 8
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for p := range in {
				entry := &FileEntry{Path: p}
				src, err := os.ReadFile(p)
				if err != nil {
					entry.Err = err
				} else {
					fs, err := ExtractSymbols(src)
					if err != nil {
						entry.Err = err
					} else {
						entry.Symbols = fs
					}
				}
				out <- result{p, entry}
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
	idx.buildPkgMap()
	return idx, nil
}

func (idx *Index) buildPkgMap() {
	for path, fe := range idx.files {
		if fe.Symbols == nil {
			continue
		}
		pkgImportPath := idx.FileToImportPath(path)
		if pkgImportPath != "" {
			idx.byPkg[pkgImportPath] = append(idx.byPkg[pkgImportPath], path)
		}
	}
}

// fileToImportPath computes the Go import path for a file based on
// its directory relative to the module root.
func (idx *Index) FileToImportPath(file string) string {
	dir := filepath.Dir(file)
	rel, err := filepath.Rel(idx.Root, dir)
	if err != nil {
		return ""
	}
	if rel == "." {
		return idx.ModulePath
	}
	return idx.ModulePath + "/" + filepath.ToSlash(rel)
}

// File returns the entry for an exact path.
func (idx *Index) File(path string) *FileEntry {
	return idx.files[path]
}

// AllFiles returns every indexed file path.
func (idx *Index) AllFiles() []string {
	out := make([]string, 0, len(idx.files))
	for p := range idx.files {
		out = append(out, p)
	}
	return out
}

// FilesInPackage returns all .go files with the given import path.
func (idx *Index) FilesInPackage(importPath string) []string {
	return idx.byPkg[importPath]
}

// FilesImporting returns paths of files that import the given package.
func (idx *Index) FilesImporting(importPath string) []string {
	var out []string
	for path, fe := range idx.files {
		if fe.Symbols == nil {
			continue
		}
		for _, imp := range fe.Symbols.Imports {
			if imp.Path == importPath {
				out = append(out, path)
				break
			}
		}
	}
	return out
}

// FindDeclaration locates the file and declaration where name is
// declared in the given package.
func (idx *Index) FindDeclaration(pkgImportPath, name string) (*FileEntry, *Declaration) {
	for _, path := range idx.byPkg[pkgImportPath] {
		fe := idx.files[path]
		if fe == nil || fe.Symbols == nil {
			continue
		}
		for i := range fe.Symbols.Declarations {
			if fe.Symbols.Declarations[i].Name == name {
				return fe, &fe.Symbols.Declarations[i]
			}
		}
	}
	return nil, nil
}

// ImportPathToDir returns the absolute directory for an import path
// within this module.
func (idx *Index) ImportPathToDir(importPath string) string {
	if importPath == idx.ModulePath {
		return idx.Root
	}
	if !strings.HasPrefix(importPath, idx.ModulePath+"/") {
		return ""
	}
	rel := importPath[len(idx.ModulePath)+1:]
	return filepath.Join(idx.Root, filepath.FromSlash(rel))
}

func readModulePath(root string) (string, error) {
	data, err := os.ReadFile(filepath.Join(root, "go.mod"))
	if err != nil {
		return "", err
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "module ") {
			return strings.TrimSpace(line[len("module "):]), nil
		}
	}
	return "", os.ErrNotExist
}
