package java

import "testing"

func TestIsValidIdentifier(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{"", false},
		{"foo", true},
		{"Foo", true},
		{"_foo", true},
		{"$foo", true},
		{"foo$bar", true},
		{"foo_bar", true},
		{"foo123", true},
		{"123foo", false},
		{"foo-bar", false},
		{"foo bar", false},
		{"αβγ", true},
		{"foo.bar", false},
	}
	for _, tc := range cases {
		if got := IsValidIdentifier(tc.in); got != tc.want {
			t.Errorf("IsValidIdentifier(%q) = %v, want %v", tc.in, got, tc.want)
		}
	}
}
