<p align="center">
  <img src="icon/refactorio.png" alt="Refactorio" width="150">
</p>

# Refactorio

A cross-file refactoring CLI for Java, Kotlin, Go, TypeScript, JavaScript, Rust, C#, and Python 3 projects. Pure Go, zero dependencies, no LSP or compiler required.

Refactorio uses hand-rolled lexers and convention-based rules to find and rename symbols across an entire project. It is intentionally not 100% precise (no type system, no semantic analysis) but handles the common cases correctly and flags uncertain edits with confidence markers.

## Contents

- [Installation](#installation) · [Supported languages](#supported-languages) · [Commands](#commands)
- Commands in detail: [rename](#rename) · [move](#move) · [delete](#delete) · [usages](#usages) · [rename-package](#rename-package) · [clean-comments](#clean-comments) · [stats](#stats) · [doctor](#doctor)
- [Arguments reference](#arguments-reference) · [Rollback](#rollback) · [Dry run](#dry-run)
- [Per-language limitations](#per-language-limitations) — read this before trusting a rename
- [Multi-module support](#multi-module-project-support) · [Adding a language](#adding-a-new-language) · [Architecture](#architecture)

---

## Installation

```bash
go install github.com/serfom256/refactorio/cmd/refactorio@latest
```

Or build from source:

```bash
git clone https://github.com/serfom256/refactorio.git
cd refactorio
go build -o refactorio ./cmd/refactorio
```

`go install` puts the binary in `$(go env GOPATH)/bin`, which is **not** on
`PATH` by default. If `refactorio` isn't found afterwards, add it:

```bash
export PATH="$(go env GOPATH)/bin:$PATH"   # add to ~/.zshenv or ~/.bashrc
```

Put it in `~/.zshenv` rather than `~/.zshrc` if you want non-interactive shells
— scripts, editors, agent tooling — to find it too; `~/.zshrc` is only read by
interactive shells.

No C compiler needed. No cgo. Cross-compiles to any OS/arch Go supports.

### Usage statistics are recorded locally

Every invocation is appended to `~/.refactorio/stats.jsonl` so that
`refactorio stats` can report what you use and what fails. **Nothing is ever
transmitted anywhere**, and symbol names, the file paths inside your project
and raw error text are deliberately not recorded. To turn it off entirely:

```bash
export REFACTORIO_NO_STATS=1
```

See [stats](#stats) for exactly what a record contains.

---

## Supported Languages

| Language | Extensions | Project Detection | Import System |
|---|---|---|---|
| Java | `.java` | Gradle (`settings.gradle`, `build.gradle`), Maven (`pom.xml`) | Package-based FQN (`import com.foo.Bar`) |
| Kotlin | `.kt`, `.kts` | Gradle, Maven (same as Java) | Package-based FQN (`import com.foo.Bar`) |
| Go | `.go` | `go.mod` | Module-path-based (`import "example.com/proj/pkg"`) |
| TypeScript | `.ts`, `.tsx`, `.mts`, `.cts` | Directory walk (excludes `node_modules`, `dist`, `build`) | File-path-based (`import { Foo } from './foo'`) |
| JavaScript | `.js`, `.jsx`, `.mjs`, `.cjs` | Directory walk (same as TypeScript) | File-path-based (ES modules only, no CommonJS `require()`) |
| Rust | `.rs` | `Cargo.toml`, workspace `[workspace.members]` | Module-path-based (`use crate::foo::Bar`) |
| C# | `.cs` | `.csproj`, `.sln` (or any directory) | Namespace-based (`using Foo.Bar`) |
| Python 3 | `.py` | `pyproject.toml`, `setup.py`, `setup.cfg` (or any directory) | Module-path-based (`from foo.bar import Baz`) |

---

## Commands

Language is auto-detected from the file extension. Each command supports `--help` for detailed usage.

| Command | Description | Usage |
|---|---|---|
| `rename` | Rename a symbol across the project | `rename in=<file> name=<symbol> to=<newName>` |
| `move` | Move a file, update all imports | `move in=<file> to=<newPath>` |
| `delete` | Delete a file, clean up imports | `delete in=<file> [force=true]` |
| `rename-package` | Rename a package — owned (moves files) or external (rewrites references) | `rename-package from=<old> to=<new>` |
| `usages` | Find where a symbol is declared and referenced (read-only) | `usages in=<file> name=<symbol> [format=text\|json]` |
| `rollback` | Undo the last refactoring operation (per-project) | `rollback` |
| `stats` | Show how Refactorio itself has been used (local, private) | `stats [scope=project\|all] [since=7d]` |
| `history` | List recent refactoring operations (per-project) | `history` |
| `doctor` | Report the detected project: modules, source roots, file counts | `doctor [root=<dir>]` |
| `clean-comments` | Remove comments that restate the code, and commented-out code | `clean-comments in=<path> [mode=report\|apply]` |

All commands support `dry-run=true` to preview changes and `root=<dir>` to override where project
detection starts. `root=` is a starting point, not a scope limit: detection climbs to the outermost
project root at or above it (stopping at the enclosing VCS root), so a refactor started inside one
module still sees the sibling modules that reference its code.

---

## rename

Rename a symbol across the project. All references in importing files are updated.

```
refactorio rename in=<file> name=<symbol> to=<newName> [root=<dir>] [dry-run=true]
```

**Java:**
```bash
refactorio rename in=src/main/java/com/foo/Bar.java name=Bar to=Baz
```

**Kotlin:**
```bash
refactorio rename in=src/main/kotlin/com/foo/Bar.kt name=Bar to=Baz
```

**Go:**
```bash
refactorio rename in=pkg/foo.go name=Hello to=Greet
```

**TypeScript / JavaScript:**
```bash
refactorio rename in=src/foo.ts name=Foo to=Bar
refactorio rename in=src/utils.js name=greet to=sayHello
refactorio rename in=src/Button.jsx name=Button to=Btn
```

**Rust:**
```bash
refactorio rename in=src/foo.rs name=Bar to=Baz
```

**C#:**
```bash
refactorio rename in=src/Foo.cs name=Foo to=Bar
```

**Python 3:**
```bash
refactorio rename in=src/foo.py name=Foo to=Bar
```

**What gets renamed:**
- The declaration in the source file
- All usages in the same package/module
- Import statements and bindings in importing files (`use`, `import`)
- Qualified references (`pkg.Symbol`, `Bar.method()`, `crate::foo::Bar`)
- Inline fully-qualified references (`com.foo.Bar x = ...` in Java/Kotlin)
- Template literal / string template substitutions (`${name}`, `$name`)
- `impl` blocks when renaming a Rust struct/enum/trait
- Constructors when renaming a Java/Kotlin class
- The file itself when it matches the old name (e.g. `Bar.java` → `Baz.java`)

**What stays untouched:**
- String literals (`"Bar"`)
- Comments (`// Bar`)
- Local variable declarations that share the symbol name (`let Bar = 1` won't be renamed)
- Unrelated symbols with the same name in different scopes

---

## move

Move a file and update all import paths that reference it.

```
refactorio move in=<file> to=<newPath> [root=<dir>] [dry-run=true]
```

```bash
# Java — move class to a different package (directory or file path)
refactorio move in=src/main/java/com/foo/Bar.java to=src/main/java/com/qux/Bar.java
refactorio move in=src/main/java/com/foo/Bar.java to=src/main/java/com/qux/

# Kotlin
refactorio move in=src/main/kotlin/com/foo/Bar.kt to=src/main/kotlin/com/qux/Bar.kt

# Go — move file to a different package
refactorio move in=pkg/foo.go to=lib/foo.go

# TypeScript / JavaScript
refactorio move in=src/utils/foo.ts to=src/lib/foo.ts

# Rust
refactorio move in=src/foo.rs to=src/bar.rs

# C#
refactorio move in=src/Foo.cs to=src/lib/Foo.cs

# Python 3
refactorio move in=utils/foo.py to=lib/foo.py
```

**What gets updated:**
- **Java/Kotlin:** package declaration rewritten, imports in dependents updated, same-package files that use the class get explicit imports added, inline fully-qualified references (`com.foo.Bar`) rewritten
- **Go:** package declaration updated to match new directory, import paths rewritten in all importing files
- **TS/JS:** import paths rewritten in all importing files, relative imports inside the moved file recalculated
- **Rust:** `use` paths rewritten across the crate
- **C#:** `namespace` declaration updated to match new directory, `using` directives in dependents rewritten
- **Python:** module path updates in dependents (`from foo.bar import X` → `from lib.foo import X`)

Note: `to=` can be a directory — the source filename is appended automatically.

**Java-specific safety check:** when moving a class out of its package, Refactorio detects package-private cross-references that will break after the move and prints a warning (the move still proceeds). Example: if the moved class uses a package-private sibling `Helper`, you'll see `Helper is package-private in com.foo and will not be importable after the move`.

---

## delete

Remove a file and clean up import statements in all dependent files.

```
refactorio delete in=<file> [root=<dir>] [dry-run=true]
```

```bash
refactorio delete in=src/main/java/com/foo/Bar.java
refactorio delete in=src/main/kotlin/com/foo/Bar.kt
refactorio delete in=pkg/foo.go
refactorio delete in=src/foo.ts
refactorio delete in=src/foo.rs
refactorio delete in=src/Foo.cs
refactorio delete in=src/foo.py
```

**What gets done:**
- The file is deleted from disk
- Import statements referencing the file/class are removed from all dependents (`import`, `use`)
- `mod` declarations removed (Rust)
- Files that still reference the deleted symbol are listed by name, and the
  delete is **refused** unless `force=true` is passed

Surviving references are computed with the same machinery as `usages`, so the
list matches what `usages` reports. Two consequences worth knowing:

- **Same-package users are included**, even though they have no import to
  clean. This is the case that matters most — in Java a sibling class in the
  same package needs no import, so an import-driven check never sees it and
  the delete breaks the build silently.
- **Import-only dependents are not listed.** If a file's only mention of the
  symbol was the import, removing that import *is* the cleanup.

```
$ refactorio delete in=src/main/java/com/ex/Doomed.java

warning: 2 files still reference the deleted symbol and will not compile:
  src/main/java/com/ex/SamePackageUser.java
  src/main/java/com/other/RealUser.java
error: refusing to delete: 2 file(s) still reference it — fix those, or re-run with force=true
```
- Wildcard imports are NOT removed (they cover the whole package)

---

## usages

Read-only command that reports where a symbol is declared and referenced across the project. No files are modified — this is the "find all references" / "where used" feature.

```
refactorio usages in=<file> name=<symbol> [format=text|json] [root=<dir>]
```

```bash
refactorio usages in=src/main/java/com/foo/Bar.java name=Bar
refactorio usages in=src/foo.ts name=Foo format=json
```

**Output ordering:**
1. Primary declaration
2. All usages, sorted by file:line
3. Additional declarations (method overloads, etc.) at the bottom

**Text output (default):**
```
src/Foo.cs:5:14    public class Foo {  (declaration)
src/Main.cs:8:18   var f = new Foo();
src/Other.cs:3:1   Foo helper;

3 usage(s) of "Foo" across 3 file(s).
```

Paths are relative to the working directory and the category tag trails the
source line, so plain usages — the overwhelming majority — pay nothing for it.

**JSON output** (`format=json`): structured findings with file/line/column/category/text for tool integration.

Implementation: drives the rename engine with a probe name and converts each candidate edit into a finding — guaranteeing usages and rename are consistent (what you see is what rename would touch). Works for all 7 supported languages.

---

## rename-package

Rename an entire package — all files, all imports across the project.

```
refactorio rename-package from=<oldPkg> to=<newPkg> [root=<dir>] [dry-run=true]
```

```bash
# Java / Kotlin (dotted package names)
refactorio rename-package from=com.foo to=com.bar

# Go (import paths)
refactorio rename-package from=example.com/proj/pkg to=example.com/proj/lib
```

**What gets updated:**
- Package declarations in every file in the package
- All files moved to the new directory
- Import statements in all dependent files rewritten
- Wildcard imports updated
- Sub-packages are NOT renamed (only the exact package)

### External packages (javax.* → jakarta.*)

If the package has no sources in the project, `rename-package` switches to
reference-only mode: imports and inline fully-qualified references are
rewritten, and **no file is moved, created or deleted**. The mode is chosen
automatically — sources present means move, sources absent means rewrite.

```bash
refactorio rename-package from=javax.persistence to=jakarta.persistence
```

```java
// before                                    after
import javax.persistence.Entity;          →  import jakarta.persistence.Entity;
import javax.persistence.criteria.Root;   →  import jakarta.persistence.criteria.Root;
import javax.persistence.*;               →  import jakarta.persistence.*;
import static javax.persistence.X.ALL;    →  import static jakarta.persistence.X.ALL;
javax.persistence.EntityManager em;       →  jakarta.persistence.EntityManager em;

import javax.sql.DataSource;              →  unchanged  (JDK, must not move)
javax.sql.rowset.CachedRowSet crs;        →  unchanged
// javax.persistence in a comment         →  unchanged
"javax.persistence.Entity"                →  unchanged  (string literal)
```

Matching is on **dot boundaries**, which is the reason to use this rather than
`sed` for the job: `javax.persistence` covers `javax.persistence.criteria` but
never `javax.sql` or `javax.persistencex`. Sub-packages are included by design
— a migration that skipped them would leave the project half-converted.

Java and Kotlin only. If the package is neither owned nor referenced anywhere,
the command errors rather than silently doing nothing.

**A clean run is not a finished migration.** Refactorio reads source, so it
cannot see `persistence.xml`, `web.xml`, Gradle/Maven coordinates, Spring XML,
or provider class names inside configuration strings. Those still need changing
by hand.

Note: TypeScript/JavaScript and Rust do not have a package-rename equivalent — use `move` to relocate individual files, or rename the module in Rust's `mod` declarations manually.

---

## Arguments Reference

| Argument | Description |
|---|---|
| `in=<path>` | The file to operate on. Language detected from extension. |
| `name=<symbol>` | The symbol to rename — any declaration in the file (class, function, type, variable). |
| `to=<target>` | New name (for rename) or new file path (for move) or new package (for rename-package). |
| `from=<package>` | Source package (for rename-package). |
| `root=<dir>` | Project root directory. Defaults to working directory. |
| `dry-run=true` | Preview changes without writing to disk. |

---

## clean-comments

Removes the comments from a file or directory tree, on the premise that code
should document itself. Reports by default and writes nothing until
`mode=apply`.

```bash
refactorio clean-comments in=src                # report
refactorio clean-comments in=src mode=apply     # remove them
```

```
1052 comments in 102 files: 1049 to remove, 3 kept

kept:
  internal/op/cleancomments/cli.go:206  (directive)   //nolint:nilerr // an unreadable...
  internal/op/doctor/doctor.go:153      (directive)   //nolint:nilerr // an unreadable...
  internal/project/discover.go:29       (directive)   //nolint:nilerr // see comment

to remove:
  internal/edit/edit.go          28 comments, 72 lines
  internal/cli/dispatch.go       14 comments, 21 lines
  ...
```

### What is kept

Three things, and none of them is a judgement about whether the prose is any
good:

| Kept | Why |
|---|---|
| **Directives** | `//go:embed`, `//nolint`, `# noqa`, `// +build`, `eslint-*`, `@ts-*`, `#!`, `// Code generated … DO NOT EDIT` — an instruction to the build, not documentation. Removing one changes what the build does. |
| **Licence headers** | copyright, licence and SPDX notices. Removing them is a legal question, not a style one. |
| **TODO/FIXME** | a backlog item rather than documentation. Deleting it deletes the item. |

Everything else goes: doc comments, Python docstrings, commented-out code,
section banners, trailing comments, why-comments. There are no categories and
no threshold to tune — `in=` is the only thing to decide.

A directive or a TODO always forms its own block, so it survives even when the
prose directly above it does not:

```go
// Kind is a kind.                      ← removed
//go:generate stringer -type=Kind       ← kept
type Kind int
```

Both matchers are anchored to the start of the comment, so a comment that
merely mentions one is prose: `// a TODO is an item on a list` goes, while
`// TODO: handle this` stays.

### What it will not break

- **Strings that look like comments.** `"https://example.com"`, a regex
  literal containing `//`, and every `#` inside a Python f-string are left
  alone, because the per-language lexers already separate a comment from a
  string. `sed -i 's|//.*||'` corrupts all three.
- **Python blocks.** A docstring that is the entire body of a `def` or `class`
  is kept: Python has no empty block, so removing it would stop the file
  parsing.
- **Paragraphs.** Consecutive line comments are removed as one unit, never
  half of one.
- **Layout.** Removal takes whole lines, and a comment that sat between two
  blank lines takes one of them, so no double blank is left behind.

Running a formatter afterwards is worth it in languages that align trailing
comments: stripping them from a `const` block leaves the padding `gofmt` had
added for them. The removal itself is correct; only the alignment is stale.

Applied runs go through the normal history, so [`rollback`](#rollback) undoes
a cleaning pass like any other refactor.

---

## stats

Reports how Refactorio itself has been used: which commands ran, which
arguments they were given, how often they succeeded, and how much code was
rewritten.

```
refactorio stats [scope=project|all] [since=<7d|2w|12h|YYYY-MM-DD>] [format=text|json] [reset=true]
```

```
$ refactorio stats

Refactorio usage — /Users/me/work/acme
63 invocations

command           runs    ok   err  dry-run     lines
rename              38    35     3       22        914
usages              14    14     0        0          0
move                 7     6     1        4        203

language           runs
java                41
kotlin              12

argument           uses
in                  59
name                52
dry-run             26

error                     count
package not found             3
symbol not found              2

code refactored (applied runs only)
  1117 lines changed across 318 files (1204 edits)
  86 lines moved
```

`lines` counts **distinct source lines rewritten**, not edits — a rename often
lands several edits on one line, and counting edits would overstate the work.
Only runs that were actually applied contribute: a dry run or a failed run
adds to the usage counts but changes no code, so it contributes no lines.
`move` and `delete` also report lines moved and lines deleted.

### Privacy

Everything is recorded locally in `~/.refactorio/stats.jsonl` and **nothing is
ever transmitted anywhere**. Symbol names, the file paths inside your project
and raw error text are deliberately not recorded — argument *names* and a
classified error *kind* answer "what gets used and what fails" without turning
the log into a record of the code being refactored. The project root is kept,
because statistics are scoped per project and the rollback history already
stores it.

A recorded entry looks like this in full:

```json
{"ts":"2026-09-13T20:26:08Z","version":"dev","command":"rename",
 "args":["in","name","to"],"lang":"java","project":"/Users/me/work/acme",
 "ok":true,"ms":33,"files":2,"edits":5,"lines_changed":3}
```

Turn recording off with `REFACTORIO_NO_STATS=1`, and clear what has been
recorded with `refactorio stats reset=true`. The log is capped at 8 MB; past
that the older half is dropped.

---

## doctor

Reports the project model every other command works from. Run it when a
command finds nothing, or touches fewer files than expected.

```bash
refactorio doctor
```

```
project root /home/me/work/intentflow
build system gradle
modules      12 (11 with source roots, 21 source roots in total)

module                                                 roots  files
<root>                                                     0      0
:intentflow-core                                           3    213
:intentflow-spring-boot-starter                            2     12
:intentflow-dialects:intentflow-mysql                      2      4
...

indexed files: java 308
```

Two things to read it for:

- **A module missing from the list** is a module no refactor will reach. The
  usual cause is a source layout off the convention path — see
  [Multi-module project support](#multi-module-project-support).
- **`warning: no source roots found`** means no code is visible at all.

For a project with no Gradle or Maven build, the command reports the file
census instead; the Go, TypeScript, Rust, C# and Python operations index from a
directory and need no build model.

Reaching for different `root=` values is not a fix. `root=` only says where
detection starts, and detection climbs to the outermost project root from
there, so pointing at a submodule does not narrow the search.

---

## Rollback

Every refactoring operation is automatically saved to `~/.refactorio/projects/<hash>/history/` (per-project — the hash is derived from the absolute project root). If you make a mistake, undo it:

```bash
# Undo the last operation for the current project
refactorio rollback

# See what operations are in this project's stack
refactorio history
```

**Per-project stacks** — each project gets its own history of the last 10 operations. The project root is auto-detected by walking up from the working directory (or `root=<dir>`) looking for `go.mod`, `Cargo.toml`, `pyproject.toml`, `setup.py`, `settings.gradle{.kts}`, `pom.xml`, `package.json`, or `tsconfig.json`. Two projects on the same machine maintain independent histories.

Each entry stores the original file contents; rollback restores files exactly as they were, reverses moves, and recreates deleted files. History is stored outside your project directory so it doesn't pollute your repo.

---

## Dry Run

Every command supports `dry-run=true`:

```
$ refactorio rename in=pkg/foo.go name=Hello to=Greet dry-run=true

2 files, 3 edits, 0 moves, 0 deletes
  pkg/foo.go (2 edits)
    3  "Hello" -> "Greet"  rename declaration Hello -> Greet
    5  "Hello" -> "Greet"  rename Hello -> Greet
  main.go (1 edit)
    5  "Hello" -> "Greet"  rename pkg.Hello -> pkg.Greet
```

The format is deliberately compact, since the output is usually consumed by a
tool or an agent rather than scrolled by a human. Paths are relative to the
working directory, each file is named once for its whole group rather than
once per edit, and edits sharing a replacement and a reason collapse onto a
single line listing the lines they occur on:

```
  internal/edit/edit_test.go (8 edits)
    37,47,56,69,87,107,123,134  "NewSet" -> "CreateSet"  rename NewSet -> CreateSet
```

Two edits on the same line are shown as `5(2)` rather than a repeated `5,5`.

On a 20-file / 45-edit rename this cuts the report from ~8.9 KB to ~2.4 KB
without losing a single edit location.

---

## Per-Language Limitations

### Java

| What works | What doesn't |
|---|---|
| Class rename (position-aware, skips local vars with same name) | No overload disambiguation — renames ALL methods with that name |
| Method rename with receiver-type tracking (HIGH/MEDIUM confidence), including method references (`obj::m`, `this::m`, `Type::m`) | No inheritance tracking — overrides in subclasses not found |
| Field rename with shadow detection (skips shadowed bare references) | Generic/array/qualified types not recognized by type scanner |
| Constructor rename when class is renamed | Reflection (`Class.forName(...)`) and annotation strings not detected |
| Annotation types (`@interface`) — rename, move, delete, usages, rename-package; `@Foo` usage sites renamed too | Nested types (inner classes, nested `@interface`) are not indexed as types |
| Inline FQN references (`com.foo.Bar x`) renamed/rewritten on move | Custom Gradle `srcDirs` outside `src/<sourceSet>/<java\|kotlin>` not detected |
| Import rewriting (explicit, static, wildcard) | `module-info.java` not updated — a renamed package leaves `exports`/`opens` stale |
| Move class across packages (same-package files get imports added) | |
| Package-private access warning on move (type-level) | Member-level access (package-private methods/fields) not tracked |
| Delete class + import cleanup | |
| Rename entire package, including its `package-info.java` (annotations and javadoc move with it) | |
| Multi-module Gradle and Maven projects, including modules registered programmatically in `settings.gradle.kts` | |
| Strings, comments, text blocks correctly excluded | |

### Kotlin

| What works | What doesn't |
|---|---|
| Class/interface/object/enum/function/property/typealias rename | No method-level receiver-type tracking |
| String template `$name` and `${expr}` references renamed | No inheritance tracking |
| Move class to different package | Backtick identifiers matched literally including backticks |
| Delete class + import cleanup | |
| Rename entire package | |
| Nested block comments handled correctly | |
| Multi-module Gradle projects | |

### Go

| What works | What doesn't |
|---|---|
| Exported function/type/var/const rename across packages | Unexported symbols only renamed within the same package |
| Qualified references (`pkg.Symbol`) renamed | Interface method tracking not implemented |
| Aliased imports (`import p "pkg"`) handled | Embedded struct field promotion not tracked |
| Dot imports (`import . "pkg"`) handled | `go:generate` directives not updated |
| Move file across packages (updates package decl + imports) | Vendored dependencies not followed |
| Delete file + import cleanup | |
| Rename package (move dir + rewrite all imports) | |
| Raw strings and rune literals correctly excluded | |

### TypeScript / JavaScript

| What works | What doesn't |
|---|---|
| Named imports (`import { Foo }`) renamed + all usages | CommonJS (`require()` / `module.exports`) not parsed |
| Default imports (`import Foo from`) renamed | `tsconfig.json` path aliases (`@/*`) not resolved |
| Namespace imports (`import * as X`) handled | Barrel re-exports (`export * from`) not deeply resolved |
| Aliased imports kept intact (`{ Foo as F }` → `{ Bar as F }`) | Dynamic imports (`import()`) not followed |
| Template literal `${name}` substitutions renamed | Regex content may produce false positives (rare) |
| Move file + update all import paths | |
| Delete file + import cleanup | |
| JSX/TSX component renames | |
| Mixed JS/TS projects (`.js` importing `.ts`) | |

### Rust

| What works | What doesn't |
|---|---|
| struct/enum/trait/fn/type/const/static rename across crate | No cross-crate renaming (workspace crates indexed independently) |
| `impl` blocks renamed with their type | Macro-generated identifiers invisible |
| `use` paths rewritten (simple, grouped `use std::{io, fs}`, aliased, glob) | Proc macro attribute arguments not scanned |
| Move file + `use` path rewriting | `mod.rs` ↔ `foo.rs` edition duality not auto-detected |
| Delete file + `use`/`mod` cleanup | Trait method implementations in other types not tracked |
| Nested block comments handled correctly | |
| Raw strings `r#"..."#` correctly excluded | |
| Lifetime `'a` vs char `'x'` correctly distinguished | |
| Local variable declarations (`let Foo = 1`) not wrongly renamed | |

### C#

| What works | What doesn't |
|---|---|
| class/interface/struct/enum/record/delegate rename across project | No overload disambiguation — renames ALL methods with that name |
| Method/property/field rename | No inheritance tracking — overrides in derived classes not found |
| Block-scoped + file-scoped namespaces (`namespace Foo;`) | Cross-project (`.csproj` boundaries) renaming not tracked |
| All `using` forms: regular, `using static`, `using X = Y` alias, `global using` | Reflection (`Type.GetType(...)`) not detected |
| Verbatim strings `@"..."` excluded (no false positives in paths) | Roslyn analyzers / source generators invisible |
| Interpolated strings `$"hello {name}"` — references inside `{...}` renamed | |
| Raw strings `"""..."""` (C# 11) handled | |
| Multi-line attributes `[Serializable]` correctly skipped | |
| Move file + namespace declaration update + `using` rewrites | |
| Local variable declarations (`int Foo = 1`) not wrongly renamed | |

### Python 3

| What works | What doesn't |
|---|---|
| Top-level class/function/async-function/variable rename | Local variable rename inside function bodies not supported (would need full scope analysis) |
| All import forms: `import x`, `import x as y`, `from x import a`, `from . import b`, parenthesised multi-line, `from x import *` | Star imports (`from x import *`) — usages of those bindings not tracked |
| Aliased imports preserved (`from x import Foo as F` — `F` and its usages untouched) | Decorators that modify visibility/binding (e.g. `@classmethod`) not type-checked |
| Module-qualified usages (`foo.Bar()`) renamed | Type stubs (`.pyi`) not indexed |
| f-string interpolation `f"{name}"` — references renamed | Reflection / `getattr(obj, "name")` not detected |
| Triple-quoted strings `"""..."""` correctly excluded | |
| Raw / byte string prefixes (`r"..."`, `b"..."`, `rb"..."`) handled | |
| Decorators (`@property`, `@dataclass`) correctly skipped | |
| Move file + `from x import` / `import x.y` path updates | |
| `__pycache__`, `.venv`, `venv`, `dist`, `build` excluded automatically | |

### Cross-Language Limitations (all languages)

- **Position-aware but not type-aware.** Local variable declarations sharing a symbol name are skipped (`let Foo = 1`, `String Foo = "x"`, `val Foo`, `Foo := 1`). But two types with the same name in different scopes may still collide without type resolution.
- **No semantic analysis.** The tool cannot tell whether `x.foo()` calls `Bar.foo` or `Baz.foo` without tracking `x`'s type. Java method/field renames use receiver-type tracking for common cases.
- **No cross-language refactoring.** Renaming a Java class does not update Kotlin files that import it (and vice versa). Each language is indexed independently.
- **Strings and comments are never modified.** This is by design. Template literal substitutions (`${name}`, `$name`) ARE modified because they ARE code references.
- **Inline FQN references are handled for Java/Kotlin.** `com.foo.Bar x = ...` is detected and rewritten during both rename and move operations.

---

## Multi-Module Project Support

Detection climbs to the **outermost** project root at or above the starting directory, stopping at the enclosing VCS root (`.git`, `.hg`, `.svn`). This matters: a Gradle submodule has its own `build.gradle.kts` and a Maven submodule its own `pom.xml`, so the *nearest* build file is usually the submodule — and a refactor scoped to one module cannot see the siblings that reference its code. It would rewrite the declaring module, report success, and leave the siblings uncompilable.

**Gradle** — reads `settings.gradle` or `settings.gradle.kts`. Both Groovy and Kotlin DSL are supported, including argument lists that span lines:

```kotlin
include(
    "core",
    "starter",
)
```

**Maven** — reads `pom.xml` for `<modules>` and recurses into child poms.

**Modules the build script hides** — build scripts are read, not executed, so a module registered programmatically is invisible to the parser:

```kotlin
listOf("mysql", "postgres").forEach {
    include(it)
    project(":$it").projectDir = file("dialects/$it")
}
```

After parsing, the filesystem is swept for any directory holding a conventional source root that the parser did not report, and the union is what refactors operate on. Build output (`build`, `target`, `out`, `bin`, `dist`, `node_modules`) and dot-directories are never searched.

**Source sets** — every `src/<sourceSet>/<java|kotlin>` directory is a source root, not just `main` and `test`. `src/testFixtures/java` (the `java-test-fixtures` plugin) and hand-rolled sets like `src/integrationTest/java` hold code that references the rest of the project, so a rename that skipped them would break the build. A source set whose name contains "test" is marked as test code.

Run [`doctor`](#doctor) to see exactly which modules and files were found.

**Go modules** — reads `go.mod` for the module path. All `.go` files under the module root are indexed (excluding `vendor`, `testdata`).

**npm/yarn** — walks the directory tree for `.ts`/`.js` files, excluding `node_modules`, `dist`, `build`, `.git`, `.next`, `coverage`.

**Cargo** — reads `Cargo.toml` for the crate name. All `.rs` files under `src/` are indexed (excluding `target/`). Module paths computed from filesystem layout (`src/foo.rs` → `crate::foo`, `src/foo/mod.rs` → `crate::foo`).

**.NET** — walks the directory tree for `.cs` files, excluding `bin/`, `obj/`, `node_modules`, `.git`. Namespaces detected from file content, not directory structure.

**Python** — walks the directory tree for `.py` files, excluding `.venv`, `venv`, `__pycache__`, `.git`, `.tox`, `dist`, `build`, `.eggs`, `.pytest_cache`, `.mypy_cache`, `.ruff_cache`, `node_modules`. Module paths derived from filesystem (`foo/bar.py` → `foo.bar`, `foo/__init__.py` → `foo`).

---

## Adding a New Language

1. **Create a lexer** (`internal/lang/<language>/lexer.go`) — tokenize source, emit identifiers as `TIdent`, mask strings/comments as their own token kinds. ~300 LOC.

2. **Create a symbol extractor** (`internal/lang/<language>/symbols.go`) — walk the token stream, extract imports and declarations with byte ranges.

3. **Create an index** (`internal/lang/<language>/index.go`) — map exported symbols to files, resolve import paths, implement `FilesImporting()`.

4. **Add to the unified dispatcher** (`internal/op/unified/unified.go`) — add a case in `detectLang()` and implement `rename<Lang>()`, `move<Lang>()`, `delete<Lang>()`.

5. **Write tests** — verify rename, move, delete across multiple files.

---

## Architecture

```
cmd/refactorio/main.go             CLI entry (10 commands: rename, move, delete, rename-package, usages, rollback, history, stats, doctor, clean-comments)
internal/cli/                      key=value arg parser + subcommand dispatch + --help system
internal/edit/                     Edit/Move/Delete sets, atomic apply, dry-run
internal/project/                  Java/Kotlin project detection (Gradle/Maven) + filesystem module discovery
internal/index/                    Java project-wide symbol index
internal/history/                  Per-project undo/rollback history (~/.refactorio/projects/<hash>/)
internal/stats/                    Local usage statistics (~/.refactorio/stats.jsonl)
internal/lang/
  common/                          Shared Range + TokenKind types
  java/                            Java lexer + symbol extractor
  kotlin/                          Kotlin lexer + symbol extractor
  golang/                          Go lexer + symbol extractor + index
  typescript/                      TS/JS lexer + symbol extractor + index
  rust/                            Rust lexer + symbol extractor + index
  csharp/                          C# lexer + symbol extractor + index
  python/                          Python 3 lexer + symbol extractor + index
internal/op/
  unified/                         Unified rename/move/delete dispatcher + FQN scanner + external package rename
  usages/                          Read-only find-usages (drives the rename layer)
  doctor/                          Reports the detected project model (modules, source roots, files)
  cleancomments/                   Comment classifier + removal (deny-list, categories, confidence)
  refs/                            Shared dangling-reference check used by delete
  renameclass/                     Java class rename logic (position-aware)
  renamemethod/                    Java method rename logic (receiver-typed)
  renamefield/                     Java field rename logic (shadow-aware)
  moveclass/                       Java move class logic
  deleteclass/                     Java delete class logic
  renamepkg/                       Java/Kotlin rename package logic
  ktrename/                        Kotlin rename/move/delete logic
  gorename/                        Go rename/move/delete/rename-package logic
  tsrename/                        TS/JS rename/move/delete logic
  rstrename/                       Rust rename/move/delete logic
  csrename/                        C# rename/move/delete logic
  pyrename/                        Python 3 rename/move/delete logic
```

## Running Tests

```bash
go test ./...
```

435 tests across 30 packages. No external tools or services required — no cgo, no network, no LSP.

## License

MIT — see [LICENSE](LICENSE).
