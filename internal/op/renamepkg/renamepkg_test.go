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
		"build.gradle":                   "",
		"src/main/java/com/foo/Bar.java": "package com.foo; public class Bar {}\n",
		"src/main/java/com/foo/Baz.java": "package com.foo; public class Baz {}\n",
	})
	set, err := Plan(Request{OldPackage: "com.foo", NewPackage: "com.qux", Index: idx})
	if err != nil {
		t.Fatal(err)
	}
	if err := set.Apply(); err != nil {
		t.Fatal(err)
	}
	bar := read(t, filepath.Join(root, "src/main/java/com/qux/Bar.java"))
	if !strings.Contains(bar, "package com.qux;") {
		t.Errorf("Bar package not updated:\n%s", bar)
	}
	baz := read(t, filepath.Join(root, "src/main/java/com/qux/Baz.java"))
	if !strings.Contains(baz, "package com.qux;") {
		t.Errorf("Baz package not updated:\n%s", baz)
	}
	if _, err := os.Stat(filepath.Join(root, "src/main/java/com/foo/Bar.java")); !os.IsNotExist(err) {
		t.Errorf("old Bar.java still exists")
	}
}

func TestRenamePkg_ImportsRewritten(t *testing.T) {
	idx, root := mk(t, map[string]string{
		"build.gradle":                   "",
		"src/main/java/com/foo/Bar.java": "package com.foo; public class Bar {}\n",
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
		"build.gradle":                   "",
		"src/main/java/com/foo/Bar.java": "package com.foo; public class Bar {}\n",
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
		"build.gradle":                   "",
		"src/main/java/com/foo/Bar.java": "package com.foo; public class Bar { public static int X; }\n",
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
		"settings.gradle":                    "include ':app', ':lib'",
		"app/build.gradle":                   "",
		"lib/build.gradle":                   "",
		"lib/src/main/java/com/foo/Bar.java": "package com.foo; public class Bar {}\n",
		"lib/src/main/java/com/foo/Baz.java": "package com.foo; public class Baz {}\n",
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
		"build.gradle":                        "",
		"src/main/java/com/foo/Bar.java":      "package com.foo; public class Bar {}\n",
		"src/main/java/com/foo/sub/Deep.java": "package com.foo.sub; public class Deep {}\n",
	})
	set, err := Plan(Request{OldPackage: "com.foo", NewPackage: "com.qux", Index: idx})
	if err != nil {
		t.Fatal(err)
	}
	if err := set.Apply(); err != nil {
		t.Fatal(err)
	}
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

func TestRenamePkg_MovesPackageInfo(t *testing.T) {
	idx, root := mk(t, map[string]string{
		"build.gradle": "",
		"src/main/java/com/foo/package-info.java": "@NullMarked\npackage com.foo;\n\n" +
			"import org.jspecify.annotations.NullMarked;\n",
		"src/main/java/com/foo/Bar.java": "package com.foo;\npublic class Bar {}\n",
	})
	set, err := Plan(Request{OldPackage: "com.foo", NewPackage: "com.baz", Index: idx})
	if err != nil {
		t.Fatal(err)
	}

	pkgInfo := filepath.Join(root, "src/main/java/com/foo/package-info.java")
	var moved bool
	for _, m := range set.Moves() {
		if m.From == pkgInfo {
			moved = true
			want := filepath.Join(root, "src/main/java/com/baz/package-info.java")
			if m.To != want {
				t.Errorf("package-info moved to %s, want %s", m.To, want)
			}
		}
	}
	if !moved {
		t.Error("package-info.java was not moved with the rest of the package")
	}

	if err := set.Apply(); err != nil {
		t.Fatal(err)
	}
	got := read(t, filepath.Join(root, "src/main/java/com/baz/package-info.java"))
	if !strings.Contains(got, "package com.baz;") {
		t.Errorf("package declaration not rewritten:\n%s", got)
	}
	if !strings.Contains(got, "@NullMarked") || !strings.Contains(got, "org.jspecify.annotations.NullMarked") {
		t.Errorf("package-level annotation lost:\n%s", got)
	}
	if _, err := os.Stat(filepath.Join(root, "src/main/java/com/foo/package-info.java")); err == nil {
		t.Error("package-info.java left behind in the old package directory")
	}
}

func TestRenamePkg_AnnotationOnlyPackage(t *testing.T) {
	idx, root := mk(t, map[string]string{
		"build.gradle": "",
		"src/main/java/com/foo/anno/package-info.java": "@ParametersAreNonnullByDefault\n" +
			"package com.foo.anno;\n\nimport javax.annotation.ParametersAreNonnullByDefault;\n",
	})
	set, err := Plan(Request{OldPackage: "com.foo.anno", NewPackage: "com.foo.annotations", Index: idx})
	if err != nil {
		t.Fatalf("renaming an annotation-only package must work: %v", err)
	}
	if err := set.Apply(); err != nil {
		t.Fatal(err)
	}
	got := read(t, filepath.Join(root, "src/main/java/com/foo/annotations/package-info.java"))
	if !strings.Contains(got, "package com.foo.annotations;") {
		t.Errorf("package declaration not rewritten:\n%s", got)
	}
}

func TestRenamePkg_PlanIsDeterministic(t *testing.T) {
	files := map[string]string{
		"build.gradle":                   "",
		"src/main/java/com/foo/A.java":   "package com.foo;\npublic class A {}\n",
		"src/main/java/com/foo/B.java":   "package com.foo;\npublic class B {}\n",
		"src/main/java/com/foo/C.java":   "package com.foo;\npublic class C {}\n",
		"src/main/java/com/app/Use.java": "package com.app;\nimport com.foo.A;\npublic class Use { A a; }\n",
	}
	var first string
	for i := 0; i < 5; i++ {
		idx, root := mk(t, files)
		set, err := Plan(Request{OldPackage: "com.foo", NewPackage: "com.baz", Index: idx})
		if err != nil {
			t.Fatal(err)
		}
		var b strings.Builder
		for _, m := range set.Moves() {
			b.WriteString(strings.TrimPrefix(m.From, root))
			b.WriteString(" -> ")
			b.WriteString(strings.TrimPrefix(m.To, root))
			b.WriteString("\n")
		}
		if i == 0 {
			first = b.String()
			continue
		}
		if b.String() != first {
			t.Fatalf("plan differs between runs:\nfirst:\n%s\nrun %d:\n%s", first, i, b.String())
		}
	}
}
