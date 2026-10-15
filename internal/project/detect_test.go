package project

import (
	"path/filepath"
	"sort"
	"testing"
)

func sourceFileCount(t *testing.T, p *Project) int {
	t.Helper()
	files, err := p.SourceFiles(".java", ".kt")
	if err != nil {
		t.Fatal(err)
	}
	return len(files)
}

func TestParseGradleSettings_MultiLineInclude(t *testing.T) {
	for _, tc := range []struct {
		name     string
		settings string
		want     []string
	}{
		{"one per line", "include(\n    \"core\",\n    \"starter\"\n)", []string{"core", "starter"}},
		{"trailing comma", "include(\n    \"core\",\n    \"starter\",\n)", []string{"core", "starter"}},
		{"single line", `include("core", "starter")`, []string{"core", "starter"}},
		{"groovy per line", "include ':core'\ninclude ':starter'", []string{"core", "starter"}},
		{"groovy one line", "include ':core', ':starter'", []string{"core", "starter"}},
		{"colon paths", "include(\n  \":a:b\"\n)", []string{"a:b"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			writeTree(t, dir, map[string]string{"settings.gradle.kts": tc.settings})
			got, err := parseGradleSettings(dir)
			if err != nil {
				t.Fatal(err)
			}
			sort.Strings(got)
			if len(got) != len(tc.want) {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Fatalf("got %v, want %v", got, tc.want)
				}
			}
		})
	}
}

func TestParseGradleSettings_IgnoresIncludeBuild(t *testing.T) {
	dir := t.TempDir()
	writeTree(t, dir, map[string]string{
		"settings.gradle.kts": "includeBuild(\"../other\")\ninclude(\"core\")\n",
	})
	got, err := parseGradleSettings(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != "core" {
		t.Errorf("got %v, want [core]", got)
	}
}

func TestDetectFrom_ClimbsToOutermostRoot(t *testing.T) {
	for _, tc := range []struct {
		name  string
		files map[string]string
	}{
		{"gradle", map[string]string{
			".git/":                            "",
			"settings.gradle.kts":              "include(\n  \"mod-a\",\n  \"mod-b\"\n)",
			"mod-a/build.gradle.kts":           "plugins {}",
			"mod-a/src/main/java/com/x/A.java": "package com.x;\nclass A{}",
			"mod-b/build.gradle.kts":           "plugins {}",
			"mod-b/src/main/java/com/x/B.java": "package com.x;\nclass B{}",
		}},
		{"maven", map[string]string{
			".git/":                            "",
			"pom.xml":                          "<project><modules><module>mod-a</module><module>mod-b</module></modules></project>",
			"mod-a/pom.xml":                    "<project/>",
			"mod-a/src/main/java/com/x/A.java": "package com.x;\nclass A{}",
			"mod-b/pom.xml":                    "<project/>",
			"mod-b/src/main/java/com/x/B.java": "package com.x;\nclass B{}",
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			writeTree(t, root, tc.files)

			deep := filepath.Join(root, "mod-a/src/main/java/com/x")
			p, err := DetectFrom(deep)
			if err != nil {
				t.Fatal(err)
			}
			if p.Root != root {
				t.Errorf("Root = %s, want the outermost root %s", p.Root, root)
			}
			if n := sourceFileCount(t, p); n != 2 {
				t.Errorf("saw %d source files from inside mod-a, want both modules' 2", n)
			}
		})
	}
}

func TestDetectFrom_StopsAtVCSRoot(t *testing.T) {
	outer := t.TempDir()
	writeTree(t, outer, map[string]string{
		"pom.xml":                              "<project><modules><module>other</module></modules></project>",
		"other/pom.xml":                        "<project/>",
		"other/src/main/java/com/x/Other.java": "package com.x;\nclass Other{}",
		"repo/.git/":                           "",
		"repo/pom.xml":                         "<project/>",
		"repo/src/main/java/com/x/Mine.java":   "package com.x;\nclass Mine{}",
	})
	p, err := DetectFrom(filepath.Join(outer, "repo/src/main/java"))
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(outer, "repo")
	if p.Root != want {
		t.Errorf("Root = %s, want %s — detection must not climb past the VCS root", p.Root, want)
	}
	if n := sourceFileCount(t, p); n != 1 {
		t.Errorf("saw %d source files, want only the 1 inside the repo", n)
	}
}

func TestDiscover_FindsProgrammaticallyRegisteredModules(t *testing.T) {
	root := t.TempDir()
	writeTree(t, root, map[string]string{
		".git/": "",
		"settings.gradle.kts": `include("core")
listOf("mysql", "postgres").forEach {
    include(it)
    project(":$it").projectDir = file("dialects/$it")
}`,
		"core/src/main/java/com/x/Core.java":            "package com.x;\nclass Core{}",
		"dialects/mysql/src/main/java/com/x/My.java":    "package com.x;\nclass My{}",
		"dialects/postgres/src/main/java/com/x/Pg.java": "package com.x;\nclass Pg{}",
		"build/generated/src/main/java/com/x/Gen.java":  "package com.x;\nclass Gen{}",
		".hidden/src/main/java/com/x/Hidden.java":       "package com.x;\nclass Hidden{}",
	})
	p, err := DetectFrom(root)
	if err != nil {
		t.Fatal(err)
	}
	if n := sourceFileCount(t, p); n != 3 {
		t.Errorf("saw %d source files, want 3 (core + both dialects, excluding build/ and .hidden/)\nmodules: %v",
			n, moduleNames(p))
	}
	for _, m := range p.Modules {
		if base := filepath.Base(m.Path); base == "generated" || base == ".hidden" {
			t.Errorf("module discovered inside an excluded directory: %s", m.Path)
		}
	}
}

func TestDiscover_IsIdempotent(t *testing.T) {
	root := t.TempDir()
	writeTree(t, root, map[string]string{
		".git/":                           "",
		"settings.gradle.kts":             `include("core")`,
		"core/src/main/java/com/x/A.java": "package com.x;\nclass A{}",
	})
	p, err := DetectFrom(root)
	if err != nil {
		t.Fatal(err)
	}
	before := len(p.Modules)
	p.Discover()
	if len(p.Modules) != before {
		t.Errorf("modules went from %d to %d; discovery must only fill gaps", before, len(p.Modules))
	}
}

func TestStandardSourceRoots_CoversAllSourceSets(t *testing.T) {
	root := t.TempDir()
	writeTree(t, root, map[string]string{
		"src/main/java/com/x/A.java":               "package com.x;\nclass A{}",
		"src/main/kotlin/com/x/B.kt":               "package com.x\nclass B",
		"src/test/java/com/x/ATest.java":           "package com.x;\nclass ATest{}",
		"src/testFixtures/java/com/x/Fixture.java": "package com.x;\nclass Fixture{}",
		"src/integrationTest/java/com/x/IT.java":   "package com.x;\nclass IT{}",
		"src/main/resources/application.yml":       "a: b",
	})
	roots := standardSourceRoots(root)
	if len(roots) != 5 {
		t.Fatalf("got %d source roots, want 5: %v", len(roots), roots)
	}
	byPath := map[string]SourceRoot{}
	for _, sr := range roots {
		byPath[filepath.Base(filepath.Dir(sr.Path))] = sr
	}
	for set, wantTest := range map[string]bool{
		"main": false, "test": true, "testFixtures": true, "integrationTest": true,
	} {
		sr, ok := byPath[set]
		if !ok {
			t.Errorf("source set %q not found", set)
			continue
		}
		if sr.IsTest != wantTest {
			t.Errorf("source set %q: IsTest = %v, want %v", set, sr.IsTest, wantTest)
		}
	}
	if _, ok := byPath["resources"]; ok {
		t.Error("src/main/resources must not be a source root")
	}
}
