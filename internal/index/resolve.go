package index

import (
	"os"
	"sort"
	"strings"
)

func (idx *Index) File(path string) *FileEntry {
	return idx.files[path]
}

func (idx *Index) Files() []*FileEntry {
	out := make([]*FileEntry, 0, len(idx.files))
	for _, fe := range idx.files {
		out = append(out, fe)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out
}

func (idx *Index) Errors() map[string]error {
	out := map[string]error{}
	for _, fe := range idx.files {
		if fe.Err != nil {
			out[fe.Path] = fe.Err
		}
	}
	return out
}

func (idx *Index) LookupFQN(fqn string) *TypeDecl {
	return idx.byFQN[fqn]
}

func (idx *Index) LookupType(simpleName string) []*TypeDecl {
	out := idx.byName[simpleName]
	cp := make([]*TypeDecl, len(out))
	copy(cp, out)
	return cp
}

func (idx *Index) FilesInPackage(pkg string) []string {
	return idx.packageFiles[pkg]
}

func (idx *Index) FilesImporting(fqn string) []string {
	seen := map[string]struct{}{}
	add := func(p string) { seen[p] = struct{}{} }

	for _, p := range idx.explicitImports[fqn] {
		add(p)
	}
	pkg := dropLastSegment(fqn)
	for _, p := range idx.wildcardImports[pkg] {
		add(p)
	}
	for _, p := range idx.packageFiles[pkg] {
		add(p)
	}
	if decl := idx.byFQN[fqn]; decl != nil {
		add(decl.File)
	}

	out := make([]string, 0, len(seen))
	for p := range seen {
		out = append(out, p)
	}
	sort.Strings(out)
	return out
}

func MatchesWildcardPackage(path, pkg string) bool {
	if path == pkg {
		return true
	}
	return strings.HasPrefix(path, pkg+".")
}

func readFile(path string) ([]byte, error) {
	return os.ReadFile(path)
}
