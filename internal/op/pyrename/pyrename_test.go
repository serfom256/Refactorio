package pyrename

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/serfom256/refactorio/internal/lang/python"
)

func mk(t *testing.T, files map[string]string) (*python.Index, string) {
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
	idx, err := python.BuildIndex(root)
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

func TestRename_TopLevelClass(t *testing.T) {
	idx, root := mk(t, map[string]string{
		"foo.py": `class Foo:
    def greet(self):
        return "hi"

def make_one():
    return Foo()
`,
		"main.py": `from foo import Foo

f = Foo()
`,
	})
	file := filepath.Join(root, "foo.py")
	set, err := PlanRename(RenameRequest{File: file, Symbol: "Foo", NewName: "Bar", Index: idx})
	if err != nil {
		t.Fatal(err)
	}
	if err := set.Apply(); err != nil {
		t.Fatal(err)
	}
	foo := read(t, filepath.Join(root, "foo.py"))
	if !strings.Contains(foo, "class Bar:") {
		t.Errorf("decl not renamed:\n%s", foo)
	}
	if !strings.Contains(foo, "return Bar()") {
		t.Errorf("self-reference not renamed:\n%s", foo)
	}
	main := read(t, filepath.Join(root, "main.py"))
	if !strings.Contains(main, "from foo import Bar") {
		t.Errorf("from-import not renamed:\n%s", main)
	}
	if !strings.Contains(main, "f = Bar()") {
		t.Errorf("usage not renamed:\n%s", main)
	}
}

func TestRename_TopLevelFunction(t *testing.T) {
	idx, root := mk(t, map[string]string{
		"utils.py": `def greet(name):
    return f"Hello {name}"
`,
		"main.py": `from utils import greet

print(greet("world"))
`,
	})
	file := filepath.Join(root, "utils.py")
	set, err := PlanRename(RenameRequest{File: file, Symbol: "greet", NewName: "say_hello", Index: idx})
	if err != nil {
		t.Fatal(err)
	}
	if err := set.Apply(); err != nil {
		t.Fatal(err)
	}
	utils := read(t, filepath.Join(root, "utils.py"))
	if !strings.Contains(utils, "def say_hello(") {
		t.Errorf("decl not renamed:\n%s", utils)
	}
	main := read(t, filepath.Join(root, "main.py"))
	if !strings.Contains(main, "from utils import say_hello") {
		t.Errorf("import binding not renamed:\n%s", main)
	}
	if !strings.Contains(main, `say_hello("world")`) {
		t.Errorf("usage not renamed:\n%s", main)
	}
}

func TestRename_ModuleQualifiedUsage(t *testing.T) {
	idx, root := mk(t, map[string]string{
		"foo.py": `class Bar:
    pass
`,
		"main.py": `import foo

x = foo.Bar()
`,
	})
	file := filepath.Join(root, "foo.py")
	set, err := PlanRename(RenameRequest{File: file, Symbol: "Bar", NewName: "Baz", Index: idx})
	if err != nil {
		t.Fatal(err)
	}
	if err := set.Apply(); err != nil {
		t.Fatal(err)
	}
	main := read(t, filepath.Join(root, "main.py"))
	if !strings.Contains(main, "foo.Baz()") {
		t.Errorf("module-qualified usage not renamed:\n%s", main)
	}
}

func TestRename_AliasedImport(t *testing.T) {
	idx, root := mk(t, map[string]string{
		"foo.py": `class Bar:
    pass
`,
		"main.py": `from foo import Bar as B

x = B()
`,
	})
	file := filepath.Join(root, "foo.py")
	set, err := PlanRename(RenameRequest{File: file, Symbol: "Bar", NewName: "Baz", Index: idx})
	if err != nil {
		t.Fatal(err)
	}
	if err := set.Apply(); err != nil {
		t.Fatal(err)
	}
	main := read(t, filepath.Join(root, "main.py"))
	if !strings.Contains(main, "Baz as B") {
		t.Errorf("import specifier not renamed:\n%s", main)
	}
	if !strings.Contains(main, "x = B()") {
		t.Errorf("alias usage should NOT be renamed:\n%s", main)
	}
}

func TestRename_IgnoresStringsAndComments(t *testing.T) {
	idx, root := mk(t, map[string]string{
		"foo.py": `# Foo is great
class Foo:
    msg = "Foo says hi"
`,
	})
	file := filepath.Join(root, "foo.py")
	set, err := PlanRename(RenameRequest{File: file, Symbol: "Foo", NewName: "Bar", Index: idx})
	if err != nil {
		t.Fatal(err)
	}
	if err := set.Apply(); err != nil {
		t.Fatal(err)
	}
	got := read(t, filepath.Join(root, "foo.py"))
	if !strings.Contains(got, "# Foo is great") {
		t.Errorf("comment modified:\n%s", got)
	}
	if !strings.Contains(got, `"Foo says hi"`) {
		t.Errorf("string modified:\n%s", got)
	}
	if !strings.Contains(got, "class Bar:") {
		t.Errorf("decl not renamed:\n%s", got)
	}
}

func TestRename_FStringInterpolation(t *testing.T) {
	idx, root := mk(t, map[string]string{
		"main.py": `name = "world"
greeting = f"Hello {name}"
`,
	})
	file := filepath.Join(root, "main.py")
	set, err := PlanRename(RenameRequest{File: file, Symbol: "name", NewName: "who", Index: idx})
	if err != nil {
		t.Fatal(err)
	}
	if err := set.Apply(); err != nil {
		t.Fatal(err)
	}
	got := read(t, filepath.Join(root, "main.py"))
	if !strings.Contains(got, "{who}") {
		t.Errorf("f-string interpolation not renamed:\n%s", got)
	}
}

func TestRename_SkipsLocalAssignment(t *testing.T) {
	idx, root := mk(t, map[string]string{
		"main.py": `class Foo:
    pass

def use():
    Foo = 42
    return Foo
`,
	})
	file := filepath.Join(root, "main.py")
	set, err := PlanRename(RenameRequest{File: file, Symbol: "Foo", NewName: "Bar", Index: idx})
	if err != nil {
		t.Fatal(err)
	}
	if err := set.Apply(); err != nil {
		t.Fatal(err)
	}
	got := read(t, filepath.Join(root, "main.py"))
	if !strings.Contains(got, "class Bar:") {
		t.Errorf("class not renamed:\n%s", got)
	}
	if !strings.Contains(got, "Foo = 42") {
		t.Errorf("local assignment wrongly renamed:\n%s", got)
	}
}

func TestRename_NotFound(t *testing.T) {
	idx, root := mk(t, map[string]string{
		"foo.py": "class Foo:\n    pass\n",
	})
	_, err := PlanRename(RenameRequest{File: filepath.Join(root, "foo.py"), Symbol: "ghost", NewName: "x", Index: idx})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestMove_UpdatesImporters(t *testing.T) {
	idx, root := mk(t, map[string]string{
		"utils/foo.py":      "class Foo:\n    pass\n",
		"utils/__init__.py": "",
		"main.py":           "from utils.foo import Foo\nx = Foo()\n",
	})
	from := filepath.Join(root, "utils/foo.py")
	to := filepath.Join(root, "lib/foo.py")
	set, err := PlanMove(MoveRequest{File: from, NewPath: to, Index: idx})
	if err != nil {
		t.Fatal(err)
	}
	if err := set.Apply(); err != nil {
		t.Fatal(err)
	}
	main := read(t, filepath.Join(root, "main.py"))
	if !strings.Contains(main, "from lib.foo import Foo") {
		t.Errorf("import path not updated:\n%s", main)
	}
	if _, err := os.Stat(from); !os.IsNotExist(err) {
		t.Errorf("old file still exists")
	}
}

func TestDelete_RemovesFileAndImports(t *testing.T) {
	idx, root := mk(t, map[string]string{
		"foo.py":  "class Foo:\n    pass\n",
		"main.py": "from foo import Foo\nx = Foo()\n",
	})
	file := filepath.Join(root, "foo.py")
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
	main := read(t, filepath.Join(root, "main.py"))
	if strings.Contains(main, "from foo import") {
		t.Errorf("import not removed:\n%s", main)
	}
}

func TestPlanDelete_DeadImportOnlyIsNotReported(t *testing.T) {
	idx, root := mk(t, map[string]string{
		"foo.py":  "class Foo:\n    pass\n",
		"dead.py": "from foo import Foo\nn = 1\n",
		"live.py": "from foo import Foo\nx = Foo()\n",
	})
	r, err := PlanDelete(DeleteRequest{File: filepath.Join(root, "foo.py"), Index: idx})
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, p := range r.FilesWithReferences {
		got = append(got, filepath.Base(p))
	}
	if len(got) != 1 || got[0] != "live.py" {
		t.Errorf("FilesWithReferences = %v, want only [live.py]", got)
	}
}

func TestPlanDelete_TracksAliasedBinding(t *testing.T) {
	idx, root := mk(t, map[string]string{
		"foo.py":  "class Foo:\n    pass\n",
		"user.py": "from foo import Foo as F\nx = F()\n",
	})
	r, err := PlanDelete(DeleteRequest{File: filepath.Join(root, "foo.py"), Index: idx})
	if err != nil {
		t.Fatal(err)
	}
	if len(r.FilesWithReferences) != 1 {
		t.Errorf("aliased usage not detected: %v", r.FilesWithReferences)
	}
}
