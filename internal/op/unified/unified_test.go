package unified

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/serfom256/refactorio/internal/cli"
	"github.com/serfom256/refactorio/internal/op/usages"
)

func usagesFind(t *testing.T, file, name, root string) (*usages.Result, error) {
	t.Helper()
	return usages.Find(file, name, root, Rename)
}

func usagesFormatText(w io.Writer, r *usages.Result) error { return usages.FormatText(w, r) }
func usagesFormatJSON(w io.Writer, r *usages.Result) error { return usages.FormatJSON(w, r) }

// runDelete drives the delete command the way the CLI does, so the tests cover
// the refusal gate and not just the planning layer.
func runDelete(t *testing.T, out io.Writer, argv ...string) error {
	t.Helper()
	args, err := cli.Parse(append([]string{"delete"}, argv...))
	if err != nil {
		t.Fatal(err)
	}
	return HandleDelete(args, out, io.Discard)
}

func writeTree(t *testing.T, root string, files map[string]string) {
	t.Helper()
	for rel, body := range files {
		full := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func read(t *testing.T, p string) string {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// --- Java ---

func TestUnifiedRename_Java(t *testing.T) {
	root := t.TempDir()
	writeTree(t, root, map[string]string{
		"build.gradle": "",
		"src/main/java/com/foo/Bar.java": `package com.foo;
public class Bar {}
`,
		"src/main/java/com/app/User.java": `package com.app;
import com.foo.Bar;
public class User { Bar b; }
`,
	})
	file := filepath.Join(root, "src/main/java/com/foo/Bar.java")
	set, err := Rename(file, "Bar", "Baz", root)
	if err != nil {
		t.Fatal(err)
	}
	if err := set.Apply(); err != nil {
		t.Fatal(err)
	}
	user := read(t, filepath.Join(root, "src/main/java/com/app/User.java"))
	if !strings.Contains(user, "import com.foo.Baz;") || !strings.Contains(user, "Baz b;") {
		t.Errorf("Java rename failed:\n%s", user)
	}
}

func TestUnifiedMove_Java(t *testing.T) {
	root := t.TempDir()
	writeTree(t, root, map[string]string{
		"build.gradle": "",
		"src/main/java/com/foo/Bar.java": `package com.foo;
public class Bar {}
`,
		"src/main/java/com/app/User.java": `package com.app;
import com.foo.Bar;
public class User { Bar b; }
`,
	})
	from := filepath.Join(root, "src/main/java/com/foo/Bar.java")
	to := filepath.Join(root, "src/main/java/com/qux/Bar.java")
	set, _, err := Move(from, to, root)
	if err != nil {
		t.Fatal(err)
	}
	if err := set.Apply(); err != nil {
		t.Fatal(err)
	}
	bar := read(t, to)
	if !strings.Contains(bar, "package com.qux;") {
		t.Errorf("package not updated:\n%s", bar)
	}
	user := read(t, filepath.Join(root, "src/main/java/com/app/User.java"))
	if !strings.Contains(user, "import com.qux.Bar;") {
		t.Errorf("import not updated:\n%s", user)
	}
}

// Same-package files that use a class without an import must get an
// explicit import added when the class is moved out of the package.
func TestUnifiedMove_Java_SamePackageGetsImport(t *testing.T) {
	root := t.TempDir()
	writeTree(t, root, map[string]string{
		"build.gradle": "",
		"src/main/java/com/test/A.java": `package com.test;
public class A { public int value; }
`,
		"src/main/java/com/test/B.java": `package com.test;
public class B { A a = new A(); }
`,
		"src/main/java/com/test/C.java": `package com.test;
public class C { A a; }
`,
		"src/main/java/com/test/Unrelated.java": `package com.test;
public class Unrelated { int x; }
`,
	})
	from := filepath.Join(root, "src/main/java/com/test/A.java")
	to := filepath.Join(root, "src/main/java/com/other/A.java")
	set, _, err := Move(from, to, root)
	if err != nil {
		t.Fatal(err)
	}
	if err := set.Apply(); err != nil {
		t.Fatal(err)
	}
	// B.java uses A — should now have an import.
	b := read(t, filepath.Join(root, "src/main/java/com/test/B.java"))
	if !strings.Contains(b, "import com.other.A;") {
		t.Errorf("B.java missing import for moved A:\n%s", b)
	}
	// C.java uses A — should now have an import.
	c := read(t, filepath.Join(root, "src/main/java/com/test/C.java"))
	if !strings.Contains(c, "import com.other.A;") {
		t.Errorf("C.java missing import for moved A:\n%s", c)
	}
	// Unrelated.java does NOT use A — should NOT get an import.
	u := read(t, filepath.Join(root, "src/main/java/com/test/Unrelated.java"))
	if strings.Contains(u, "import") {
		t.Errorf("Unrelated.java should not have an import:\n%s", u)
	}
}

// Moving a class that uses a package-private sibling must warn that
// the sibling won't be importable from the new package.
func TestUnifiedMove_Java_PackagePrivateWarning(t *testing.T) {
	root := t.TempDir()
	writeTree(t, root, map[string]string{
		"build.gradle": "",
		"src/main/java/com/test/A.java": `package com.test;
public class A {
    Helper h = new Helper();
}
`,
		// Helper is package-private (no `public`).
		"src/main/java/com/test/Helper.java": `package com.test;
class Helper {}
`,
	})
	from := filepath.Join(root, "src/main/java/com/test/A.java")
	to := filepath.Join(root, "src/main/java/com/other/A.java")
	_, warnings, err := Move(from, to, root)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, w := range warnings {
		if strings.Contains(w, "Helper") && strings.Contains(w, "package-private") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected package-private warning for Helper, got: %v", warnings)
	}
}

// Moving a class that only uses PUBLIC siblings produces no warning.
func TestUnifiedMove_Java_NoWarningForPublic(t *testing.T) {
	root := t.TempDir()
	writeTree(t, root, map[string]string{
		"build.gradle": "",
		"src/main/java/com/test/A.java": `package com.test;
public class A { Helper h; }
`,
		"src/main/java/com/test/Helper.java": `package com.test;
public class Helper {}
`,
	})
	from := filepath.Join(root, "src/main/java/com/test/A.java")
	to := filepath.Join(root, "src/main/java/com/other/A.java")
	_, warnings, err := Move(from, to, root)
	if err != nil {
		t.Fatal(err)
	}
	if len(warnings) != 0 {
		t.Errorf("expected no warnings, got: %v", warnings)
	}
}

func TestUnifiedDelete_Java(t *testing.T) {
	root := t.TempDir()
	writeTree(t, root, map[string]string{
		"build.gradle":                    "",
		"src/main/java/com/foo/Bar.java":  "package com.foo; public class Bar {}\n",
		"src/main/java/com/app/User.java": "package com.app;\nimport com.foo.Bar;\npublic class User { Bar b; }\n",
	})
	file := filepath.Join(root, "src/main/java/com/foo/Bar.java")
	set, _, err := Delete(file, root)
	if err != nil {
		t.Fatal(err)
	}
	if err := set.Apply(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(file); !os.IsNotExist(err) {
		t.Errorf("file still exists")
	}
	user := read(t, filepath.Join(root, "src/main/java/com/app/User.java"))
	if strings.Contains(user, "import com.foo.Bar;") {
		t.Errorf("import not removed:\n%s", user)
	}
}

// --- TypeScript ---

func TestUnifiedRename_TS(t *testing.T) {
	root := t.TempDir()
	writeTree(t, root, map[string]string{
		"src/foo.ts":  `export class Foo {}`,
		"src/main.ts": "import { Foo } from './foo';\nconst f = new Foo();\n",
	})
	file := filepath.Join(root, "src/foo.ts")
	set, err := Rename(file, "Foo", "Bar", root)
	if err != nil {
		t.Fatal(err)
	}
	if err := set.Apply(); err != nil {
		t.Fatal(err)
	}
	main := read(t, filepath.Join(root, "src/main.ts"))
	if !strings.Contains(main, "import { Bar }") || !strings.Contains(main, "new Bar()") {
		t.Errorf("TS rename failed:\n%s", main)
	}
}

func TestUnifiedMove_TS(t *testing.T) {
	root := t.TempDir()
	writeTree(t, root, map[string]string{
		"src/utils/foo.ts": `export class Foo {}`,
		"src/main.ts":      "import { Foo } from './utils/foo';\n",
	})
	from := filepath.Join(root, "src/utils/foo.ts")
	to := filepath.Join(root, "src/lib/foo.ts")
	set, _, err := Move(from, to, root)
	if err != nil {
		t.Fatal(err)
	}
	if err := set.Apply(); err != nil {
		t.Fatal(err)
	}
	main := read(t, filepath.Join(root, "src/main.ts"))
	if !strings.Contains(main, "'./lib/foo'") {
		t.Errorf("TS import not updated:\n%s", main)
	}
}

func TestUnifiedDelete_TS(t *testing.T) {
	root := t.TempDir()
	writeTree(t, root, map[string]string{
		"src/foo.ts":  `export class Foo {}`,
		"src/main.ts": "import { Foo } from './foo';\n",
	})
	file := filepath.Join(root, "src/foo.ts")
	set, _, err := Delete(file, root)
	if err != nil {
		t.Fatal(err)
	}
	if err := set.Apply(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(file); !os.IsNotExist(err) {
		t.Errorf("file still exists")
	}
}

// --- Go ---

func TestUnifiedRename_Go(t *testing.T) {
	root := t.TempDir()
	writeTree(t, root, map[string]string{
		"go.mod":     "module example.com/proj\ngo 1.21\n",
		"pkg/foo.go": "package pkg\n\nfunc Hello() string { return \"hi\" }\n",
		"main.go":    "package main\n\nimport \"example.com/proj/pkg\"\n\nfunc main() { println(pkg.Hello()) }\n",
	})
	file := filepath.Join(root, "pkg/foo.go")
	set, err := Rename(file, "Hello", "Greet", root)
	if err != nil {
		t.Fatal(err)
	}
	if err := set.Apply(); err != nil {
		t.Fatal(err)
	}
	main := read(t, filepath.Join(root, "main.go"))
	if !strings.Contains(main, "pkg.Greet()") {
		t.Errorf("Go rename failed:\n%s", main)
	}
}

func TestUnifiedMove_Go(t *testing.T) {
	root := t.TempDir()
	writeTree(t, root, map[string]string{
		"go.mod":     "module example.com/proj\ngo 1.21\n",
		"pkg/foo.go": "package pkg\nfunc Hello() {}\n",
		"main.go":    "package main\nimport \"example.com/proj/pkg\"\nfunc main() { pkg.Hello() }\n",
	})
	from := filepath.Join(root, "pkg/foo.go")
	to := filepath.Join(root, "lib/foo.go")
	set, _, err := Move(from, to, root)
	if err != nil {
		t.Fatal(err)
	}
	if err := set.Apply(); err != nil {
		t.Fatal(err)
	}
	main := read(t, filepath.Join(root, "main.go"))
	if !strings.Contains(main, `"example.com/proj/lib"`) {
		t.Errorf("Go import not updated:\n%s", main)
	}
}

func TestUnifiedDelete_Go(t *testing.T) {
	root := t.TempDir()
	writeTree(t, root, map[string]string{
		"go.mod":     "module example.com/proj\ngo 1.21\n",
		"pkg/foo.go": "package pkg\nfunc Hello() {}\n",
		"main.go":    "package main\nimport \"example.com/proj/pkg\"\nfunc main() { pkg.Hello() }\n",
	})
	file := filepath.Join(root, "pkg/foo.go")
	set, _, err := Delete(file, root)
	if err != nil {
		t.Fatal(err)
	}
	if err := set.Apply(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(file); !os.IsNotExist(err) {
		t.Errorf("file still exists")
	}
}

// --- Kotlin ---

func TestUnifiedRename_Kotlin(t *testing.T) {
	root := t.TempDir()
	writeTree(t, root, map[string]string{
		"build.gradle":                    "",
		"src/main/kotlin/com/foo/Bar.kt":  "package com.foo\nclass Bar\n",
		"src/main/kotlin/com/app/User.kt": "package com.app\nimport com.foo.Bar\nval b = Bar()\n",
	})
	file := filepath.Join(root, "src/main/kotlin/com/foo/Bar.kt")
	set, err := Rename(file, "Bar", "Baz", root)
	if err != nil {
		t.Fatal(err)
	}
	if err := set.Apply(); err != nil {
		t.Fatal(err)
	}
	user := read(t, filepath.Join(root, "src/main/kotlin/com/app/User.kt"))
	if !strings.Contains(user, "import com.foo.Baz") || !strings.Contains(user, "Baz()") {
		t.Errorf("Kotlin rename failed:\n%s", user)
	}
}

// Regression test: renaming a class must not modify same-package files
// that happen to use the same identifier for a different purpose.
func TestUnifiedRename_Java_DoesNotTouchSamePackageCollision(t *testing.T) {
	root := t.TempDir()
	writeTree(t, root, map[string]string{
		"build.gradle": "",
		"src/main/java/com/foo/Foo.java": `package com.foo;
public class Foo {
    public String greet() { return "hi"; }
}
`,
		"src/main/java/com/foo/Other.java": `package com.foo;
public class Other {
    String Foo = "local variable named Foo";
    void run() { System.out.println(Foo); }
}
`,
	})
	file := filepath.Join(root, "src/main/java/com/foo/Foo.java")
	set, err := Rename(file, "Foo", "Bar", root)
	if err != nil {
		t.Fatal(err)
	}
	if err := set.Apply(); err != nil {
		t.Fatal(err)
	}
	// Other.java: the local variable `Foo` must NOT be renamed — it's
	// a string variable, not a reference to the class.
	other := read(t, filepath.Join(root, "src/main/java/com/foo/Other.java"))
	if strings.Contains(other, `String Bar = "local`) {
		t.Errorf("local variable was wrongly renamed:\n%s", other)
	}
}

// Renaming a symbol that doesn't exist as a declaration should error.
// Regression: inline fully-qualified references like
// `com.foo.Field x = new com.foo.Field()` must be renamed too.
func TestUnifiedRename_Java_InlineFQN(t *testing.T) {
	root := t.TempDir()
	writeTree(t, root, map[string]string{
		"build.gradle": "",
		"src/main/java/com/foo/Field.java": `package com.foo;
public class Field { public int value; }
`,
		"src/main/java/com/bar/User.java": `package com.bar;
public class User {
    com.foo.Field f = new com.foo.Field();
}
`,
	})
	file := filepath.Join(root, "src/main/java/com/foo/Field.java")
	set, err := Rename(file, "Field", "Column", root)
	if err != nil {
		t.Fatal(err)
	}
	if err := set.Apply(); err != nil {
		t.Fatal(err)
	}
	user := read(t, filepath.Join(root, "src/main/java/com/bar/User.java"))
	if strings.Contains(user, "com.foo.Field") {
		t.Errorf("inline FQN not renamed:\n%s", user)
	}
	if !strings.Contains(user, "com.foo.Column") {
		t.Errorf("expected com.foo.Column:\n%s", user)
	}
}

func TestUnifiedMove_Java_InlineFQN(t *testing.T) {
	root := t.TempDir()
	writeTree(t, root, map[string]string{
		"build.gradle": "",
		"src/main/java/com/foo/Field.java": `package com.foo;
public class Field {}
`,
		"src/main/java/com/bar/User.java": `package com.bar;
public class User {
    com.foo.Field f;
}
`,
	})
	from := filepath.Join(root, "src/main/java/com/foo/Field.java")
	to := filepath.Join(root, "src/main/java/com/qux/Field.java")
	set, _, err := Move(from, to, root)
	if err != nil {
		t.Fatal(err)
	}
	if err := set.Apply(); err != nil {
		t.Fatal(err)
	}
	user := read(t, filepath.Join(root, "src/main/java/com/bar/User.java"))
	if strings.Contains(user, "com.foo.Field") {
		t.Errorf("inline FQN not rewritten:\n%s", user)
	}
	if !strings.Contains(user, "com.qux.Field") {
		t.Errorf("expected com.qux.Field:\n%s", user)
	}
}

func TestUnifiedRename_Java_UnknownSymbolErrors(t *testing.T) {
	root := t.TempDir()
	writeTree(t, root, map[string]string{
		"build.gradle": "",
		"src/main/java/com/foo/Utils.java": `package com.foo;
public class Utils { public static Class wrap(Object o) { return o.getClass(); } }
`,
	})
	file := filepath.Join(root, "src/main/java/com/foo/Utils.java")
	_, err := Rename(file, "Class", "Clazz", root)
	if err == nil {
		t.Fatal("expected error — Class is java.lang.Class, not a local declaration")
	}
}

// Verify that files with explicit imports DO get renamed.
func TestUnifiedRename_Java_ExplicitImportStillWorks(t *testing.T) {
	root := t.TempDir()
	writeTree(t, root, map[string]string{
		"build.gradle": "",
		"src/main/java/com/foo/Bar.java": `package com.foo;
public class Bar {}
`,
		"src/main/java/com/app/User.java": `package com.app;
import com.foo.Bar;
public class User { Bar b; }
`,
	})
	file := filepath.Join(root, "src/main/java/com/foo/Bar.java")
	set, err := Rename(file, "Bar", "Baz", root)
	if err != nil {
		t.Fatal(err)
	}
	if err := set.Apply(); err != nil {
		t.Fatal(err)
	}
	user := read(t, filepath.Join(root, "src/main/java/com/app/User.java"))
	if !strings.Contains(user, "import com.foo.Baz;") {
		t.Errorf("import not rewritten:\n%s", user)
	}
	if !strings.Contains(user, "Baz b;") {
		t.Errorf("usage not renamed:\n%s", user)
	}
}

// End-to-end usages: rename probe → real findings across languages.
func TestUsages_Java(t *testing.T) {
	root := t.TempDir()
	writeTree(t, root, map[string]string{
		"build.gradle": "",
		"src/main/java/com/foo/Bar.java": `package com.foo;
public class Bar {}
`,
		"src/main/java/com/app/User.java": `package com.app;
import com.foo.Bar;
public class User { Bar b = new Bar(); }
`,
	})
	file := filepath.Join(root, "src/main/java/com/foo/Bar.java")
	result, err := usagesFind(t, file, "Bar", root)
	if err != nil {
		t.Fatal(err)
	}
	if result.Total < 3 {
		t.Errorf("expected at least 3 findings (1 decl + 2 usages), got %d: %+v", result.Total, result.Findings)
	}
	if result.Findings[0].Category != "declaration" {
		t.Errorf("first finding should be declaration: %+v", result.Findings[0])
	}
}

func TestUsages_FormatTextAndJSON(t *testing.T) {
	root := t.TempDir()
	writeTree(t, root, map[string]string{
		"src/foo.ts":  `export class Foo {}`,
		"src/main.ts": "import { Foo } from './foo';\nconst f = new Foo();\n",
	})
	file := filepath.Join(root, "src/foo.ts")
	result, err := usagesFind(t, file, "Foo", root)
	if err != nil {
		t.Fatal(err)
	}
	if result.Total == 0 {
		t.Fatal("expected non-zero findings")
	}
	// Text format
	var textBuf bytes.Buffer
	if err := usagesFormatText(&textBuf, result); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(textBuf.String(), "(declaration)") {
		t.Errorf("text format missing declaration tag:\n%s", textBuf.String())
	}
	// JSON format round-trip
	var jsonBuf bytes.Buffer
	if err := usagesFormatJSON(&jsonBuf, result); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(jsonBuf.String(), `"category": "declaration"`) {
		t.Errorf("json missing declaration:\n%s", jsonBuf.String())
	}
}

func TestUnifiedRename_UnsupportedExtension(t *testing.T) {
	_, err := Rename("/tmp/foo.rb", "Foo", "Bar", "/tmp")
	if err == nil || !strings.Contains(err.Error(), "unsupported") {
		t.Errorf("expected unsupported error, got: %v", err)
	}
}

// The two ways the delete warning used to be wrong, on one tree:
//
//	SamePackageUser  uses Doomed with no import   — was never examined
//	RealUser         imports and uses Doomed      — correctly reported
//	ImportOnly       imports but never uses it    — was wrongly reported
func TestDelete_SurvivingRefsAreAccurate(t *testing.T) {
	root := t.TempDir()
	writeTree(t, root, map[string]string{
		"build.gradle": "",
		"src/main/java/com/ex/Doomed.java": `package com.ex;
public class Doomed { public String hello() { return "hi"; } }
`,
		"src/main/java/com/ex/SamePackageUser.java": `package com.ex;
public class SamePackageUser { public String go() { return new Doomed().hello(); } }
`,
		"src/main/java/com/other/RealUser.java": `package com.other;
import com.ex.Doomed;
public class RealUser { Doomed d = new Doomed(); }
`,
		"src/main/java/com/other/ImportOnly.java": `package com.other;
import com.ex.Doomed;
public class ImportOnly { int n = 1; }
`,
	})

	_, refs, err := Delete(filepath.Join(root, "src/main/java/com/ex/Doomed.java"), root)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, r := range refs {
		got = append(got, filepath.Base(r))
	}
	want := map[string]bool{"SamePackageUser.java": true, "RealUser.java": true}
	if len(got) != 2 {
		t.Fatalf("refs = %v, want exactly SamePackageUser.java and RealUser.java", got)
	}
	for _, g := range got {
		if !want[g] {
			t.Errorf("unexpected file reported: %s (refs = %v)", g, got)
		}
	}
}

// A same-package user needs no import, so it was invisible to the old
// import-driven check and delete broke the build with no warning at all.
func TestDelete_SamePackageUserIsReported(t *testing.T) {
	root := t.TempDir()
	writeTree(t, root, map[string]string{
		"build.gradle": "",
		"src/main/java/com/ex/Doomed.java": `package com.ex;
public class Doomed { public String hello() { return "hi"; } }
`,
		"src/main/java/com/ex/SamePackageUser.java": `package com.ex;
public class SamePackageUser { public String go() { return new Doomed().hello(); } }
`,
	})
	_, refs, err := Delete(filepath.Join(root, "src/main/java/com/ex/Doomed.java"), root)
	if err != nil {
		t.Fatal(err)
	}
	if len(refs) != 1 || filepath.Base(refs[0]) != "SamePackageUser.java" {
		t.Errorf("refs = %v, want [SamePackageUser.java]", refs)
	}
}

// A dependent whose import is merely cleaned needs no manual work.
func TestDelete_ImportOnlyDependentIsNotReported(t *testing.T) {
	root := t.TempDir()
	writeTree(t, root, map[string]string{
		"build.gradle": "",
		"src/main/java/com/ex/Doomed.java": `package com.ex;
public class Doomed { public String hello() { return "hi"; } }
`,
		"src/main/java/com/other/ImportOnly.java": `package com.other;
import com.ex.Doomed;
public class ImportOnly { int n = 1; }
`,
	})
	_, refs, err := Delete(filepath.Join(root, "src/main/java/com/ex/Doomed.java"), root)
	if err != nil {
		t.Fatal(err)
	}
	if len(refs) != 0 {
		t.Errorf("refs = %v, want none", refs)
	}
}

// Delete must refuse to run when references survive, and say which files.
func TestHandleDelete_RefusesWhenReferencesSurvive(t *testing.T) {
	root := t.TempDir()
	writeTree(t, root, map[string]string{
		"build.gradle": "",
		"src/main/java/com/ex/Doomed.java": `package com.ex;
public class Doomed { public String hello() { return "hi"; } }
`,
		"src/main/java/com/ex/SamePackageUser.java": `package com.ex;
public class SamePackageUser { public String go() { return new Doomed().hello(); } }
`,
	})
	target := filepath.Join(root, "src/main/java/com/ex/Doomed.java")

	var out bytes.Buffer
	err := runDelete(t, &out, "in="+target, "root="+root)
	if err == nil {
		t.Fatal("expected delete to be refused")
	}
	if !strings.Contains(err.Error(), "force=true") {
		t.Errorf("error should point at force=true, got: %v", err)
	}
	if !strings.Contains(out.String(), "SamePackageUser.java") {
		t.Errorf("warning should name the referencing file, got:\n%s", out.String())
	}
	if _, statErr := os.Stat(target); statErr != nil {
		t.Error("file was deleted despite the refusal")
	}

	// force=true goes through.
	out.Reset()
	if err := runDelete(t, &out, "in="+target, "root="+root, "force=true"); err != nil {
		t.Fatalf("force=true should succeed: %v", err)
	}
	if _, statErr := os.Stat(target); !os.IsNotExist(statErr) {
		t.Error("force=true did not delete the file")
	}
}

func TestInPackage_DotBoundary(t *testing.T) {
	for _, tc := range []struct {
		path, pkg string
		want      bool
	}{
		{"javax.persistence", "javax.persistence", true},
		{"javax.persistence.Entity", "javax.persistence", true},
		{"javax.persistence.criteria.Root", "javax.persistence", true},
		// The cases a naive search-and-replace gets wrong:
		{"javax.sql.DataSource", "javax.persistence", false},
		{"javax.persistencex.Foo", "javax.persistence", false},
		{"javax.persistenceX", "javax.persistence", false},
		{"com.javax.persistence.Foo", "javax.persistence", false},
		{"javax", "javax.persistence", false},
	} {
		if got := inPackage(tc.path, tc.pkg); got != tc.want {
			t.Errorf("inPackage(%q, %q) = %v, want %v", tc.path, tc.pkg, got, tc.want)
		}
	}
}

// The javax -> jakarta migration, including the JDK packages that share the
// javax prefix and must not move.
func TestRenamePackage_ExternalRewritesReferencesOnly(t *testing.T) {
	root := t.TempDir()
	writeTree(t, root, map[string]string{
		"build.gradle": "",
		"src/main/java/com/app/Order.java": `package com.app;

import javax.persistence.Entity;
import javax.persistence.criteria.Root;
import javax.persistence.*;
import static javax.persistence.CascadeType.ALL;
import javax.sql.DataSource;

@Entity
public class Order {
    private DataSource ds;
    private javax.persistence.EntityManager em;
    private javax.sql.rowset.CachedRowSet crs;
    // javax.persistence in a comment
    String s = "javax.persistence.Entity";
}
`,
	})

	set, err := RenamePackage("javax.persistence", "jakarta.persistence", root)
	if err != nil {
		t.Fatal(err)
	}
	if len(set.Moves()) != 0 || len(set.Deletes()) != 0 {
		t.Errorf("external rename must not move or delete files: %d moves, %d deletes",
			len(set.Moves()), len(set.Deletes()))
	}
	if err := set.Apply(); err != nil {
		t.Fatal(err)
	}
	got := read(t, filepath.Join(root, "src/main/java/com/app/Order.java"))

	for _, want := range []string{
		"import jakarta.persistence.Entity;",
		"import jakarta.persistence.criteria.Root;",
		"import jakarta.persistence.*;",
		"import static jakarta.persistence.CascadeType.ALL;",
		"private jakarta.persistence.EntityManager em;",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing rewrite %q in:\n%s", want, got)
		}
	}
	// JDK packages sharing the javax prefix must be untouched.
	for _, keep := range []string{
		"import javax.sql.DataSource;",
		"private javax.sql.rowset.CachedRowSet crs;",
		"// javax.persistence in a comment",
		`String s = "javax.persistence.Entity";`,
	} {
		if !strings.Contains(got, keep) {
			t.Errorf("wrongly rewrote %q in:\n%s", keep, got)
		}
	}
}

func TestRenamePackage_ExternalErrorsWhenNoReferences(t *testing.T) {
	root := t.TempDir()
	writeTree(t, root, map[string]string{
		"build.gradle":                     "",
		"src/main/java/com/app/Order.java": "package com.app;\npublic class Order {}\n",
	})
	_, err := RenamePackage("org.nowhere.absent", "org.other.absent", root)
	if err == nil {
		t.Fatal("expected an error when the package is neither owned nor referenced")
	}
	if !strings.Contains(err.Error(), "no references") {
		t.Errorf("error should explain both cases, got: %v", err)
	}
}

// A package the project DOES own must still be moved, not merely rewritten.
func TestRenamePackage_OwnedPackageStillMovesFiles(t *testing.T) {
	root := t.TempDir()
	writeTree(t, root, map[string]string{
		"build.gradle":                    "",
		"src/main/java/com/foo/Bar.java":  "package com.foo;\npublic class Bar {}\n",
		"src/main/java/com/app/User.java": "package com.app;\nimport com.foo.Bar;\npublic class User { Bar b; }\n",
	})
	set, err := RenamePackage("com.foo", "com.baz", root)
	if err != nil {
		t.Fatal(err)
	}
	if len(set.Moves()) == 0 {
		t.Error("renaming an owned package must move its files")
	}
}

// A package containing only annotation types used to fail with
// "no types found in package".
func TestRenamePackage_AnnotationOnlyPackage(t *testing.T) {
	root := t.TempDir()
	writeTree(t, root, map[string]string{
		"build.gradle": "",
		"src/main/java/com/om/annotations/Table.java": `package com.om.annotations;
public @interface Table { String value(); }
`,
		"src/main/java/com/app/User.java": `package com.app;
import com.om.annotations.Table;
@Table("users")
public class User {}
`,
	})
	set, err := RenamePackage("com.om.annotations", "com.om.meta", root)
	if err != nil {
		t.Fatal(err)
	}
	if err := set.Apply(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(read(t, filepath.Join(root, "src/main/java/com/app/User.java")), "import com.om.meta.Table;") {
		t.Error("import in dependent not rewritten")
	}
	if _, err := os.Stat(filepath.Join(root, "src/main/java/com/om/meta/Table.java")); err != nil {
		t.Errorf("annotation type not moved: %v", err)
	}
}

func TestRename_AnnotationType(t *testing.T) {
	root := t.TempDir()
	writeTree(t, root, map[string]string{
		"build.gradle":                    "",
		"src/main/java/com/om/Table.java": "package com.om;\npublic @interface Table { String value(); }\n",
		"src/main/java/com/app/User.java": "package com.app;\nimport com.om.Table;\n@Table(\"users\")\npublic class User {}\n",
	})
	set, err := Rename(filepath.Join(root, "src/main/java/com/om/Table.java"), "Table", "Entity", root)
	if err != nil {
		t.Fatal(err)
	}
	if err := set.Apply(); err != nil {
		t.Fatal(err)
	}
	user := read(t, filepath.Join(root, "src/main/java/com/app/User.java"))
	for _, want := range []string{"import com.om.Entity;", `@Entity("users")`} {
		if !strings.Contains(user, want) {
			t.Errorf("missing %q in:\n%s", want, user)
		}
	}
}
