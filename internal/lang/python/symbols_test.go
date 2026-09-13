package python

import "testing"

func TestExtract_SimpleImport(t *testing.T) {
	fs, _ := ExtractSymbols([]byte("import os\nimport sys\n"))
	if len(fs.Imports) != 2 {
		t.Fatalf("imports = %d: %+v", len(fs.Imports), fs.Imports)
	}
	if fs.Imports[0].Path != "os" || fs.Imports[1].Path != "sys" {
		t.Errorf("paths: %+v", fs.Imports)
	}
}

func TestExtract_DottedImport(t *testing.T) {
	fs, _ := ExtractSymbols([]byte("import foo.bar.baz\n"))
	if len(fs.Imports) != 1 || fs.Imports[0].Path != "foo.bar.baz" {
		t.Errorf("dotted import: %+v", fs.Imports)
	}
}

func TestExtract_ImportAs(t *testing.T) {
	fs, _ := ExtractSymbols([]byte("import numpy as np\n"))
	if len(fs.Imports) != 1 {
		t.Fatalf("imports: %+v", fs.Imports)
	}
	if len(fs.Imports[0].Names) != 1 || fs.Imports[0].Names[0].Alias != "np" {
		t.Errorf("alias: %+v", fs.Imports[0])
	}
}

func TestExtract_FromImport(t *testing.T) {
	fs, _ := ExtractSymbols([]byte("from foo.bar import Baz, Qux\n"))
	if len(fs.Imports) != 1 {
		t.Fatalf("imports: %+v", fs.Imports)
	}
	imp := fs.Imports[0]
	if !imp.IsFrom || imp.Path != "foo.bar" {
		t.Errorf("from import: %+v", imp)
	}
	if len(imp.Names) != 2 {
		t.Errorf("names: %+v", imp.Names)
	}
}

func TestExtract_FromImportAs(t *testing.T) {
	fs, _ := ExtractSymbols([]byte("from foo import Bar as B\n"))
	if len(fs.Imports) != 1 {
		t.Fatal(fs.Imports)
	}
	if fs.Imports[0].Names[0].Name != "Bar" || fs.Imports[0].Names[0].Alias != "B" {
		t.Errorf("alias: %+v", fs.Imports[0].Names[0])
	}
}

func TestExtract_FromImportStar(t *testing.T) {
	fs, _ := ExtractSymbols([]byte("from foo import *\n"))
	if len(fs.Imports) != 1 || fs.Imports[0].Names[0].Name != "*" {
		t.Errorf("star: %+v", fs.Imports)
	}
}

func TestExtract_RelativeImport(t *testing.T) {
	fs, _ := ExtractSymbols([]byte("from . import sibling\nfrom ..parent import x\n"))
	if len(fs.Imports) != 2 {
		t.Fatal(fs.Imports)
	}
	if fs.Imports[0].Path != "." {
		t.Errorf("relative: %+v", fs.Imports[0])
	}
}

func TestExtract_ParenthesisedImport(t *testing.T) {
	src := []byte(`from foo import (
    Bar,
    Baz,
    Qux,
)
`)
	fs, _ := ExtractSymbols(src)
	if len(fs.Imports) != 1 || len(fs.Imports[0].Names) != 3 {
		t.Errorf("paren import: %+v", fs.Imports)
	}
}

func TestExtract_TopLevelClass(t *testing.T) {
	fs, _ := ExtractSymbols([]byte("class Foo:\n    pass\n"))
	if len(fs.Declarations) != 1 || fs.Declarations[0].Name != "Foo" || fs.Declarations[0].Kind != "class" {
		t.Errorf("class: %+v", fs.Declarations)
	}
}

func TestExtract_TopLevelFunction(t *testing.T) {
	fs, _ := ExtractSymbols([]byte("def greet(name):\n    return name\n"))
	if len(fs.Declarations) != 1 || fs.Declarations[0].Name != "greet" || fs.Declarations[0].Kind != "function" {
		t.Errorf("function: %+v", fs.Declarations)
	}
}

func TestExtract_AsyncFunction(t *testing.T) {
	fs, _ := ExtractSymbols([]byte("async def fetch(url):\n    pass\n"))
	if len(fs.Declarations) != 1 || fs.Declarations[0].Kind != "async_function" {
		t.Errorf("async fn: %+v", fs.Declarations)
	}
}

func TestExtract_ClassMembers(t *testing.T) {
	src := []byte(`class Foo:
    name = "default"
    age: int = 0

    def __init__(self):
        self.x = 1

    def greet(self):
        return self.name
`)
	fs, _ := ExtractSymbols(src)
	if len(fs.Declarations) != 1 {
		t.Fatalf("declarations: %+v", fs.Declarations)
	}
	d := fs.Declarations[0]
	if d.Name != "Foo" {
		t.Errorf("class name: %s", d.Name)
	}
	wantMembers := map[string]string{
		"name":     "variable",
		"age":      "variable",
		"__init__": "function",
		"greet":    "function",
	}
	got := map[string]string{}
	for _, m := range d.Members {
		got[m.Name] = m.Kind
	}
	for n, k := range wantMembers {
		if got[n] != k {
			t.Errorf("%s = %s, want %s (members: %+v)", n, got[n], k, d.Members)
		}
	}
}

func TestExtract_TopLevelVariable(t *testing.T) {
	fs, _ := ExtractSymbols([]byte("API_URL = 'https://example.com'\nMAX_RETRIES: int = 3\n"))
	if len(fs.Declarations) != 2 {
		t.Fatalf("declarations: %+v", fs.Declarations)
	}
	if fs.Declarations[0].Name != "API_URL" || fs.Declarations[1].Name != "MAX_RETRIES" {
		t.Errorf("vars: %+v", fs.Declarations)
	}
}

func TestExtract_DecoratorSkipped(t *testing.T) {
	src := []byte(`@dataclass
@property
class Foo:
    pass
`)
	fs, _ := ExtractSymbols(src)
	if len(fs.Declarations) != 1 || fs.Declarations[0].Name != "Foo" {
		t.Errorf("decorator-prefixed: %+v", fs.Declarations)
	}
}

func TestExtract_LocalVariableNotTopLevel(t *testing.T) {
	// `x` inside the function body must NOT appear as a top-level
	// declaration.
	src := []byte(`def foo():
    x = 1
    return x
`)
	fs, _ := ExtractSymbols(src)
	if len(fs.Declarations) != 1 || fs.Declarations[0].Name != "foo" {
		t.Errorf("expected only foo: %+v", fs.Declarations)
	}
}

func TestExtract_NameRange(t *testing.T) {
	src := []byte("class FooBar:\n    pass\n")
	fs, _ := ExtractSymbols(src)
	r := fs.Declarations[0].NameRange
	if string(src[r.Start:r.End]) != "FooBar" {
		t.Errorf("NameRange = %q", src[r.Start:r.End])
	}
}

func TestExtract_ImportPathRange(t *testing.T) {
	src := []byte("from foo.bar import X\n")
	fs, _ := ExtractSymbols(src)
	r := fs.Imports[0].PathRange
	if string(src[r.Start:r.End]) != "foo.bar" {
		t.Errorf("PathRange = %q", src[r.Start:r.End])
	}
}
