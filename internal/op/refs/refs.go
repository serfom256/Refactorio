package refs

type Span struct{ Start, End int }

type Ident struct {
	Name  string
	Start int
}

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
