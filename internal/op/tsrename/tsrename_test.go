package tsrename

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/serfom256/refactorio/internal/lang/typescript"
)

func mk(t *testing.T, files map[string]string) (*typescript.Index, string) {
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
	idx, err := typescript.BuildIndex(root)
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

func TestRename_ExportedClass(t *testing.T) {
	idx, root := mk(t, map[string]string{
		"src/foo.ts": `export class Foo {
  greet(): string { return "hi"; }
}
`,
		"src/bar.ts": `import { Foo } from './foo';
const f = new Foo();
console.log(f.greet());
`,
	})
	set, err := PlanRename(RenameRequest{
		File: filepath.Join(root, "src/foo.ts"), Symbol: "Foo", NewName: "Bar", Index: idx,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := set.Apply(); err != nil {
		t.Fatal(err)
	}
	foo := read(t, filepath.Join(root, "src/foo.ts"))
	if !strings.Contains(foo, "export class Bar") {
		t.Errorf("declaration not renamed:\n%s", foo)
	}
	bar := read(t, filepath.Join(root, "src/bar.ts"))
	if !strings.Contains(bar, "import { Bar }") {
		t.Errorf("import not renamed:\n%s", bar)
	}
	if !strings.Contains(bar, "new Bar()") {
		t.Errorf("usage not renamed:\n%s", bar)
	}
}

func TestRename_ExportedFunction(t *testing.T) {
	idx, root := mk(t, map[string]string{
		"src/utils.ts": `export function greet(name: string): string {
  return "Hello " + name;
}
`,
		"src/main.ts": `import { greet } from './utils';
console.log(greet("world"));
`,
	})
	set, err := PlanRename(RenameRequest{
		File: filepath.Join(root, "src/utils.ts"), Symbol: "greet", NewName: "sayHello", Index: idx,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := set.Apply(); err != nil {
		t.Fatal(err)
	}
	utils := read(t, filepath.Join(root, "src/utils.ts"))
	if !strings.Contains(utils, "export function sayHello") {
		t.Errorf("declaration not renamed:\n%s", utils)
	}
	main := read(t, filepath.Join(root, "src/main.ts"))
	if !strings.Contains(main, "import { sayHello }") {
		t.Errorf("import binding not renamed:\n%s", main)
	}
	if !strings.Contains(main, `sayHello("world")`) {
		t.Errorf("usage not renamed:\n%s", main)
	}
}

func TestRename_AliasedImportKeepsAlias(t *testing.T) {
	// `import { Foo as F }` — renaming Foo to Bar should update
	// the import to `{ Bar as F }` but NOT rename F in code.
	idx, root := mk(t, map[string]string{
		"src/foo.ts": `export class Foo {}`,
		"src/bar.ts": `import { Foo as F } from './foo';
const x = new F();
`,
	})
	set, err := PlanRename(RenameRequest{
		File: filepath.Join(root, "src/foo.ts"), Symbol: "Foo", NewName: "Qux", Index: idx,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := set.Apply(); err != nil {
		t.Fatal(err)
	}
	bar := read(t, filepath.Join(root, "src/bar.ts"))
	if !strings.Contains(bar, "Qux as F") {
		t.Errorf("import source name not updated:\n%s", bar)
	}
	if !strings.Contains(bar, "new F()") {
		t.Errorf("alias usage was wrongly renamed:\n%s", bar)
	}
}

func TestRename_IgnoresStringsAndComments(t *testing.T) {
	idx, root := mk(t, map[string]string{
		"src/foo.ts": `export class Foo {}`,
		"src/bar.ts": `import { Foo } from './foo';
// Foo is great
const s = "Foo is a string";
const x = new Foo();
`,
	})
	set, err := PlanRename(RenameRequest{
		File: filepath.Join(root, "src/foo.ts"), Symbol: "Foo", NewName: "Bar", Index: idx,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := set.Apply(); err != nil {
		t.Fatal(err)
	}
	bar := read(t, filepath.Join(root, "src/bar.ts"))
	if !strings.Contains(bar, `"Foo is a string"`) {
		t.Errorf("string was modified:\n%s", bar)
	}
	if !strings.Contains(bar, "// Foo is great") {
		t.Errorf("comment was modified:\n%s", bar)
	}
	if !strings.Contains(bar, "new Bar()") {
		t.Errorf("usage not renamed:\n%s", bar)
	}
}

func TestRename_TemplateLiteralSubstitution(t *testing.T) {
	idx, root := mk(t, map[string]string{
		"src/foo.ts": `export const name = "world";`,
		"src/bar.ts": "import { name } from './foo';\nconst s = `Hello ${name}`;\n",
	})
	set, err := PlanRename(RenameRequest{
		File: filepath.Join(root, "src/foo.ts"), Symbol: "name", NewName: "who", Index: idx,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := set.Apply(); err != nil {
		t.Fatal(err)
	}
	bar := read(t, filepath.Join(root, "src/bar.ts"))
	if !strings.Contains(bar, "${who}") {
		t.Errorf("template substitution not renamed:\n%s", bar)
	}
}

func TestRename_SymbolNotFound(t *testing.T) {
	idx, root := mk(t, map[string]string{
		"src/foo.ts": `export class Foo {}`,
	})
	_, err := PlanRename(RenameRequest{
		File: filepath.Join(root, "src/foo.ts"), Symbol: "Ghost", NewName: "X", Index: idx,
	})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestMove_UpdatesImporterPaths(t *testing.T) {
	idx, root := mk(t, map[string]string{
		"src/utils/foo.ts": `export class Foo {}`,
		"src/main.ts": `import { Foo } from './utils/foo';
const f = new Foo();
`,
	})
	set, err := PlanMove(MoveRequest{
		File:    filepath.Join(root, "src/utils/foo.ts"),
		NewPath: filepath.Join(root, "src/lib/foo.ts"),
		Index:   idx,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := set.Apply(); err != nil {
		t.Fatal(err)
	}
	main := read(t, filepath.Join(root, "src/main.ts"))
	if !strings.Contains(main, "'./lib/foo'") {
		t.Errorf("import path not updated:\n%s", main)
	}
	if _, err := os.Stat(filepath.Join(root, "src/utils/foo.ts")); !os.IsNotExist(err) {
		t.Errorf("old file still exists")
	}
	if _, err := os.Stat(filepath.Join(root, "src/lib/foo.ts")); err != nil {
		t.Errorf("new file missing: %v", err)
	}
}

func TestMove_UpdatesRelativeImportsInMovedFile(t *testing.T) {
	idx, root := mk(t, map[string]string{
		"src/utils/helper.ts": `export const x = 1;`,
		"src/utils/foo.ts": `import { x } from './helper';
export const y = x + 1;
`,
		"src/main.ts": `import { y } from './utils/foo';`,
	})
	set, err := PlanMove(MoveRequest{
		File:    filepath.Join(root, "src/utils/foo.ts"),
		NewPath: filepath.Join(root, "src/foo.ts"),
		Index:   idx,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := set.Apply(); err != nil {
		t.Fatal(err)
	}
	foo := read(t, filepath.Join(root, "src/foo.ts"))
	if !strings.Contains(foo, "'./utils/helper'") {
		t.Errorf("relative import in moved file not updated:\n%s", foo)
	}
	main := read(t, filepath.Join(root, "src/main.ts"))
	if !strings.Contains(main, "'./foo'") {
		t.Errorf("importer path not updated:\n%s", main)
	}
}

func TestMove_SameFile(t *testing.T) {
	idx, root := mk(t, map[string]string{
		"src/foo.ts": `export class Foo {}`,
	})
	_, err := PlanMove(MoveRequest{
		File:    filepath.Join(root, "src/foo.ts"),
		NewPath: filepath.Join(root, "src/foo.ts"),
		Index:   idx,
	})
	if err == nil {
		t.Fatal("expected error")
	}
}

// A dependent whose import of the deleted file is removed, and which never
// used the binding, needs no manual cleanup — it must not be reported.
func TestPlanDelete_DeadImportOnlyIsNotReported(t *testing.T) {
	idx, root := mk(t, map[string]string{
		"src/foo.ts":  "export class Foo {}\n",
		"src/dead.ts": "import { Foo } from './foo';\nexport const n = 1;\n",
		"src/live.ts": "import { Foo } from './foo';\nexport const f = new Foo();\n",
	})
	res, err := PlanDelete(DeleteRequest{File: filepath.Join(root, "src/foo.ts"), Index: idx})
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, p := range res.FilesWithReferences {
		got = append(got, filepath.Base(p))
	}
	if len(got) != 1 || got[0] != "live.ts" {
		t.Errorf("FilesWithReferences = %v, want only [live.ts]", got)
	}
}

// The name left dangling by `import { Foo as F }` is F, not Foo.
func TestPlanDelete_TracksAliasedBinding(t *testing.T) {
	idx, root := mk(t, map[string]string{
		"src/foo.ts":  "export class Foo {}\n",
		"src/user.ts": "import { Foo as F } from './foo';\nexport const x = new F();\n",
	})
	res, err := PlanDelete(DeleteRequest{File: filepath.Join(root, "src/foo.ts"), Index: idx})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.FilesWithReferences) != 1 {
		t.Errorf("aliased usage not detected: %v", res.FilesWithReferences)
	}
}
