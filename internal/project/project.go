package project

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

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

type Project struct {
	Root    string
	Kind    Kind
	Modules []*Module
}

type Module struct {
	Name string

	Path string

	SourceRoots []SourceRoot
}

type SourceRoot struct {
	Path      string
	Languages []string
	IsTest    bool
}

var ErrNoProject = errors.New("no project root found (no settings.gradle / pom.xml / build.gradle above this path)")

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

	outerDir, outerKind := "", KindUnknown
	dir := abs
	for {
		if k := detectKind(dir); k != KindUnknown {
			outerDir, outerKind = dir, k
		}
		if isVCSRoot(dir) {
			break
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	if outerKind == KindUnknown {
		return nil, fmt.Errorf("%w (searched from %s)", ErrNoProject, abs)
	}

	p, err := load(outerDir, outerKind)
	if err != nil {
		return nil, err
	}
	p.Discover()
	return p, nil
}

func isVCSRoot(dir string) bool {
	for _, marker := range []string{".git", ".hg", ".svn"} {
		if exists(filepath.Join(dir, marker)) {
			return true
		}
	}
	return false
}

func detectKind(dir string) Kind {
	if exists(filepath.Join(dir, "settings.gradle")) ||
		exists(filepath.Join(dir, "settings.gradle.kts")) {
		return KindGradle
	}
	if exists(filepath.Join(dir, "pom.xml")) {
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

func standardSourceRoots(moduleDir string) []SourceRoot {
	entries, err := os.ReadDir(filepath.Join(moduleDir, "src"))
	if err != nil {
		return nil
	}
	sets := make([]string, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() {
			sets = append(sets, e.Name())
		}
	}
	sort.Strings(sets)

	byPath := map[string]*SourceRoot{}
	var order []*SourceRoot
	for _, set := range sets {
		isTest := strings.Contains(strings.ToLower(set), "test")
		for _, lang := range []string{"java", "kotlin"} {
			p := filepath.Join(moduleDir, "src", set, lang)
			if !exists(p) {
				continue
			}
			if sr, ok := byPath[p]; ok {
				sr.Languages = append(sr.Languages, lang)
				continue
			}
			sr := &SourceRoot{Path: p, Languages: []string{lang}, IsTest: isTest}
			byPath[p] = sr
			order = append(order, sr)
		}
	}
	out := make([]SourceRoot, len(order))
	for i, sr := range order {
		out[i] = *sr
	}
	return out
}

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
