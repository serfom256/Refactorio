package rust

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
)

type Index struct {
	Root      string
	CrateName string
	files     map[string]*FileEntry
	byModule  map[string][]string // module path → files
}

type FileEntry struct {
	Path    string
	Symbols *FileSymbols
	Err     error
}

func BuildIndex(root string) (*Index, error) {
	root, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	crateName, err := readCrateName(root)
	if err != nil {
		return nil, err
	}

	idx := &Index{
		Root:      root,
		CrateName: crateName,
		files:     map[string]*FileEntry{},
		byModule:  map[string][]string{},
	}

	var paths []string
	_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			base := d.Name()
			if base == "target" || base == ".git" || base == "vendor" {
				return filepath.SkipDir
			}
			return nil
		}
		if filepath.Ext(path) == ".rs" {
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
	idx.buildModuleMap()
	return idx, nil
}

func (idx *Index) buildModuleMap() {
	for path := range idx.files {
		modPath := idx.FileToModulePath(path)
		if modPath != "" {
			idx.byModule[modPath] = append(idx.byModule[modPath], path)
		}
	}
}

// FileToModulePath computes the Rust module path for a file.
// src/lib.rs → "crate"
// src/foo.rs → "crate::foo"
// src/foo/mod.rs → "crate::foo"
// src/foo/bar.rs → "crate::foo::bar"
func (idx *Index) FileToModulePath(file string) string {
	srcDir := filepath.Join(idx.Root, "src")
	rel, err := filepath.Rel(srcDir, file)
	if err != nil {
		return ""
	}
	rel = filepath.ToSlash(rel)

	if rel == "lib.rs" || rel == "main.rs" {
		return "crate"
	}

	rel = strings.TrimSuffix(rel, ".rs")
	rel = strings.TrimSuffix(rel, "/mod")
	parts := strings.Split(rel, "/")
	return "crate::" + strings.Join(parts, "::")
}

func (idx *Index) File(path string) *FileEntry { return idx.files[path] }

func (idx *Index) AllFiles() []string {
	out := make([]string, 0, len(idx.files))
	for p := range idx.files {
		out = append(out, p)
	}
	return out
}

// FilesUsing returns files that have a `use` path matching or prefixed
// by the given module path + symbol.
func (idx *Index) FilesUsing(modulePath, symbol string) []string {
	fullPath := modulePath + "::" + symbol
	var out []string
	for path, fe := range idx.files {
		if fe.Symbols == nil {
			continue
		}
		for _, u := range fe.Symbols.Uses {
			if u.Path == fullPath || strings.HasPrefix(u.Path, fullPath+"::") {
				out = append(out, path)
				break
			}
			// Glob use: `use crate::foo::*`
			if strings.HasSuffix(u.Path, "::*") {
				prefix := strings.TrimSuffix(u.Path, "::*")
				if prefix == modulePath {
					out = append(out, path)
					break
				}
			}
		}
	}
	return out
}

// FilesInModule returns all files in the given module path.
func (idx *Index) FilesInModule(modulePath string) []string {
	return idx.byModule[modulePath]
}

func readCrateName(root string) (string, error) {
	data, err := os.ReadFile(filepath.Join(root, "Cargo.toml"))
	if err != nil {
		return "", err
	}
	inPackage := false
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "[package]" {
			inPackage = true
			continue
		}
		if strings.HasPrefix(line, "[") {
			inPackage = false
			continue
		}
		if inPackage && strings.HasPrefix(line, "name") {
			parts := strings.SplitN(line, "=", 2)
			if len(parts) == 2 {
				name := strings.TrimSpace(parts[1])
				name = strings.Trim(name, `"'`)
				return name, nil
			}
		}
	}
	return filepath.Base(root), nil
}
