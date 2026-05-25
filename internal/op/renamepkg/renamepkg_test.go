package renamepkg

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

func TestRenamePkg_PackageDecls(t *testing.T) {
	idx, root := mk(t, map[string]string{
		"build.gradle":                      "",
		"src/main/java/com/foo/Bar.java":    "package com.foo; public class Bar {}\n",
		"src/main/java/com/foo/Baz.java":    "package com.foo; public class Baz {}\n",
	})
	set, err := Plan(Request{OldPackage: "com.foo", NewPackage: "com.qux", Index: idx})
	if err != nil {
		t.Fatal(err)
	}
	if err := set.Apply(); err != nil {
		t.Fatal(err)
	}
	// Files moved to new directory.
	bar := read(t, filepath.Join(root, "src/main/java/com/qux/Bar.java"))
	if !strings.Contains(bar, "package com.qux;") {
		t.Errorf("Bar package not updated:\n%s", bar)
	}
	baz := read(t, filepath.Join(root, "src/main/java/com/qux/Baz.java"))
	if !strings.Contains(baz, "package com.qux;") {
		t.Errorf("Baz package not updated:\n%s", baz)
	}
	// Old dir should be empty or gone.
	if _, err := os.Stat(filepath.Join(root, "src/main/java/com/foo/Bar.java")); !os.IsNotExist(err) {
		t.Errorf("old Bar.java still exists")
	}
}

func TestRenamePkg_ImportsRewritten(t *testing.T) {
	idx, root := mk(t, map[string]string{
		"build.gradle":                      "",
		"src/main/java/com/foo/Bar.java":    "package com.foo; public class Bar {}\n",
		"src/main/java/com/app/User.java": `package com.app;
import com.foo.Bar;
public class User { Bar b; }
`,
	})
	set, err := Plan(Request{OldPackage: "com.foo", NewPackage: "com.qux", Index: idx})
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

func TestRenamePkg_WildcardImportRewritten(t *testing.T) {
	idx, root := mk(t, map[string]string{
		"build.gradle":                      "",
		"src/main/java/com/foo/Bar.java":    "package com.foo; public class Bar {}\n",
		"src/main/java/com/app/User.java": `package com.app;
import com.foo.*;
public class User { Bar b; }
`,
	})
	set, err := Plan(Request{OldPackage: "com.foo", NewPackage: "com.qux", Index: idx})
	if err != nil {
		t.Fatal(err)
	}
	if err := set.Apply(); err != nil {
		t.Fatal(err)
	}
	user := read(t, filepath.Join(root, "src/main/java/com/app/User.java"))
	if !strings.Contains(user, "import com.qux.*;") {
		t.Errorf("wildcard import not rewritten:\n%s", user)
	}
}

func TestRenamePkg_StaticImportsRewritten(t *testing.T) {
	idx, root := mk(t, map[string]string{
		"build.gradle":                      "",
		"src/main/java/com/foo/Bar.java":    "package com.foo; public class Bar { public static int X; }\n",
		"src/main/java/com/app/User.java": `package com.app;
import static com.foo.Bar.X;
public class User { int x = X; }
`,
	})
	set, err := Plan(Request{OldPackage: "com.foo", NewPackage: "com.qux", Index: idx})
	if err != nil {
		t.Fatal(err)
	}
	if err := set.Apply(); err != nil {
		t.Fatal(err)
	}
	user := read(t, filepath.Join(root, "src/main/java/com/app/User.java"))
	if !strings.Contains(user, "import static com.qux.Bar.X;") {
		t.Errorf("static import not rewritten:\n%s", user)
	}
}

func TestRenamePkg_MultiModule(t *testing.T) {
	idx, root := mk(t, map[string]string{
		"settings.gradle":                            "include ':app', ':lib'",
		"app/build.gradle":                           "",
		"lib/build.gradle":                           "",
		"lib/src/main/java/com/foo/Bar.java":         "package com.foo; public class Bar {}\n",
		"lib/src/main/java/com/foo/Baz.java":         "package com.foo; public class Baz {}\n",
		"app/src/main/java/com/app/User.java": `package com.app;
import com.foo.Bar;
import com.foo.Baz;
public class User { Bar b; Baz z; }
`,
	})
	set, err := Plan(Request{OldPackage: "com.foo", NewPackage: "com.qux", Index: idx})
	if err != nil {
		t.Fatal(err)
	}
	if err := set.Apply(); err != nil {
		t.Fatal(err)
	}
	user := read(t, filepath.Join(root, "app/src/main/java/com/app/User.java"))
	if !strings.Contains(user, "import com.qux.Bar;") || !strings.Contains(user, "import com.qux.Baz;") {
		t.Errorf("imports not rewritten:\n%s", user)
	}
	bar := read(t, filepath.Join(root, "lib/src/main/java/com/qux/Bar.java"))
	if !strings.Contains(bar, "package com.qux;") {
		t.Errorf("Bar package not updated:\n%s", bar)
	}
}

func TestRenamePkg_DoesNotTouchOtherPackages(t *testing.T) {
	idx, root := mk(t, map[string]string{
		"build.gradle":                         "",
		"src/main/java/com/foo/Bar.java":       "package com.foo; public class Bar {}\n",
		"src/main/java/com/foo/sub/Deep.java":  "package com.foo.sub; public class Deep {}\n",
	})
	set, err := Plan(Request{OldPackage: "com.foo", NewPackage: "com.qux", Index: idx})
	if err != nil {
		t.Fatal(err)
	}
	if err := set.Apply(); err != nil {
		t.Fatal(err)
	}
	// com.foo.sub should NOT be renamed — it's a different package.
	deep := read(t, filepath.Join(root, "src/main/java/com/foo/sub/Deep.java"))
	if !strings.Contains(deep, "package com.foo.sub;") {
		t.Errorf("sub-package was incorrectly renamed:\n%s", deep)
	}
}

func TestRenamePkg_NoTypes(t *testing.T) {
	idx, _ := mk(t, map[string]string{
		"build.gradle":                   "",
		"src/main/java/com/foo/Bar.java": "package com.foo; public class Bar {}\n",
	})
	_, err := Plan(Request{OldPackage: "com.ghost", NewPackage: "com.qux", Index: idx})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestRenamePkg_InvalidPackage(t *testing.T) {
	idx, _ := mk(t, map[string]string{
		"build.gradle":                   "",
		"src/main/java/com/foo/Bar.java": "package com.foo; public class Bar {}\n",
	})
	_, err := Plan(Request{OldPackage: "com.foo", NewPackage: "com.1bad", Index: idx})
	if err == nil {
		t.Fatal("expected error")
	}
}
