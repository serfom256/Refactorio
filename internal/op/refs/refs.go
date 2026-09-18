// Package refs answers the one question every delete implementation shares:
// once the imports naming a deleted file have been removed, does a dependent
// file still refer to something that no longer exists?
//
// Getting this wrong is what made the old warning useless. Reporting a file
// simply because its import was removed made the warning true for every file
// the delete had already cleaned, and scanning for the symbol name alone
// counted the identifier inside the import statement being removed. Matching
// on the original name also missed aliased imports, where the name left
// dangling in code is the local alias rather than the name that was imported.
//
// The check is name-based, not type-aware, so it stays a heuristic: a local
// variable that happens to share a name with a deleted symbol still counts.
// It errs toward reporting, since a missed dangling reference is a broken
// build the user was not warned about.
package refs

// Span is a half-open byte range [Start, End).
type Span struct{ Start, End int }

// Ident is one identifier occurrence in a source file.
type Ident struct {
	Name  string
	Start int
}

// Dangling reports whether any name in bound still appears as an identifier
// outside every span in removed.
//
// bound holds the local names that the removed imports introduced — the alias
// where there is one, because that is the name the code actually uses. Names
// in stillBound are re-introduced by an import that survives (Python's
// `import foo.bar` and `import foo.baz` both bind foo, so removing one leaves
// the name live), and so are never dangling. Blank and underscore names are
// ignored: Go's `_` side-effect imports and dot imports bind nothing that can
// be traced back to a call site.
func Dangling(idents []Ident, bound, stillBound []string, removed []Span) bool {
	want := make(map[string]bool, len(bound))
	for _, n := range bound {
		if n != "" && n != "_" {
			want[n] = true
		}
	}
	for _, n := range stillBound {
		delete(want, n)
	}
	if len(want) == 0 {
		return false
	}
	for _, id := range idents {
		if want[id.Name] && !within(removed, id.Start) {
			return true
		}
	}
	return false
}

func within(spans []Span, off int) bool {
	for _, s := range spans {
		if off >= s.Start && off < s.End {
			return true
		}
	}
	return false
}
