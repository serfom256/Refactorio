// Package typescript index builds a map of every TS/JS file in a
// directory tree: its symbols, its imports, and which files import
// each export. This powers cross-file rename and move operations.
package typescript

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// Index holds the project-wide view of TS/JS files.
type Index struct {
	files map[string]*FileEntry // abs path → entry
	// byExport maps "absPath::exportName" → declaring FileEntry.
	// Used to find where a symbol is declared.
	byExport map[string]*FileEntry
}

// FileEntry is one parsed source file.
type FileEntry struct {
	Path    string
	Symbols *FileSymbols
	Err     error
}

// BuildIndex walks root for .ts/.tsx/.js/.jsx files (excluding
// node_modules, dist, build, .git), parses each, and builds the
// lookup tables.
func BuildIndex(root string) (*Index, error) {
	root, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	var paths []string
	err = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			base := d.Name()
			if base == "node_modules" || base == "dist" || base == "build" ||
				base == ".git" || base == ".next" || base == "coverage" {
				return filepath.SkipDir
			}
			return nil
		}
		ext := filepath.Ext(path)
		switch ext {
		case ".ts", ".tsx", ".js", ".jsx", ".mts", ".cts", ".mjs", ".cjs":
			paths = append(paths, path)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	idx := &Index{
		files:    map[string]*FileEntry{},
		byExport: map[string]*FileEntry{},
	}

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
	idx.buildExportMap()
	return idx, nil
}

func (idx *Index) buildExportMap() {
	for _, fe := range idx.files {
		if fe.Symbols == nil {
			continue
		}
		for _, d := range fe.Symbols.Declarations {
			if d.IsExport && d.Name != "" {
				key := fe.Path + "::" + d.Name
				idx.byExport[key] = fe
			}
		}
	}
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

// FindDeclaration locates the file and declaration where symbolName
// is exported. If file hint is provided, searches that file first.
func (idx *Index) FindDeclaration(symbolName, fileHint string) (*FileEntry, *Declaration) {
	if fileHint != "" {
		fe := idx.files[fileHint]
		if fe != nil && fe.Symbols != nil {
			for i := range fe.Symbols.Declarations {
				if fe.Symbols.Declarations[i].Name == symbolName {
					return fe, &fe.Symbols.Declarations[i]
				}
			}
		}
	}
	// Search all files.
	for _, fe := range idx.files {
		if fe.Symbols == nil {
			continue
		}
		for i := range fe.Symbols.Declarations {
			d := &fe.Symbols.Declarations[i]
			if d.Name == symbolName && d.IsExport {
				return fe, d
			}
		}
	}
	return nil, nil
}

// FilesImporting returns paths of files that import from the given
// absolute file path (resolving relative import paths).
func (idx *Index) FilesImporting(targetFile string) []string {
	var out []string
	for path, fe := range idx.files {
		if fe.Symbols == nil || path == targetFile {
			continue
		}
		for _, imp := range fe.Symbols.Imports {
			resolved := ResolveImportPath(imp.Path, path)
			if resolved == targetFile {
				out = append(out, path)
				break
			}
		}
	}
	return out
}

// ImportsOf returns the import declarations in file that reference
// targetFile.
func (idx *Index) ImportsOf(file, targetFile string) []ImportDecl {
	fe := idx.files[file]
	if fe == nil || fe.Symbols == nil {
		return nil
	}
	var out []ImportDecl
	for _, imp := range fe.Symbols.Imports {
		resolved := ResolveImportPath(imp.Path, file)
		if resolved == targetFile {
			out = append(out, imp)
		}
	}
	return out
}

// ComputeNewImportPath computes the relative import path from
// fromFile to toFile, without extension, using POSIX separators.
func ComputeNewImportPath(fromFile, toFile string) string {
	fromDir := filepath.Dir(fromFile)
	rel, err := filepath.Rel(fromDir, toFile)
	if err != nil {
		return toFile
	}
	// Convert to POSIX separators.
	rel = strings.ReplaceAll(rel, string(filepath.Separator), "/")
	// Strip extension.
	for _, ext := range []string{".tsx", ".ts", ".jsx", ".js", ".mts", ".cts", ".mjs", ".cjs"} {
		if strings.HasSuffix(rel, ext) {
			rel = rel[:len(rel)-len(ext)]
			break
		}
	}
	// Strip /index suffix.
	rel = strings.TrimSuffix(rel, "/index")
	// Ensure relative prefix.
	if !strings.HasPrefix(rel, ".") {
		rel = "./" + rel
	}
	return rel
}
