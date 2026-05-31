package csharp

import "testing"

func TestExtract_FileScopedNamespace(t *testing.T) {
	src := []byte(`namespace Foo.Bar;

public class Baz { }
`)
	fs, _ := ExtractSymbols(src)
	if fs.Namespace != "Foo.Bar" || !fs.FileScoped {
		t.Errorf("namespace: %+v", fs)
	}
	if len(fs.Types) != 1 || fs.Types[0].Name != "Baz" {
		t.Errorf("types: %+v", fs.Types)
	}
}

func TestExtract_BlockScopedNamespace(t *testing.T) {
	src := []byte(`namespace Foo.Bar {
    public class Baz { }
}
`)
	fs, _ := ExtractSymbols(src)
	if fs.Namespace != "Foo.Bar" || fs.FileScoped {
		t.Errorf("namespace: %+v", fs)
	}
	if len(fs.Types) != 1 || fs.Types[0].Name != "Baz" {
		t.Errorf("types: %+v", fs.Types)
	}
}

func TestExtract_Usings(t *testing.T) {
	src := []byte(`using System;
using System.Collections.Generic;
using static System.Console;
using IO = System.IO;
global using System.Linq;

namespace X;
class C { }
`)
	fs, _ := ExtractSymbols(src)
	if len(fs.Usings) != 5 {
		t.Fatalf("usings = %d: %+v", len(fs.Usings), fs.Usings)
	}
	if fs.Usings[2].Path != "System.Console" || !fs.Usings[2].IsStatic {
		t.Errorf("static using: %+v", fs.Usings[2])
	}
	if fs.Usings[3].Alias != "IO" || fs.Usings[3].Path != "System.IO" {
		t.Errorf("alias using: %+v", fs.Usings[3])
	}
	if !fs.Usings[4].IsGlobal {
		t.Errorf("global using: %+v", fs.Usings[4])
	}
}

func TestExtract_TopLevelClass(t *testing.T) {
	src := []byte(`public class Foo {
    int x;
    public string Name { get; set; }
    public void DoIt() { }
    public Foo() { }
}
`)
	fs, _ := ExtractSymbols(src)
	if len(fs.Types) != 1 {
		t.Fatalf("types: %+v", fs.Types)
	}
	td := fs.Types[0]
	if td.Name != "Foo" || td.Kind != "class" || !td.IsPublic {
		t.Errorf("type: %+v", td)
	}
	// 1 field (x), 1 property (Name), 1 method (DoIt), 1 constructor (Foo).
	if len(td.Fields) != 1 || td.Fields[0].Name != "x" {
		t.Errorf("fields: %+v", td.Fields)
	}
	if len(td.Properties) != 1 || td.Properties[0].Name != "Name" {
		t.Errorf("properties: %+v", td.Properties)
	}
	var hasMethod, hasCtor bool
	for _, m := range td.Methods {
		if m.Name == "DoIt" && m.Kind == "method" {
			hasMethod = true
		}
		if m.Name == "Foo" && m.Kind == "constructor" {
			hasCtor = true
		}
	}
	if !hasMethod {
		t.Errorf("missing DoIt method: %+v", td.Methods)
	}
	if !hasCtor {
		t.Errorf("missing Foo constructor: %+v", td.Methods)
	}
}

func TestExtract_InternalIsDefault(t *testing.T) {
	src := []byte(`class Foo { }`) // no `public` → internal
	fs, _ := ExtractSymbols(src)
	if len(fs.Types) != 1 || fs.Types[0].IsPublic {
		t.Errorf("expected non-public Foo: %+v", fs.Types)
	}
}

func TestExtract_Interface(t *testing.T) {
	src := []byte(`public interface IFoo { void DoIt(); }`)
	fs, _ := ExtractSymbols(src)
	if len(fs.Types) != 1 || fs.Types[0].Kind != "interface" {
		t.Errorf("interface: %+v", fs.Types)
	}
}

func TestExtract_Record(t *testing.T) {
	src := []byte(`public record Point(int X, int Y);`)
	fs, _ := ExtractSymbols(src)
	if len(fs.Types) != 1 || fs.Types[0].Kind != "record" || fs.Types[0].Name != "Point" {
		t.Errorf("record: %+v", fs.Types)
	}
}

func TestExtract_Struct(t *testing.T) {
	src := []byte(`public struct Vec { public int X; }`)
	fs, _ := ExtractSymbols(src)
	if len(fs.Types) != 1 || fs.Types[0].Kind != "struct" {
		t.Errorf("struct: %+v", fs.Types)
	}
}

func TestExtract_Enum(t *testing.T) {
	src := []byte(`public enum Color { Red, Green, Blue }`)
	fs, _ := ExtractSymbols(src)
	if len(fs.Types) != 1 || fs.Types[0].Kind != "enum" {
		t.Errorf("enum: %+v", fs.Types)
	}
}

func TestExtract_AttributeSkipped(t *testing.T) {
	src := []byte(`[Serializable]
[Obsolete("use Bar instead")]
public class Foo { }
`)
	fs, _ := ExtractSymbols(src)
	if len(fs.Types) != 1 || fs.Types[0].Name != "Foo" {
		t.Errorf("attribute-prefixed type: %+v", fs.Types)
	}
}

func TestExtract_MultipleFieldDeclarators(t *testing.T) {
	src := []byte(`class Foo { int a, b, c; }`)
	fs, _ := ExtractSymbols(src)
	if len(fs.Types) != 1 {
		t.Fatal(fs.Types)
	}
	names := map[string]bool{}
	for _, f := range fs.Types[0].Fields {
		names[f.Name] = true
	}
	if !names["a"] || !names["b"] || !names["c"] {
		t.Errorf("multi-field: %+v", fs.Types[0].Fields)
	}
}

func TestExtract_NameRange(t *testing.T) {
	src := []byte(`public class FooBar { }`)
	fs, _ := ExtractSymbols(src)
	r := fs.Types[0].NameRange
	if string(src[r.Start:r.End]) != "FooBar" {
		t.Errorf("NameRange points at %q", src[r.Start:r.End])
	}
}

func TestExtract_UsingPathRange(t *testing.T) {
	src := []byte(`using System.Linq;`)
	fs, _ := ExtractSymbols(src)
	r := fs.Usings[0].PathRange
	if string(src[r.Start:r.End]) != "System.Linq" {
		t.Errorf("PathRange points at %q", src[r.Start:r.End])
	}
}
