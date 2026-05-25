package renamemethod

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

func TestRenameMethod_DeclAndSelfReference(t *testing.T) {
	idx, root := mk(t, map[string]string{
		"build.gradle": "",
		"src/main/java/com/foo/Bar.java": `package com.foo;
public class Bar {
    public int fizzBuzz() { return 1; }
    public int caller() { return fizzBuzz(); }
}
`,
	})
	set, err := Plan(Request{ClassFQN: "com.foo.Bar", Method: "fizzBuzz", NewName: "FizzBuzz", Index: idx})
	if err != nil {
		t.Fatal(err)
	}
	if err := set.Apply(); err != nil {
		t.Fatal(err)
	}
	got := read(t, filepath.Join(root, "src/main/java/com/foo/Bar.java"))
	if strings.Count(got, "FizzBuzz") != 2 {
		t.Errorf("expected 2 occurrences of FizzBuzz, got:\n%s", got)
	}
}

func TestRenameMethod_TypedReceiver(t *testing.T) {
	idx, root := mk(t, map[string]string{
		"build.gradle": "",
		"src/main/java/com/foo/Bar.java": `package com.foo;
public class Bar { public int fizzBuzz() { return 1; } }
`,
		"src/main/java/com/app/User.java": `package com.app;
import com.foo.Bar;
public class User {
    Bar field = new Bar();
    public void run() {
        Bar local = new Bar();
        int a = field.fizzBuzz();
        int b = local.fizzBuzz();
    }
}
`,
	})
	set, err := Plan(Request{ClassFQN: "com.foo.Bar", Method: "fizzBuzz", NewName: "FB", Index: idx})
	if err != nil {
		t.Fatal(err)
	}
	if err := set.Apply(); err != nil {
		t.Fatal(err)
	}
	user := read(t, filepath.Join(root, "src/main/java/com/app/User.java"))
	if !strings.Contains(user, "field.FB()") {
		t.Errorf("field call not renamed:\n%s", user)
	}
	if !strings.Contains(user, "local.FB()") {
		t.Errorf("local call not renamed:\n%s", user)
	}
}

func TestRenameMethod_OtherClassSameMethodNameUntouched(t *testing.T) {
	// Other has a method also named fizzBuzz. Calls on Other-typed
	// receivers must NOT be renamed.
	idx, root := mk(t, map[string]string{
		"build.gradle": "",
		"src/main/java/com/foo/Bar.java": `package com.foo;
public class Bar { public int fizzBuzz() { return 1; } }
`,
		"src/main/java/com/foo/Other.java": `package com.foo;
public class Other { public int fizzBuzz() { return 2; } }
`,
		"src/main/java/com/foo/User.java": `package com.foo;
public class User {
    public void run() {
        Bar b = new Bar();
        Other o = new Other();
        b.fizzBuzz();
        o.fizzBuzz();
    }
}
`,
	})
	set, err := Plan(Request{ClassFQN: "com.foo.Bar", Method: "fizzBuzz", NewName: "FB", Index: idx})
	if err != nil {
		t.Fatal(err)
	}
	if err := set.Apply(); err != nil {
		t.Fatal(err)
	}
	user := read(t, filepath.Join(root, "src/main/java/com/foo/User.java"))
	if !strings.Contains(user, "b.FB()") {
		t.Errorf("Bar call not renamed:\n%s", user)
	}
	if !strings.Contains(user, "o.fizzBuzz()") {
		t.Errorf("Other call WAS renamed (must NOT be):\n%s", user)
	}
	// And Other's declaration must stay as fizzBuzz.
	other := read(t, filepath.Join(root, "src/main/java/com/foo/Other.java"))
	if !strings.Contains(other, "public int fizzBuzz()") {
		t.Errorf("Other.fizzBuzz declaration changed:\n%s", other)
	}
}

func TestRenameMethod_Overloads(t *testing.T) {
	// All overloads of the same method name are renamed together.
	idx, root := mk(t, map[string]string{
		"build.gradle": "",
		"src/main/java/com/foo/Bar.java": `package com.foo;
public class Bar {
    public int fizzBuzz() { return 1; }
    public int fizzBuzz(int x) { return x; }
    public int fizzBuzz(String s) { return s.length(); }
}
`,
	})
	set, err := Plan(Request{ClassFQN: "com.foo.Bar", Method: "fizzBuzz", NewName: "FB", Index: idx})
	if err != nil {
		t.Fatal(err)
	}
	if err := set.Apply(); err != nil {
		t.Fatal(err)
	}
	got := read(t, filepath.Join(root, "src/main/java/com/foo/Bar.java"))
	if strings.Contains(got, "fizzBuzz") {
		t.Errorf("fizzBuzz still present:\n%s", got)
	}
	if strings.Count(got, "FB(") != 3 {
		t.Errorf("expected 3 FB( declarations, got:\n%s", got)
	}
}

func TestRenameMethod_StaticCall(t *testing.T) {
	idx, root := mk(t, map[string]string{
		"build.gradle": "",
		"src/main/java/com/foo/Bar.java": `package com.foo;
public class Bar { public static int fizzBuzz() { return 1; } }
`,
		"src/main/java/com/app/User.java": `package com.app;
import com.foo.Bar;
public class User { int x = Bar.fizzBuzz(); }
`,
	})
	set, err := Plan(Request{ClassFQN: "com.foo.Bar", Method: "fizzBuzz", NewName: "FB", Index: idx})
	if err != nil {
		t.Fatal(err)
	}
	if err := set.Apply(); err != nil {
		t.Fatal(err)
	}
	user := read(t, filepath.Join(root, "src/main/java/com/app/User.java"))
	if !strings.Contains(user, "Bar.FB()") {
		t.Errorf("static call not renamed:\n%s", user)
	}
}

func TestRenameMethod_StaticImportMember(t *testing.T) {
	// `import static com.foo.Bar.fizzBuzz` means bare fizzBuzz() in the
	// file refers to target.fizzBuzz. The import path is also rewritten.
	idx, root := mk(t, map[string]string{
		"build.gradle": "",
		"src/main/java/com/foo/Bar.java": `package com.foo;
public class Bar { public static int fizzBuzz() { return 1; } }
`,
		"src/main/java/com/app/User.java": `package com.app;
import static com.foo.Bar.fizzBuzz;
public class User { int x = fizzBuzz(); }
`,
	})
	set, err := Plan(Request{ClassFQN: "com.foo.Bar", Method: "fizzBuzz", NewName: "FB", Index: idx})
	if err != nil {
		t.Fatal(err)
	}
	if err := set.Apply(); err != nil {
		t.Fatal(err)
	}
	user := read(t, filepath.Join(root, "src/main/java/com/app/User.java"))
	if !strings.Contains(user, "import static com.foo.Bar.FB;") {
		t.Errorf("static import path not rewritten:\n%s", user)
	}
	if !strings.Contains(user, "= FB()") {
		t.Errorf("bare call not renamed:\n%s", user)
	}
}

func TestRenameMethod_UnknownReceiverIsMedium(t *testing.T) {
	// The receiver `unk` has no declared type in the file, but the file
	// resolves the target via import. We emit a MEDIUM edit so the user
	// gets the option in dry-run.
	idx, root := mk(t, map[string]string{
		"build.gradle": "",
		"src/main/java/com/foo/Bar.java": `package com.foo;
public class Bar { public int fizzBuzz() { return 1; } }
`,
		"src/main/java/com/app/User.java": `package com.app;
import com.foo.Bar;
public class User { public void run(Object unk) { /* cast */ ((Bar) unk).fizzBuzz(); } }
`,
	})
	set, err := Plan(Request{ClassFQN: "com.foo.Bar", Method: "fizzBuzz", NewName: "FB", Index: idx})
	if err != nil {
		t.Fatal(err)
	}
	// At least one edit on the User file with MEDIUM tag.
	var hasMedium bool
	for _, e := range set.Edits() {
		if strings.Contains(e.Why, "MEDIUM") && strings.HasSuffix(e.File, "User.java") {
			hasMedium = true
		}
	}
	if !hasMedium {
		t.Errorf("expected a MEDIUM edit in User.java; edits=%v", set.Edits())
	}
	_ = root
}

func TestRenameMethod_BareCallOutsideTargetClassNotTouched(t *testing.T) {
	// Bare fizzBuzz() inside an unrelated class is NOT a reference to
	// the target; it could be that class's own method or a free
	// function (Java has none, but the principle stands).
	idx, root := mk(t, map[string]string{
		"build.gradle": "",
		"src/main/java/com/foo/Bar.java": `package com.foo;
public class Bar { public int fizzBuzz() { return 1; } }
`,
		"src/main/java/com/foo/Other.java": `package com.foo;
public class Other {
    public int fizzBuzz() { return 2; }
    public int caller() { return fizzBuzz(); }
}
`,
	})
	set, err := Plan(Request{ClassFQN: "com.foo.Bar", Method: "fizzBuzz", NewName: "FB", Index: idx})
	if err != nil {
		t.Fatal(err)
	}
	if err := set.Apply(); err != nil {
		t.Fatal(err)
	}
	other := read(t, filepath.Join(root, "src/main/java/com/foo/Other.java"))
	if !strings.Contains(other, "public int fizzBuzz()") {
		t.Errorf("Other's fizzBuzz decl changed:\n%s", other)
	}
	if !strings.Contains(other, "return fizzBuzz()") {
		t.Errorf("Other's self-reference changed:\n%s", other)
	}
}

func TestRenameMethod_MethodNotFound(t *testing.T) {
	idx, _ := mk(t, map[string]string{
		"build.gradle":                   "",
		"src/main/java/com/foo/Bar.java": "package com.foo; public class Bar {}\n",
	})
	_, err := Plan(Request{ClassFQN: "com.foo.Bar", Method: "ghost", NewName: "x", Index: idx})
	if err == nil || !strings.Contains(err.Error(), "no method") {
		t.Errorf("err = %v", err)
	}
}

func TestRenameMethod_InvalidName(t *testing.T) {
	idx, _ := mk(t, map[string]string{
		"build.gradle":                   "",
		"src/main/java/com/foo/Bar.java": "package com.foo; public class Bar { void f() {} }\n",
	})
	_, err := Plan(Request{ClassFQN: "com.foo.Bar", Method: "f", NewName: "1bad", Index: idx})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestRenameMethod_DoesNotMatchSubstring(t *testing.T) {
	idx, root := mk(t, map[string]string{
		"build.gradle": "",
		"src/main/java/com/foo/Bar.java": `package com.foo;
public class Bar {
    public int foo() { return 0; }
    public int foobar() { return 0; }
    public int caller() { foo(); foobar(); return 0; }
}
`,
	})
	set, err := Plan(Request{ClassFQN: "com.foo.Bar", Method: "foo", NewName: "qux", Index: idx})
	if err != nil {
		t.Fatal(err)
	}
	if err := set.Apply(); err != nil {
		t.Fatal(err)
	}
	got := read(t, filepath.Join(root, "src/main/java/com/foo/Bar.java"))
	if !strings.Contains(got, "public int foobar()") {
		t.Errorf("foobar wrongly modified:\n%s", got)
	}
	if !strings.Contains(got, "qux()") || !strings.Contains(got, "foobar()") {
		t.Errorf("rename incomplete:\n%s", got)
	}
}
