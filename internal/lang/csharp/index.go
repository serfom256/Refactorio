package csharp

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// Index holds the project-wide view of C# source.
type Index struct {
	Root  string
	files map[string]*FileEntry
	// byNS maps namespace → files that declare in that namespace.
	byNS map[string][]string
	// byFQN maps "Namespace.TypeName" → declaration.
	byFQN map[string]*TypeRef
}

type FileEntry struct {
	Path    string
	Symbols *FileSymbols
	Err     error
}

type TypeRef struct {
	TypeDecl
	File      string
	Namespace string
	FQN       string
}

// BuildIndex walks root for .cs files (excluding bin/, obj/, .git).
// The root is the directory that contains the .csproj/.sln (or any
// directory the user passes — we don't require either).
func BuildIndex(root string) (*Index, error) {
	root, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	idx := &Index{
		Root:  root,
		files: map[string]*FileEntry{},
		byNS:  map[string][]string{},
		byFQN: map[string]*TypeRef{},
	}

	var paths []string
	_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			base := d.Name()
			if base == "bin" || base == "obj" || base == ".git" || base == "node_modules" {
				return filepath.SkipDir
			}
			return nil
		}
		if filepath.Ext(path) == ".cs" {
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
		ns := fe.Symbols.Namespace
		if ns != "" {
			idx.byNS[ns] = append(idx.byNS[ns], path)
		}
		for i := range fe.Symbols.Types {
			td := &fe.Symbols.Types[i]
			fqn := td.Name
			if ns != "" {
				fqn = ns + "." + td.Name
			}
			idx.byFQN[fqn] = &TypeRef{TypeDecl: *td, File: path, Namespace: ns, FQN: fqn}
		}
	}
}

func (idx *Index) File(path string) *FileEntry { return idx.files[path] }

func (idx *Index) AllFiles() []string {
	out := make([]string, 0, len(idx.files))
	for p := range idx.files {
		out = append(out, p)
	}
	return out
}

func (idx *Index) Files() []*FileEntry {
	out := make([]*FileEntry, 0, len(idx.files))
	for _, fe := range idx.files {
		out = append(out, fe)
	}
	return out
}

func (idx *Index) LookupFQN(fqn string) *TypeRef { return idx.byFQN[fqn] }

func (idx *Index) FilesInNamespace(ns string) []string { return idx.byNS[ns] }

// FilesUsing returns files that have a using directive matching ns,
// or are in the same namespace as ns.
func (idx *Index) FilesUsing(ns string) []string {
	seen := map[string]struct{}{}
	for path, fe := range idx.files {
		if fe.Symbols == nil {
			continue
		}
		if fe.Symbols.Namespace == ns {
			seen[path] = struct{}{}
			continue
		}
		for _, u := range fe.Symbols.Usings {
			if u.Path == ns || strings.HasPrefix(u.Path, ns+".") {
				seen[path] = struct{}{}
				break
			}
		}
	}
	out := make([]string, 0, len(seen))
	for p := range seen {
		out = append(out, p)
	}
	return out
}
