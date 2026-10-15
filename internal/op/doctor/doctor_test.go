package doctor

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/serfom256/refactorio/internal/cli"
)

func writeTree(t *testing.T, root string, files map[string]string) {
	t.Helper()
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
}

func report(t *testing.T, root string) string {
	t.Helper()
	args, err := cli.Parse([]string{"doctor", "root=" + root})
	if err != nil {
		t.Fatal(err)
	}
	var out, errBuf bytes.Buffer
	if err := run(args, &out, &errBuf); err != nil {
		t.Fatal(err)
	}
	return out.String()
}

func TestDoctor_ListsEveryModuleAndItsFiles(t *testing.T) {
	root := t.TempDir()
	writeTree(t, root, map[string]string{
		".git/":                              "",
		"settings.gradle.kts":                "include(\n  \"core\",\n  \"starter\"\n)",
		"core/src/main/java/com/x/A.java":    "package com.x;\nclass A{}",
		"starter/src/main/java/com/x/B.java": "package com.x;\nclass B{}",
	})
	got := report(t, root)

	for _, want := range []string{"gradle", ":core", ":starter", "java 2"} {
		if !strings.Contains(got, want) {
			t.Errorf("report should mention %q:\n%s", want, got)
		}
	}
}

func TestDoctor_WarnsWhenNoSourceRootsExist(t *testing.T) {
	root := t.TempDir()
	writeTree(t, root, map[string]string{
		".git/":               "",
		"settings.gradle.kts": `rootProject.name = "empty"`,
	})
	got := report(t, root)
	if !strings.Contains(got, "warning: no source roots found") {
		t.Errorf("expected a warning about having nothing to work on:\n%s", got)
	}
}

func TestDoctor_NonJVMProjectStillReportsFiles(t *testing.T) {
	root := t.TempDir()
	writeTree(t, root, map[string]string{
		"go.mod":              "module example.com/x\n",
		"main.go":             "package main\nfunc main(){}",
		"internal/a/a.go":     "package a",
		"node_modules/d/x.go": "package d",
	})
	got := report(t, root)
	if !strings.Contains(got, "none detected") {
		t.Errorf("expected the report to say no JVM project was found:\n%s", got)
	}
	if !strings.Contains(got, "go 2") {
		t.Errorf("expected 2 Go files, excluding node_modules:\n%s", got)
	}
}
