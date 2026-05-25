// Package project models a multi-module JVM project (gradle or maven)
// so other layers can find every source file that belongs to it.
//
// Detection is filesystem-only — we do NOT evaluate the build script.
// For gradle, we read settings.gradle{.kts} and scan for `include ':a'`
// patterns. For maven, we read pom.xml and scan for <module> tags. This
// means projects that register modules programmatically (e.g.
// `rootProject.children.forEach { ... }` in a Kotlin settings file) will
// look single-module to us. That is a known limit; the user-visible
// effect is that refactors only see modules we detected. We log what we
// found at higher levels so users can spot under-detection.
//
// Per-module source roots are discovered by checking standard
// convention paths (src/main/java, src/test/java, ...). Non-standard
// source root configuration (e.g. gradle `sourceSets.main.java.srcDirs`)
// is not yet supported.
package project

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// Kind identifies the build system.
type Kind int

const (
	KindUnknown Kind = iota
	KindGradle
	KindMaven
)

func (k Kind) String() string {
	switch k {
	case KindGradle:
		return "gradle"
	case KindMaven:
		return "maven"
	default:
		return "unknown"
	}
}

// Project is the detected project root and its modules.
type Project struct {
	Root    string    // absolute path
	Kind    Kind
	Modules []*Module
}

// Module is one buildable unit. For a single-module project there is
// exactly one entry whose Path equals Project.Root.
type Module struct {
	// Name is the module identifier used by the build system.
	// Gradle uses colon-separated paths (":app", ":lib:core"); maven
	// uses the directory name (or artifactId, but we use directory).
	// The root module is named ":" (gradle) or "" (maven).
	Name string

	// Path is the absolute filesystem path to the module directory.
	Path string

	// SourceRoots are the directories under Path that contain source.
	SourceRoots []SourceRoot
}

// SourceRoot is a directory containing source files for one or more
// languages.
type SourceRoot struct {
	Path      string   // absolute
	Languages []string // e.g. ["java"], ["kotlin"]
	IsTest    bool
}

// ErrNoProject is returned when DetectFrom walks up to the filesystem
// root without finding a build file.
var ErrNoProject = errors.New("no project root found (no settings.gradle / pom.xml / build.gradle above this path)")

// DetectFrom walks up from start (file or directory) until a project
// root marker is found. The first build file encountered determines
// Kind; if both gradle and maven markers appear at the same level
// (rare), gradle wins.
func DetectFrom(start string) (*Project, error) {
	abs, err := filepath.Abs(start)
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(abs)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		abs = filepath.Dir(abs)
	}
	dir := abs
	for {
		if k := detectKind(dir); k != KindUnknown {
			return load(dir, k)
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return nil, fmt.Errorf("%w (searched from %s)", ErrNoProject, abs)
		}
		dir = parent
	}
}

// detectKind reports the build system if dir contains a marker file.
// For gradle we accept settings.gradle{.kts} OR a build.gradle{.kts}
// without an enclosing settings file (single-module project).
func detectKind(dir string) Kind {
	if exists(filepath.Join(dir, "settings.gradle")) ||
		exists(filepath.Join(dir, "settings.gradle.kts")) {
		return KindGradle
	}
	if exists(filepath.Join(dir, "pom.xml")) {
		// Could still be a child pom; the caller climbs further until
		// it finds the topmost pom. We treat any pom.xml as a marker
		// and rely on load() to resolve the actual root.
		return KindMaven
	}
	if exists(filepath.Join(dir, "build.gradle")) ||
		exists(filepath.Join(dir, "build.gradle.kts")) {
		return KindGradle
	}
	return KindUnknown
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func load(root string, k Kind) (*Project, error) {
	switch k {
	case KindGradle:
		return loadGradle(root)
	case KindMaven:
		return loadMaven(root)
	default:
		return nil, fmt.Errorf("unsupported project kind %s", k)
	}
}

// standardSourceRoots returns the standard convention source roots
// that exist under moduleDir.
func standardSourceRoots(moduleDir string) []SourceRoot {
	candidates := []struct {
		rel    string
		lang   string
		isTest bool
	}{
		{"src/main/java", "java", false},
		{"src/main/kotlin", "kotlin", false},
		{"src/test/java", "java", true},
		{"src/test/kotlin", "kotlin", true},
	}
	// Group by (path, isTest) so a src/main/java that also contains
	// kotlin files (rare but happens) stays as one source root with
	// multiple languages. With strict convention paths this collapses
	// to one entry per candidate path.
	byPath := map[string]*SourceRoot{}
	var order []*SourceRoot
	for _, c := range candidates {
		p := filepath.Join(moduleDir, c.rel)
		if !exists(p) {
			continue
		}
		if sr, ok := byPath[p]; ok {
			sr.Languages = append(sr.Languages, c.lang)
			continue
		}
		sr := &SourceRoot{Path: p, Languages: []string{c.lang}, IsTest: c.isTest}
		byPath[p] = sr
		order = append(order, sr)
	}
	out := make([]SourceRoot, len(order))
	for i, sr := range order {
		out[i] = *sr
	}
	return out
}

// SourceFiles returns absolute paths of all source files across all
// modules whose extension is in exts (which must include the leading
// dot, e.g. ".java").
func (p *Project) SourceFiles(exts ...string) ([]string, error) {
	want := map[string]struct{}{}
	for _, e := range exts {
		want[e] = struct{}{}
	}
	var files []string
	for _, m := range p.Modules {
		for _, sr := range m.SourceRoots {
			err := filepath.WalkDir(sr.Path, func(path string, d os.DirEntry, err error) error {
				if err != nil {
					return err
				}
				if d.IsDir() {
					return nil
				}
				ext := filepath.Ext(path)
				if _, ok := want[ext]; ok {
					files = append(files, path)
				}
				return nil
			})
			if err != nil {
				return nil, err
			}
		}
	}
	return files, nil
}
