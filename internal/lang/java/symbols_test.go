package java

import (
	"reflect"
	"sort"
	"strings"
	"testing"
)

func TestExtractSymbols_PackageAndImports(t *testing.T) {
	src := []byte(`package com.foo.bar;

import java.util.List;
import java.util.*;
import static java.util.Arrays.asList;
import static java.util.Collections.*;

public class Demo {}
`)
	fs, err := ExtractSymbols(src)
	if err != nil {
		t.Fatal(err)
	}
	if fs.Package != "com.foo.bar" {
		t.Errorf("Package = %q, want com.foo.bar", fs.Package)
	}
	if len(fs.Imports) != 4 {
		t.Fatalf("Imports = %d, want 4: %+v", len(fs.Imports), fs.Imports)
	}
	want := []ImportSpec{
		{Path: "java.util.List"},
		{Path: "java.util", IsWildcard: true},
		{Path: "java.util.Arrays.asList", IsStatic: true},
		{Path: "java.util.Collections", IsStatic: true, IsWildcard: true},
	}
	for i, w := range want {
		got := fs.Imports[i]
		if got.Path != w.Path || got.IsWildcard != w.IsWildcard || got.IsStatic != w.IsStatic {
			t.Errorf("Imports[%d] = %+v, want %+v", i, got, w)
		}
	}
}

func TestExtractSymbols_DefaultPackage(t *testing.T) {
	src := []byte(`public class Demo {}
`)
	fs, err := ExtractSymbols(src)
	if err != nil {
		t.Fatal(err)
	}
	if fs.Package != "" {
		t.Errorf("expected empty package, got %q", fs.Package)
	}
}

func TestExtractSymbols_TypesAndMembers(t *testing.T) {
	src := []byte(`package com.foo;

public class Demo {
    int a, b = 1, c;
    private String s = "x";

    public Demo() {}
    public Demo(int x) {}

    public int fizzBuzz() { return 1; }
    void fizzBuzz(String s) {}

    static class Inner {
        int innerField;
        void innerMethod() {}
    }

    interface Listener { void onChange(); }
}
`)
	fs, err := ExtractSymbols(src)
	if err != nil {
		t.Fatal(err)
	}
	if len(fs.Types) != 1 {
		t.Fatalf("top-level types = %d, want 1", len(fs.Types))
	}
	demo := fs.Types[0]
	if demo.Name != "Demo" || demo.Kind != "class" {
		t.Errorf("Demo: name=%q kind=%q", demo.Name, demo.Kind)
	}

	// Fields: a, b, c, s
	gotFields := memberNames(demo.Fields)
	wantFields := []string{"a", "b", "c", "s"}
	sort.Strings(gotFields)
	sort.Strings(wantFields)
	if !reflect.DeepEqual(gotFields, wantFields) {
		t.Errorf("fields = %v, want %v", gotFields, wantFields)
	}

	// Methods + constructors. Two ctors + two fizzBuzz methods.
	gotMethods := memberNames(demo.Methods)
	sort.Strings(gotMethods)
	wantMethods := []string{"Demo", "Demo", "fizzBuzz", "fizzBuzz"}
	sort.Strings(wantMethods)
	if !reflect.DeepEqual(gotMethods, wantMethods) {
		t.Errorf("methods = %v, want %v", gotMethods, wantMethods)
	}
	// Verify constructor vs method kinds.
	ctorCount, methCount := 0, 0
	for _, m := range demo.Methods {
		switch m.Kind {
		case "constructor":
			ctorCount++
		case "method":
			methCount++
		default:
			t.Errorf("unexpected member kind %q", m.Kind)
		}
	}
	if ctorCount != 2 || methCount != 2 {
		t.Errorf("constructors=%d methods=%d, want 2,2", ctorCount, methCount)
	}

	// Nested types: Inner (class) + Listener (interface)
	if len(demo.Nested) != 2 {
		t.Fatalf("nested = %d, want 2", len(demo.Nested))
	}
	gotNested := map[string]string{}
	for _, n := range demo.Nested {
		gotNested[n.Name] = n.Kind
	}
	if gotNested["Inner"] != "class" || gotNested["Listener"] != "interface" {
		t.Errorf("nested kinds wrong: %v", gotNested)
	}
}

func TestExtractSymbols_Enum(t *testing.T) {
	src := []byte(`package com.foo;

public enum Color {
    RED, GREEN, BLUE;

    int rank;
    public String label() { return name(); }
}
`)
	fs, err := ExtractSymbols(src)
	if err != nil {
		t.Fatal(err)
	}
	if len(fs.Types) != 1 {
		t.Fatalf("types = %d", len(fs.Types))
	}
	c := fs.Types[0]
	if c.Kind != "enum" {
		t.Errorf("Kind = %q, want enum", c.Kind)
	}
	// We descend through enum_body_declarations, so the method+field
	// should be picked up. (Enum constants RED/GREEN/BLUE are NOT
	// reported as fields — they have their own node kind. That is fine
	// for v1; we will revisit if a rename needs to touch them.)
	if len(c.Methods) != 1 || c.Methods[0].Name != "label" {
		t.Errorf("methods = %v", c.Methods)
	}
	if len(c.Fields) != 1 || c.Fields[0].Name != "rank" {
		t.Errorf("fields = %v", c.Fields)
	}
}

func TestExtractSymbols_NameRangePointsAtIdentifier(t *testing.T) {
	// NameRange must cover just the identifier, not the whole decl.
	// This is what the rename op will use to splice the new name in.
	src := []byte(`class Foo {}
`)
	fs, _ := ExtractSymbols(src)
	r := fs.Types[0].NameRange
	got := string(src[r.Start:r.End])
	if got != "Foo" {
		t.Errorf("NameRange points at %q, want Foo", got)
	}
	if r.Line != 1 {
		t.Errorf("Line = %d, want 1", r.Line)
	}
}

func TestExtractSymbols_MultiLineLineNumbers(t *testing.T) {
	src := []byte(`package com.foo;

class A {}

class B {
    int field;
}
`)
	fs, _ := ExtractSymbols(src)
	if fs.PackageRange.Line != 1 {
		t.Errorf("package line = %d", fs.PackageRange.Line)
	}
	if fs.Types[0].NameRange.Line != 3 {
		t.Errorf("A line = %d, want 3", fs.Types[0].NameRange.Line)
	}
	if fs.Types[1].NameRange.Line != 5 {
		t.Errorf("B line = %d, want 5", fs.Types[1].NameRange.Line)
	}
	if fs.Types[1].Fields[0].NameRange.Line != 6 {
		t.Errorf("field line = %d, want 6", fs.Types[1].Fields[0].NameRange.Line)
	}
}

func TestExtractSymbols_TolerantToParseErrors(t *testing.T) {
	// Missing semicolon — still parseable enough to extract.
	src := []byte(`package com.foo

class Demo {
    int x
}
`)
	fs, err := ExtractSymbols(src)
	if err != nil {
		t.Fatal(err)
	}
	if len(fs.Types) != 1 {
		t.Errorf("got %d types from malformed source, want 1", len(fs.Types))
	}
	if !strings.Contains(fs.Package, "com.foo") {
		t.Errorf("package = %q", fs.Package)
	}
}

func memberNames(ms []MemberDecl) []string {
	out := make([]string, len(ms))
	for i, m := range ms {
		out[i] = m.Name
	}
	return out
}
