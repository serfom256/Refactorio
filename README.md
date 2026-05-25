# Refactorio

A cross-file refactoring CLI for Java, Kotlin, Go, TypeScript, and JavaScript projects. Pure Go, zero dependencies, no LSP or compiler required.

Refactorio uses hand-rolled lexers and convention-based rules to find and rename symbols across an entire project. It is intentionally not 100% precise (no type system, no semantic analysis) but handles the common cases correctly and flags uncertain edits with confidence markers.

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

No C compiler needed. No cgo. Cross-compiles to any OS/arch Go supports.

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

---

## Commands

Four commands. Language detected automatically from the file extension.

| Command | Description | Usage |
|---|---|---|
| `rename` | Rename a symbol across the project | `rename in=<file> name=<symbol> to=<newName>` |
| `move` | Move a file, update all imports | `move in=<file> to=<newPath>` |
| `delete` | Delete a file, clean up imports | `delete in=<file>` |
| `rename-package` | Rename an entire package | `rename-package from=<old> to=<new>` |
| `rollback` | Undo the last refactoring operation | `rollback` |
| `history` | List recent refactoring operations | `history` |

All commands support `dry-run=true` to preview changes and `root=<dir>` to override the project root.

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
```

**What gets updated:**
- **Java/Kotlin:** package declaration rewritten, imports in dependents updated, same-package files that use the class get explicit imports added, inline fully-qualified references (`com.foo.Bar`) rewritten
- **Go:** package declaration updated to match new directory, import paths rewritten in all importing files
- **TS/JS:** import paths rewritten in all importing files, relative imports inside the moved file recalculated
- **Rust:** `use` paths rewritten across the crate

Note: `to=` can be a directory — the source filename is appended automatically.

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
```

**What gets done:**
- The file is deleted from disk
- Import statements referencing the file/class are removed from all dependents (`import`, `use`)
- `mod` declarations removed (Rust)
- A warning lists files that still contain code references (manual cleanup needed)
- Wildcard imports are NOT removed (they cover the whole package)

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

## Rollback

Every refactoring operation is automatically saved to `~/.refactorio/history/`. If you make a mistake, undo it:

```bash
# Undo the last operation
refactorio rollback

# See what operations are in the history stack
refactorio history
```

The history stack holds the last 10 operations. Each entry stores the original file contents, so rollback restores files exactly as they were before the refactoring. Rollback reverses moves and restores deleted files.

History is stored outside your project directory (`~/.refactorio/history/`), so it doesn't pollute your repo and works across projects.

---

## Dry Run

Every command supports `dry-run=true`:

```
$ refactorio rename in=pkg/foo.go name=Hello to=Greet dry-run=true

2 files would be modified, 3 edits, 0 moves, 0 deletes
  pkg/foo.go (2 edits)
    pkg/foo.go:3  "Hello" -> "Greet"  rename declaration Hello -> Greet
    pkg/foo.go:5  "Hello" -> "Greet"  rename Hello -> Greet
  main.go (1 edits)
    main.go:5  "Hello" -> "Greet"  rename pkg.Hello -> pkg.Greet
```

---

## Per-Language Limitations

### Java

| What works | What doesn't |
|---|---|
| Class rename (position-aware, skips local vars with same name) | No overload disambiguation — renames ALL methods with that name |
| Method rename with receiver-type tracking (HIGH/MEDIUM confidence) | No inheritance tracking — overrides in subclasses not found |
| Field rename with shadow detection (skips shadowed bare references) | Generic/array/qualified types not recognized by type scanner |
| Constructor rename when class is renamed | Reflection (`Class.forName(...)`) and annotation strings not detected |
| Inline FQN references (`com.foo.Bar x`) renamed/rewritten on move | Non-standard Gradle source roots not detected |
| Import rewriting (explicit, static, wildcard) | Programmatic Gradle includes invisible |
| Move class across packages (same-package files get imports added) | |
| Delete class + import cleanup | |
| Rename entire package | |
| Multi-module Gradle and Maven projects | |
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

### Cross-Language Limitations (all languages)

- **Position-aware but not type-aware.** Local variable declarations sharing a symbol name are skipped (`let Foo = 1`, `String Foo = "x"`, `val Foo`, `Foo := 1`). But two types with the same name in different scopes may still collide without type resolution.
- **No semantic analysis.** The tool cannot tell whether `x.foo()` calls `Bar.foo` or `Baz.foo` without tracking `x`'s type. Java method/field renames use receiver-type tracking for common cases.
- **No cross-language refactoring.** Renaming a Java class does not update Kotlin files that import it (and vice versa). Each language is indexed independently.
- **Strings and comments are never modified.** This is by design. Template literal substitutions (`${name}`, `$name`) ARE modified because they ARE code references.
- **Inline FQN references are handled for Java/Kotlin.** `com.foo.Bar x = ...` is detected and rewritten during both rename and move operations.

---

## Multi-Module Project Support

**Gradle** — reads `settings.gradle` or `settings.gradle.kts` for `include ':app'`, `include(":lib:core")` patterns. Both Groovy and Kotlin DSL supported.

**Maven** — reads `pom.xml` for `<modules>` and recurses into child poms.

**Go modules** — reads `go.mod` for the module path. All `.go` files under the module root are indexed (excluding `vendor`, `testdata`).

**npm/yarn** — walks the directory tree for `.ts`/`.js` files, excluding `node_modules`, `dist`, `build`, `.git`, `.next`, `coverage`.

**Cargo** — reads `Cargo.toml` for the crate name. All `.rs` files under `src/` are indexed (excluding `target/`). Module paths computed from filesystem layout (`src/foo.rs` → `crate::foo`, `src/foo/mod.rs` → `crate::foo`).

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
cmd/refactorio/main.go             CLI entry (6 commands: rename, move, delete, rename-package, rollback, history)
internal/cli/                      key=value arg parser + subcommand dispatch
internal/edit/                     Edit/Move/Delete sets, atomic apply, dry-run
internal/project/                  Java/Kotlin project detection (Gradle/Maven)
internal/index/                    Java project-wide symbol index
internal/lang/
  common/                          Shared Range + TokenKind types
  java/                            Java lexer + symbol extractor
  kotlin/                          Kotlin lexer + symbol extractor
  golang/                          Go lexer + symbol extractor + index
  typescript/                      TS/JS lexer + symbol extractor + index
  rust/                            Rust lexer + symbol extractor + index
internal/history/                  Undo/rollback history (~/.refactorio/history/)
internal/op/
  unified/                         Unified rename/move/delete dispatcher + FQN scanner
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
```

## Running Tests

```bash
go test ./...
```

~170 tests across 22 packages. No external tools or services required.

## License

MIT
