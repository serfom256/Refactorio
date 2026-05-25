package rstrename

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/serfom256/refactorio/internal/lang/rust"
)

func mk(t *testing.T, files map[string]string) (*rust.Index, string) {
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
	idx, err := rust.BuildIndex(root)
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

func TestRename_Struct(t *testing.T) {
	idx, root := mk(t, map[string]string{
		"Cargo.toml": "[package]\nname = \"myproj\"\nversion = \"0.1.0\"\n",
		"src/lib.rs":  "pub mod foo;\n",
		"src/foo.rs": `pub struct Bar { pub x: i32 }

impl Bar {
    pub fn new() -> Bar { Bar { x: 0 } }
}
`,
		"src/main.rs": `mod foo;
use crate::foo::Bar;

fn main() {
    let b = Bar::new();
    println!("{}", b.x);
}
`,
	})
	file := filepath.Join(root, "src/foo.rs")
	set, err := PlanRename(RenameRequest{File: file, Symbol: "Bar", NewName: "Baz", Index: idx})
	if err != nil {
		t.Fatal(err)
	}
	if err := set.Apply(); err != nil {
		t.Fatal(err)
	}
	foo := read(t, filepath.Join(root, "src/foo.rs"))
	if !strings.Contains(foo, "pub struct Baz") {
		t.Errorf("struct not renamed:\n%s", foo)
	}
	if !strings.Contains(foo, "impl Baz") {
		t.Errorf("impl not renamed:\n%s", foo)
	}
	main := read(t, filepath.Join(root, "src/main.rs"))
	if !strings.Contains(main, "crate::foo::Baz") {
		t.Errorf("use path not updated:\n%s", main)
	}
	if !strings.Contains(main, "Baz::new()") {
		t.Errorf("usage not renamed:\n%s", main)
	}
}

func TestRename_Function(t *testing.T) {
	idx, root := mk(t, map[string]string{
		"Cargo.toml": "[package]\nname = \"myproj\"\n",
		"src/lib.rs": `pub fn greet(name: &str) -> String {
    format!("Hello {}", name)
}
`,
		"src/main.rs": `use myproj::greet;
fn main() { println!("{}", greet("world")); }
`,
	})
	file := filepath.Join(root, "src/lib.rs")
	set, err := PlanRename(RenameRequest{File: file, Symbol: "greet", NewName: "say_hello", Index: idx})
	if err != nil {
		t.Fatal(err)
	}
	if err := set.Apply(); err != nil {
		t.Fatal(err)
	}
	lib := read(t, filepath.Join(root, "src/lib.rs"))
	if !strings.Contains(lib, "pub fn say_hello") {
		t.Errorf("fn not renamed:\n%s", lib)
	}
}

func TestRename_IgnoresStringsAndComments(t *testing.T) {
	idx, root := mk(t, map[string]string{
		"Cargo.toml": "[package]\nname = \"myproj\"\n",
		"src/lib.rs": `// Bar is great
pub struct Bar {}
let s = "Bar";
`,
	})
	file := filepath.Join(root, "src/lib.rs")
	set, err := PlanRename(RenameRequest{File: file, Symbol: "Bar", NewName: "Baz", Index: idx})
	if err != nil {
		t.Fatal(err)
	}
	if err := set.Apply(); err != nil {
		t.Fatal(err)
	}
	lib := read(t, filepath.Join(root, "src/lib.rs"))
	if !strings.Contains(lib, "// Bar is great") {
		t.Errorf("comment modified:\n%s", lib)
	}
	if !strings.Contains(lib, `"Bar"`) {
		t.Errorf("string modified:\n%s", lib)
	}
}

func TestRename_SkipsLocalVariable(t *testing.T) {
	idx, root := mk(t, map[string]string{
		"Cargo.toml": "[package]\nname = \"myproj\"\n",
		"src/lib.rs": `pub struct Foo {}
pub fn test() {
    let Foo = 1;
    println!("{}", Foo);
}
`,
	})
	file := filepath.Join(root, "src/lib.rs")
	set, err := PlanRename(RenameRequest{File: file, Symbol: "Foo", NewName: "Bar", Index: idx})
	if err != nil {
		t.Fatal(err)
	}
	if err := set.Apply(); err != nil {
		t.Fatal(err)
	}
	lib := read(t, filepath.Join(root, "src/lib.rs"))
	if !strings.Contains(lib, "pub struct Bar") {
		t.Errorf("struct not renamed:\n%s", lib)
	}
	if !strings.Contains(lib, "let Foo = 1") {
		t.Errorf("local variable wrongly renamed:\n%s", lib)
	}
}

func TestRename_NotFound(t *testing.T) {
	idx, root := mk(t, map[string]string{
		"Cargo.toml": "[package]\nname = \"myproj\"\n",
		"src/lib.rs": "pub fn greet() {}\n",
	})
	_, err := PlanRename(RenameRequest{File: filepath.Join(root, "src/lib.rs"), Symbol: "ghost", NewName: "x", Index: idx})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestDelete_RemovesFileAndUseStatements(t *testing.T) {
	idx, root := mk(t, map[string]string{
		"Cargo.toml": "[package]\nname = \"myproj\"\n",
		"src/lib.rs":  "pub mod foo;\n",
		"src/foo.rs":  "pub fn greet() {}\n",
		"src/main.rs": "use myproj::foo::greet;\nfn main() { greet(); }\n",
	})
	file := filepath.Join(root, "src/foo.rs")
	result, err := PlanDelete(DeleteRequest{File: file, Index: idx})
	if err != nil {
		t.Fatal(err)
	}
	if err := result.Set.Apply(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(file); !os.IsNotExist(err) {
		t.Errorf("file still exists")
	}
}
