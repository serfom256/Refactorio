package golang

import "testing"

func TestExtract_Package(t *testing.T) {
	fs, _ := ExtractSymbols([]byte("package main\n"))
	if fs.Package != "main" {
		t.Errorf("Package = %q", fs.Package)
	}
}

func TestExtract_SingleImport(t *testing.T) {
	fs, _ := ExtractSymbols([]byte(`package main
import "fmt"
`))
	if len(fs.Imports) != 1 || fs.Imports[0].Path != "fmt" {
		t.Errorf("imports: %+v", fs.Imports)
	}
}

func TestExtract_GroupedImports(t *testing.T) {
	fs, _ := ExtractSymbols([]byte(`package main
import (
	"fmt"
	"os"
	f "flag"
	. "strings"
	_ "net/http/pprof"
)
`))
	if len(fs.Imports) != 5 {
		t.Fatalf("imports = %d: %+v", len(fs.Imports), fs.Imports)
	}
	if fs.Imports[0].Path != "fmt" || fs.Imports[0].Alias != "" {
		t.Errorf("[0] = %+v", fs.Imports[0])
	}
	if fs.Imports[2].Path != "flag" || fs.Imports[2].Alias != "f" {
		t.Errorf("[2] = %+v", fs.Imports[2])
	}
	if fs.Imports[3].Path != "strings" || fs.Imports[3].Alias != "." {
		t.Errorf("[3] = %+v", fs.Imports[3])
	}
	if fs.Imports[4].Path != "net/http/pprof" || fs.Imports[4].Alias != "_" {
		t.Errorf("[4] = %+v", fs.Imports[4])
	}
	if fs.Imports[2].PackageName() != "f" {
		t.Errorf("PackageName for aliased = %q", fs.Imports[2].PackageName())
	}
	if fs.Imports[0].PackageName() != "fmt" {
		t.Errorf("PackageName for default = %q", fs.Imports[0].PackageName())
	}
}

func TestExtract_FuncDeclaration(t *testing.T) {
	fs, _ := ExtractSymbols([]byte(`package main
func Hello() string { return "hi" }
func helper() {}
`))
	if len(fs.Declarations) != 2 {
		t.Fatalf("decls = %d", len(fs.Declarations))
	}
	if fs.Declarations[0].Name != "Hello" || fs.Declarations[0].Kind != "func" || !fs.Declarations[0].IsExport {
		t.Errorf("[0] = %+v", fs.Declarations[0])
	}
	if fs.Declarations[1].Name != "helper" || fs.Declarations[1].IsExport {
		t.Errorf("[1] = %+v", fs.Declarations[1])
	}
}

func TestExtract_Method(t *testing.T) {
	fs, _ := ExtractSymbols([]byte(`package main
type T struct{}
func (t *T) Foo() int { return 0 }
func (t T) bar() {}
`))
	var methods []Declaration
	for _, d := range fs.Declarations {
		if d.Kind == "method" {
			methods = append(methods, d)
		}
	}
	if len(methods) != 2 {
		t.Fatalf("methods = %d", len(methods))
	}
	if methods[0].Name != "Foo" || methods[0].Receiver != "T" {
		t.Errorf("[0] = %+v", methods[0])
	}
	if methods[1].Name != "bar" || methods[1].Receiver != "T" {
		t.Errorf("[1] = %+v", methods[1])
	}
}

func TestExtract_TypeDeclaration(t *testing.T) {
	fs, _ := ExtractSymbols([]byte(`package main
type Foo struct { X int }
type Bar interface { Do() }
type ID = string
`))
	var types []Declaration
	for _, d := range fs.Declarations {
		if d.Kind == "type" {
			types = append(types, d)
		}
	}
	if len(types) != 3 {
		t.Fatalf("types = %d", len(types))
	}
	if types[0].Name != "Foo" || !types[0].IsExport {
		t.Errorf("[0] = %+v", types[0])
	}
	if types[2].Name != "ID" {
		t.Errorf("[2] = %+v", types[2])
	}
}

func TestExtract_GroupedTypeDecl(t *testing.T) {
	fs, _ := ExtractSymbols([]byte(`package main
type (
	Foo struct{}
	Bar int
)
`))
	var types []Declaration
	for _, d := range fs.Declarations {
		if d.Kind == "type" {
			types = append(types, d)
		}
	}
	if len(types) != 2 || types[0].Name != "Foo" || types[1].Name != "Bar" {
		t.Errorf("grouped types: %+v", types)
	}
}

func TestExtract_VarConst(t *testing.T) {
	fs, _ := ExtractSymbols([]byte(`package main
var X = 1
const Y = "hello"
var (
	A int
	B string
)
`))
	names := map[string]string{}
	for _, d := range fs.Declarations {
		names[d.Name] = d.Kind
	}
	if names["X"] != "var" || names["Y"] != "const" || names["A"] != "var" || names["B"] != "var" {
		t.Errorf("var/const: %v", names)
	}
}

func TestExtract_NameRange(t *testing.T) {
	src := []byte("package main\nfunc Hello() {}\n")
	fs, _ := ExtractSymbols(src)
	r := fs.Declarations[0].NameRange
	got := string(src[r.Start:r.End])
	if got != "Hello" {
		t.Errorf("NameRange -> %q, want Hello", got)
	}
}

func TestExtract_ImportPathRange(t *testing.T) {
	src := []byte(`package main
import "fmt"
`)
	fs, _ := ExtractSymbols(src)
	r := fs.Imports[0].PathRange
	got := string(src[r.Start:r.End])
	if got != "fmt" {
		t.Errorf("PathRange -> %q, want fmt", got)
	}
}
