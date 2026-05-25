package java

import (
	"strings"

	"github.com/serfom256/refactorio/internal/lang/common"
)

type FileSymbols struct {
	Package      string
	PackageRange Range
	Imports      []ImportSpec
	Types        []TypeDecl
}

// Range is an alias for the shared Range type.
type Range = common.Range

type ImportSpec struct {
	Path       string
	IsWildcard bool
	IsStatic   bool
	Range      Range
	PathRange  Range
}

type TypeDecl struct {
	Name      string
	Kind      string
	NameRange Range
	BodyRange Range
	Methods   []MemberDecl
	Fields    []MemberDecl
	Nested    []TypeDecl
}

type MemberDecl struct {
	Name      string
	Kind      string
	NameRange Range
}

func ExtractSymbols(src []byte) (*FileSymbols, error) {
	tokens := Tokenize(src)
	p := &symParser{tokens: tokens, src: src}
	return p.parse(), nil
}

type symParser struct {
	tokens []Token
	pos    int
	src    []byte
}

func (p *symParser) at() Token {
	if p.pos >= len(p.tokens) {
		return Token{Kind: TEOF}
	}
	return p.tokens[p.pos]
}

func (p *symParser) text() string { return p.at().Text }

func (p *symParser) skip() { p.pos++ }

func (p *symParser) skipComments() {
	for p.pos < len(p.tokens) && p.tokens[p.pos].Kind == TComment {
		p.pos++
	}
}

func (p *symParser) parse() *FileSymbols {
	fs := &FileSymbols{}
	for p.pos < len(p.tokens) {
		p.skipComments()
		if p.pos >= len(p.tokens) {
			break
		}
		tok := p.at()
		if tok.Kind == TIdent && tok.Text == "package" {
			fs.Package, fs.PackageRange = p.parsePackage()
			continue
		}
		if tok.Kind == TIdent && tok.Text == "import" {
			if imp, ok := p.parseImport(); ok {
				fs.Imports = append(fs.Imports, imp)
			}
			continue
		}
		if p.atTypeKeyword() {
			if td, ok := p.parseType(); ok {
				fs.Types = append(fs.Types, td)
			}
			continue
		}
		p.skip()
	}
	return fs
}

func (p *symParser) parsePackage() (string, Range) {
	p.skip() // "package"
	start := p.pos
	for p.pos < len(p.tokens) && p.text() != ";" && !p.atTypeKeyword() && p.text() != "import" {
		p.pos++
	}
	if start >= len(p.tokens) {
		return "", Range{}
	}
	nameStart := p.tokens[start].Start
	nameEnd := p.tokens[p.pos-1].End
	var name strings.Builder
	for i := start; i < p.pos; i++ {
		name.WriteString(p.tokens[i].Text)
	}
	if p.pos < len(p.tokens) && p.text() == ";" {
		p.skip()
	}
	return name.String(), Range{Start: nameStart, End: nameEnd, Line: p.tokens[start].Line}
}

func (p *symParser) parseImport() (ImportSpec, bool) {
	importStart := p.at().Start
	p.skip() // "import"

	imp := ImportSpec{}
	if p.pos < len(p.tokens) && p.text() == "static" {
		imp.IsStatic = true
		p.skip()
	}

	pathStart, pathEnd := -1, -1
	var path strings.Builder
	for p.pos < len(p.tokens) {
		tok := p.at()
		if tok.Text == ";" {
			break
		}
		if tok.Text == "*" {
			imp.IsWildcard = true
			p.skip()
			continue
		}
		// The dot before '*' belongs to the wildcard syntax, not the path.
		if tok.Text == "." && p.pos+1 < len(p.tokens) && p.tokens[p.pos+1].Text == "*" {
			p.skip() // skip the dot
			continue
		}
		if pathStart < 0 {
			pathStart = tok.Start
		}
		pathEnd = tok.End
		path.WriteString(tok.Text)
		p.skip()
	}

	semiEnd := pathEnd
	if p.pos < len(p.tokens) && p.text() == ";" {
		semiEnd = p.at().End
		p.skip()
	}

	imp.Path = strings.TrimSuffix(path.String(), ".")
	if pathStart >= 0 {
		imp.PathRange = Range{Start: pathStart, End: pathEnd, Line: lineFor(pathStart, p.src)}
	}
	imp.Range = Range{Start: importStart, End: semiEnd, Line: lineFor(importStart, p.src)}
	if imp.Path == "" {
		return ImportSpec{}, false
	}
	return imp, true
}

func (p *symParser) atTypeKeyword() bool {
	tok := p.at()
	if tok.Kind == TPunct && tok.Text == "@" && p.pos+1 < len(p.tokens) && p.tokens[p.pos+1].Text == "interface" {
		return true
	}
	if tok.Kind != TIdent {
		return false
	}
	switch tok.Text {
	case "class", "interface", "enum", "record":
		return true
	}
	return false
}

func (p *symParser) parseType() (TypeDecl, bool) {
	td := TypeDecl{}
	if p.text() == "@" {
		p.skip()
		td.Kind = "annotation"
	} else {
		td.Kind = p.text()
	}
	p.skip() // skip keyword (class/interface/enum/record/interface-after-@)

	// The name is the next non-keyword TIdent.
	p.skipComments()
	for p.pos < len(p.tokens) && (p.at().Kind != TIdent || p.at().IsKeyword()) {
		p.skip()
	}
	if p.pos >= len(p.tokens) {
		return TypeDecl{}, false
	}
	nameTok := p.at()
	td.Name = nameTok.Text
	td.NameRange = Range{Start: nameTok.Start, End: nameTok.End, Line: nameTok.Line}
	p.skip()

	// Skip to opening '{'.
	for p.pos < len(p.tokens) && p.text() != "{" {
		p.skip()
	}
	if p.pos >= len(p.tokens) {
		return td, true
	}

	bodyStart := p.at().Start
	p.skip() // skip '{'
	// Enum constants (RED, GREEN, BLUE;) precede regular members.
	// Skip them to avoid misidentifying them as fields.
	if td.Kind == "enum" {
		p.skipEnumConstants()
	}
	p.parseTypeBody(&td)
	bodyEnd := bodyStart
	if p.pos > 0 {
		bodyEnd = p.tokens[p.pos-1].End
	}
	td.BodyRange = Range{Start: bodyStart, End: bodyEnd, Line: lineFor(bodyStart, p.src)}
	return td, true
}

// parseTypeBody scans inside a type body and fills methods, fields,
// and nested types. Stops after consuming the matching '}'.
// Uses a clean brace-depth counter that is NEVER affected by
// member-scanning lookaheads.
func (p *symParser) parseTypeBody(td *TypeDecl) {
	depth := 1
	for p.pos < len(p.tokens) && depth > 0 {
		p.skipComments()
		if p.pos >= len(p.tokens) {
			break
		}
		tok := p.at()

		if tok.Text == "{" {
			depth++
			p.skip()
			continue
		}
		if tok.Text == "}" {
			depth--
			p.skip()
			continue
		}

		// Nested type declaration: only match at depth==1 (direct child).
		if depth == 1 && p.atTypeKeyword() {
			if nested, ok := p.parseType(); ok {
				td.Nested = append(td.Nested, nested)
			}
			continue
		}

		// At depth 1, try to recognize a member. Always continue after
		// the attempt — tryParseMembers advances pos by at least 1,
		// so the next iteration re-checks for {/}/type keywords at the
		// new position. Without this, restored positions at '{' get
		// skipped without depth tracking.
		if depth == 1 && tok.Kind == TIdent {
			members := p.tryParseMembers(td.Name)
			for _, m := range members {
				switch m.Kind {
				case "method", "constructor":
					td.Methods = append(td.Methods, m)
				case "field":
					td.Fields = append(td.Fields, m)
				}
			}
			continue
		}
		p.skip()
	}
}

// tryParseMembers scans forward from the current identifier token,
// looking for a method/constructor/field declaration. Returns all
// fields from a multi-declarator statement (e.g. `int a, b, c;`).
// Advances p.pos past the matched declaration. Returns nil if no
// recognisable pattern is found (p.pos advanced by 1).
func (p *symParser) tryParseMembers(className string) []MemberDecl {
	save := p.pos

	// Skip annotations and modifiers.
	for p.pos < len(p.tokens) {
		tok := p.at()
		if tok.Text == "@" {
			p.skip()
			p.skipAnnotation()
			continue
		}
		if tok.Kind == TIdent && isModifier(tok.Text) {
			p.skip()
			continue
		}
		break
	}

	// Collect non-keyword identifiers while skipping type syntax
	// (generics, arrays, dots for qualified names, keywords like void/int).
	var idents []Token
	for p.pos < len(p.tokens) {
		tok := p.at()

		if tok.Text == "{" || tok.Text == "}" {
			break
		}
		if tok.Kind == TIdent && !tok.IsKeyword() {
			idents = append(idents, tok)
			p.skip()
			continue
		}
		// Type keywords (void, int, boolean, ...) are part of the type.
		if tok.Kind == TIdent && isTypeToken(tok.Text) {
			p.skip()
			continue
		}
		if tok.Text == "<" {
			p.skipAngleBrackets()
			continue
		}
		if tok.Text == "[" || tok.Text == "]" {
			p.skip()
			continue
		}
		if tok.Text == "." {
			p.skip()
			continue
		}
		// Hit '(' → method/constructor. The last identifier is the name.
		if tok.Text == "(" && len(idents) > 0 {
			name := idents[len(idents)-1]
			kind := "method"
			if name.Text == className {
				kind = "constructor"
			}
			p.skipBalanced('(', ')')
			// Skip throws clause.
			for p.pos < len(p.tokens) && p.text() == "throws" {
				p.skip()
				for p.pos < len(p.tokens) && p.text() != "{" && p.text() != ";" {
					p.skip()
				}
			}
			if p.pos < len(p.tokens) && p.text() == "{" {
				p.skipBalanced('{', '}')
			} else if p.pos < len(p.tokens) && p.text() == ";" {
				p.skip()
			}
			return []MemberDecl{{
				Name: name.Text, Kind: kind,
				NameRange: Range{Start: name.Start, End: name.End, Line: name.Line},
			}}
		}
		// Hit ';', '=', or ',' → field declaration.
		if (tok.Text == ";" || tok.Text == "=" || tok.Text == ",") && len(idents) > 0 {
			return p.parseFieldDeclarators(idents)
		}
		// Anything else we don't understand — stop trying.
		break
	}

	p.pos = save + 1
	return nil
}

// parseFieldDeclarators handles `int a, b = 1, c;`. Called when we've
// collected identifiers and hit `;`, `=`, or `,`. The last identifier
// before each separator is a field name. We advance past the full `;`.
func (p *symParser) parseFieldDeclarators(idents []Token) []MemberDecl {
	// The last ident is the first field name. Preceding idents are
	// the type (e.g. [Map, String, Integer] for `Map<String,Integer> x`).
	if len(idents) == 0 {
		return nil
	}
	var fields []MemberDecl
	name := idents[len(idents)-1]
	fields = append(fields, MemberDecl{
		Name: name.Text, Kind: "field",
		NameRange: Range{Start: name.Start, End: name.End, Line: name.Line},
	})

	// Now scan through the rest: skip initializers, collect more names.
	for p.pos < len(p.tokens) {
		tok := p.at()
		if tok.Text == ";" {
			p.skip()
			return fields
		}
		if tok.Text == "=" {
			p.skip()
			p.skipFieldInitializer()
			continue
		}
		if tok.Text == "," {
			p.skip()
			// Next non-comment ident is another field name.
			p.skipComments()
			if p.pos < len(p.tokens) && p.at().Kind == TIdent && !p.at().IsKeyword() {
				n := p.at()
				fields = append(fields, MemberDecl{
					Name: n.Text, Kind: "field",
					NameRange: Range{Start: n.Start, End: n.End, Line: n.Line},
				})
				p.skip()
			}
			continue
		}
		p.skip()
	}
	return fields
}

// skipFieldInitializer advances past an initializer expression: `= <expr>`.
// Stops before ',' or ';' at brace/paren depth 0.
func (p *symParser) skipFieldInitializer() {
	depth := 0
	for p.pos < len(p.tokens) {
		tok := p.at()
		switch tok.Text {
		case "{", "(":
			depth++
		case "}", ")":
			depth--
		case ",", ";":
			if depth <= 0 {
				return
			}
		}
		p.skip()
	}
}

// skipEnumConstants advances past enum constant entries until `;`.
// Handles constants with arguments `RED(1)` and bodies `RED { ... }`.
func (p *symParser) skipEnumConstants() {
	depth := 0
	for p.pos < len(p.tokens) {
		tok := p.at()
		if tok.Text == "{" {
			depth++
			p.skip()
			continue
		}
		if tok.Text == "}" {
			if depth <= 0 {
				return // hit the outer type's closing brace
			}
			depth--
			p.skip()
			continue
		}
		if tok.Text == ";" && depth == 0 {
			p.skip()
			return
		}
		p.skip()
	}
}

func (p *symParser) skipAnnotation() {
	if p.pos < len(p.tokens) && p.at().Kind == TIdent {
		p.skip()
	}
	for p.pos+1 < len(p.tokens) && p.text() == "." && p.tokens[p.pos+1].Kind == TIdent {
		p.pos += 2
	}
	if p.pos < len(p.tokens) && p.text() == "(" {
		p.skipBalanced('(', ')')
	}
}

func (p *symParser) skipBalanced(open, close byte) {
	if p.pos >= len(p.tokens) || p.at().Text[0] != open {
		return
	}
	depth := 1
	p.skip()
	for p.pos < len(p.tokens) && depth > 0 {
		t := p.at().Text
		if len(t) == 1 {
			if t[0] == open {
				depth++
			} else if t[0] == close {
				depth--
			}
		}
		p.skip()
	}
}

func (p *symParser) skipAngleBrackets() {
	if p.pos >= len(p.tokens) || p.text() != "<" {
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
		case ">>>":
			depth -= 3
		case ";", "{", "}":
			return
		}
		p.skip()
	}
}

func isModifier(s string) bool {
	switch s {
	case "public", "private", "protected", "static", "final", "abstract",
		"native", "synchronized", "transient", "volatile", "strictfp",
		"default", "sealed", "permits":
		return true
	}
	return false
}

func isTypeToken(s string) bool {
	switch s {
	case "void", "int", "long", "short", "byte", "float", "double",
		"boolean", "char", "var":
		return true
	}
	return false
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
