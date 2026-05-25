package ktrename

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/serfom256/refactorio/internal/project"
)

func mk(t *testing.T, files map[string]string) (*KtIndex, string) {
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
	idx, err := BuildIndex(p)
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

func TestKtRename_Class(t *testing.T) {
	idx, root := mk(t, map[string]string{
		"build.gradle": "",
		"src/main/kotlin/com/foo/Bar.kt": `package com.foo

class Bar {
    fun greet(): String = "hi"
}
`,
		"src/main/kotlin/com/app/User.kt": `package com.app

import com.foo.Bar

fun main() {
    val b = Bar()
    println(b.greet())
}
`,
	})
	set, err := PlanRename(RenameRequest{FQN: "com.foo.Bar", NewName: "Baz", Index: idx})
	if err != nil {
		t.Fatal(err)
	}
	if err := set.Apply(); err != nil {
		t.Fatal(err)
	}
	bar := read(t, filepath.Join(root, "src/main/kotlin/com/foo/Bar.kt"))
	if !strings.Contains(bar, "class Baz") {
		t.Errorf("declaration not renamed:\n%s", bar)
	}
	user := read(t, filepath.Join(root, "src/main/kotlin/com/app/User.kt"))
	if !strings.Contains(user, "import com.foo.Baz") {
		t.Errorf("import not rewritten:\n%s", user)
	}
	if !strings.Contains(user, "val b = Baz()") {
		t.Errorf("usage not renamed:\n%s", user)
	}
}

func TestKtRename_Function(t *testing.T) {
	idx, root := mk(t, map[string]string{
		"build.gradle": "",
		"src/main/kotlin/com/foo/Utils.kt": `package com.foo

fun greet(name: String): String = "Hello $name"
`,
		"src/main/kotlin/com/app/Main.kt": `package com.app

import com.foo.greet

fun main() {
    println(greet("world"))
}
`,
	})
	set, err := PlanRename(RenameRequest{FQN: "com.foo.greet", NewName: "sayHello", Index: idx})
	if err != nil {
		t.Fatal(err)
	}
	if err := set.Apply(); err != nil {
		t.Fatal(err)
	}
	utils := read(t, filepath.Join(root, "src/main/kotlin/com/foo/Utils.kt"))
	if !strings.Contains(utils, "fun sayHello(") {
		t.Errorf("declaration not renamed:\n%s", utils)
	}
	main := read(t, filepath.Join(root, "src/main/kotlin/com/app/Main.kt"))
	if !strings.Contains(main, "import com.foo.sayHello") {
		t.Errorf("import not rewritten:\n%s", main)
	}
	if !strings.Contains(main, "sayHello(\"world\")") {
		t.Errorf("usage not renamed:\n%s", main)
	}
}

func TestKtRename_SamePackage(t *testing.T) {
	idx, root := mk(t, map[string]string{
		"build.gradle": "",
		"src/main/kotlin/com/foo/Bar.kt":     "package com.foo\nclass Bar\n",
		"src/main/kotlin/com/foo/Sibling.kt":  "package com.foo\nval b = Bar()\n",
	})
	set, err := PlanRename(RenameRequest{FQN: "com.foo.Bar", NewName: "Qux", Index: idx})
	if err != nil {
		t.Fatal(err)
	}
	if err := set.Apply(); err != nil {
		t.Fatal(err)
	}
	sib := read(t, filepath.Join(root, "src/main/kotlin/com/foo/Sibling.kt"))
	if !strings.Contains(sib, "Qux()") {
		t.Errorf("same-package ref not renamed:\n%s", sib)
	}
}

func TestKtRename_WildcardImport(t *testing.T) {
	idx, root := mk(t, map[string]string{
		"build.gradle": "",
		"src/main/kotlin/com/foo/Bar.kt": "package com.foo\nclass Bar\n",
		"src/main/kotlin/com/app/User.kt": `package com.app
import com.foo.*
val b = Bar()
`,
	})
	set, err := PlanRename(RenameRequest{FQN: "com.foo.Bar", NewName: "Qux", Index: idx})
	if err != nil {
		t.Fatal(err)
	}
	if err := set.Apply(); err != nil {
		t.Fatal(err)
	}
	user := read(t, filepath.Join(root, "src/main/kotlin/com/app/User.kt"))
	if !strings.Contains(user, "Qux()") {
		t.Errorf("wildcard-imported ref not renamed:\n%s", user)
	}
}

func TestKtRename_StringTemplateRenamed(t *testing.T) {
	idx, root := mk(t, map[string]string{
		"build.gradle": "",
		"src/main/kotlin/com/foo/Main.kt": `package com.foo

val name = "world"
val greeting = "Hello $name"
`,
	})
	set, err := PlanRename(RenameRequest{FQN: "com.foo.name", NewName: "who", Index: idx})
	if err != nil {
		t.Fatal(err)
	}
	if err := set.Apply(); err != nil {
		t.Fatal(err)
	}
	main := read(t, filepath.Join(root, "src/main/kotlin/com/foo/Main.kt"))
	if !strings.Contains(main, "$who") {
		t.Errorf("template ref not renamed:\n%s", main)
	}
}

func TestKtRename_IgnoresStringContent(t *testing.T) {
	idx, root := mk(t, map[string]string{
		"build.gradle": "",
		"src/main/kotlin/com/foo/Main.kt": `package com.foo

class Bar
val s = "Bar is a string"
`,
	})
	set, err := PlanRename(RenameRequest{FQN: "com.foo.Bar", NewName: "Qux", Index: idx})
	if err != nil {
		t.Fatal(err)
	}
	if err := set.Apply(); err != nil {
		t.Fatal(err)
	}
	main := read(t, filepath.Join(root, "src/main/kotlin/com/foo/Main.kt"))
	if !strings.Contains(main, `"Bar is a string"`) {
		t.Errorf("string content was modified:\n%s", main)
	}
	if !strings.Contains(main, "class Qux") {
		t.Errorf("declaration not renamed:\n%s", main)
	}
}

func TestKtRename_MultiModule(t *testing.T) {
	idx, root := mk(t, map[string]string{
		"settings.gradle":                             "include ':app', ':lib'",
		"app/build.gradle":                            "",
		"lib/build.gradle":                            "",
		"lib/src/main/kotlin/com/foo/Bar.kt":          "package com.foo\nclass Bar\n",
		"app/src/main/kotlin/com/app/User.kt": `package com.app
import com.foo.Bar
val b = Bar()
`,
	})
	set, err := PlanRename(RenameRequest{FQN: "com.foo.Bar", NewName: "Baz", Index: idx})
	if err != nil {
		t.Fatal(err)
	}
	if err := set.Apply(); err != nil {
		t.Fatal(err)
	}
	user := read(t, filepath.Join(root, "app/src/main/kotlin/com/app/User.kt"))
	if !strings.Contains(user, "import com.foo.Baz") || !strings.Contains(user, "Baz()") {
		t.Errorf("cross-module rename failed:\n%s", user)
	}
}

func TestKtRename_NotFound(t *testing.T) {
	idx, _ := mk(t, map[string]string{
		"build.gradle":                       "",
		"src/main/kotlin/com/foo/Bar.kt":     "package com.foo\nclass Bar\n",
	})
	_, err := PlanRename(RenameRequest{FQN: "com.foo.Ghost", NewName: "X", Index: idx})
	if err == nil {
		t.Fatal("expected error")
	}
}
