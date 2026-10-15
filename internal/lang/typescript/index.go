package typescript

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
)

type Index struct {
	files    map[string]*FileEntry
	byExport map[string]*FileEntry
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

func (idx *Index) File(path string) *FileEntry {
	return idx.files[path]
}

func (idx *Index) AllFiles() []string {
	out := make([]string, 0, len(idx.files))
	for p := range idx.files {
		out = append(out, p)
	}
	return out
}

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

func ComputeNewImportPath(fromFile, toFile string) string {
	fromDir := filepath.Dir(fromFile)
	rel, err := filepath.Rel(fromDir, toFile)
	if err != nil {
		return toFile
	}
	rel = strings.ReplaceAll(rel, string(filepath.Separator), "/")
	for _, ext := range []string{".tsx", ".ts", ".jsx", ".js", ".mts", ".cts", ".mjs", ".cjs"} {
		if strings.HasSuffix(rel, ext) {
			rel = rel[:len(rel)-len(ext)]
			break
		}
	}
	rel = strings.TrimSuffix(rel, "/index")
	if !strings.HasPrefix(rel, ".") {
		rel = "./" + rel
	}
	return rel
}
