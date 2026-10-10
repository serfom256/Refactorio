package renameclass

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/serfom256/refactorio/internal/index"
	"github.com/serfom256/refactorio/internal/project"
)

func mkProject(t *testing.T, files map[string]string) (*project.Project, string) {
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
	p, err := project.DetectFrom(root)
	if err != nil {
		t.Fatal(err)
	}
	return p, root
}

func buildIndex(t *testing.T, p *project.Project) *index.Index {
	t.Helper()
	idx, err := index.Build(p, ".java")
	if err != nil {
		t.Fatal(err)
	}
	return idx
}

func read(t *testing.T, p string) string {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestRenameClass_RenamesConstructors(t *testing.T) {
	// Constructor names are `identifier` nodes (not type_identifier).
	// Leaving them named after the old class turns them into ordinary
	// methods and the file stops compiling.
	p, root := mkProject(t, map[string]string{
		"build.gradle": "",
		"src/main/java/com/foo/Bar.java": `package com.foo;
public class Bar {
    public Bar() {}
    public Bar(int x) {}
    public int compute() { return 0; }
}
`,
	})
	idx := buildIndex(t, p)
	set, err := Plan(Request{FQN: "com.foo.Bar", NewName: "Baz", Index: idx})
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if err := set.Apply(); err != nil {
		t.Fatal(err)
	}
	got := read(t, filepath.Join(root, "src/main/java/com/foo/Baz.java"))
	// Both constructors must be renamed.
	if strings.Contains(got, "public Bar(") {
		t.Errorf("constructor still named Bar:\n%s", got)
	}
	// And the regular method must NOT be renamed.
	if !strings.Contains(got, "public int compute()") {
		t.Errorf("regular method was disturbed:\n%s", got)
	}
}

func TestRenameClass_DeclarationOnly(t *testing.T) {
	p, root := mkProject(t, map[string]string{
		"build.gradle": "",
		"src/main/java/com/foo/Bar.java": `package com.foo;
public class Bar {
    Bar self() { return new Bar(); }
}
`,
	})
	idx := buildIndex(t, p)
	set, err := Plan(Request{FQN: "com.foo.Bar", NewName: "Baz", Index: idx})
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	// `class Bar`, return type `Bar`, `new Bar` = 3 type_identifier hits.
	if set.Len() != 3 {
		t.Errorf("edits = %d, want 3", set.Len())
	}
	if err := set.Apply(); err != nil {
		t.Fatal(err)
	}
	got := read(t, filepath.Join(root, "src/main/java/com/foo/Baz.java"))
	if strings.Contains(got, "Bar") {
		t.Errorf("Bar still present:\n%s", got)
	}
	if !strings.Contains(got, "class Baz") || !strings.Contains(got, "Baz self()") || !strings.Contains(got, "new Baz()") {
		t.Errorf("missing rewrites:\n%s", got)
	}
}

func TestRenameClass_ExplicitImporter(t *testing.T) {
	p, root := mkProject(t, map[string]string{
		"build.gradle": "",
		"src/main/java/com/foo/Bar.java": `package com.foo;
public class Bar {}
`,
		"src/main/java/app/User.java": `package app;

import com.foo.Bar;

public class User {
    Bar field;
    Bar make() { return new Bar(); }
}
`,
	})
	idx := buildIndex(t, p)
	set, err := Plan(Request{FQN: "com.foo.Bar", NewName: "Baz", Index: idx})
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if err := set.Apply(); err != nil {
		t.Fatal(err)
	}
	user := read(t, filepath.Join(root, "src/main/java/app/User.java"))
	if !strings.Contains(user, "import com.foo.Baz;") {
		t.Errorf("import not rewritten:\n%s", user)
	}
	if strings.Contains(user, "Bar") {
		t.Errorf("Bar still in User.java:\n%s", user)
	}
}

func TestRenameClass_WildcardImporter(t *testing.T) {
	p, root := mkProject(t, map[string]string{
		"build.gradle": "",
		"src/main/java/com/foo/Bar.java": `package com.foo;
public class Bar {}
`,
		"src/main/java/app/User.java": `package app;

import com.foo.*;

public class User {
    Bar b;
}
`,
	})
	idx := buildIndex(t, p)
	set, err := Plan(Request{FQN: "com.foo.Bar", NewName: "Baz", Index: idx})
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if err := set.Apply(); err != nil {
		t.Fatal(err)
	}
	user := read(t, filepath.Join(root, "src/main/java/app/User.java"))
	// The wildcard import stays as-is; only the field type changes.
	if !strings.Contains(user, "import com.foo.*;") {
		t.Errorf("wildcard import should stay:\n%s", user)
	}
	if !strings.Contains(user, "Baz b;") {
		t.Errorf("Bar field not renamed:\n%s", user)
	}
}

func TestRenameClass_ConflictingExplicitImportIsSkipped(t *testing.T) {
	// User has `import com.other.Bar` which shadows the wildcard.
	// References to Bar in User.java are com.other.Bar — must NOT rename.
	p, root := mkProject(t, map[string]string{
		"build.gradle": "",
		"src/main/java/com/foo/Bar.java": `package com.foo;
public class Bar {}
`,
		"src/main/java/com/other/Bar.java": `package com.other;
public class Bar {}
`,
		"src/main/java/app/User.java": `package app;

import com.foo.*;
import com.other.Bar;

public class User {
    Bar b;
}
`,
	})
	idx := buildIndex(t, p)
	set, err := Plan(Request{FQN: "com.foo.Bar", NewName: "Baz", Index: idx})
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if err := set.Apply(); err != nil {
		t.Fatal(err)
	}
	user := read(t, filepath.Join(root, "src/main/java/app/User.java"))
	// com.other.Bar should be untouched in both the import and the field.
	if !strings.Contains(user, "import com.other.Bar;") {
		t.Errorf("com.other.Bar import was modified:\n%s", user)
	}
	if !strings.Contains(user, "Bar b;") {
		t.Errorf("field reference to com.other.Bar was modified:\n%s", user)
	}
	// com.foo.Bar renamed in declaration file (and file renamed).
	fooBar := read(t, filepath.Join(root, "src/main/java/com/foo/Baz.java"))
	if !strings.Contains(fooBar, "class Baz") {
		t.Errorf("com.foo.Bar declaration not renamed:\n%s", fooBar)
	}
}

func TestRenameClass_SamePackage(t *testing.T) {
	p, root := mkProject(t, map[string]string{
		"build.gradle": "",
		"src/main/java/com/foo/Bar.java": `package com.foo;
public class Bar {}
`,
		"src/main/java/com/foo/Sibling.java": `package com.foo;
public class Sibling { Bar b; }
`,
		"src/main/java/elsewhere/Unrelated.java": `package elsewhere;
public class Unrelated { /* this Bar is undefined here */ }
`,
	})
	idx := buildIndex(t, p)
	set, err := Plan(Request{FQN: "com.foo.Bar", NewName: "Baz", Index: idx})
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if err := set.Apply(); err != nil {
		t.Fatal(err)
	}
	sib := read(t, filepath.Join(root, "src/main/java/com/foo/Sibling.java"))
	if !strings.Contains(sib, "Baz b;") {
		t.Errorf("same-package reference not renamed:\n%s", sib)
	}
	other := read(t, filepath.Join(root, "src/main/java/elsewhere/Unrelated.java"))
	if !strings.Contains(other, "Unrelated") {
		t.Errorf("elsewhere file content corrupted:\n%s", other)
	}
}

func TestRenameClass_StaticImport(t *testing.T) {
	p, root := mkProject(t, map[string]string{
		"build.gradle": "",
		"src/main/java/com/foo/Bar.java": `package com.foo;
public class Bar { public static int X = 1; public static void m() {} }
`,
		"src/main/java/a/UsesMember.java": `package a;

import static com.foo.Bar.m;

public class UsesMember { void f() { m(); } }
`,
		"src/main/java/b/UsesStar.java": `package b;

import static com.foo.Bar.*;

public class UsesStar { void f() { m(); } }
`,
	})
	idx := buildIndex(t, p)
	set, err := Plan(Request{FQN: "com.foo.Bar", NewName: "Baz", Index: idx})
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if err := set.Apply(); err != nil {
		t.Fatal(err)
	}
	m := read(t, filepath.Join(root, "src/main/java/a/UsesMember.java"))
	if !strings.Contains(m, "import static com.foo.Baz.m;") {
		t.Errorf("static member import not rewritten:\n%s", m)
	}
	s := read(t, filepath.Join(root, "src/main/java/b/UsesStar.java"))
	if !strings.Contains(s, "import static com.foo.Baz.*;") {
		t.Errorf("static wildcard import not rewritten:\n%s", s)
	}
}

func TestRenameClass_MultiModuleGradle(t *testing.T) {
	p, root := mkProject(t, map[string]string{
		"settings.gradle":  "include ':app', ':lib'",
		"app/build.gradle": "",
		"lib/build.gradle": "",
		"lib/src/main/java/com/foo/Bar.java": `package com.foo;
public class Bar {}
`,
		"app/src/main/java/app/User.java": `package app;
import com.foo.Bar;
public class User { Bar b; }
`,
	})
	idx := buildIndex(t, p)
	set, err := Plan(Request{FQN: "com.foo.Bar", NewName: "Baz", Index: idx})
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	files := set.Files()
	sort.Strings(files)
	if len(files) != 2 {
		t.Fatalf("modified files = %v, want 2", files)
	}
	if err := set.Apply(); err != nil {
		t.Fatal(err)
	}
	bar := read(t, filepath.Join(root, "lib/src/main/java/com/foo/Baz.java"))
	if !strings.Contains(bar, "class Baz") {
		t.Errorf("declaration not renamed:\n%s", bar)
	}
	user := read(t, filepath.Join(root, "app/src/main/java/app/User.java"))
	if !strings.Contains(user, "import com.foo.Baz;") || !strings.Contains(user, "Baz b;") {
		t.Errorf("user file not updated:\n%s", user)
	}
}

func TestRenameClass_UnrelatedSameNameUntouched(t *testing.T) {
	// com.foo.Bar exists; com.other.Bar also exists. Renaming com.foo.Bar
	// must NOT touch com.other.Bar's declaration.
	p, root := mkProject(t, map[string]string{
		"build.gradle":                     "",
		"src/main/java/com/foo/Bar.java":   "package com.foo; public class Bar {}\n",
		"src/main/java/com/other/Bar.java": "package com.other; public class Bar { Bar self() { return this; } }\n",
	})
	idx := buildIndex(t, p)
	if _, err := Plan(Request{FQN: "com.foo.Bar", NewName: "Baz", Index: idx}); err != nil {
		t.Fatalf("Plan: %v", err)
	}
	other := read(t, filepath.Join(root, "src/main/java/com/other/Bar.java"))
	if !strings.Contains(other, "public class Bar") {
		t.Errorf("com.other.Bar must remain untouched:\n%s", other)
	}
}

func TestRenameClass_UnknownFQN(t *testing.T) {
	p, _ := mkProject(t, map[string]string{
		"build.gradle":                   "",
		"src/main/java/com/foo/Bar.java": "package com.foo; public class Bar {}\n",
	})
	idx := buildIndex(t, p)
	_, err := Plan(Request{FQN: "com.foo.Ghost", NewName: "Phantom", Index: idx})
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "did you mean") && !strings.Contains(err.Error(), "no class") {
		t.Errorf("err = %v", err)
	}
}

func TestRenameClass_SuggestsCandidate(t *testing.T) {
	p, _ := mkProject(t, map[string]string{
		"build.gradle":                 "",
		"src/main/java/com/a/Bar.java": "package com.a; public class Bar {}\n",
		"src/main/java/com/b/Bar.java": "package com.b; public class Bar {}\n",
	})
	idx := buildIndex(t, p)
	_, err := Plan(Request{FQN: "com.wrong.Bar", NewName: "Baz", Index: idx})
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "com.a.Bar") || !strings.Contains(err.Error(), "com.b.Bar") {
		t.Errorf("error should list candidates: %v", err)
	}
}

func TestRenameClass_InvalidName(t *testing.T) {
	p, _ := mkProject(t, map[string]string{
		"build.gradle":                   "",
		"src/main/java/com/foo/Bar.java": "package com.foo; public class Bar {}\n",
	})
	idx := buildIndex(t, p)
	_, err := Plan(Request{FQN: "com.foo.Bar", NewName: "1Bad", Index: idx})
	if err == nil || !strings.Contains(err.Error(), "not a valid") {
		t.Errorf("err = %v", err)
	}
}

func TestRenameClass_DoesNotMatchSubstring(t *testing.T) {
	// Renaming Bar must NOT touch BarBaz or PreBar.
	p, root := mkProject(t, map[string]string{
		"build.gradle": "",
		"src/main/java/com/foo/Bar.java": `package com.foo;
public class Bar {}
`,
		"src/main/java/com/foo/BarBaz.java": `package com.foo;
public class BarBaz {}
`,
		"src/main/java/com/foo/PreBar.java": `package com.foo;
public class PreBar { Bar b; BarBaz bb; PreBar self; }
`,
	})
	idx := buildIndex(t, p)
	set, err := Plan(Request{FQN: "com.foo.Bar", NewName: "Q", Index: idx})
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if err := set.Apply(); err != nil {
		t.Fatal(err)
	}
	pre := read(t, filepath.Join(root, "src/main/java/com/foo/PreBar.java"))
	if !strings.Contains(pre, "Q b;") {
		t.Errorf("Bar should be renamed in PreBar.java:\n%s", pre)
	}
	if !strings.Contains(pre, "BarBaz bb;") || !strings.Contains(pre, "PreBar self;") {
		t.Errorf("substring matches were renamed:\n%s", pre)
	}
}
