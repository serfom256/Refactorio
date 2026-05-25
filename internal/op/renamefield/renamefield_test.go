package renamefield

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

func TestRenameField_DeclAndThisAccess(t *testing.T) {
	idx, root := mk(t, map[string]string{
		"build.gradle": "",
		"src/main/java/com/foo/Bar.java": `package com.foo;
public class Bar {
    int count;
    void inc() { this.count = this.count + 1; }
}
`,
	})
	set, err := Plan(Request{ClassFQN: "com.foo.Bar", Field: "count", NewName: "n", Index: idx})
	if err != nil {
		t.Fatal(err)
	}
	if err := set.Apply(); err != nil {
		t.Fatal(err)
	}
	got := read(t, filepath.Join(root, "src/main/java/com/foo/Bar.java"))
	if strings.Contains(got, "count") {
		t.Errorf("count still present:\n%s", got)
	}
}

func TestRenameField_BareReferenceInTargetClass(t *testing.T) {
	// `count++` is a bare identifier reference (not field_access). We
	// rewrite it because the identifier is inside the target class body
	// and there is no local/param shadowing.
	idx, root := mk(t, map[string]string{
		"build.gradle": "",
		"src/main/java/com/foo/Bar.java": `package com.foo;
public class Bar {
    int count;
    void inc() { count = count + 1; }
}
`,
	})
	set, err := Plan(Request{ClassFQN: "com.foo.Bar", Field: "count", NewName: "n", Index: idx})
	if err != nil {
		t.Fatal(err)
	}
	if err := set.Apply(); err != nil {
		t.Fatal(err)
	}
	got := read(t, filepath.Join(root, "src/main/java/com/foo/Bar.java"))
	if strings.Contains(got, "count") {
		t.Errorf("bare reference not renamed:\n%s", got)
	}
}

func TestRenameField_ShadowedByLocalIsSkipped(t *testing.T) {
	// A local var named `count` shadows the field. We conservatively
	// skip bare-identifier rewrites in the file. The declaration and
	// any field_access references still apply.
	idx, root := mk(t, map[string]string{
		"build.gradle": "",
		"src/main/java/com/foo/Bar.java": `package com.foo;
public class Bar {
    int count;
    void shadow() {
        int count = 5;
        System.out.println(count);
        this.count = count;
    }
}
`,
	})
	set, err := Plan(Request{ClassFQN: "com.foo.Bar", Field: "count", NewName: "n", Index: idx})
	if err != nil {
		t.Fatal(err)
	}
	if err := set.Apply(); err != nil {
		t.Fatal(err)
	}
	got := read(t, filepath.Join(root, "src/main/java/com/foo/Bar.java"))
	// Declaration renamed.
	if !strings.Contains(got, "int n;") {
		t.Errorf("declaration not renamed:\n%s", got)
	}
	// this.n still present (field_access via this).
	if !strings.Contains(got, "this.n") {
		t.Errorf("this.count not renamed:\n%s", got)
	}
	// The local `int count = 5;` MUST stay.
	if !strings.Contains(got, "int count = 5;") {
		t.Errorf("local shadow was renamed:\n%s", got)
	}
	// And `println(count)` (the local) must stay.
	if !strings.Contains(got, "println(count)") {
		t.Errorf("local reference was renamed:\n%s", got)
	}
}

func TestRenameField_OneOfMultipleInDeclaration(t *testing.T) {
	// `int a, b, c;` — renaming `b` must only touch `b`.
	idx, root := mk(t, map[string]string{
		"build.gradle": "",
		"src/main/java/com/foo/Bar.java": `package com.foo;
public class Bar { int a, b, c; }
`,
	})
	set, err := Plan(Request{ClassFQN: "com.foo.Bar", Field: "b", NewName: "bee", Index: idx})
	if err != nil {
		t.Fatal(err)
	}
	if err := set.Apply(); err != nil {
		t.Fatal(err)
	}
	got := read(t, filepath.Join(root, "src/main/java/com/foo/Bar.java"))
	if !strings.Contains(got, "a, bee, c") {
		t.Errorf("got:\n%s", got)
	}
}

func TestRenameField_TypedReceiver(t *testing.T) {
	idx, root := mk(t, map[string]string{
		"build.gradle": "",
		"src/main/java/com/foo/Bar.java": `package com.foo;
public class Bar { public int count; }
`,
		"src/main/java/com/app/User.java": `package com.app;
import com.foo.Bar;
public class User {
    Bar field = new Bar();
    public void run() {
        Bar local = new Bar();
        int a = field.count;
        int b = local.count;
    }
}
`,
	})
	set, err := Plan(Request{ClassFQN: "com.foo.Bar", Field: "count", NewName: "n", Index: idx})
	if err != nil {
		t.Fatal(err)
	}
	if err := set.Apply(); err != nil {
		t.Fatal(err)
	}
	got := read(t, filepath.Join(root, "src/main/java/com/app/User.java"))
	if !strings.Contains(got, "field.n") || !strings.Contains(got, "local.n") {
		t.Errorf("typed accesses not renamed:\n%s", got)
	}
}

func TestRenameField_OtherClassSameFieldNameUntouched(t *testing.T) {
	idx, root := mk(t, map[string]string{
		"build.gradle": "",
		"src/main/java/com/foo/Bar.java": `package com.foo;
public class Bar { public int count; }
`,
		"src/main/java/com/foo/Other.java": `package com.foo;
public class Other { public int count; }
`,
		"src/main/java/com/foo/User.java": `package com.foo;
public class User {
    public void run() {
        Bar b = new Bar();
        Other o = new Other();
        int x = b.count;
        int y = o.count;
    }
}
`,
	})
	set, err := Plan(Request{ClassFQN: "com.foo.Bar", Field: "count", NewName: "n", Index: idx})
	if err != nil {
		t.Fatal(err)
	}
	if err := set.Apply(); err != nil {
		t.Fatal(err)
	}
	user := read(t, filepath.Join(root, "src/main/java/com/foo/User.java"))
	if !strings.Contains(user, "b.n") {
		t.Errorf("Bar.count access not renamed:\n%s", user)
	}
	if !strings.Contains(user, "o.count") {
		t.Errorf("Other.count was renamed (must NOT be):\n%s", user)
	}
	other := read(t, filepath.Join(root, "src/main/java/com/foo/Other.java"))
	if !strings.Contains(other, "public int count;") {
		t.Errorf("Other's field decl was touched:\n%s", other)
	}
}

func TestRenameField_StaticAccess(t *testing.T) {
	idx, root := mk(t, map[string]string{
		"build.gradle": "",
		"src/main/java/com/foo/Bar.java": `package com.foo;
public class Bar { public static int counter; }
`,
		"src/main/java/com/app/User.java": `package com.app;
import com.foo.Bar;
public class User { int x = Bar.counter; }
`,
	})
	set, err := Plan(Request{ClassFQN: "com.foo.Bar", Field: "counter", NewName: "n", Index: idx})
	if err != nil {
		t.Fatal(err)
	}
	if err := set.Apply(); err != nil {
		t.Fatal(err)
	}
	user := read(t, filepath.Join(root, "src/main/java/com/app/User.java"))
	if !strings.Contains(user, "Bar.n") {
		t.Errorf("static access not renamed:\n%s", user)
	}
}

func TestRenameField_StaticImportRewrites(t *testing.T) {
	idx, root := mk(t, map[string]string{
		"build.gradle": "",
		"src/main/java/com/foo/Bar.java": `package com.foo;
public class Bar { public static int counter; }
`,
		"src/main/java/com/app/User.java": `package com.app;
import static com.foo.Bar.counter;
public class User { int x = counter; }
`,
	})
	set, err := Plan(Request{ClassFQN: "com.foo.Bar", Field: "counter", NewName: "n", Index: idx})
	if err != nil {
		t.Fatal(err)
	}
	if err := set.Apply(); err != nil {
		t.Fatal(err)
	}
	user := read(t, filepath.Join(root, "src/main/java/com/app/User.java"))
	if !strings.Contains(user, "import static com.foo.Bar.n;") {
		t.Errorf("static import not rewritten:\n%s", user)
	}
}

func TestRenameField_NotFound(t *testing.T) {
	idx, _ := mk(t, map[string]string{
		"build.gradle":                   "",
		"src/main/java/com/foo/Bar.java": "package com.foo; public class Bar {}\n",
	})
	_, err := Plan(Request{ClassFQN: "com.foo.Bar", Field: "ghost", NewName: "x", Index: idx})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestRenameField_DoesNotMatchSubstring(t *testing.T) {
	idx, root := mk(t, map[string]string{
		"build.gradle": "",
		"src/main/java/com/foo/Bar.java": `package com.foo;
public class Bar { public int count; public int counter; void f() { count++; counter++; } }
`,
	})
	set, err := Plan(Request{ClassFQN: "com.foo.Bar", Field: "count", NewName: "n", Index: idx})
	if err != nil {
		t.Fatal(err)
	}
	if err := set.Apply(); err != nil {
		t.Fatal(err)
	}
	got := read(t, filepath.Join(root, "src/main/java/com/foo/Bar.java"))
	if !strings.Contains(got, "public int counter;") || !strings.Contains(got, "counter++") {
		t.Errorf("counter wrongly renamed:\n%s", got)
	}
	if !strings.Contains(got, "public int n;") || !strings.Contains(got, "n++") {
		t.Errorf("count not properly renamed:\n%s", got)
	}
}
