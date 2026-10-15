package refs

import "testing"

func TestDangling(t *testing.T) {
	imp := []Span{{Start: 0, End: 20}}

	tests := []struct {
		name       string
		idents     []Ident
		bound      []string
		stillBound []string
		removed    []Span
		want       bool
	}{
		{
			name:    "only occurrence is inside the removed import",
			idents:  []Ident{{Name: "Bar", Start: 7}},
			bound:   []string{"Bar"},
			removed: imp,
			want:    false,
		},
		{
			name:    "used in code outside the import",
			idents:  []Ident{{Name: "Bar", Start: 7}, {Name: "Bar", Start: 30}},
			bound:   []string{"Bar"},
			removed: imp,
			want:    true,
		},
		{
			name:    "alias is what dangles, not the original name",
			idents:  []Ident{{Name: "F", Start: 30}},
			bound:   []string{"F"},
			removed: imp,
			want:    true,
		},
		{
			name:    "original name in code is irrelevant once aliased",
			idents:  []Ident{{Name: "Foo", Start: 30}},
			bound:   []string{"F"},
			removed: imp,
			want:    false,
		},
		{
			name:       "name still bound by a surviving import",
			idents:     []Ident{{Name: "foo", Start: 30}},
			bound:      []string{"foo"},
			stillBound: []string{"foo"},
			removed:    imp,
			want:       false,
		},
		{
			name:    "blank and underscore bind nothing",
			idents:  []Ident{{Name: "_", Start: 30}},
			bound:   []string{"", "_"},
			removed: imp,
			want:    false,
		},
		{
			name:    "no bindings at all",
			idents:  []Ident{{Name: "Bar", Start: 30}},
			bound:   nil,
			removed: imp,
			want:    false,
		},
		{
			name:    "boundary: offset at End is outside the span",
			idents:  []Ident{{Name: "Bar", Start: 20}},
			bound:   []string{"Bar"},
			removed: imp,
			want:    true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := Dangling(tc.idents, tc.bound, tc.stillBound, tc.removed)
			if got != tc.want {
				t.Errorf("Dangling() = %v, want %v", got, tc.want)
			}
		})
	}
}
