package index

import (
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/serfom256/refactorio/internal/project"
)

func mkProject(t *testing.T, files map[string]string) *project.Project {
	t.Helper()
	root := t.TempDir()
	for rel, body := range files {
		full := filepath.Join(root, rel)
		if strings.HasSuffix(rel, "/") {
			if err := os.MkdirAll(full, 0o755); err != nil {
				t.Fatal(err)
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	p, err := project.DetectFrom(root)
	if err != nil {
		t.Fatalf("project.DetectFrom: %v", err)
	}
	return p
}

func TestBuild_SimpleSingleModule(t *testing.T) {
	p := mkProject(t, map[string]string{
		"build.gradle": "",
		"src/main/java/com/foo/Bar.java": `package com.foo;
public class Bar { int x; void m() {} }
`,
		"src/main/java/com/foo/Baz.java": `package com.foo;
public class Baz {}
`,
	})
	idx, err := Build(p, ".java")
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if len(idx.Files()) != 2 {
		t.Errorf("Files = %d, want 2", len(idx.Files()))
	}
	bar := idx.LookupFQN("com.foo.Bar")
	if bar == nil {
		t.Fatalf("LookupFQN missing com.foo.Bar")
	}
	if bar.Name != "Bar" || bar.Package != "com.foo" {
		t.Errorf("Bar resolved as %+v", bar)
	}
	if got := idx.LookupType("Bar"); len(got) != 1 || got[0].FQN != "com.foo.Bar" {
		t.Errorf("LookupType(Bar) = %v", got)
	}
}

func TestBuild_SameSimpleNameAcrossPackages(t *testing.T) {
	p := mkProject(t, map[string]string{
		"build.gradle":              "",
		"src/main/java/a/Util.java": "package a; public class Util {}\n",
		"src/main/java/b/Util.java": "package b; public class Util {}\n",
	})
	idx, _ := Build(p, ".java")
	got := idx.LookupType("Util")
	if len(got) != 2 {
		t.Fatalf("LookupType(Util) = %d, want 2", len(got))
	}
	fqns := []string{got[0].FQN, got[1].FQN}
	sort.Strings(fqns)
	if !reflect.DeepEqual(fqns, []string{"a.Util", "b.Util"}) {
		t.Errorf("FQNs = %v", fqns)
	}
}

func TestFilesImporting_ExplicitImport(t *testing.T) {
	p := mkProject(t, map[string]string{
		"build.gradle":                   "",
		"src/main/java/com/foo/Bar.java": "package com.foo; public class Bar {}\n",
		"src/main/java/app/User.java": `package app;
import com.foo.Bar;
public class User { Bar b; }
`,
		"src/main/java/other/Lone.java": "package other; public class Lone {}\n",
	})
	idx, _ := Build(p, ".java")
	got := idx.FilesImporting("com.foo.Bar")
	// Should include: the declaring file + the importing file. NOT Lone.
	if len(got) != 2 {
		t.Fatalf("FilesImporting(com.foo.Bar) = %v, want 2", got)
	}
	for _, g := range got {
		if strings.Contains(g, "Lone.java") {
			t.Errorf("Lone.java should not appear (no import): %v", got)
		}
	}
}

func TestFilesImporting_WildcardImport(t *testing.T) {
	p := mkProject(t, map[string]string{
		"build.gradle":                   "",
		"src/main/java/com/foo/Bar.java": "package com.foo; public class Bar {}\n",
		"src/main/java/app/User.java": `package app;
import com.foo.*;
public class User { Bar b; }
`,
	})
	idx, _ := Build(p, ".java")
	got := idx.FilesImporting("com.foo.Bar")
	if len(got) != 2 {
		t.Fatalf("FilesImporting = %v, want 2 (decl + wildcard importer)", got)
	}
}

func TestFilesImporting_SamePackage(t *testing.T) {
	// A file in the same package as Bar can reference it without an import.
	p := mkProject(t, map[string]string{
		"build.gradle":                       "",
		"src/main/java/com/foo/Bar.java":     "package com.foo; public class Bar {}\n",
		"src/main/java/com/foo/Sibling.java": "package com.foo; public class Sibling { Bar b; }\n",
		"src/main/java/elsewhere/X.java":     "package elsewhere; public class X {}\n",
	})
	idx, _ := Build(p, ".java")
	got := idx.FilesImporting("com.foo.Bar")
	// Should be Bar.java + Sibling.java (same package). X.java excluded.
	if len(got) != 2 {
		t.Fatalf("FilesImporting = %v, want 2", got)
	}
	for _, g := range got {
		if strings.Contains(g, "elsewhere") {
			t.Errorf("elsewhere/X.java should not appear: %v", got)
		}
	}
}

func TestFilesImporting_StaticImports(t *testing.T) {
	// `import static com.foo.Bar.method` and `import static com.foo.Bar.*`
	// both mean the file references the Bar TYPE.
	p := mkProject(t, map[string]string{
		"build.gradle": "",
		"src/main/java/com/foo/Bar.java": `package com.foo;
public class Bar { public static int x; public static void m() {} }
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
	idx, _ := Build(p, ".java")
	got := idx.FilesImporting("com.foo.Bar")
	// Decl + both static importers = 3.
	if len(got) != 3 {
		t.Fatalf("FilesImporting = %v, want 3", got)
	}
}

func TestFilesImporting_MultiModuleGradle(t *testing.T) {
	p := mkProject(t, map[string]string{
		"settings.gradle":                    "include ':app', ':lib'",
		"app/build.gradle":                   "",
		"lib/build.gradle":                   "",
		"lib/src/main/java/com/foo/Bar.java": "package com.foo; public class Bar {}\n",
		"app/src/main/java/app/User.java": `package app;
import com.foo.Bar;
public class User { Bar b; }
`,
	})
	idx, _ := Build(p, ".java")
	got := idx.FilesImporting("com.foo.Bar")
	if len(got) != 2 {
		t.Fatalf("FilesImporting = %v, want 2", got)
	}
	// Confirm both modules contributed.
	var inApp, inLib bool
	for _, g := range got {
		if strings.Contains(g, "/app/") {
			inApp = true
		}
		if strings.Contains(g, "/lib/") {
			inLib = true
		}
	}
	if !inApp || !inLib {
		t.Errorf("expected files from both modules, got %v", got)
	}
}

func TestBuild_RecordsParseErrors(t *testing.T) {
	// One malformed file; the index should still build, with that file
	// reported in Errors() but the lexer is tolerant enough to extract
	// partial symbols, so it MAY still appear in Files() without error.
	// What we test here: index never returns an error from Build itself.
	p := mkProject(t, map[string]string{
		"build.gradle":              "",
		"src/main/java/Good.java":   "public class Good {}\n",
		"src/main/java/Broken.java": "class\n", // syntactically incomplete
	})
	idx, err := Build(p, ".java")
	if err != nil {
		t.Fatalf("Build returned %v; expected nil even with broken file", err)
	}
	if idx.LookupFQN("Good") == nil {
		t.Errorf("Good should still be indexed")
	}
}

func TestErrors_ReturnsOnlyFailures(t *testing.T) {
	// Force a read error by having SourceFiles enumerate a file that
	// gets deleted between enumeration and read. Synthesise via a
	// non-existent symlink to simplify the test.
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "src/main/java"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "build.gradle"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "src/main/java/Ok.java"), []byte("class Ok {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Dangling symlink looks like a .java file but isn't readable.
	if err := os.Symlink("/nonexistent/Ghost.java", filepath.Join(root, "src/main/java/Ghost.java")); err != nil {
		t.Skip("symlink not supported")
	}
	p, _ := project.DetectFrom(root)
	idx, err := Build(p, ".java")
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	errs := idx.Errors()
	if len(errs) != 1 {
		t.Errorf("Errors = %v, want exactly 1", errs)
	}
	for path := range errs {
		if !strings.Contains(path, "Ghost.java") {
			t.Errorf("error path = %s, want Ghost.java", path)
		}
	}
}
