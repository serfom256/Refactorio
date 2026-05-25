package unified

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

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
	set, err := Move(from, to, root)
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
	set, err := Move(from, to, root)
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

func TestUnifiedDelete_Java(t *testing.T) {
	root := t.TempDir()
	writeTree(t, root, map[string]string{
		"build.gradle": "",
		"src/main/java/com/foo/Bar.java": "package com.foo; public class Bar {}\n",
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
	set, err := Move(from, to, root)
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
		"go.mod":   "module example.com/proj\ngo 1.21\n",
		"pkg/foo.go": "package pkg\n\nfunc Hello() string { return \"hi\" }\n",
		"main.go":  "package main\n\nimport \"example.com/proj/pkg\"\n\nfunc main() { println(pkg.Hello()) }\n",
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
		"go.mod":    "module example.com/proj\ngo 1.21\n",
		"pkg/foo.go": "package pkg\nfunc Hello() {}\n",
		"main.go":   "package main\nimport \"example.com/proj/pkg\"\nfunc main() { pkg.Hello() }\n",
	})
	from := filepath.Join(root, "pkg/foo.go")
	to := filepath.Join(root, "lib/foo.go")
	set, err := Move(from, to, root)
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
		"go.mod":    "module example.com/proj\ngo 1.21\n",
		"pkg/foo.go": "package pkg\nfunc Hello() {}\n",
		"main.go":   "package main\nimport \"example.com/proj/pkg\"\nfunc main() { pkg.Hello() }\n",
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
		"build.gradle": "",
		"src/main/kotlin/com/foo/Bar.kt": "package com.foo\nclass Bar\n",
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
	set, err := Move(from, to, root)
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

func TestUnifiedRename_UnsupportedExtension(t *testing.T) {
	_, err := Rename("/tmp/foo.rb", "Foo", "Bar", "/tmp")
	if err == nil || !strings.Contains(err.Error(), "unsupported") {
		t.Errorf("expected unsupported error, got: %v", err)
	}
}
