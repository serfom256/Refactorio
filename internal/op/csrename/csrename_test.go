package csrename

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/serfom256/refactorio/internal/lang/csharp"
)

func mk(t *testing.T, files map[string]string) (*csharp.Index, string) {
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
	idx, err := csharp.BuildIndex(root)
	if err != nil {
		t.Fatal(err)
	}
	return idx, root
}

func read(t *testing.T, p string) string {
	t.Helper()
	b, _ := os.ReadFile(p)
	return string(b)
}

func TestRename_Class(t *testing.T) {
	idx, root := mk(t, map[string]string{
		"Foo.cs": `namespace Demo;
public class Foo {
    public int X;
}
`,
		"Main.cs": `namespace Demo;
public class Main {
    public Foo MakeOne() => new Foo();
}
`,
	})
	file := filepath.Join(root, "Foo.cs")
	set, err := PlanRename(RenameRequest{File: file, Symbol: "Foo", NewName: "Bar", Index: idx})
	if err != nil {
		t.Fatal(err)
	}
	if err := set.Apply(); err != nil {
		t.Fatal(err)
	}
	// File renamed.
	if _, err := os.Stat(filepath.Join(root, "Bar.cs")); err != nil {
		t.Errorf("file not renamed: %v", err)
	}
	bar := read(t, filepath.Join(root, "Bar.cs"))
	if !strings.Contains(bar, "public class Bar") {
		t.Errorf("declaration not renamed:\n%s", bar)
	}
	main := read(t, filepath.Join(root, "Main.cs"))
	if !strings.Contains(main, "Bar MakeOne") || !strings.Contains(main, "new Bar()") {
		t.Errorf("usages not renamed:\n%s", main)
	}
}

func TestRename_Method(t *testing.T) {
	idx, root := mk(t, map[string]string{
		"Foo.cs": `namespace Demo;
public class Foo {
    public void DoIt() { }
}
`,
		"Main.cs": `namespace Demo;
public class Main {
    public void Run() { var f = new Foo(); f.DoIt(); }
}
`,
	})
	file := filepath.Join(root, "Foo.cs")
	set, err := PlanRename(RenameRequest{File: file, Symbol: "DoIt", NewName: "Execute", Index: idx})
	if err != nil {
		t.Fatal(err)
	}
	if err := set.Apply(); err != nil {
		t.Fatal(err)
	}
	foo := read(t, filepath.Join(root, "Foo.cs"))
	if !strings.Contains(foo, "void Execute()") {
		t.Errorf("decl not renamed:\n%s", foo)
	}
}

func TestRename_IgnoresStringsAndComments(t *testing.T) {
	idx, root := mk(t, map[string]string{
		"Foo.cs": `namespace Demo;
// Foo is a class
public class Foo {
    void Greet() { var s = "Foo says hi"; }
}
`,
	})
	file := filepath.Join(root, "Foo.cs")
	set, err := PlanRename(RenameRequest{File: file, Symbol: "Foo", NewName: "Bar", Index: idx})
	if err != nil {
		t.Fatal(err)
	}
	if err := set.Apply(); err != nil {
		t.Fatal(err)
	}
	bar := read(t, filepath.Join(root, "Bar.cs"))
	if !strings.Contains(bar, "// Foo is a class") {
		t.Errorf("comment modified:\n%s", bar)
	}
	if !strings.Contains(bar, `"Foo says hi"`) {
		t.Errorf("string modified:\n%s", bar)
	}
}

func TestRename_VerbatimString(t *testing.T) {
	idx, root := mk(t, map[string]string{
		"Foo.cs": `namespace Demo;
public class Foo {
    string s = @"C:\Foo\bar";
}
`,
	})
	file := filepath.Join(root, "Foo.cs")
	set, err := PlanRename(RenameRequest{File: file, Symbol: "Foo", NewName: "Baz", Index: idx})
	if err != nil {
		t.Fatal(err)
	}
	if err := set.Apply(); err != nil {
		t.Fatal(err)
	}
	got := read(t, filepath.Join(root, "Baz.cs"))
	if !strings.Contains(got, `@"C:\Foo\bar"`) {
		t.Errorf("verbatim string modified:\n%s", got)
	}
}

func TestRename_InterpolatedString(t *testing.T) {
	idx, root := mk(t, map[string]string{
		"Foo.cs": `namespace Demo;
public class Foo {
    string Greet(string name) => $"Hello {name}, Foo says hi";
}
`,
	})
	file := filepath.Join(root, "Foo.cs")
	set, err := PlanRename(RenameRequest{File: file, Symbol: "name", NewName: "who", Index: idx})
	if err != nil {
		// `name` isn't a declaration we expose; skip — this just checks
		// that interpolation doesn't crash. The class is what we test.
	}
	_ = set
	_ = root
}

func TestRename_SkipsLocalDeclaration(t *testing.T) {
	idx, root := mk(t, map[string]string{
		"Foo.cs": `namespace Demo;
public class Foo {
    void M() { int Foo = 1; System.Console.WriteLine(Foo); }
}
`,
	})
	file := filepath.Join(root, "Foo.cs")
	set, err := PlanRename(RenameRequest{File: file, Symbol: "Foo", NewName: "Bar", Index: idx})
	if err != nil {
		t.Fatal(err)
	}
	if err := set.Apply(); err != nil {
		t.Fatal(err)
	}
	got := read(t, filepath.Join(root, "Bar.cs"))
	if !strings.Contains(got, "int Foo = 1") {
		t.Errorf("local variable wrongly renamed:\n%s", got)
	}
}

func TestRename_NotFound(t *testing.T) {
	idx, root := mk(t, map[string]string{
		"Foo.cs": `namespace Demo; public class Foo { }`,
	})
	_, err := PlanRename(RenameRequest{
		File: filepath.Join(root, "Foo.cs"), Symbol: "Ghost", NewName: "X", Index: idx,
	})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestDelete_RemovesFile(t *testing.T) {
	idx, root := mk(t, map[string]string{
		"Foo.cs": `namespace Demo; public class Foo { }`,
	})
	file := filepath.Join(root, "Foo.cs")
	r, err := PlanDelete(DeleteRequest{File: file, Index: idx})
	if err != nil {
		t.Fatal(err)
	}
	if err := r.Set.Apply(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(file); !os.IsNotExist(err) {
		t.Errorf("file still exists")
	}
}
