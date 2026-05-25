package rust

import (
	"strings"

	"github.com/serfom256/refactorio/internal/lang/common"
)

type Range = common.Range

type FileSymbols struct {
	// ModDecls are `mod foo;` declarations that reference child modules.
	ModDecls []ModDecl
	// Uses are `use` statements.
	Uses []UseDecl
	// Declarations are top-level fn/struct/enum/trait/type/const/static/impl.
	Declarations []Declaration
}

type ModDecl struct {
	Name  string
	Range Range
}

type UseDecl struct {
	// Path is the full use path, e.g. "crate::foo::Bar", "std::io::Read".
	// For grouped uses `use std::{io, fs}`, each group member gets its
	// own UseDecl with the expanded path.
	Path  string
	Alias string // for `use foo::Bar as B`
	Range Range  // full `use ...;` statement
	// PathRange covers the path portion inside quotes (for rewriting).
	PathRange Range
}

type Declaration struct {
	Name      string
	Kind      string // "fn" | "struct" | "enum" | "trait" | "type" | "const" | "static" | "impl" | "mod"
	NameRange Range
	IsPublic  bool
}

func ExtractSymbols(src []byte) (*FileSymbols, error) {
	tokens := Tokenize(src)
	p := &rsParser{tokens: tokens, src: src}
	return p.parse(), nil
}

type rsParser struct {
	tokens []Token
	pos    int
	src    []byte
}

func (p *rsParser) at() Token {
	if p.pos >= len(p.tokens) {
		return Token{Kind: TEOF}
	}
	return p.tokens[p.pos]
}

func (p *rsParser) text() string { return p.at().Text }
func (p *rsParser) skip()        { p.pos++ }

func (p *rsParser) skipComments() {
	for p.pos < len(p.tokens) && p.tokens[p.pos].Kind == TComment {
		p.pos++
	}
}

func (p *rsParser) parse() *FileSymbols {
	fs := &FileSymbols{}
	for p.pos < len(p.tokens) {
		p.skipComments()
		if p.pos >= len(p.tokens) {
			break
		}

		isPublic := false
		// Skip attributes #[...] and #![...]
		if p.text() == "#" {
			p.skip()
			if p.text() == "!" {
				p.skip()
			}
			if p.text() == "[" {
				p.skipBrackets()
			}
			continue
		}

		if p.text() == "pub" {
			isPublic = true
			p.skip()
			p.skipComments()
			// pub(crate), pub(super), etc.
			if p.text() == "(" {
				p.skipParens()
				p.skipComments()
			}
		}

		tok := p.at()
		switch tok.Text {
		case "mod":
			if md, ok := p.parseMod(isPublic); ok {
				if md.Kind == "mod" {
					fs.ModDecls = append(fs.ModDecls, ModDecl{Name: md.Name, Range: md.NameRange})
				}
				fs.Declarations = append(fs.Declarations, md)
			}
		case "use":
			fs.Uses = append(fs.Uses, p.parseUse()...)
		case "fn":
			if d, ok := p.parseDecl("fn", isPublic); ok {
				fs.Declarations = append(fs.Declarations, d)
			}
		case "struct":
			if d, ok := p.parseDecl("struct", isPublic); ok {
				fs.Declarations = append(fs.Declarations, d)
			}
		case "enum":
			if d, ok := p.parseDecl("enum", isPublic); ok {
				fs.Declarations = append(fs.Declarations, d)
			}
		case "trait":
			if d, ok := p.parseDecl("trait", isPublic); ok {
				fs.Declarations = append(fs.Declarations, d)
			}
		case "type":
			if d, ok := p.parseDecl("type", isPublic); ok {
				fs.Declarations = append(fs.Declarations, d)
			}
		case "const":
			if d, ok := p.parseDecl("const", isPublic); ok {
				fs.Declarations = append(fs.Declarations, d)
			}
		case "static":
			if d, ok := p.parseDecl("static", isPublic); ok {
				fs.Declarations = append(fs.Declarations, d)
			}
		case "impl":
			if d, ok := p.parseImpl(); ok {
				fs.Declarations = append(fs.Declarations, d)
			}
		case "unsafe", "async", "extern":
			p.skip() // skip modifier, next iteration picks up the keyword
		default:
			p.skip()
		}
	}
	return fs
}

func (p *rsParser) parseMod(isPublic bool) (Declaration, bool) {
	p.skip() // "mod"
	p.skipComments()
	if p.at().Kind != TIdent {
		return Declaration{}, false
	}
	name := p.at()
	d := Declaration{
		Name: name.Text, Kind: "mod",
		NameRange: Range{Start: name.Start, End: name.End, Line: name.Line},
		IsPublic:  isPublic,
	}
	p.skip()
	p.skipComments()
	if p.text() == "{" {
		// Inline module: `mod foo { ... }` — skip the body.
		p.skipBraces()
	} else if p.text() == ";" {
		p.skip() // `mod foo;` — file-based module.
	}
	return d, true
}

func (p *rsParser) parseUse() []UseDecl {
	stmtStart := p.at().Start
	stmtLine := p.at().Line
	p.skip() // "use"
	p.skipComments()

	var uses []UseDecl
	p.collectUsePaths("", stmtStart, stmtLine, &uses)

	// Skip to ;
	for p.pos < len(p.tokens) && p.text() != ";" {
		p.skip()
	}
	stmtEnd := p.at().End
	if p.text() == ";" {
		p.skip()
	}

	// Set Range on all collected uses.
	for i := range uses {
		uses[i].Range = Range{Start: stmtStart, End: stmtEnd, Line: stmtLine}
	}
	return uses
}

// collectUsePaths handles simple and grouped use paths recursively.
//   use foo::Bar;           → "foo::Bar"
//   use foo::{Bar, Baz};    → "foo::Bar", "foo::Baz"
//   use foo::Bar as B;      → "foo::Bar" alias "B"
//   use foo::*;             → "foo::*"
func (p *rsParser) collectUsePaths(prefix string, stmtStart, stmtLine int, out *[]UseDecl) {
	p.skipComments()
	if p.text() == "{" {
		p.skip() // {
		for p.pos < len(p.tokens) && p.text() != "}" {
			p.skipComments()
			if p.text() == "}" {
				break
			}
			p.collectUsePaths(prefix, stmtStart, stmtLine, out)
			p.skipComments()
			if p.text() == "," {
				p.skip()
			}
		}
		if p.text() == "}" {
			p.skip()
		}
		return
	}

	// Collect path segments: ident :: ident :: ...
	pathStart := p.at().Start
	var segments []string
	for p.pos < len(p.tokens) {
		if p.at().Kind == TIdent || p.text() == "self" || p.text() == "super" || p.text() == "crate" || p.text() == "*" {
			segments = append(segments, p.text())
			p.skip()
		} else {
			break
		}
		p.skipComments()
		if p.text() == "::" {
			p.skip()
			p.skipComments()
			// Check for grouped path: `foo::{Bar, Baz}`
			if p.text() == "{" {
				fullPrefix := prefix + strings.Join(segments, "::") + "::"
				p.collectUsePaths(fullPrefix, stmtStart, stmtLine, out)
				return
			}
			continue
		}
		break
	}

	if len(segments) == 0 {
		return
	}

	path := prefix + strings.Join(segments, "::")
	pathEnd := p.tokens[p.pos-1].End

	u := UseDecl{
		Path:      path,
		PathRange: Range{Start: pathStart, End: pathEnd, Line: lineFor(pathStart, p.src)},
	}

	// Check for `as Alias`.
	p.skipComments()
	if p.text() == "as" {
		p.skip()
		p.skipComments()
		if p.at().Kind == TIdent {
			u.Alias = p.text()
			p.skip()
		}
	}

	*out = append(*out, u)
}

func (p *rsParser) parseDecl(kind string, isPublic bool) (Declaration, bool) {
	p.skip() // keyword
	p.skipComments()
	// Skip generic params for fn<T>, struct<T>, etc.
	if p.text() == "<" {
		p.skipAngleBrackets()
		p.skipComments()
	}
	if p.at().Kind != TIdent || p.at().IsKeyword() {
		p.skipToEndOfDecl()
		return Declaration{}, false
	}
	name := p.at()
	d := Declaration{
		Name: name.Text, Kind: kind,
		NameRange: Range{Start: name.Start, End: name.End, Line: name.Line},
		IsPublic:  isPublic,
	}
	p.skip()
	p.skipToEndOfDecl()
	return d, true
}

func (p *rsParser) parseImpl() (Declaration, bool) {
	p.skip() // "impl"
	p.skipComments()
	// Skip generic params.
	if p.text() == "<" {
		p.skipAngleBrackets()
		p.skipComments()
	}
	if p.at().Kind != TIdent {
		p.skipToEndOfDecl()
		return Declaration{}, false
	}
	name := p.at()
	d := Declaration{
		Name:      name.Text,
		Kind:      "impl",
		NameRange: Range{Start: name.Start, End: name.End, Line: name.Line},
	}
	p.skip()
	p.skipToEndOfDecl()
	return d, true
}

func (p *rsParser) skipToEndOfDecl() {
	for p.pos < len(p.tokens) {
		if p.text() == "{" {
			p.skipBraces()
			return
		}
		if p.text() == ";" {
			p.skip()
			return
		}
		p.skip()
	}
}

func (p *rsParser) skipBraces() {
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

func (p *rsParser) skipBrackets() {
	depth := 0
	for p.pos < len(p.tokens) {
		if p.text() == "[" {
			depth++
		}
		if p.text() == "]" {
			depth--
			if depth <= 0 {
				p.skip()
				return
			}
		}
		p.skip()
	}
}

func (p *rsParser) skipParens() {
	depth := 0
	for p.pos < len(p.tokens) {
		if p.text() == "(" {
			depth++
		}
		if p.text() == ")" {
			depth--
			if depth <= 0 {
				p.skip()
				return
			}
		}
		p.skip()
	}
}

func (p *rsParser) skipAngleBrackets() {
	if p.text() != "<" {
		return
	}
	depth := 1
	p.skip()
	for p.pos < len(p.tokens) && depth > 0 {
		switch p.text() {
		case "<":
			depth++
		case ">":
			depth--
		case ">>":
			depth -= 2
		case ";", "{", "}":
			return
		}
		p.skip()
	}
}

func lineFor(off int, src []byte) int {
	line := 1
	for i := 0; i < off && i < len(src); i++ {
		if src[i] == '\n' {
			line++
		}
	}
	return line
}
