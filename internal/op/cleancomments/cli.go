package cleancomments

import (
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/serfom256/refactorio/internal/cli"
	"github.com/serfom256/refactorio/internal/edit"
	"github.com/serfom256/refactorio/internal/op/unified"
	"github.com/serfom256/refactorio/internal/stats"
)

func init() {
	cli.RegisterCommand(cli.Command{
		Name:    "clean-comments",
		Handler: Handle,
		Summary: "remove comments, keeping directives, licence headers and TODOs",
		Usage:   "clean-comments in=<file-or-dir> [mode=report|apply] [dry-run=true]",
		Help: `Removes the comments from a file or directory tree, on the premise
that code should document itself. Reports by default and writes nothing
until mode=apply.

Three things are kept, and none of them is a judgement about the prose:

  directives       //go:embed, //nolint, # noqa, eslint-*, @ts-*, +build
                   — an instruction to the build, not documentation.
                   Removing one changes what the build does.
  licence headers  copyright, licence and SPDX notices.
  TODO/FIXME       a backlog item rather than documentation.

Everything else goes: doc comments, Python docstrings, commented-out
code, section banners, trailing comments. There are no categories and no
threshold to tune.

arguments:
  in=<path>      a file, or a directory scanned recursively
  mode=report    report (default) or apply
  dry-run=true   with mode=apply, print the edits without writing
  root=<dir>     project root for the rollback history

Applied runs go through the normal history, so "refactorio rollback"
undoes a cleaning pass like any other refactor.`,
	})
}

func Handle(args *cli.Args, stdout, stderr io.Writer) error {
	in, err := args.Require("in")
	if err != nil {
		return err
	}
	if unknown := args.Unknown("in", "mode", "dry-run", "root"); len(unknown) > 0 {
		return fmt.Errorf("unknown arguments: %v", unknown)
	}
	dryRun, err := args.Bool("dry-run", false)
	if err != nil {
		return err
	}
	mode, _ := args.Get("mode")
	if mode == "" {
		mode = "report"
	}
	if mode != "report" && mode != "apply" {
		return fmt.Errorf("mode must be \"report\" or \"apply\", got %q", mode)
	}

	files, err := collect(in)
	if err != nil {
		return err
	}
	if len(files) == 0 {
		return fmt.Errorf("no source files Refactorio recognises under %s", in)
	}

	stats.NoteContext(Lang(files[0]), dryRun || mode == "report")

	set, res, err := Plan(files)
	if err != nil {
		return err
	}
	stats.Note(set.Measure())

	if mode == "report" {
		return writeReport(stdout, res)
	}
	if set.Len() == 0 {
		fmt.Fprintf(stdout, "nothing to remove: %d comments in %d files, all of them kept\n",
			res.Comments, res.Scanned)
		return nil
	}
	if dryRun {
		out, err := set.DryRun()
		if err != nil {
			return err
		}
		_, _ = io.WriteString(stdout, out)
		return nil
	}
	if err := unified.ApplyWithHistory(set, args, stdout); err != nil {
		return err
	}
	lines := 0
	for _, c := range res.Candidates {
		lines += c.Lines()
	}
	fmt.Fprintf(stdout, "%d comments removed from %d files (%d lines)\n",
		set.Len(), len(set.Files()), lines)
	if len(res.Kept) > 0 {
		fmt.Fprintf(stdout, "%d kept: %s\n", len(res.Kept), keptSummary(res))
	}
	return nil
}

func writeReport(w io.Writer, res *Result) error {
	var b strings.Builder
	cwd, _ := os.Getwd()

	fmt.Fprintf(&b, "%d comments in %d files: %d to remove, %d kept\n\n",
		res.Comments, res.Scanned, len(res.Candidates), len(res.Kept))

	if len(res.Kept) > 0 {
		b.WriteString("kept:\n")
		for _, k := range res.Kept {
			fmt.Fprintf(&b, "  %s:%d  %-34s %s\n",
				edit.RelTo(cwd, k.File), k.Line, "("+k.Reason+")", truncate(k.Text, 58))
		}
		b.WriteString("\n")
	}

	if len(res.Candidates) == 0 {
		b.WriteString("nothing to remove\n")
		_, err := io.WriteString(w, b.String())
		return err
	}

	byFile := map[string][]Candidate{}
	var order []string
	for _, c := range res.Candidates {
		if _, seen := byFile[c.File]; !seen {
			order = append(order, c.File)
		}
		byFile[c.File] = append(byFile[c.File], c)
	}
	b.WriteString("to remove:\n")
	for _, f := range order {
		cs := byFile[f]
		lines := 0
		for _, c := range cs {
			lines += c.Lines()
		}
		fmt.Fprintf(&b, "  %-56s %d comments, %d lines\n",
			edit.RelTo(cwd, f), len(cs), lines)
	}
	fmt.Fprintf(&b, "\nre-run with mode=apply to remove them (add dry-run=true to see every edit first)\n")

	_, err := io.WriteString(w, b.String())
	return err
}

func keptSummary(res *Result) string {
	counts := map[string]int{}
	for _, k := range res.Kept {
		counts[k.Reason]++
	}
	names := make([]string, 0, len(counts))
	for n := range counts {
		names = append(names, n)
	}
	sort.Strings(names)
	parts := make([]string, 0, len(names))
	for _, n := range names {
		parts = append(parts, fmt.Sprintf("%d %s", counts[n], n))
	}
	return strings.Join(parts, ", ")
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n-1] + "…"
}

func collect(in string) ([]string, error) {
	abs, err := filepath.Abs(in)
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(abs)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		if Lang(abs) == "" {
			return nil, fmt.Errorf("unsupported file type: %s", filepath.Ext(abs))
		}
		return []string{abs}, nil
	}
	var out []string
	err = filepath.WalkDir(abs, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil //nolint:nilerr // an unreadable directory is skipped, not fatal
		}
		if d.IsDir() {
			name := d.Name()
			if path != abs && (strings.HasPrefix(name, ".") || skipDir(name)) {
				return filepath.SkipDir
			}
			return nil
		}
		if Lang(path) != "" {
			out = append(out, path)
		}
		return nil
	})
	return out, err
}

func skipDir(name string) bool {
	switch name {
	case "build", "target", "out", "bin", "obj", "dist", "node_modules",
		"vendor", "venv", "__pycache__", "testdata":
		return true
	}
	return false
}
