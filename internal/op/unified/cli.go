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
	cli.RegisterHandler("rename", HandleRename)
	cli.RegisterHandler("move", HandleMove)
	cli.RegisterHandler("delete", HandleDelete)
	cli.RegisterHandler("rename-package", HandleRenamePackage)
	cli.RegisterHandler("rollback", HandleRollback)
	cli.RegisterHandler("history", HandleHistory)
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

	set, err := Move(inFile, to, rootArg)
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
