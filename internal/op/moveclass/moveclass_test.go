package moveclass

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/serfom256/refactorio/internal/index"
	"github.com/serfom256/refactorio/internal/project"
)

func mk(t *testing.T, files map[string]string) (*index.Index, string) {
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
	idx, err := index.Build(p, ".java")
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

func TestMoveClass_DeclarationFile(t *testing.T) {
	idx, root := mk(t, map[string]string{
		"build.gradle": "",
		"src/main/java/com/foo/Bar.java": `package com.foo;

public class Bar { int x; }
`,
	})
	set, err := Plan(Request{FQN: "com.foo.Bar", NewPackage: "com.qux", Index: idx})
	if err != nil {
		t.Fatal(err)
	}
	if err := set.Apply(); err != nil {
		t.Fatal(err)
	}
	// Old file must be gone.
	if _, err := os.Stat(filepath.Join(root, "src/main/java/com/foo/Bar.java")); !os.IsNotExist(err) {
		t.Errorf("old file still exists: %v", err)
	}
	// New file must exist with rewritten package.
	newFile := filepath.Join(root, "src/main/java/com/qux/Bar.java")
	got := read(t, newFile)
	if !strings.Contains(got, "package com.qux;") {
		t.Errorf("package not rewritten:\n%s", got)
	}
	if !strings.Contains(got, "public class Bar") {
		t.Errorf("class body changed:\n%s", got)
	}
}

func TestMoveClass_ExplicitImporter(t *testing.T) {
	idx, root := mk(t, map[string]string{
		"build.gradle": "",
		"src/main/java/com/foo/Bar.java": `package com.foo;
public class Bar {}
`,
		"src/main/java/com/app/User.java": `package com.app;
import com.foo.Bar;
public class User { Bar b; }
`,
	})
	set, err := Plan(Request{FQN: "com.foo.Bar", NewPackage: "com.qux", Index: idx})
	if err != nil {
		t.Fatal(err)
	}
	if err := set.Apply(); err != nil {
		t.Fatal(err)
	}
	user := read(t, filepath.Join(root, "src/main/java/com/app/User.java"))
	if !strings.Contains(user, "import com.qux.Bar;") {
		t.Errorf("import not rewritten:\n%s", user)
	}
}

func TestMoveClass_SamePackageGetsImportAdded(t *testing.T) {
	// Sibling lives in com.foo and used Bar with same-package access.
	// After moving Bar to com.qux, Sibling must `import com.qux.Bar`.
	idx, root := mk(t, map[string]string{
		"build.gradle": "",
		"src/main/java/com/foo/Bar.java": `package com.foo;
public class Bar {}
`,
		"src/main/java/com/foo/Sibling.java": `package com.foo;
public class Sibling { Bar b; }
`,
	})
	set, err := Plan(Request{FQN: "com.foo.Bar", NewPackage: "com.qux", Index: idx})
	if err != nil {
		t.Fatal(err)
	}
	if err := set.Apply(); err != nil {
		t.Fatal(err)
	}
	sib := read(t, filepath.Join(root, "src/main/java/com/foo/Sibling.java"))
	if !strings.Contains(sib, "import com.qux.Bar;") {
		t.Errorf("sibling missing new import:\n%s", sib)
	}
}

func TestMoveClass_WildcardImporterGetsExplicitAdded(t *testing.T) {
	idx, root := mk(t, map[string]string{
		"build.gradle": "",
		"src/main/java/com/foo/Bar.java": `package com.foo;
public class Bar {}
`,
		"src/main/java/com/app/User.java": `package com.app;
import com.foo.*;
public class User { Bar b; }
`,
	})
	set, err := Plan(Request{FQN: "com.foo.Bar", NewPackage: "com.qux", Index: idx})
	if err != nil {
		t.Fatal(err)
	}
	if err := set.Apply(); err != nil {
		t.Fatal(err)
	}
	user := read(t, filepath.Join(root, "src/main/java/com/app/User.java"))
	// Wildcard should stay; explicit import added.
	if !strings.Contains(user, "import com.foo.*;") {
		t.Errorf("wildcard import was modified:\n%s", user)
	}
	if !strings.Contains(user, "import com.qux.Bar;") {
		t.Errorf("explicit import not added:\n%s", user)
	}
}

func TestMoveClass_FileInNewPackageGetsNothingExtra(t *testing.T) {
	// User in com.qux had `import com.foo.Bar`. After Bar moves to
	// com.qux, the import gets rewritten to `import com.qux.Bar`
	// (technically redundant since they're now in the same package,
	// but harmless and simpler than removing it).
	idx, root := mk(t, map[string]string{
		"build.gradle": "",
		"src/main/java/com/foo/Bar.java": `package com.foo;
public class Bar {}
`,
		"src/main/java/com/qux/User.java": `package com.qux;
import com.foo.Bar;
public class User { Bar b; }
`,
	})
	set, err := Plan(Request{FQN: "com.foo.Bar", NewPackage: "com.qux", Index: idx})
	if err != nil {
		t.Fatal(err)
	}
	if err := set.Apply(); err != nil {
		t.Fatal(err)
	}
	user := read(t, filepath.Join(root, "src/main/java/com/qux/User.java"))
	if !strings.Contains(user, "import com.qux.Bar;") {
		t.Errorf("import not rewritten:\n%s", user)
	}
	// Should NOT contain duplicate imports.
	count := strings.Count(user, "import com.qux.Bar;")
	if count != 1 {
		t.Errorf("got %d imports, want 1:\n%s", count, user)
	}
}

func TestMoveClass_StaticImports(t *testing.T) {
	idx, root := mk(t, map[string]string{
		"build.gradle": "",
		"src/main/java/com/foo/Bar.java": `package com.foo;
public class Bar { public static int X = 1; }
`,
		"src/main/java/com/app/UseMember.java": `package com.app;
import static com.foo.Bar.X;
public class UseMember { int y = X; }
`,
		"src/main/java/com/app/UseStar.java": `package com.app;
import static com.foo.Bar.*;
public class UseStar { int y = X; }
`,
	})
	set, err := Plan(Request{FQN: "com.foo.Bar", NewPackage: "com.qux", Index: idx})
	if err != nil {
		t.Fatal(err)
	}
	if err := set.Apply(); err != nil {
		t.Fatal(err)
	}
	m := read(t, filepath.Join(root, "src/main/java/com/app/UseMember.java"))
	if !strings.Contains(m, "import static com.qux.Bar.X;") {
		t.Errorf("static member import not rewritten:\n%s", m)
	}
	s := read(t, filepath.Join(root, "src/main/java/com/app/UseStar.java"))
	if !strings.Contains(s, "import static com.qux.Bar.*;") {
		t.Errorf("static wildcard import not rewritten:\n%s", s)
	}
}

func TestMoveClass_MultiModuleGradle(t *testing.T) {
	idx, root := mk(t, map[string]string{
		"settings.gradle":                "include ':app', ':lib'",
		"app/build.gradle":               "",
		"lib/build.gradle":               "",
		"lib/src/main/java/com/foo/Bar.java": "package com.foo; public class Bar {}\n",
		"app/src/main/java/com/app/User.java": `package com.app;
import com.foo.Bar;
public class User { Bar b; }
`,
	})
	set, err := Plan(Request{FQN: "com.foo.Bar", NewPackage: "com.qux", Index: idx})
	if err != nil {
		t.Fatal(err)
	}
	if err := set.Apply(); err != nil {
		t.Fatal(err)
	}
	// Old file gone.
	if _, err := os.Stat(filepath.Join(root, "lib/src/main/java/com/foo/Bar.java")); !os.IsNotExist(err) {
		t.Errorf("old file still exists: %v", err)
	}
	// New file in the SAME source root (lib/), under the new package directory.
	newFile := filepath.Join(root, "lib/src/main/java/com/qux/Bar.java")
	if _, err := os.Stat(newFile); err != nil {
		t.Errorf("new file missing: %v", err)
	}
	user := read(t, filepath.Join(root, "app/src/main/java/com/app/User.java"))
	if !strings.Contains(user, "import com.qux.Bar;") {
		t.Errorf("import not rewritten:\n%s", user)
	}
}

func TestMoveClass_TargetAlreadyInNewPackage(t *testing.T) {
	idx, _ := mk(t, map[string]string{
		"build.gradle":                   "",
		"src/main/java/com/foo/Bar.java": "package com.foo; public class Bar {}\n",
	})
	_, err := Plan(Request{FQN: "com.foo.Bar", NewPackage: "com.foo", Index: idx})
	if err == nil || !strings.Contains(err.Error(), "already") {
		t.Errorf("err = %v", err)
	}
}

func TestMoveClass_InvalidPackage(t *testing.T) {
	idx, _ := mk(t, map[string]string{
		"build.gradle":                   "",
		"src/main/java/com/foo/Bar.java": "package com.foo; public class Bar {}\n",
	})
	_, err := Plan(Request{FQN: "com.foo.Bar", NewPackage: "com.1foo", Index: idx})
	if err == nil || !strings.Contains(err.Error(), "valid") {
		t.Errorf("err = %v", err)
	}
}

func TestMoveClass_UnknownClass(t *testing.T) {
	idx, _ := mk(t, map[string]string{
		"build.gradle":                   "",
		"src/main/java/com/foo/Bar.java": "package com.foo; public class Bar {}\n",
	})
	_, err := Plan(Request{FQN: "com.ghost.X", NewPackage: "com.qux", Index: idx})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestComputeSourceRoot(t *testing.T) {
	cases := []struct {
		file, pkg, className, want string
		ok                         bool
	}{
		{"/abs/src/main/java/com/foo/Bar.java", "com.foo", "Bar", "/abs/src/main/java", true},
		{"/abs/src/main/java/lone/Solo.java", "lone", "Solo", "/abs/src/main/java", true},
		{"/abs/Bar.java", "", "Bar", "/abs", true},
		{"/abs/src/wrong/Bar.java", "com.foo", "Bar", "", false},
	}
	for _, c := range cases {
		got, ok := computeSourceRoot(c.file, c.pkg, c.className)
		if ok != c.ok || got != c.want {
			t.Errorf("computeSourceRoot(%q, %q, %q) = %q, %v; want %q, %v",
				c.file, c.pkg, c.className, got, ok, c.want, c.ok)
		}
	}
}
