package typescript

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/serfom256/refactorio/internal/lang/common"
)

// FileSymbols is the per-file structural data for TS/JS: imports,
// exports, and top-level declarations.
type FileSymbols struct {
	Imports      []ImportDecl
	Exports      []ExportDecl
	Declarations []Declaration
}

type Range = common.Range

// ImportDecl is one import statement.
type ImportDecl struct {
	// Names are the imported bindings: `import { Foo, Bar as B } from '...'`
	// yields [{Name:"Foo"}, {Name:"Bar", Alias:"B"}].
	// For `import * as X`, Names is [{Name:"*", Alias:"X"}].
	// For `import Foo`, Names is [{Name:"default", Alias:"Foo"}].
	Names []ImportName
	// Path is the module specifier: './foo', '@scope/pkg', etc.
	Path string
	// Range covers the full import statement.
	Range Range
	// PathRange covers just the path string (inside quotes).
	PathRange Range
	// IsTypeOnly is true for `import type { ... }`.
	IsTypeOnly bool
}

type ImportName struct {
	Name  string // original name or "default" or "*"
	Alias string // local binding name (empty if same as Name)
	Range Range  // range of the local binding in source
}

// LocalName returns the name used in code (alias if set, else name).
func (n ImportName) LocalName() string {
	if n.Alias != "" {
		return n.Alias
	}
	return n.Name
}

// ExportDecl tracks exports for reference resolution.
type ExportDecl struct {
	Names []ExportName
	// From is non-empty for re-exports: `export { Foo } from './bar'`.
	From      string
	FromRange Range
	Range     Range
}

type ExportName struct {
	Name  string // local name being exported
	Alias string // exported-as name (empty if same)
	Range Range
}

// Declaration is a top-level class, function, interface, type alias,
// enum, or const/let/var binding.
type Declaration struct {
	Name      string
	Kind      string // "class" | "function" | "interface" | "type" | "enum" | "const" | "let" | "var"
	NameRange Range
	IsExport  bool
	IsDefault bool
}

// ExtractSymbols parses a TS/JS file using the lexer and returns
// its structural view.
func ExtractSymbols(src []byte) (*FileSymbols, error) {
	tokens := Tokenize(src)
	p := &tsParser{tokens: tokens, src: src}
	return p.parse(), nil
}

type tsParser struct {
	tokens []Token
	pos    int
	src    []byte
}

func (p *tsParser) at() Token {
	if p.pos >= len(p.tokens) {
		return Token{Kind: TEOF}
	}
	return p.tokens[p.pos]
}

func (p *tsParser) text() string { return p.at().Text }
func (p *tsParser) skip()        { p.pos++ }

func (p *tsParser) skipComments() {
	for p.pos < len(p.tokens) && p.tokens[p.pos].Kind == TComment {
		p.pos++
	}
}

func (p *tsParser) parse() *FileSymbols {
	fs := &FileSymbols{}
	for p.pos < len(p.tokens) {
		p.skipComments()
		if p.pos >= len(p.tokens) {
			break
		}
		tok := p.at()

		if tok.Kind == TIdent && tok.Text == "import" {
			if imp, ok := p.parseImport(); ok {
				fs.Imports = append(fs.Imports, imp)
			}
			continue
		}

		isExport := false
		if tok.Kind == TIdent && tok.Text == "export" {
			isExport = true
			p.skip()
			p.skipComments()
			if p.pos >= len(p.tokens) {
				break
			}
			tok = p.at()

			// Re-export: `export { ... } from '...'` or `export * from '...'`
			if tok.Text == "{" || tok.Text == "*" {
				if exp, ok := p.parseExportBraces(); ok {
					fs.Exports = append(fs.Exports, exp)
				}
				continue
			}
			// `export default ...`
			if tok.Text == "default" {
				p.skip()
				p.skipComments()
				if p.pos >= len(p.tokens) {
					break
				}
				tok = p.at()
				if isDeclarationKeyword(tok.Text) {
					if d, ok := p.parseDeclaration(true, true); ok {
						fs.Declarations = append(fs.Declarations, d)
					}
					continue
				}
				// `export default <expr>` — anonymous default export
				p.skipToSemiOrNewline()
				continue
			}
			// `export type { ... }` — type-only re-export
			if tok.Text == "type" && p.peekText(1) == "{" {
				p.skip() // skip "type"
				if exp, ok := p.parseExportBraces(); ok {
					fs.Exports = append(fs.Exports, exp)
				}
				continue
			}
		}

		if isDeclarationKeyword(tok.Text) {
			if d, ok := p.parseDeclaration(isExport, false); ok {
				fs.Declarations = append(fs.Declarations, d)
			}
			continue
		}

		p.skip()
	}
	return fs
}

func isDeclarationKeyword(s string) bool {
	switch s {
	case "class", "function", "interface", "type", "enum",
		"const", "let", "var", "abstract", "async", "declare":
		return true
	}
	return false
}

// parseImport handles all import forms:
//
//	import { Foo, Bar as B } from './path'
//	import * as X from './path'
//	import Foo from './path'
//	import './path'  (side-effect)
//	import type { Foo } from './path'
func (p *tsParser) parseImport() (ImportDecl, bool) {
	imp := ImportDecl{Range: Range{Start: p.at().Start, Line: p.at().Line}}
	p.skip() // "import"
	p.skipComments()

	if p.text() == "type" && p.peekText(1) != "(" {
		imp.IsTypeOnly = true
		p.skip()
		p.skipComments()
	}

	// Side-effect import: `import './path'`
	if p.at().Kind == TString {
		imp.Path, imp.PathRange = p.extractStringPath()
		imp.Range.End = p.at().Start
		p.skipToSemiOrNewline()
		return imp, imp.Path != ""
	}

	// `import * as X from '...'`
	if p.text() == "*" {
		p.skip()
		p.skipComments()
		if p.text() == "as" {
			p.skip()
			p.skipComments()
		}
		if p.at().Kind == TIdent {
			imp.Names = append(imp.Names, ImportName{
				Name: "*", Alias: p.text(),
				Range: Range{Start: p.at().Start, End: p.at().End, Line: p.at().Line},
			})
			p.skip()
		}
		imp.Path, imp.PathRange = p.consumeFromClause()
		imp.Range.End = p.at().Start
		p.skipToSemiOrNewline()
		return imp, imp.Path != ""
	}

	// `import { Foo, Bar as B } from '...'`
	if p.text() == "{" {
		imp.Names = p.parseNamedBindings()
		imp.Path, imp.PathRange = p.consumeFromClause()
		imp.Range.End = p.at().Start
		p.skipToSemiOrNewline()
		return imp, imp.Path != ""
	}

	// `import Foo from '...'` — default import
	// Could also be `import Foo, { Bar } from '...'`
	if p.at().Kind == TIdent && !p.at().IsKeyword() {
		imp.Names = append(imp.Names, ImportName{
			Name: "default", Alias: p.text(),
			Range: Range{Start: p.at().Start, End: p.at().End, Line: p.at().Line},
		})
		p.skip()
		p.skipComments()
		if p.text() == "," {
			p.skip()
			p.skipComments()
			if p.text() == "{" {
				imp.Names = append(imp.Names, p.parseNamedBindings()...)
			}
		}
		imp.Path, imp.PathRange = p.consumeFromClause()
		imp.Range.End = p.at().Start
		p.skipToSemiOrNewline()
		return imp, imp.Path != ""
	}

	p.skipToSemiOrNewline()
	return ImportDecl{}, false
}

func (p *tsParser) parseNamedBindings() []ImportName {
	if p.text() != "{" {
		return nil
	}
	p.skip() // {
	var names []ImportName
	for p.pos < len(p.tokens) && p.text() != "}" {
		p.skipComments()
		if p.text() == "}" || p.pos >= len(p.tokens) {
			break
		}
		// Skip `type` prefix in `import { type Foo } from '...'`
		if p.text() == "type" && p.peekText(1) != "}" && p.peekText(1) != "," {
			p.skip()
			p.skipComments()
		}
		if p.at().Kind != TIdent {
			p.skip()
			continue
		}
		name := ImportName{
			Name:  p.text(),
			Range: Range{Start: p.at().Start, End: p.at().End, Line: p.at().Line},
		}
		p.skip()
		p.skipComments()
		if p.text() == "as" {
			p.skip()
			p.skipComments()
			if p.at().Kind == TIdent {
				name.Alias = p.text()
				name.Range = Range{Start: p.at().Start, End: p.at().End, Line: p.at().Line}
				p.skip()
			}
		}
		names = append(names, name)
		p.skipComments()
		if p.text() == "," {
			p.skip()
		}
	}
	if p.text() == "}" {
		p.skip()
	}
	return names
}

func (p *tsParser) consumeFromClause() (string, Range) {
	p.skipComments()
	if p.text() == "from" {
		p.skip()
		p.skipComments()
	}
	if p.at().Kind == TString {
		return p.extractStringPath()
	}
	return "", Range{}
}

func (p *tsParser) extractStringPath() (string, Range) {
	tok := p.at()
	p.skip()
	text := tok.Text
	if len(text) >= 2 {
		inner := text[1 : len(text)-1]
		return inner, Range{Start: tok.Start + 1, End: tok.End - 1, Line: tok.Line}
	}
	return text, Range{Start: tok.Start, End: tok.End, Line: tok.Line}
}

func (p *tsParser) parseExportBraces() (ExportDecl, bool) {
	exp := ExportDecl{Range: Range{Start: p.at().Start, Line: p.at().Line}}

	if p.text() == "*" {
		p.skip()
		p.skipComments()
		// `export * from '...'` or `export * as X from '...'`
		if p.text() == "as" {
			p.skip()
			p.skipComments()
			if p.at().Kind == TIdent {
				exp.Names = append(exp.Names, ExportName{Name: "*", Alias: p.text()})
				p.skip()
			}
		}
		exp.From, exp.FromRange = p.consumeFromClause()
		exp.Range.End = p.at().Start
		p.skipToSemiOrNewline()
		return exp, true
	}

	if p.text() == "{" {
		p.skip()
		for p.pos < len(p.tokens) && p.text() != "}" {
			p.skipComments()
			if p.text() == "}" {
				break
			}
			if p.at().Kind != TIdent {
				p.skip()
				continue
			}
			en := ExportName{
				Name:  p.text(),
				Range: Range{Start: p.at().Start, End: p.at().End, Line: p.at().Line},
			}
			p.skip()
			p.skipComments()
			if p.text() == "as" {
				p.skip()
				p.skipComments()
				if p.at().Kind == TIdent {
					en.Alias = p.text()
					p.skip()
				}
			}
			exp.Names = append(exp.Names, en)
			p.skipComments()
			if p.text() == "," {
				p.skip()
			}
		}
		if p.text() == "}" {
			p.skip()
		}
		p.skipComments()
		if p.text() == "from" {
			exp.From, exp.FromRange = p.consumeFromClause()
		}
		exp.Range.End = p.at().Start
		p.skipToSemiOrNewline()
		return exp, true
	}
	return ExportDecl{}, false
}

// parseDeclaration handles class, function, interface, type, enum,
// const/let/var declarations.
func (p *tsParser) parseDeclaration(isExport, isDefault bool) (Declaration, bool) {
	d := Declaration{IsExport: isExport, IsDefault: isDefault}

	// Skip prefixes: abstract, async, declare
	for p.at().Kind == TIdent && isModifier(p.text()) {
		p.skip()
		p.skipComments()
	}

	tok := p.at()
	switch tok.Text {
	case "class", "interface", "enum":
		d.Kind = tok.Text
		p.skip()
		p.skipComments()
		if p.at().Kind == TIdent {
			d.Name = p.text()
			d.NameRange = Range{Start: p.at().Start, End: p.at().End, Line: p.at().Line}
			p.skip()
		}
		// Skip to end of declaration body.
		p.skipToMatchingBrace()
		return d, d.Name != ""

	case "function":
		d.Kind = "function"
		p.skip()
		p.skipComments()
		// Generator: function*
		if p.text() == "*" {
			p.skip()
			p.skipComments()
		}
		if p.at().Kind == TIdent {
			d.Name = p.text()
			d.NameRange = Range{Start: p.at().Start, End: p.at().End, Line: p.at().Line}
			p.skip()
		}
		p.skipToMatchingBrace()
		return d, d.Name != ""

	case "type":
		d.Kind = "type"
		p.skip()
		p.skipComments()
		if p.at().Kind == TIdent {
			d.Name = p.text()
			d.NameRange = Range{Start: p.at().Start, End: p.at().End, Line: p.at().Line}
			p.skip()
		}
		p.skipToSemiOrNewline()
		return d, d.Name != ""

	case "const", "let", "var":
		d.Kind = tok.Text
		p.skip()
		p.skipComments()
		if p.at().Kind == TIdent {
			d.Name = p.text()
			d.NameRange = Range{Start: p.at().Start, End: p.at().End, Line: p.at().Line}
			p.skip()
		}
		p.skipToSemiOrNewline()
		return d, d.Name != ""
	}

	p.skip()
	return Declaration{}, false
}

func isModifier(s string) bool {
	switch s {
	case "abstract", "async", "declare", "readonly", "static":
		return true
	}
	return false
}

func (p *tsParser) skipToMatchingBrace() {
	// Skip until {, then balance braces.
	for p.pos < len(p.tokens) && p.text() != "{" {
		p.skip()
	}
	if p.pos >= len(p.tokens) {
		return
	}
	depth := 0
	for p.pos < len(p.tokens) {
		if p.text() == "{" {
			depth++
		}
		if p.text() == "}" {
			depth--
			if depth <= 0 {
				p.skip()
				return
			}
		}
		p.skip()
	}
}

func (p *tsParser) skipToSemiOrNewline() {
	for p.pos < len(p.tokens) {
		if p.text() == ";" {
			p.skip()
			return
		}
		// For TS, newline after expression is a valid statement terminator
		// (ASI). Approximate by stopping at next top-level keyword.
		if p.at().Kind == TIdent && isTopLevelBoundary(p.text()) {
			return
		}
		if p.text() == "{" {
			p.skipToMatchingBrace()
			continue
		}
		p.skip()
	}
}

func isTopLevelBoundary(s string) bool {
	switch s {
	case "import", "export", "class", "function", "interface", "type",
		"enum", "const", "let", "var", "declare", "abstract":
		return true
	}
	return false
}

func (p *tsParser) peekText(offset int) string {
	idx := p.pos + offset
	if idx < 0 || idx >= len(p.tokens) {
		return ""
	}
	return p.tokens[idx].Text
}

// IsValidIdentifier checks whether s is a valid JS/TS identifier.
func IsValidIdentifier(s string) bool {
	if s == "" {
		return false
	}
	for i, r := range s {
		if i == 0 {
			if !isIdentStart(r) {
				return false
			}
			continue
		}
		if !isIdentContinue(r) {
			return false
		}
	}
	_, reserved := tsKeywords[s]
	if reserved {
		switch s {
		case "undefined", "from", "of", "as", "get", "set",
			"type", "declare", "module", "namespace", "abstract",
			"async", "readonly", "is", "keyof", "require":
			return true // contextual keywords, valid as identifiers
		default:
			return false
		}
	}
	return true
}

// ResolveImportPath resolves a relative import path to an absolute
// file path. Handles extensionless imports by trying common suffixes.
func ResolveImportPath(importPath, fromFile string) string {
	if !strings.HasPrefix(importPath, ".") {
		return "" // node_modules / bare specifier — not resolved
	}
	dir := filepath.Dir(fromFile)
	resolved := filepath.Join(dir, importPath)

	// Try exact path first, then with common extensions.
	for _, ext := range []string{"", ".ts", ".tsx", ".js", ".jsx", "/index.ts", "/index.tsx", "/index.js"} {
		candidate := resolved + ext
		if fileExists(candidate) {
			return candidate
		}
	}
	return ""
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
