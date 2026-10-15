package cli

import (
	"strings"
	"testing"
)

func TestParse_OK(t *testing.T) {
	a, err := Parse([]string{"rename", "file=foo.go", "symbol=Old", "to=New"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if a.Subcommand != "rename" {
		t.Errorf("subcommand = %q, want rename", a.Subcommand)
	}
	if v, _ := a.Get("file"); v != "foo.go" {
		t.Errorf("file = %q, want foo.go", v)
	}
	if v, _ := a.Get("to"); v != "New" {
		t.Errorf("to = %q, want New", v)
	}
}

func TestParse_ValueMayContainEquals(t *testing.T) {
	a, err := Parse([]string{"ast-replace", "find=a == b", "replace=a.Eq(b)"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if v, _ := a.Get("find"); v != "a == b" {
		t.Errorf("find = %q", v)
	}
	if v, _ := a.Get("replace"); v != "a.Eq(b)" {
		t.Errorf("replace = %q", v)
	}
}

func TestParse_Errors(t *testing.T) {
	cases := []struct {
		name string
		argv []string
		want string
	}{
		{"empty", nil, "missing subcommand"},
		{"subcommand looks like kv", []string{"file=foo.go"}, "first argument must be a subcommand"},
		{"not kv form", []string{"rename", "file"}, "not in key=value form"},
		{"empty key", []string{"rename", "=foo"}, "not in key=value form"},
		{"duplicate", []string{"rename", "file=a", "file=b"}, "duplicate key"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Parse(tc.argv)
			if err == nil {
				t.Fatalf("expected error")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error = %q, want substring %q", err, tc.want)
			}
		})
	}
}

func TestRequire(t *testing.T) {
	a, _ := Parse([]string{"rename", "file=foo.go", "symbol="})
	if _, err := a.Require("missing"); err == nil {
		t.Errorf("expected missing error")
	}
	if _, err := a.Require("symbol"); err == nil {
		t.Errorf("expected empty-value error")
	}
	if v, err := a.Require("file"); err != nil || v != "foo.go" {
		t.Errorf("Require(file) = %q, %v", v, err)
	}
}

func TestBool(t *testing.T) {
	a, _ := Parse([]string{"rename", "dry-run=true", "weird=yes"})
	if v, err := a.Bool("dry-run", false); err != nil || v != true {
		t.Errorf("dry-run bool = %v, %v", v, err)
	}
	if v, err := a.Bool("missing", true); err != nil || v != true {
		t.Errorf("default not returned: %v, %v", v, err)
	}
	if _, err := a.Bool("weird", false); err == nil {
		t.Errorf("expected error for non-bool value")
	}
}

func TestUnknown(t *testing.T) {
	a, _ := Parse([]string{"rename", "file=a", "typo=b", "to=c"})
	u := a.Unknown("file", "to")
	if len(u) != 1 || u[0] != "typo" {
		t.Errorf("Unknown = %v, want [typo]", u)
	}
}
