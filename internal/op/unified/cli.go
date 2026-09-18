package unified

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/serfom256/refactorio/internal/cli"
	"github.com/serfom256/refactorio/internal/edit"
	"github.com/serfom256/refactorio/internal/history"
	"github.com/serfom256/refactorio/internal/op/usages"
	"github.com/serfom256/refactorio/internal/stats"
)

func init() {
	cli.RegisterCommand(cli.Command{
		Name:    "rename",
		Handler: HandleRename,
		Summary: "rename a symbol (class/function/type/field) across the project",
		Usage:   "rename in=<file> name=<symbol> to=<newName> [root=<dir>] [dry-run=true]",
		Help: `Renames a declared symbol and every reference to it across the project.
Language is auto-detected from the file extension.

arguments:
  in=<file>      file containing the symbol's declaration (required)
  name=<symbol>  the symbol to rename — a class, function, type, field,
                 or variable declared in that file (required)
  to=<newName>   the new name (required)
  root=<dir>     project root; defaults to the working directory
  dry-run=true   preview the edits without writing to disk

what gets renamed:
  - the declaration and all in-file usages
  - import statements / use paths in dependent files + their usages
  - qualified references (pkg.Symbol, Bar.method(), crate::foo::Bar)
  - inline fully-qualified references (com.foo.Bar) for Java/Kotlin
  - impl blocks (Rust), constructors (Java/Kotlin)
  - the file itself when its name matches the symbol (Bar.java -> Baz.java)

what is left alone:
  - strings, comments
  - local variables that share the name (let Foo = 1)
  - unrelated same-named symbols in other scopes

examples:
  refactorio rename in=src/main/java/com/foo/Bar.java name=Bar to=Baz
  refactorio rename in=pkg/foo.go name=Hello to=Greet dry-run=true
  refactorio rename in=src/foo.ts name=Foo to=Bar`,
	})

	cli.RegisterCommand(cli.Command{
		Name:    "move",
		Handler: HandleMove,
		Summary: "move a file to a new package/directory and update imports",
		Usage:   "move in=<file> to=<newPath-or-dir> [root=<dir>] [dry-run=true]",
		Help: `Moves a file and rewrites every import that references it.

arguments:
  in=<file>            file to move (required)
  to=<newPath-or-dir>  destination path, or a directory (the source
                       filename is appended automatically) (required)
  root=<dir>           project root; defaults to the working directory
  dry-run=true         preview without writing

what gets updated:
  - Java/Kotlin: package declaration, imports in dependents, same-package
    files that use the class get an explicit import, inline FQN references
  - Go: package declaration + import paths in importers
  - TS/JS: import paths in importers + relative imports inside the moved file
  - Rust: use paths across the crate

examples:
  refactorio move in=src/main/java/com/foo/Bar.java to=src/main/java/com/qux/
  refactorio move in=src/utils/foo.ts to=src/lib/foo.ts
  refactorio move in=pkg/foo.go to=lib/foo.go dry-run=true`,
	})

	cli.RegisterCommand(cli.Command{
		Name:    "delete",
		Handler: HandleDelete,
		Summary: "delete a file and remove its imports from dependents",
		Usage:   "delete in=<file> [root=<dir>] [dry-run=true] [force=true]",
		Help: `Deletes a file and removes import statements referencing it.

arguments:
  in=<file>     file to delete (required)
  root=<dir>    project root; defaults to the working directory
  dry-run=true  preview without writing
  force=true    delete even though references survive (breaks the build)

notes:
  - explicit and static imports / use statements are removed
  - mod declarations are removed (Rust)
  - wildcard imports are NOT removed (they cover the whole package)
  - files that still contain CODE references are listed by name, and the
    delete is REFUSED unless force=true; there is nothing to replace such a
    reference with, so it is a human decision
  - same-package users are included even though they have no import to clean,
    which is exactly the case a delete would otherwise break silently
  - a dependent whose only mention was the import is not listed: removing
    that import is the cleanup

example:
  refactorio delete in=src/main/java/com/foo/Bar.java dry-run=true`,
	})

	cli.RegisterCommand(cli.Command{
		Name:    "rename-package",
		Handler: HandleRenamePackage,
		Summary: "rename a package — owned (moves files) or external (rewrites refs)",
		Usage:   "rename-package from=<oldPkg> to=<newPkg> [root=<dir>] [dry-run=true]",
		Help: `Renames a package. The mode is chosen automatically:

  owned     the package has sources in the project: every file is moved to the
            new directory and imports are rewritten in dependents.
            Sub-packages are NOT affected.
  external  the package has no sources (javax.* -> jakarta.*, or swapping one
            dependency's API for another): imports and inline fully-qualified
            references are rewritten in place and NO file is moved, created or
            deleted. Sub-packages ARE included. Java and Kotlin only.

arguments:
  from=<oldPkg>  existing package (dotted for Java/Kotlin, import path for Go)
  to=<newPkg>    new package name
  root=<dir>     project root; defaults to the working directory
  dry-run=true   preview without writing

examples:
  refactorio rename-package from=com.foo to=com.bar
  refactorio rename-package from=javax.persistence to=jakarta.persistence
  refactorio rename-package from=example.com/proj/pkg to=example.com/proj/lib

notes:
  - external matching is on dot boundaries, so renaming javax.persistence
    rewrites javax.persistence.criteria but never javax.sql or
    javax.persistencex; strings and comments are never touched
  - external mode cannot see persistence.xml, web.xml, build coordinates or
    class names inside config strings — a clean run is not a finished migration
  - TypeScript/JavaScript and Rust have no package-rename equivalent —
    use 'move' for individual files.`,
	})

	cli.RegisterCommand(cli.Command{
		Name:    "usages",
		Handler: HandleUsages,
		Summary: "find where a symbol is declared and referenced",
		Usage:   "usages in=<file> name=<symbol> [format=text|json] [root=<dir>]",
		Help: `Reports where a symbol is declared and referenced across the project.
Read-only: no files are modified.

arguments:
  in=<file>      file containing the symbol's declaration (required)
  name=<symbol>  the symbol to look up (required)
  format=<fmt>   output format: text (default, human-readable) or json
  root=<dir>     project root; defaults to the working directory

ordering:
  1. primary declaration
  2. usages, sorted by file:line
  3. overload declarations (additional declarations) at the bottom

what gets reported:
  - the declaration site
  - all references the rename engine would touch — same scoping rules

what is NOT reported:
  - import / using / namespace declarations
  - file moves or path rewrites

example:
  refactorio usages in=src/main/java/com/foo/Bar.java name=Bar
  refactorio usages in=src/foo.ts name=Foo format=json`,
	})

	cli.RegisterCommand(cli.Command{
		Name:    "rollback",
		Handler: HandleRollback,
		Summary: "undo the last refactoring operation for the current project",
		Usage:   "rollback [root=<dir>]",
		Help: `Restores the files changed by the most recent operation. History is
per-project, stored under ~/.refactorio/projects/<hash>/history/ and keyed by
the absolute path of the project root. The project root is detected by walking
up from the working directory (or 'root=<dir>') looking for go.mod / Cargo.toml
/ settings.gradle / pom.xml / package.json / tsconfig.json. The stack holds
the last 10 operations. Reverses moves and restores deleted files.

Run 'refactorio history' to see the stack.`,
	})

	cli.RegisterCommand(cli.Command{
		Name:    "history",
		Handler: HandleHistory,
		Summary: "list recent refactoring operations for the current project",
		Usage:   "history [root=<dir>]",
		Help: `Lists the operations stored for the current project (newest last).
The project is identified by the absolute path of its root (auto-detected from
the working directory or 'root=<dir>').`,
	})
}

// applyWithHistory snapshots the edit set, applies it, then saves
// the snapshot under the project-scoped history directory.
func applyWithHistory(set *edit.Set, args *cli.Args, stdout io.Writer) error {
	command := rebuildCommand(args)
	projectRoot := resolveProjectRoot(args)
	cs, err := history.Snapshot(set, command, projectRoot)
	if err != nil {
		return fmt.Errorf("snapshot: %w", err)
	}
	if err := set.Apply(); err != nil {
		return err
	}
	if err := history.Save(cs); err != nil {
		fmt.Fprintf(stdout, "warning: changes applied but history save failed: %v\n", err)
	}
	return nil
}

// resolveProjectRoot uses the `root=` arg if present, otherwise the
// working directory, then walks up for a project marker.
func resolveProjectRoot(args *cli.Args) string {
	start, _ := args.Get("root")
	if start == "" {
		start, _ = os.Getwd()
	}
	return history.FindProjectRoot(start)
}

func rebuildCommand(args *cli.Args) string {
	return args.Subcommand + " " + strings.Join(args.RawPairs(), " ")
}

// HandleUsages: `refactorio usages in=<file> name=<symbol> [format=text|json] [root=<dir>]`
func HandleUsages(args *cli.Args, stdout, stderr io.Writer) error {
	inFile, err := args.Require("in")
	if err != nil {
		return err
	}
	name, err := args.Require("name")
	if err != nil {
		return err
	}
	format, _ := args.Get("format")
	if format == "" {
		format = "text"
	}
	if format != "text" && format != "json" {
		return fmt.Errorf("format must be 'text' or 'json', got %q", format)
	}
	rootArg, _ := args.Get("root")
	if unknown := args.Unknown("in", "name", "format", "root"); len(unknown) > 0 {
		return fmt.Errorf("unknown arguments: %v", unknown)
	}

	// usages is read-only, so it records language and outcome but no lines.
	stats.NoteContext(detectLang(inFile), false)

	result, err := usages.Find(inFile, name, rootArg, Rename)
	if err != nil {
		return err
	}
	if format == "json" {
		return usages.FormatJSON(stdout, result)
	}
	return usages.FormatText(stdout, result)
}

func HandleRollback(args *cli.Args, stdout, stderr io.Writer) error {
	rootArg, _ := args.Get("root")
	if unknown := args.Unknown("root"); len(unknown) > 0 {
		return fmt.Errorf("unknown arguments: %v", unknown)
	}
	_ = rootArg
	projectRoot := resolveProjectRoot(args)
	desc, err := history.Rollback(projectRoot)
	if err != nil {
		return err
	}
	fmt.Fprintln(stdout, desc)
	return nil
}

func HandleHistory(args *cli.Args, stdout, stderr io.Writer) error {
	if unknown := args.Unknown("root"); len(unknown) > 0 {
		return fmt.Errorf("unknown arguments: %v", unknown)
	}
	projectRoot := resolveProjectRoot(args)
	entries, err := history.List(projectRoot)
	if err != nil {
		return err
	}
	fmt.Fprintf(stdout, "project: %s\n", projectRoot)
	if len(entries) == 0 {
		fmt.Fprintln(stdout, "no history")
		return nil
	}
	for _, e := range entries {
		fmt.Fprintln(stdout, e)
	}
	return nil
}

// HandleRename: `refactorio rename in=<file> name=<symbol> to=<newName> [root=<dir>] [dry-run=true]`
func HandleRename(args *cli.Args, stdout, stderr io.Writer) error {
	inFile, err := args.Require("in")
	if err != nil {
		return err
	}
	name, err := args.Require("name")
	if err != nil {
		return err
	}
	to, err := args.Require("to")
	if err != nil {
		return err
	}
	dryRun, err := args.Bool("dry-run", false)
	if err != nil {
		return err
	}
	rootArg, _ := args.Get("root")
	if unknown := args.Unknown("in", "name", "to", "root", "dry-run"); len(unknown) > 0 {
		return fmt.Errorf("unknown arguments: %v", unknown)
	}

	stats.NoteContext(detectLang(inFile), dryRun)

	set, err := Rename(inFile, name, to, rootArg)
	if err != nil {
		return err
	}
	stats.Note(set.Measure())
	if dryRun {
		out, err := set.DryRun()
		if err != nil {
			return err
		}
		_, _ = io.WriteString(stdout, out)
		return nil
	}
	if err := applyWithHistory(set, args, stdout); err != nil {
		return err
	}
	fmt.Fprintf(stdout, "%d files modified, %d edits\n", len(set.Files()), set.Len())
	return nil
}

// HandleMove: `refactorio move in=<file> to=<newPath> [root=<dir>] [dry-run=true]`
func HandleMove(args *cli.Args, stdout, stderr io.Writer) error {
	inFile, err := args.Require("in")
	if err != nil {
		return err
	}
	to, err := args.Require("to")
	if err != nil {
		return err
	}
	dryRun, err := args.Bool("dry-run", false)
	if err != nil {
		return err
	}
	rootArg, _ := args.Get("root")
	if unknown := args.Unknown("in", "to", "root", "dry-run"); len(unknown) > 0 {
		return fmt.Errorf("unknown arguments: %v", unknown)
	}

	stats.NoteContext(detectLang(inFile), dryRun)

	set, warnings, err := Move(inFile, to, rootArg)
	if err != nil {
		return err
	}
	stats.Note(set.Measure())
	printWarnings(stdout, warnings)
	if dryRun {
		out, err := set.DryRun()
		if err != nil {
			return err
		}
		_, _ = io.WriteString(stdout, out)
		return nil
	}
	if err := applyWithHistory(set, args, stdout); err != nil {
		return err
	}
	fmt.Fprintf(stdout, "%d files modified, %d edits, %d moves\n",
		len(set.Files()), set.Len(), len(set.Moves()))
	return nil
}

func printWarnings(w io.Writer, warnings []string) {
	if len(warnings) == 0 {
		return
	}
	fmt.Fprintf(w, "warning: %d potential access issue(s) after move:\n", len(warnings))
	for _, msg := range warnings {
		fmt.Fprintf(w, "  - %s\n", msg)
	}
	fmt.Fprintln(w)
}

// HandleDelete: `refactorio delete in=<file> [root=<dir>] [dry-run=true]`
func HandleDelete(args *cli.Args, stdout, stderr io.Writer) error {
	inFile, err := args.Require("in")
	if err != nil {
		return err
	}
	dryRun, err := args.Bool("dry-run", false)
	if err != nil {
		return err
	}
	force, err := args.Bool("force", false)
	if err != nil {
		return err
	}
	rootArg, _ := args.Get("root")
	if unknown := args.Unknown("in", "root", "dry-run", "force"); len(unknown) > 0 {
		return fmt.Errorf("unknown arguments: %v", unknown)
	}

	stats.NoteContext(detectLang(inFile), dryRun)

	set, refs, err := Delete(inFile, rootArg)
	if err != nil {
		return err
	}
	stats.Note(set.Measure())
	if dryRun {
		out, err := set.DryRun()
		if err != nil {
			return err
		}
		_, _ = io.WriteString(stdout, out)
		printSurvivingRefs(stdout, refs)
		return nil
	}
	// Deleting a file that is still referenced leaves the project unable to
	// compile. Refuse by default rather than reporting it after the fact.
	if len(refs) > 0 && !force {
		printSurvivingRefs(stdout, refs)
		return fmt.Errorf("refusing to delete: %d file(s) still reference it — fix those, or re-run with force=true", len(refs))
	}
	if err := applyWithHistory(set, args, stdout); err != nil {
		return err
	}
	fmt.Fprintf(stdout, "deleted, cleaned %d files\n", set.Len())
	return nil
}

// HandleRenamePackage: `refactorio rename-package in=<dir> to=<newName> [root=<dir>] [dry-run=true]`
func HandleRenamePackage(args *cli.Args, stdout, stderr io.Writer) error {
	from, err := args.Require("from")
	if err != nil {
		return err
	}
	to, err := args.Require("to")
	if err != nil {
		return err
	}
	dryRun, err := args.Bool("dry-run", false)
	if err != nil {
		return err
	}
	rootArg, _ := args.Get("root")
	if unknown := args.Unknown("from", "to", "root", "dry-run"); len(unknown) > 0 {
		return fmt.Errorf("unknown arguments: %v", unknown)
	}

	stats.NoteContext("", dryRun)

	set, err := RenamePackage(from, to, rootArg)
	if err != nil {
		return err
	}
	stats.Note(set.Measure())
	if dryRun {
		out, err := set.DryRun()
		if err != nil {
			return err
		}
		_, _ = io.WriteString(stdout, out)
		return nil
	}
	if err := applyWithHistory(set, args, stdout); err != nil {
		return err
	}
	fmt.Fprintf(stdout, "%d files modified, %d edits, %d moves\n",
		len(set.Files()), set.Len(), len(set.Moves()))
	return nil
}

// printSurvivingRefs reports the files that still reference a deleted symbol.
// Naming them matters: the warning is a to-do list for manual cleanup, and a
// bare count gives the reader no way to check it or act on it.
func printSurvivingRefs(w io.Writer, refs []string) {
	if len(refs) == 0 {
		return
	}
	cwd, _ := os.Getwd()
	subject := "files still reference"
	if len(refs) == 1 {
		subject = "file still references"
	}
	fmt.Fprintf(w, "\nwarning: %d %s the deleted symbol and will not compile:\n", len(refs), subject)
	for _, r := range refs {
		fmt.Fprintf(w, "  %s\n", edit.RelTo(cwd, r))
	}
}
