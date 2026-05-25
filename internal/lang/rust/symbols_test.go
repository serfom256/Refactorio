package rust

import "testing"

func TestExtract_ModDecl(t *testing.T) {
	fs, _ := ExtractSymbols([]byte("mod foo;\nmod bar;\n"))
	if len(fs.ModDecls) != 2 || fs.ModDecls[0].Name != "foo" || fs.ModDecls[1].Name != "bar" {
		t.Errorf("mod decls: %+v", fs.ModDecls)
	}
}

func TestExtract_SimpleUse(t *testing.T) {
	fs, _ := ExtractSymbols([]byte("use crate::foo::Bar;\n"))
	if len(fs.Uses) != 1 || fs.Uses[0].Path != "crate::foo::Bar" {
		t.Errorf("uses: %+v", fs.Uses)
	}
}

func TestExtract_GroupedUse(t *testing.T) {
	fs, _ := ExtractSymbols([]byte("use std::{io, fs};\n"))
	if len(fs.Uses) != 2 {
		t.Fatalf("uses = %d: %+v", len(fs.Uses), fs.Uses)
	}
	paths := map[string]bool{}
	for _, u := range fs.Uses {
		paths[u.Path] = true
	}
	if !paths["std::io"] || !paths["std::fs"] {
		t.Errorf("paths: %v", paths)
	}
}

func TestExtract_NestedGroupedUse(t *testing.T) {
	fs, _ := ExtractSymbols([]byte("use std::io::{self, Read, Write};\n"))
	if len(fs.Uses) != 3 {
		t.Fatalf("uses = %d: %+v", len(fs.Uses), fs.Uses)
	}
}

func TestExtract_UseAlias(t *testing.T) {
	fs, _ := ExtractSymbols([]byte("use std::io::Result as IoResult;\n"))
	if len(fs.Uses) != 1 || fs.Uses[0].Path != "std::io::Result" || fs.Uses[0].Alias != "IoResult" {
		t.Errorf("use alias: %+v", fs.Uses)
	}
}

func TestExtract_UseGlob(t *testing.T) {
	fs, _ := ExtractSymbols([]byte("use crate::foo::*;\n"))
	if len(fs.Uses) != 1 || fs.Uses[0].Path != "crate::foo::*" {
		t.Errorf("use glob: %+v", fs.Uses)
	}
}

func TestExtract_Struct(t *testing.T) {
	fs, _ := ExtractSymbols([]byte("pub struct Foo { x: i32 }\n"))
	if len(fs.Declarations) != 1 || fs.Declarations[0].Name != "Foo" || fs.Declarations[0].Kind != "struct" || !fs.Declarations[0].IsPublic {
		t.Errorf("struct: %+v", fs.Declarations)
	}
}

func TestExtract_Enum(t *testing.T) {
	fs, _ := ExtractSymbols([]byte("pub enum Color { Red, Green, Blue }\n"))
	if len(fs.Declarations) != 1 || fs.Declarations[0].Name != "Color" || fs.Declarations[0].Kind != "enum" {
		t.Errorf("enum: %+v", fs.Declarations)
	}
}

func TestExtract_Fn(t *testing.T) {
	fs, _ := ExtractSymbols([]byte("pub fn greet(name: &str) -> String { name.to_string() }\n"))
	if len(fs.Declarations) != 1 || fs.Declarations[0].Name != "greet" || fs.Declarations[0].Kind != "fn" {
		t.Errorf("fn: %+v", fs.Declarations)
	}
}

func TestExtract_Impl(t *testing.T) {
	fs, _ := ExtractSymbols([]byte("struct Foo {}\nimpl Foo { fn bar(&self) {} }\n"))
	hasStruct, hasImpl := false, false
	for _, d := range fs.Declarations {
		if d.Kind == "struct" && d.Name == "Foo" {
			hasStruct = true
		}
		if d.Kind == "impl" && d.Name == "Foo" {
			hasImpl = true
		}
	}
	if !hasStruct {
		t.Errorf("missing struct Foo: %+v", fs.Declarations)
	}
	if !hasImpl {
		t.Errorf("missing impl Foo: %+v", fs.Declarations)
	}
}

func TestExtract_Trait(t *testing.T) {
	fs, _ := ExtractSymbols([]byte("pub trait Greetable { fn greet(&self) -> String; }\n"))
	if len(fs.Declarations) != 1 || fs.Declarations[0].Name != "Greetable" {
		t.Errorf("trait: %+v", fs.Declarations)
	}
}

func TestExtract_NameRange(t *testing.T) {
	src := []byte("pub struct FooBar { x: i32 }")
	fs, _ := ExtractSymbols(src)
	r := fs.Declarations[0].NameRange
	got := string(src[r.Start:r.End])
	if got != "FooBar" {
		t.Errorf("NameRange -> %q", got)
	}
}

func TestExtract_Attribute(t *testing.T) {
	fs, _ := ExtractSymbols([]byte("#[derive(Debug)]\npub struct Foo {}\n"))
	if len(fs.Declarations) != 1 || fs.Declarations[0].Name != "Foo" {
		t.Errorf("attribute: %+v", fs.Declarations)
	}
}
