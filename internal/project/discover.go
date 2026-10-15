package project

import (
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

var skipDirs = map[string]struct{}{
	"build":        {},
	"target":       {},
	"out":          {},
	"bin":          {},
	"node_modules": {},
	"dist":         {},
}

func (p *Project) Discover() {
	declared := make(map[string]struct{}, len(p.Modules))
	for _, m := range p.Modules {
		declared[filepath.Clean(m.Path)] = struct{}{}
	}

	var found []string
	_ = filepath.WalkDir(p.Root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || !d.IsDir() {
			return nil //nolint:nilerr // see comment
		}
		name := d.Name()
		if path != p.Root {
			if _, skip := skipDirs[name]; skip || strings.HasPrefix(name, ".") {
				return filepath.SkipDir
			}
		}
		if name != "src" {
			return nil
		}
		modDir := filepath.Dir(path)
		if _, ok := declared[modDir]; !ok {
			found = append(found, modDir)
			declared[modDir] = struct{}{}
		}
		return filepath.SkipDir
	})

	sort.Strings(found)
	for _, modDir := range found {
		roots := standardSourceRoots(modDir)
		if len(roots) == 0 {
			continue
		}
		p.Modules = append(p.Modules, &Module{
			Name:        p.moduleName(modDir),
			Path:        modDir,
			SourceRoots: roots,
		})
	}
}

func (p *Project) moduleName(modDir string) string {
	rel, err := filepath.Rel(p.Root, modDir)
	if err != nil || rel == "." {
		return ""
	}
	if p.Kind == KindGradle {
		return ":" + strings.ReplaceAll(rel, string(os.PathSeparator), ":")
	}
	return rel
}
