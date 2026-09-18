---
name: refactorio
description: >
  Cross-file refactoring for Java, Kotlin, Go, TypeScript, JavaScript, Rust,
  C#, and Python 3 projects using the Refactorio CLI. Use when the user asks to
  rename a class/function/type/symbol, move a file to another package/directory,
  delete a file and clean up its imports, rename a package — including an
  external one such as javax.* -> jakarta.* — find where a symbol is used,
  or see local statistics on how
  Refactorio itself has been used. Triggers: "rename class X to Y",
  "move FileA to package/dir", "delete class Z and clean references",
  "rename package com.foo to com.bar", "migrate javax to jakarta",
  "swap dependency package X for Y", "where is Foo used", "find usages of Bar",
  "refactorio stats", "how much code has refactorio changed",
  "which refactorings fail most".
  Prefer this over hand-editing each file when the change spans multiple files.
---

# Refactorio — cross-file refactoring CLI

Refactorio applies structural refactorings (rename, move, delete, rename-package)
across an entire project in one pass, updating every reference and import. It
also has a read-only `usages` command for finding where a symbol is referenced.
Use it instead of manually `Edit`-ing each file: it is faster, deterministic,
and does not consume context loading every dependent file.

## When to use

Use Refactorio when ALL of these hold:
- The target language is Java, Kotlin, Go, TypeScript, JavaScript, Rust, C#, or Python 3.
- The action is one of: rename a symbol, move a file, delete a file,
  rename a package (owned or external), or find usages of a symbol.
- The change/lookup spans more than one file.

Do NOT use it for: in-method edits, logic changes, formatting, or languages
not listed above. Fall back to manual edits there.

## Availability

`refactorio` is installed at `~/.apps/refactorio`, which is on `PATH`. Invoke it
directly from any project directory — there is nothing to build first:

```bash
refactorio <command> ...
```

If the command is ever not found, reinstall from the Refactorio source tree:

```bash
go build -trimpath -ldflags="-s -w" -o ~/.apps/refactorio ./cmd/refactorio
```

## Commands

Language is auto-detected from the file extension in `in=`. Every command
supports `--help` for the full usage including arguments table and examples.

| Command | Usage |
|---|---|
| rename | `refactorio rename in=<file> name=<symbol> to=<newName>` |
| move | `refactorio move in=<file> to=<newPath-or-dir>` |
| delete | `refactorio delete in=<file> [force=true]` |
| rename-package | `refactorio rename-package from=<oldPkg> to=<newPkg>` |
| usages | `refactorio usages in=<file> name=<symbol> [format=text\|json]` |
| rollback | `refactorio rollback` |
| history | `refactorio history` |
| stats | `refactorio stats [scope=project\|all] [since=7d]` |

Common args: `root=<dir>` (project root, defaults to cwd), `dry-run=true`
(preview without writing). Get detailed help with `refactorio <cmd> --help`.

## Required workflow

1. **Build** the binary if not already present.
2. **Dry-run first.** Always run with `dry-run=true` and show the user the
   planned edits. Never apply blind.
   ```bash
   refactorio rename in=src/main/java/com/foo/Bar.java name=Bar to=Baz dry-run=true
   ```
3. **Review** the output with the user. Confirm the file list and edit count
   look right, and that no unrelated files are touched. For Java moves,
   surface any package-private access warnings printed by the tool.
4. **Apply** by re-running without `dry-run`.
5. **Verify** by building/compiling the project (e.g. `mvn compile`,
   `gradle build`, `go build`, `tsc --noEmit`, `cargo check`, `dotnet build`,
   `python -m py_compile`).
6. **Rollback** if the result is wrong: `refactorio rollback` restores the
   previous state. History is per-project (last 10 operations).

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

# C# — rename a class (renames file + updates references across namespaces)
refactorio rename in=src/Foo.cs name=Foo to=Bar

# Python 3 — rename a top-level class/function (updates from-imports + module.X refs)
refactorio rename in=src/foo.py name=Foo to=Bar

# Move a file to another package (to= may be a directory)
refactorio move in=src/main/java/com/foo/Bar.java to=src/main/java/com/qux/

# Delete a file and remove imports in dependents
refactorio delete in=src/foo.ts

# Rename a whole package (Java/Kotlin dotted, Go import path)
refactorio rename-package from=com.foo to=com.bar

# Find usages of a symbol (read-only)
refactorio usages in=src/foo.py name=Foo
refactorio usages in=src/Bar.cs name=DoIt format=json
```

## Using `usages` before `rename`

When the user asks where a symbol is used, run `usages` and show the output.
When a rename touches many files, run `usages` first to give them a preview of
the blast radius — then run `rename` with `dry-run=true` for the exact edit
plan. The two commands share the same scoping logic.

## delete refuses when references survive

`delete` reports every file that still references the deleted symbol, naming
them, and **refuses to run** if there are any:

```
warning: 2 files still reference the deleted symbol and will not compile:
  src/main/java/com/ex/SamePackageUser.java
  src/main/java/com/other/RealUser.java
error: refusing to delete: 2 file(s) still reference it — fix those, or re-run with force=true
```

Same-package users are included even though they have no import to clean —
that is the case where a silent delete breaks the build. Dependents whose only
mention was an import are not reported, because removing that import is the
cleanup.

Resolve the references first. Only pass `force=true` when the user has
explicitly decided to delete anyway and accept a broken build.

## rename-package handles owned and external packages

Which mode runs is decided automatically:

- **Owned** (the package has sources in the project): files are moved to the
  new directory, package declarations rewritten, imports updated.
- **External** (no sources — `javax.*` → `jakarta.*`, or swapping one
  dependency's API for another): references are rewritten in place and no file
  is moved, created or deleted.

```bash
refactorio rename-package from=javax.persistence to=jakarta.persistence dry-run=true
```

External mode rewrites imports (plain, static, wildcard) and inline
fully-qualified references, in Java and Kotlin. It matches on dot boundaries,
which is the reason to prefer it over `sed` for this job: renaming
`javax.persistence` rewrites `javax.persistence.criteria.Root` but leaves
`javax.sql.DataSource` and `javax.persistencex` alone. Strings and comments are
untouched, as everywhere else.

Warn the user about what a lexer cannot see: `persistence.xml`, `web.xml`,
Gradle/Maven coordinates, Spring XML, and provider class names in config
strings all need changing by hand. A clean run is not a finished migration.

## Reading dry-run output

Output is compact by design. Paths are relative to the working directory, each
file is named once, and edits sharing a replacement and reason are collapsed
onto one line listing the line numbers they occur on:

```
20 files, 45 edits, 0 moves, 0 deletes
  internal/edit/edit.go (1 edit)
    60  "NewSet" -> "CreateSet"  rename declaration NewSet -> CreateSet
  internal/edit/edit_test.go (8 edits)
    37,47,56,69,87,107,123,134  "NewSet" -> "CreateSet"  rename NewSet -> CreateSet
```

Two edits on the same line are shown as `5(2)` rather than a repeated `5,5`.

Read the plan from this output. Do NOT open each listed file to verify the
edits — that discards the reason for using the tool. Open a file only when the
plan itself looks wrong and you need to judge a specific site.

## Usage statistics

Every invocation is recorded locally in `~/.refactorio/stats.jsonl` — command,
argument names, language, success, and how many distinct source lines were
rewritten. Nothing is transmitted anywhere, and symbol names, in-project file
paths and raw error text are deliberately not recorded.

Run `refactorio stats` when the user asks what they have been using Refactorio
for, which refactors fail most often, or how much code it has rewritten. Add
`scope=all` to cover every project rather than the current one.

If a user objects to the recording, tell them to set `REFACTORIO_NO_STATS=1`;
`refactorio stats reset=true` clears what is already stored.

## Limitations to warn the user about

When proposing a Refactorio refactor, mention the relevant caveat:
- **Overloads**: renaming a method renames ALL overloads with that name.
- **Inheritance**: overrides in subclasses / interface impls are not tracked.
- **Cross-language**: renaming a Java class does NOT update Kotlin files that
  import it (each language is indexed separately).
- **Reflection / macros / annotations-by-string / proc macros**: not detected;
  flag for manual review.
- **Python local variables**: only top-level symbols (class/def/module-level
  variables) can be renamed; locals inside function bodies are skipped.
- **C# cross-project**: `.csproj` boundaries aren't tracked; usages may span
  projects that aren't reachable from the index root.
- **Same-name collisions across packages** may need manual disambiguation.
- **Package-private access (Java)**: when moving a class out of its package,
  Refactorio prints warnings about package-private references that will break.
  Decide with the user whether to make those symbols public first.

Always recommend committing (or having a clean git tree) before applying, and
verify with a compile afterward.
