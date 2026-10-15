package doctor

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/serfom256/refactorio/internal/cli"
	"github.com/serfom256/refactorio/internal/project"
)

func init() {
	cli.RegisterCommand(cli.Command{
		Name:    "doctor",
		Handler: run,
		Summary: "report the project Refactorio detected and the files it can see",
		Usage:   "doctor [root=<dir>]",
		Help: `Prints the project root, build system, modules, source roots and
file counts that every other command works from.

Run it when a command reports a symbol or package it should have found,
or when a rename touches fewer files than expected. A module missing
here is a module no refactor will reach.

arguments:
  root=<dir>   where to start detection (default: current directory)`,
	})
}

var langExts = []struct {
	name string
	exts []string
}{
	{"java", []string{".java"}},
	{"kotlin", []string{".kt", ".kts"}},
	{"go", []string{".go"}},
	{"typescript", []string{".ts", ".tsx"}},
	{"javascript", []string{".js", ".jsx", ".mjs", ".cjs"}},
	{"rust", []string{".rs"}},
	{"csharp", []string{".cs"}},
	{"python", []string{".py"}},
}

func run(args *cli.Args, stdout, stderr io.Writer) error {
	if unknown := args.Unknown("root"); len(unknown) > 0 {
		return fmt.Errorf("unknown arguments: %v", unknown)
	}
	root, _ := args.Get("root")
	if root == "" {
		root, _ = os.Getwd()
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return err
	}

	var b strings.Builder
	p, detectErr := project.DetectFrom(abs)
	if detectErr != nil {
		fmt.Fprintf(&b, "start        %s\n", abs)
		fmt.Fprintf(&b, "jvm project  none detected (%v)\n\n", detectErr)
		writeCensus(&b, "files below start", censusDir(abs))
		fmt.Fprintf(&b, "\nGo, TypeScript, Rust, C# and Python commands index from the\n"+
			"directory given by root= and need no gradle or maven project.\n")
		_, err := io.WriteString(stdout, b.String())
		return err
	}

	fmt.Fprintf(&b, "project root %s\n", p.Root)
	fmt.Fprintf(&b, "build system %s\n", p.Kind)
	if abs != p.Root {
		fmt.Fprintf(&b, "started from %s (climbed to the outermost project root)\n", abs)
	}

	withSrc := 0
	roots := 0
	for _, m := range p.Modules {
		if len(m.SourceRoots) > 0 {
			withSrc++
		}
		roots += len(m.SourceRoots)
	}
	fmt.Fprintf(&b, "modules      %d (%d with source roots, %d source roots in total)\n\n",
		len(p.Modules), withSrc, roots)

	names := make([]string, len(p.Modules))
	width := len("module")
	for i, m := range p.Modules {
		names[i] = m.Name
		if names[i] == "" || names[i] == ":" {
			names[i] = "<root>"
		}
		if len(names[i]) > width {
			width = len(names[i])
		}
	}
	fmt.Fprintf(&b, "%-*s %5s %6s\n", width, "module", "roots", "files")
	for i, m := range p.Modules {
		n := 0
		for _, sr := range m.SourceRoots {
			n += len(filesUnder(sr.Path))
		}
		fmt.Fprintf(&b, "%-*s %5d %6d\n", width, names[i], len(m.SourceRoots), n)
	}

	b.WriteString("\n")
	writeCensus(&b, "indexed files", censusProject(p))

	if roots == 0 {
		fmt.Fprintf(&b, "\nwarning: no source roots found — no refactoring command can see this\n"+
			"project. Expected a conventional layout such as <module>/src/main/java.\n")
	}
	_, err = io.WriteString(stdout, b.String())
	return err
}

func censusProject(p *project.Project) map[string]int {
	out := map[string]int{}
	for _, l := range langExts {
		files, err := p.SourceFiles(l.exts...)
		if err == nil && len(files) > 0 {
			out[l.name] = len(files)
		}
	}
	return out
}

func censusDir(dir string) map[string]int {
	byExt := map[string]int{}
	for _, path := range filesUnder(dir) {
		byExt[strings.ToLower(filepath.Ext(path))]++
	}
	out := map[string]int{}
	for _, l := range langExts {
		n := 0
		for _, e := range l.exts {
			n += byExt[e]
		}
		if n > 0 {
			out[l.name] = n
		}
	}
	return out
}

func filesUnder(dir string) []string {
	var out []string
	_ = filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil //nolint:nilerr // an unreadable directory is skipped, not fatal
		}
		if d.IsDir() {
			name := d.Name()
			if path != dir && (strings.HasPrefix(name, ".") || isBuildDir(name)) {
				return filepath.SkipDir
			}
			return nil
		}
		out = append(out, path)
		return nil
	})
	return out
}

func isBuildDir(name string) bool {
	switch name {
	case "build", "target", "out", "bin", "node_modules", "dist", "vendor":
		return true
	}
	return false
}

func writeCensus(b *strings.Builder, label string, census map[string]int) {
	if len(census) == 0 {
		fmt.Fprintf(b, "%s: none\n", label)
		return
	}
	names := make([]string, 0, len(census))
	for n := range census {
		names = append(names, n)
	}
	sort.Slice(names, func(i, j int) bool {
		if census[names[i]] != census[names[j]] {
			return census[names[i]] > census[names[j]]
		}
		return names[i] < names[j]
	})
	parts := make([]string, 0, len(names))
	for _, n := range names {
		parts = append(parts, fmt.Sprintf("%s %d", n, census[n]))
	}
	fmt.Fprintf(b, "%s: %s\n", label, strings.Join(parts, ", "))
}
