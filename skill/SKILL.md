---
name: refactorio
description: >
  Cross-file refactoring for Java, Kotlin, Go, TypeScript, JavaScript, and
  Rust projects using the Refactorio CLI. Use when the user asks to rename a
  class/function/type/symbol, move a file to another package/directory, delete
  a file and clean up its imports, or rename a package across the whole
  project. Triggers: "rename class X to Y", "move FileA to package/dir",
  "delete class Z and clean references", "rename package com.foo to com.bar".
  Prefer this over hand-editing each file when the change spans multiple files.
---

# Refactorio — cross-file refactoring CLI

Refactorio applies structural refactorings (rename, move, delete, rename-package)
across an entire project in one pass, updating every reference and import. Use it
instead of manually `Edit`-ing each file: it is faster, deterministic, and does
not consume context loading every dependent file.

> NOTE: This skill is staged in `skill/` and is NOT yet installed under
> `.claude/skills/`. Do not invoke Refactorio on real user code until the tool
> has been validated on the target project. Treat it as opt-in.

## When to use

Use Refactorio when ALL of these hold:
- The target language is Java, Kotlin, Go, TypeScript, JavaScript, or Rust.
- The refactoring is one of: rename a symbol, move a file, delete a file,
  rename a package.
- The change spans more than one file (cross-file references / imports).

Do NOT use it for: in-method edits, logic changes, formatting, or languages
not listed above. Fall back to manual edits there.

## Build the binary first

The CLI must be built before use (pure Go, no cgo):

```bash
go build -o refactorio ./cmd/refactorio
```

Or run without building:

```bash
go run ./cmd/refactorio <command> ...
```

## Commands

Language is auto-detected from the file extension in `in=`.

| Command | Usage |
|---|---|
| rename | `refactorio rename in=<file> name=<symbol> to=<newName>` |
| move | `refactorio move in=<file> to=<newPath-or-dir>` |
| delete | `refactorio delete in=<file>` |
| rename-package | `refactorio rename-package from=<oldPkg> to=<newPkg>` |
| rollback | `refactorio rollback` |
| history | `refactorio history` |

Common args: `root=<dir>` (project root, defaults to cwd), `dry-run=true`
(preview without writing).

## Required workflow

1. **Build** the binary if not already present.
2. **Dry-run first.** Always run with `dry-run=true` and show the user the
   planned edits. Never apply blind.
   ```bash
   refactorio rename in=src/main/java/com/foo/Bar.java name=Bar to=Baz dry-run=true
   ```
3. **Review** the output with the user. Confirm the file list and edit count
   look right, and that no unrelated files are touched.
4. **Apply** by re-running without `dry-run`.
5. **Verify** by building/compiling the project (e.g. `mvn compile`,
   `gradle build`, `go build`, `tsc --noEmit`, `cargo check`).
6. **Rollback** if the result is wrong: `refactorio rollback` restores the
   previous state (history holds the last 10 operations).

## Examples per language

```bash
# Java — rename a class (also renames the file + constructors + imports)
refactorio rename in=src/main/java/com/foo/Bar.java name=Bar to=Baz

# Kotlin
refactorio rename in=src/main/kotlin/com/foo/Bar.kt name=Bar to=Baz

# Go — rename an exported symbol (updates pkg.Symbol references)
refactorio rename in=pkg/foo.go name=Hello to=Greet

# TypeScript / JavaScript
refactorio rename in=src/foo.ts name=Foo to=Bar

# Rust — rename a struct (also renames impl blocks + use paths)
refactorio rename in=src/foo.rs name=Bar to=Baz

# Move a file to another package (to= may be a directory)
refactorio move in=src/main/java/com/foo/Bar.java to=src/main/java/com/qux/

# Delete a file and remove imports in dependents
refactorio delete in=src/foo.ts

# Rename a whole package (Java/Kotlin dotted, Go import path)
refactorio rename-package from=com.foo to=com.bar
```

## Limitations to warn the user about

When proposing a Refactorio refactor, mention the relevant caveat:
- **Overloads**: renaming a method renames ALL overloads with that name.
- **Inheritance**: overrides in subclasses / interface impls are not tracked.
- **Cross-language**: renaming a Java class does NOT update Kotlin files that
  import it (each language is indexed separately).
- **Reflection / macros / annotations-by-string**: not detected; flag for
  manual review.
- **Same-name collisions across packages** may need manual disambiguation.

Always recommend committing (or having a clean git tree) before applying, and
verify with a compile afterward.
