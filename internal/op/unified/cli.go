package unified

import (
	"fmt"
	"io"
	"strings"

	"github.com/serfom256/refactorio/internal/cli"
	"github.com/serfom256/refactorio/internal/edit"
	"github.com/serfom256/refactorio/internal/history"
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
		Usage:   "delete in=<file> [root=<dir>] [dry-run=true]",
		Help: `Deletes a file and removes import statements referencing it.

arguments:
  in=<file>     file to delete (required)
  root=<dir>    project root; defaults to the working directory
  dry-run=true  preview without writing

notes:
  - explicit and static imports / use statements are removed
  - mod declarations are removed (Rust)
  - wildcard imports are NOT removed (they cover the whole package)
  - files that still contain CODE references are reported as a warning;
    these need manual cleanup since there is nothing to replace them with

example:
  refactorio delete in=src/main/java/com/foo/Bar.java dry-run=true`,
	})

	cli.RegisterCommand(cli.Command{
		Name:    "rename-package",
		Handler: HandleRenamePackage,
		Summary: "rename a whole package and update all imports",
		Usage:   "rename-package from=<oldPkg> to=<newPkg> [root=<dir>] [dry-run=true]",
		Help: `Renames every file in a package, moves them to the new directory,
and rewrites imports in all dependents. Sub-packages are NOT affected.

arguments:
  from=<oldPkg>  existing package (dotted for Java/Kotlin, import path for Go)
  to=<newPkg>    new package name
  root=<dir>     project root; defaults to the working directory
  dry-run=true   preview without writing

examples:
  refactorio rename-package from=com.foo to=com.bar
  refactorio rename-package from=example.com/proj/pkg to=example.com/proj/lib

note: TypeScript/JavaScript and Rust have no package-rename equivalent —
use 'move' for individual files.`,
	})

	cli.RegisterCommand(cli.Command{
		Name:    "rollback",
		Handler: HandleRollback,
		Summary: "undo the last refactoring operation",
		Usage:   "rollback",
		Help: `Restores the files changed by the most recent operation. History
holds the last 10 operations in ~/.refactorio/history/. Reverses moves
and restores deleted files. Run 'refactorio history' to see the stack.`,
	})

	cli.RegisterCommand(cli.Command{
		Name:    "history",
		Handler: HandleHistory,
		Summary: "list recent refactoring operations",
		Usage:   "history",
		Help:    `Lists the operations stored in ~/.refactorio/history/ (newest last).`,
	})
}

// applyWithHistory snapshots the edit set, applies it, then saves
// the snapshot to ~/.refactorio/history/ for rollback.
func applyWithHistory(set *edit.Set, args *cli.Args, stdout io.Writer) error {
	command := rebuildCommand(args)
	cs, err := history.Snapshot(set, command)
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

func rebuildCommand(args *cli.Args) string {
	return args.Subcommand + " " + strings.Join(args.RawPairs(), " ")
}

func HandleRollback(args *cli.Args, stdout, stderr io.Writer) error {
	desc, err := history.Rollback()
	if err != nil {
		return err
	}
	fmt.Fprintln(stdout, desc)
	return nil
}

func HandleHistory(args *cli.Args, stdout, stderr io.Writer) error {
	entries, err := history.List()
	if err != nil {
		return err
	}
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

	set, err := Rename(inFile, name, to, rootArg)
	if err != nil {
		return err
	}
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

	set, warnings, err := Move(inFile, to, rootArg)
	if err != nil {
		return err
	}
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
	rootArg, _ := args.Get("root")
	if unknown := args.Unknown("in", "root", "dry-run"); len(unknown) > 0 {
		return fmt.Errorf("unknown arguments: %v", unknown)
	}

	set, refs, err := Delete(inFile, rootArg)
	if err != nil {
		return err
	}
	if dryRun {
		out, err := set.DryRun()
		if err != nil {
			return err
		}
		_, _ = io.WriteString(stdout, out)
		if len(refs) > 0 {
			fmt.Fprintf(stdout, "\nwarning: %d files still reference the deleted file\n", len(refs))
		}
		return nil
	}
	if err := applyWithHistory(set, args, stdout); err != nil {
		return err
	}
	fmt.Fprintf(stdout, "deleted, cleaned %d files\n", set.Len())
	if len(refs) > 0 {
		fmt.Fprintf(stdout, "warning: %d files still reference the deleted file\n", len(refs))
	}
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

	set, err := RenamePackage(from, to, rootArg)
	if err != nil {
		return err
	}
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
