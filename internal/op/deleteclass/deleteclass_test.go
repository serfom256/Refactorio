package deleteclass

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

func TestDeleteClass_RemovesFile(t *testing.T) {
	idx, root := mk(t, map[string]string{
		"build.gradle":                   "",
		"src/main/java/com/foo/Bar.java": "package com.foo; public class Bar {}\n",
	})
	result, err := Plan(Request{FQN: "com.foo.Bar", Index: idx})
	if err != nil {
		t.Fatal(err)
	}
	if err := result.Set.Apply(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "src/main/java/com/foo/Bar.java")); !os.IsNotExist(err) {
		t.Errorf("file still exists: %v", err)
	}
}

func TestDeleteClass_RemovesExplicitImport(t *testing.T) {
	idx, root := mk(t, map[string]string{
		"build.gradle":                   "",
		"src/main/java/com/foo/Bar.java": "package com.foo; public class Bar {}\n",
		"src/main/java/com/app/User.java": `package com.app;
import com.foo.Bar;
public class User { Bar b; }
`,
	})
	result, err := Plan(Request{FQN: "com.foo.Bar", Index: idx})
	if err != nil {
		t.Fatal(err)
	}
	if err := result.Set.Apply(); err != nil {
		t.Fatal(err)
	}
	user := read(t, filepath.Join(root, "src/main/java/com/app/User.java"))
	if strings.Contains(user, "import com.foo.Bar;") {
		t.Errorf("import not removed:\n%s", user)
	}
}

func TestDeleteClass_RemovesStaticImport(t *testing.T) {
	idx, root := mk(t, map[string]string{
		"build.gradle":                   "",
		"src/main/java/com/foo/Bar.java": "package com.foo; public class Bar { public static int X; }\n",
		"src/main/java/com/app/User.java": `package com.app;
import static com.foo.Bar.X;
public class User { int x = X; }
`,
	})
	result, err := Plan(Request{FQN: "com.foo.Bar", Index: idx})
	if err != nil {
		t.Fatal(err)
	}
	if err := result.Set.Apply(); err != nil {
		t.Fatal(err)
	}
	user := read(t, filepath.Join(root, "src/main/java/com/app/User.java"))
	if strings.Contains(user, "import static") {
		t.Errorf("static import not removed:\n%s", user)
	}
}

func TestDeleteClass_KeepsWildcardImport(t *testing.T) {
	idx, root := mk(t, map[string]string{
		"build.gradle":                   "",
		"src/main/java/com/foo/Bar.java": "package com.foo; public class Bar {}\n",
		"src/main/java/com/app/User.java": `package com.app;
import com.foo.*;
public class User { Bar b; }
`,
	})
	result, err := Plan(Request{FQN: "com.foo.Bar", Index: idx})
	if err != nil {
		t.Fatal(err)
	}
	if err := result.Set.Apply(); err != nil {
		t.Fatal(err)
	}
	user := read(t, filepath.Join(root, "src/main/java/com/app/User.java"))
	if !strings.Contains(user, "import com.foo.*;") {
		t.Errorf("wildcard import should stay:\n%s", user)
	}
}

func TestDeleteClass_ReportsFilesWithReferences(t *testing.T) {
	idx, _ := mk(t, map[string]string{
		"build.gradle":                   "",
		"src/main/java/com/foo/Bar.java": "package com.foo; public class Bar {}\n",
		"src/main/java/com/app/User.java": `package com.app;
import com.foo.Bar;
public class User { Bar b; }
`,
		"src/main/java/com/app/NoRef.java": `package com.app;
public class NoRef {}
`,
	})
	result, err := Plan(Request{FQN: "com.foo.Bar", Index: idx})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.FilesWithReferences) != 1 {
		t.Errorf("FilesWithReferences = %v, want 1", result.FilesWithReferences)
	}
}

func TestDeleteClass_UnknownFQN(t *testing.T) {
	idx, _ := mk(t, map[string]string{
		"build.gradle":                   "",
		"src/main/java/com/foo/Bar.java": "package com.foo; public class Bar {}\n",
	})
	_, err := Plan(Request{FQN: "com.foo.Ghost", Index: idx})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestDeleteClass_MultiModule(t *testing.T) {
	idx, root := mk(t, map[string]string{
		"settings.gradle":                    "include ':app', ':lib'",
		"app/build.gradle":                   "",
		"lib/build.gradle":                   "",
		"lib/src/main/java/com/foo/Bar.java": "package com.foo; public class Bar {}\n",
		"app/src/main/java/com/app/User.java": `package com.app;
import com.foo.Bar;
public class User { Bar b; }
`,
	})
	result, err := Plan(Request{FQN: "com.foo.Bar", Index: idx})
	if err != nil {
		t.Fatal(err)
	}
	if err := result.Set.Apply(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "lib/src/main/java/com/foo/Bar.java")); !os.IsNotExist(err) {
		t.Errorf("file still exists")
	}
	user := read(t, filepath.Join(root, "app/src/main/java/com/app/User.java"))
	if strings.Contains(user, "import com.foo.Bar;") {
		t.Errorf("import not cleaned:\n%s", user)
	}
}

// A file whose only mention of the deleted class is the import we are
// about to remove needs no manual cleanup, so it must not be reported.
func TestDeleteClass_DeadImportOnlyIsNotReported(t *testing.T) {
	idx, _ := mk(t, map[string]string{
		"build.gradle":                   "",
		"src/main/java/com/foo/Bar.java": "package com.foo; public class Bar {}\n",
		"src/main/java/com/app/User.java": `package com.app;
import com.foo.Bar;
public class User { int n; }
`,
	})
	result, err := Plan(Request{FQN: "com.foo.Bar", Index: idx})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.FilesWithReferences) != 0 {
		t.Errorf("FilesWithReferences = %v, want none (import was removed)", result.FilesWithReferences)
	}
}
