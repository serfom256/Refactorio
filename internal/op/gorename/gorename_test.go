package gorename

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/serfom256/refactorio/internal/lang/golang"
)

func mk(t *testing.T, files map[string]string) (*golang.Index, string) {
	t.Helper()
	root := t.TempDir()
	for rel, body := range files {
		full := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	idx, err := golang.BuildIndex(root)
	if err != nil {
		t.Fatal(err)
	}
	return idx, root
}

func read(t *testing.T, p string) string {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestGoRename_ExportedFunc(t *testing.T) {
	idx, root := mk(t, map[string]string{
		"go.mod": "module example.com/proj\ngo 1.21\n",
		"pkg/foo.go": `package pkg

func Hello() string { return "hi" }

func caller() string { return Hello() }
`,
		"main.go": `package main

import "example.com/proj/pkg"

func main() { println(pkg.Hello()) }
`,
	})
	set, err := PlanRename(RenameRequest{
		Package: "example.com/proj/pkg", Symbol: "Hello", NewName: "Greet", Index: idx,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := set.Apply(); err != nil {
		t.Fatal(err)
	}
	foo := read(t, filepath.Join(root, "pkg/foo.go"))
	if !strings.Contains(foo, "func Greet()") {
		t.Errorf("declaration not renamed:\n%s", foo)
	}
	if !strings.Contains(foo, "return Greet()") {
		t.Errorf("same-package usage not renamed:\n%s", foo)
	}
	main := read(t, filepath.Join(root, "main.go"))
	if !strings.Contains(main, "pkg.Greet()") {
		t.Errorf("qualified usage not renamed:\n%s", main)
	}
}

func TestGoRename_ExportedType(t *testing.T) {
	idx, root := mk(t, map[string]string{
		"go.mod": "module example.com/proj\ngo 1.21\n",
		"pkg/types.go": `package pkg

type Foo struct{ X int }
`,
		"main.go": `package main

import "example.com/proj/pkg"

func main() {
	f := pkg.Foo{X: 1}
	_ = f
}
`,
	})
	set, err := PlanRename(RenameRequest{
		Package: "example.com/proj/pkg", Symbol: "Foo", NewName: "Bar", Index: idx,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := set.Apply(); err != nil {
		t.Fatal(err)
	}
	types := read(t, filepath.Join(root, "pkg/types.go"))
	if !strings.Contains(types, "type Bar struct") {
		t.Errorf("type not renamed:\n%s", types)
	}
	main := read(t, filepath.Join(root, "main.go"))
	if !strings.Contains(main, "pkg.Bar{") {
		t.Errorf("qualified type not renamed:\n%s", main)
	}
}

func TestGoRename_AliasedImport(t *testing.T) {
	idx, root := mk(t, map[string]string{
		"go.mod": "module example.com/proj\ngo 1.21\n",
		"pkg/foo.go": `package pkg

func Hello() string { return "hi" }
`,
		"main.go": `package main

import p "example.com/proj/pkg"

func main() { println(p.Hello()) }
`,
	})
	set, err := PlanRename(RenameRequest{
		Package: "example.com/proj/pkg", Symbol: "Hello", NewName: "Greet", Index: idx,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := set.Apply(); err != nil {
		t.Fatal(err)
	}
	main := read(t, filepath.Join(root, "main.go"))
	if !strings.Contains(main, "p.Greet()") {
		t.Errorf("aliased qualified usage not renamed:\n%s", main)
	}
}

func TestGoRename_DotImport(t *testing.T) {
	idx, root := mk(t, map[string]string{
		"go.mod": "module example.com/proj\ngo 1.21\n",
		"pkg/foo.go": `package pkg

func Hello() string { return "hi" }
`,
		"main.go": `package main

import . "example.com/proj/pkg"

func main() { println(Hello()) }
`,
	})
	set, err := PlanRename(RenameRequest{
		Package: "example.com/proj/pkg", Symbol: "Hello", NewName: "Greet", Index: idx,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := set.Apply(); err != nil {
		t.Fatal(err)
	}
	main := read(t, filepath.Join(root, "main.go"))
	if !strings.Contains(main, "Greet()") {
		t.Errorf("dot-import usage not renamed:\n%s", main)
	}
}

func TestGoRename_IgnoresStringsAndComments(t *testing.T) {
	idx, root := mk(t, map[string]string{
		"go.mod": "module example.com/proj\ngo 1.21\n",
		"pkg/foo.go": `package pkg

// Hello is great
func Hello() string { return "Hello world" }
`,
	})
	set, err := PlanRename(RenameRequest{
		Package: "example.com/proj/pkg", Symbol: "Hello", NewName: "Greet", Index: idx,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := set.Apply(); err != nil {
		t.Fatal(err)
	}
	foo := read(t, filepath.Join(root, "pkg/foo.go"))
	if !strings.Contains(foo, "// Hello is great") {
		t.Errorf("comment was modified:\n%s", foo)
	}
	if !strings.Contains(foo, `"Hello world"`) {
		t.Errorf("string was modified:\n%s", foo)
	}
	if !strings.Contains(foo, "func Greet()") {
		t.Errorf("declaration not renamed:\n%s", foo)
	}
}

func TestGoRename_SymbolNotFound(t *testing.T) {
	idx, _ := mk(t, map[string]string{
		"go.mod":     "module example.com/proj\ngo 1.21\n",
		"pkg/foo.go": "package pkg\nfunc Hello() {}\n",
	})
	_, err := PlanRename(RenameRequest{
		Package: "example.com/proj/pkg", Symbol: "Ghost", NewName: "X", Index: idx,
	})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestGoMove_UpdatesImportPaths(t *testing.T) {
	idx, root := mk(t, map[string]string{
		"go.mod": "module example.com/proj\ngo 1.21\n",
		"pkg/foo.go": `package pkg

func Hello() string { return "hi" }
`,
		"main.go": `package main

import "example.com/proj/pkg"

func main() { println(pkg.Hello()) }
`,
	})
	set, err := PlanMove(MoveRequest{
		File:    filepath.Join(root, "pkg/foo.go"),
		NewPath: filepath.Join(root, "lib/foo.go"),
		Index:   idx,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := set.Apply(); err != nil {
		t.Fatal(err)
	}
	main := read(t, filepath.Join(root, "main.go"))
	if !strings.Contains(main, `"example.com/proj/lib"`) {
		t.Errorf("import path not updated:\n%s", main)
	}
	foo := read(t, filepath.Join(root, "lib/foo.go"))
	if !strings.Contains(foo, "package lib") {
		t.Errorf("package decl not updated:\n%s", foo)
	}
}

func TestGoMove_SamePackage(t *testing.T) {
	idx, root := mk(t, map[string]string{
		"go.mod":     "module example.com/proj\ngo 1.21\n",
		"pkg/foo.go": "package pkg\nfunc Hello() {}\n",
	})
	set, err := PlanMove(MoveRequest{
		File:    filepath.Join(root, "pkg/foo.go"),
		NewPath: filepath.Join(root, "pkg/bar.go"),
		Index:   idx,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := set.Apply(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "pkg/bar.go")); err != nil {
		t.Errorf("new file missing: %v", err)
	}
}
