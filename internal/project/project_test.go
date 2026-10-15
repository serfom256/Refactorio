package project

import (
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
)

func writeTree(t *testing.T, root string, files map[string]string) {
	t.Helper()
	for rel, content := range files {
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
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func TestDetect_NoProject(t *testing.T) {
	dir := t.TempDir()
	if _, err := DetectFrom(dir); err == nil {
		t.Fatalf("expected ErrNoProject")
	}
}

func TestDetect_SingleModuleGradle(t *testing.T) {
	root := t.TempDir()
	writeTree(t, root, map[string]string{
		"build.gradle":          "",
		"src/main/java/A.java":  "class A {}",
		"src/test/java/AT.java": "class AT {}",
	})
	p, err := DetectFrom(filepath.Join(root, "src/main/java"))
	if err != nil {
		t.Fatalf("DetectFrom: %v", err)
	}
	if p.Kind != KindGradle {
		t.Errorf("Kind = %s, want gradle", p.Kind)
	}
	if p.Root != root {
		t.Errorf("Root = %s, want %s", p.Root, root)
	}
	if len(p.Modules) != 1 {
		t.Fatalf("Modules = %d, want 1", len(p.Modules))
	}
	if got := p.Modules[0].Name; got != ":" {
		t.Errorf("module name = %q, want :", got)
	}
	if len(p.Modules[0].SourceRoots) != 2 {
		t.Errorf("source roots = %d, want 2", len(p.Modules[0].SourceRoots))
	}
}

func TestDetect_MultiModuleGradleGroovy(t *testing.T) {
	root := t.TempDir()
	writeTree(t, root, map[string]string{
		"settings.gradle": `
			rootProject.name = 'demo'
			// includes:
			include ':app', ':lib:core'
			/* trailing */
			include 'tools' // comma-less form, deprecated but seen
		`,
		"app/build.gradle":              "",
		"app/src/main/java/A.java":      "package app; class A {}",
		"lib/core/build.gradle":         "",
		"lib/core/src/main/java/B.java": "package lib.core; class B {}",
		"tools/build.gradle":            "",
		"tools/src/main/java/T.java":    "package tools; class T {}",
	})
	p, err := DetectFrom(root)
	if err != nil {
		t.Fatalf("DetectFrom: %v", err)
	}
	if len(p.Modules) != 4 {
		t.Fatalf("Modules = %d (%v), want 4", len(p.Modules), moduleNames(p))
	}
	got := moduleNames(p)
	sort.Strings(got)
	want := []string{":", ":app", ":lib:core", ":tools"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("modules = %v, want %v", got, want)
	}
}

func TestDetect_MultiModuleGradleKotlin(t *testing.T) {
	root := t.TempDir()
	writeTree(t, root, map[string]string{
		"settings.gradle.kts": `
			rootProject.name = "demo"
			include(":app")
			include(":lib:core")
		`,
		"app/build.gradle.kts":          "",
		"app/src/main/kotlin/A.kt":      "package app",
		"lib/core/build.gradle.kts":     "",
		"lib/core/src/main/kotlin/B.kt": "package lib.core",
	})
	p, err := DetectFrom(root)
	if err != nil {
		t.Fatalf("DetectFrom: %v", err)
	}
	got := moduleNames(p)
	sort.Strings(got)
	want := []string{":", ":app", ":lib:core"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("modules = %v, want %v", got, want)
	}
}

func TestDetect_GradleIncludeMissingOnDisk(t *testing.T) {
	root := t.TempDir()
	writeTree(t, root, map[string]string{
		"settings.gradle":          "include ':app', ':ghost'",
		"app/src/main/java/A.java": "class A {}",
	})
	p, err := DetectFrom(root)
	if err != nil {
		t.Fatalf("DetectFrom: %v", err)
	}
	got := moduleNames(p)
	sort.Strings(got)
	want := []string{":", ":app"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("modules = %v, want %v (ghost should be skipped)", got, want)
	}
}

func TestDetect_MultiModuleMaven(t *testing.T) {
	root := t.TempDir()
	writeTree(t, root, map[string]string{
		"pom.xml": `<?xml version="1.0"?>
<project>
  <modelVersion>4.0.0</modelVersion>
  <groupId>com.foo</groupId>
  <artifactId>parent</artifactId>
  <version>1.0</version>
  <packaging>pom</packaging>
  <modules>
    <module>app</module>
    <module>lib</module>
  </modules>
</project>`,
		"app/pom.xml": `<?xml version="1.0"?>
<project>
  <artifactId>app</artifactId>
</project>`,
		"app/src/main/java/A.java": "class A {}",
		"lib/pom.xml": `<?xml version="1.0"?>
<project>
  <artifactId>lib</artifactId>
</project>`,
		"lib/src/main/java/B.java": "class B {}",
	})
	p, err := DetectFrom(filepath.Join(root, "app/src/main/java"))
	if err != nil {
		t.Fatalf("DetectFrom: %v", err)
	}
	p2, err := DetectFrom(root)
	if err != nil {
		t.Fatalf("DetectFrom(root): %v", err)
	}
	if p2.Kind != KindMaven {
		t.Errorf("Kind = %s, want maven", p2.Kind)
	}
	got := moduleNames(p2)
	sort.Strings(got)
	if !reflect.DeepEqual(got, []string{"", "app", "lib"}) {
		t.Errorf("modules = %v", got)
	}
	if len(p.Modules) == 0 {
		t.Errorf("DetectFrom subdir: expected at least one module")
	}
}

func TestSourceFiles_FiltersByExtension(t *testing.T) {
	root := t.TempDir()
	writeTree(t, root, map[string]string{
		"settings.gradle":              "include ':app', ':lib'",
		"app/src/main/java/A.java":     "class A {}",
		"app/src/main/java/com/X.java": "class X {}",
		"app/src/test/java/AT.java":    "class AT {}",
		"app/src/main/kotlin/K.kt":     "",
		"lib/src/main/java/L.java":     "",
		"unrelated/notes.txt":          "",
	})
	p, err := DetectFrom(root)
	if err != nil {
		t.Fatal(err)
	}
	files, err := p.SourceFiles(".java")
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 4 {
		t.Errorf("java files = %d (%v), want 4", len(files), files)
	}
	files, err = p.SourceFiles(".kt")
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 {
		t.Errorf("kt files = %d (%v), want 1", len(files), files)
	}
}

func TestDetect_WalksUpFromDeepFile(t *testing.T) {
	root := t.TempDir()
	writeTree(t, root, map[string]string{
		"settings.gradle":                       "include ':app'",
		"app/src/main/java/com/foo/deep/X.java": "class X {}",
	})
	p, err := DetectFrom(filepath.Join(root, "app/src/main/java/com/foo/deep/X.java"))
	if err != nil {
		t.Fatalf("DetectFrom: %v", err)
	}
	if p.Root != root {
		t.Errorf("Root = %s, want %s", p.Root, root)
	}
}

func moduleNames(p *Project) []string {
	out := make([]string, len(p.Modules))
	for i, m := range p.Modules {
		out[i] = m.Name
	}
	return out
}
