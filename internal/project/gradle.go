package project

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// loadGradle resolves a gradle project's modules by parsing
// settings.gradle{.kts}. If no settings file is present (single-module
// build.gradle layout), the root itself is the only module.
//
// We do not evaluate the gradle DSL. The regex extracts every
// `include 'path'` / `include(":path")` argument literal we can see.
// Programmatic registration (loops, function calls) is invisible to us
// — flagged as a known limit in project.go.
func loadGradle(root string) (*Project, error) {
	p := &Project{Root: root, Kind: KindGradle}

	includes, err := parseGradleSettings(root)
	if err != nil {
		return nil, err
	}

	// Root module — always present, even if there are subprojects.
	rootMod := &Module{
		Name:        ":",
		Path:        root,
		SourceRoots: standardSourceRoots(root),
	}
	p.Modules = append(p.Modules, rootMod)

	for _, inc := range includes {
		// Gradle paths use ':' as separator; translate to filesystem.
		rel := strings.ReplaceAll(strings.TrimPrefix(inc, ":"), ":", string(filepath.Separator))
		modPath := filepath.Join(root, rel)
		if _, err := os.Stat(modPath); err != nil {
			// Listed but missing on disk — settings file lies. Skip
			// silently; alternatives are noisy logs that we'd then
			// need to plumb up to the CLI layer.
			continue
		}
		p.Modules = append(p.Modules, &Module{
			Name:        ":" + inc,
			Path:        modPath,
			SourceRoots: standardSourceRoots(modPath),
		})
	}
	return p, nil
}

// gradleIncludeArg captures the path literal of a single include() arg.
// It matches single-quoted, double-quoted, or triple-quoted strings
// containing alphanumerics, underscores, hyphens, dots, and colons.
// Comments are pre-stripped before this runs.
var gradleIncludeArg = regexp.MustCompile(`['"]([:.\w\-]+)['"]`)

// gradleIncludeStmt captures the parenthesised or whitespace-delimited
// argument list of a single `include` call. Multiple args separated by
// commas are picked apart by gradleIncludeArg afterwards.
var gradleIncludeStmt = regexp.MustCompile(`(?m)^\s*include\s*[\(\s]([^)\n]+)\)?`)

func parseGradleSettings(root string) ([]string, error) {
	// .kts has priority because Kotlin DSL is more common in new
	// projects; both are scanned the same way.
	var bytes []byte
	for _, name := range []string{"settings.gradle.kts", "settings.gradle"} {
		b, err := os.ReadFile(filepath.Join(root, name))
		if err == nil {
			bytes = b
			break
		}
	}
	if bytes == nil {
		// Single-module project with only build.gradle{.kts} is legal.
		return nil, nil
	}
	stripped := stripGroovyKotlinComments(string(bytes))

	var includes []string
	seen := map[string]struct{}{}
	for _, m := range gradleIncludeStmt.FindAllStringSubmatchIndex(stripped, -1) {
		argList := stripped[m[2]:m[3]]
		for _, arg := range gradleIncludeArg.FindAllStringSubmatch(argList, -1) {
			path := strings.TrimPrefix(arg[1], ":")
			if path == "" {
				continue
			}
			if _, dup := seen[path]; dup {
				continue
			}
			seen[path] = struct{}{}
			includes = append(includes, path)
		}
	}
	return includes, nil
}

// stripGroovyKotlinComments removes // and /* */ style comments. Both
// Groovy and Kotlin gradle scripts share this comment syntax. Strings
// are NOT stripped — but include statements use string literals as
// arguments, so we have to leave them alone.
func stripGroovyKotlinComments(s string) string {
	var out strings.Builder
	out.Grow(len(s))
	i := 0
	for i < len(s) {
		if i+1 < len(s) && s[i] == '/' && s[i+1] == '/' {
			j := i + 2
			for j < len(s) && s[j] != '\n' {
				j++
			}
			i = j
			continue
		}
		if i+1 < len(s) && s[i] == '/' && s[i+1] == '*' {
			j := i + 2
			for j+1 < len(s) && !(s[j] == '*' && s[j+1] == '/') {
				j++
			}
			if j+1 < len(s) {
				i = j + 2
			} else {
				i = len(s)
			}
			continue
		}
		out.WriteByte(s[i])
		i++
	}
	return out.String()
}

