package index

import (
	"os"
	"sort"
	"strings"
)

// File returns the entry for an exact path, or nil if not indexed.
func (idx *Index) File(path string) *FileEntry {
	return idx.files[path]
}

// Files returns every indexed file entry, sorted by path for stable
// iteration. Including errored entries so callers can surface them.
func (idx *Index) Files() []*FileEntry {
	out := make([]*FileEntry, 0, len(idx.files))
	for _, fe := range idx.files {
		out = append(out, fe)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out
}

// Errors returns a map of path → extraction error for files that
// failed to parse.
func (idx *Index) Errors() map[string]error {
	out := map[string]error{}
	for _, fe := range idx.files {
		if fe.Err != nil {
			out[fe.Path] = fe.Err
		}
	}
	return out
}

// LookupFQN returns the declaration of a fully-qualified type, or nil.
func (idx *Index) LookupFQN(fqn string) *TypeDecl {
	return idx.byFQN[fqn]
}

// LookupType returns every top-level declaration whose simple name
// matches. Empty slice when nothing is found. When more than one entry
// is returned the caller must disambiguate (e.g. by FQN or by file).
func (idx *Index) LookupType(simpleName string) []*TypeDecl {
	out := idx.byName[simpleName]
	cp := make([]*TypeDecl, len(out))
	copy(cp, out)
	return cp
}

// FilesImporting returns the deduplicated set of file paths that could
// reference the given fully-qualified type. A file is included when:
//   - it explicitly imports fqn
//   - it imports the fqn's package with a wildcard
//   - it lives in the same package as fqn
//   - it IS the file that declares fqn
//
// Files in java.lang implicit-import scope are NOT specially handled
// here; the rules layer adds that filter when scanning references.
// FilesInPackage returns paths of files declared in the given package.
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

// MatchesWildcardPackage reports whether path is in the package prefix
// pkg or a sub-package (e.g. "com.foo" matches both "com.foo" and
// "com.foo.bar"). Helper for the rules layer; not directly used here
// but kept colocated with import resolution.
func MatchesWildcardPackage(path, pkg string) bool {
	if path == pkg {
		return true
	}
	return strings.HasPrefix(path, pkg+".")
}

// readFile is a tiny indirection so tests can mock if needed; for now
// it just delegates.
func readFile(path string) ([]byte, error) {
	return os.ReadFile(path)
}
