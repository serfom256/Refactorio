package csharp

import (
	"strings"

	"github.com/serfom256/refactorio/internal/lang/common"
)

type Range = common.Range

// FileSymbols is the per-file structural data for C# source.
type FileSymbols struct {
	// Namespace is the namespace declared in the file (block or
	// file-scoped). Empty if the file has no namespace declaration.
	Namespace      string
	NamespaceRange Range
	// FileScoped is true when the file uses `namespace Foo;` form.
	FileScoped bool
	Usings     []UsingDecl
	Types      []TypeDecl
}

type UsingDecl struct {
	Path      string
	Alias     string
	IsStatic  bool
	IsGlobal  bool
	Range     Range
	PathRange Range
}

type TypeDecl struct {
	Name       string
	Kind       string // "class" | "interface" | "struct" | "enum" | "record" | "delegate"
	NameRange  Range
	BodyRange  Range
	Methods    []MemberDecl
	Fields     []MemberDecl
	Properties []MemberDecl
	Nested     []TypeDecl
	// IsPublic is true for public top-level types. Other top-level
	// types are internal (the C# default).
	IsPublic bool
}

type MemberDecl struct {
	Name      string
	Kind      string // "method" | "constructor" | "field" | "property"
	NameRange Range
}

func ExtractSymbols(src []byte) (*FileSymbols, error) {
	tokens := Tokenize(src)
	p := &csParser{tokens: tokens, src: src}
	return p.parse(), nil
}

type csParser struct {
	tokens []Token
	pos    int
	src    []byte
}

func (p *csParser) at() Token {
	if p.pos >= len(p.tokens) {
		return Token{Kind: TEOF}
	}
	return p.tokens[p.pos]
}

func (p *csParser) text() string { return p.at().Text }
func (p *csParser) skip()        { p.pos++ }

func (p *csParser) skipComments() {
	for p.pos < len(p.tokens) && p.tokens[p.pos].Kind == TComment {
		p.pos++
	}
}

func (p *csParser) parse() *FileSymbols {
	fs := &FileSymbols{}
	pendingPublic := false
	for p.pos < len(p.tokens) {
		p.skipComments()
		if p.pos >= len(p.tokens) {
			break
		}
		tok := p.at()

		// `global using ...`
		if tok.Kind == TIdent && tok.Text == "global" && p.peekText(1) == "using" {
			p.skip()
			imp := p.parseUsing()
			imp.IsGlobal = true
			if imp.Path != "" {
				fs.Usings = append(fs.Usings, imp)
			}
			pendingPublic = false
			continue
		}

		if tok.Kind == TIdent && tok.Text == "using" {
			imp := p.parseUsing()
			if imp.Path != "" {
				fs.Usings = append(fs.Usings, imp)
			}
			pendingPublic = false
			continue
		}

		if tok.Kind == TIdent && tok.Text == "namespace" {
			ns, nsRange, fileScoped := p.parseNamespace()
			fs.Namespace = ns
			fs.NamespaceRange = nsRange
			fs.FileScoped = fileScoped
			pendingPublic = false
			// Block-scoped namespaces wrap the rest of the file in
			// `{ ... }` — we descend into it.
			if !fileScoped {
				p.parseInsideNamespace(fs)
				return fs
			}
			continue
		}

		// Visibility modifier preceding a top-level type.
		if tok.Kind == TIdent && tok.Text == "public" {
			pendingPublic = true
			p.skip()
			continue
		}
		if tok.Kind == TIdent && isCSModifier(tok.Text) {
			p.skip()
			continue
		}
		// Attribute: [Foo] or [Foo(args)]
		if tok.Kind == TPunct && tok.Text == "[" {
			p.skipBrackets()
			continue
		}

		if p.atTypeKeyword() {
			if td, ok := p.parseType(); ok {
				td.IsPublic = pendingPublic
				fs.Types = append(fs.Types, td)
			}
			pendingPublic = false
			continue
		}
		pendingPublic = false
		p.skip()
	}
	return fs
}

// parseInsideNamespace handles a block-scoped namespace body.
func (p *csParser) parseInsideNamespace(fs *FileSymbols) {
	// p is currently AT '{'
	if p.text() != "{" {
		return
	}
	p.skip()
	pendingPublic := false
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
		if depth != 1 {
			p.skip()
			continue
		}
		if tok.Kind == TIdent && tok.Text == "using" {
			imp := p.parseUsing()
			if imp.Path != "" {
				fs.Usings = append(fs.Usings, imp)
			}
			pendingPublic = false
			continue
		}
		if tok.Kind == TIdent && tok.Text == "public" {
			pendingPublic = true
			p.skip()
			continue
		}
		if tok.Kind == TIdent && isCSModifier(tok.Text) {
			p.skip()
			continue
		}
		if tok.Kind == TPunct && tok.Text == "[" {
			p.skipBrackets()
			continue
		}
		if p.atTypeKeyword() {
			if td, ok := p.parseType(); ok {
				td.IsPublic = pendingPublic
				fs.Types = append(fs.Types, td)
			}
			pendingPublic = false
			continue
		}
		pendingPublic = false
		p.skip()
	}
}

// parseUsing handles `using A.B.C;`, `using static A.B;`,
// `using X = A.B.C;`. p is currently AT `using`.
func (p *csParser) parseUsing() UsingDecl {
	u := UsingDecl{Range: Range{Start: p.at().Start, Line: p.at().Line}}
	p.skip() // "using"
	p.skipComments()

	if p.at().Kind == TIdent && p.text() == "static" {
		u.IsStatic = true
		p.skip()
		p.skipComments()
	}

	// Could be `using Name = Path;` (alias).
	// Peek: if we see an ident followed by '=', it's an alias.
	if p.at().Kind == TIdent && p.peekText(1) == "=" {
		u.Alias = p.text()
		p.skip()
		p.skip() // "="
		p.skipComments()
	}

	pathStart, pathEnd := -1, -1
	var path strings.Builder
	for p.pos < len(p.tokens) {
		tok := p.at()
		if tok.Text == ";" {
			break
		}
		if pathStart < 0 {
			pathStart = tok.Start
		}
		pathEnd = tok.End
		path.WriteString(tok.Text)
		p.skip()
	}
	stmtEnd := pathEnd
	if p.text() == ";" {
		stmtEnd = p.at().End
		p.skip()
	}
	u.Path = path.String()
	if pathStart >= 0 {
		u.PathRange = Range{Start: pathStart, End: pathEnd, Line: csLineFor(pathStart, p.src)}
	}
	u.Range.End = stmtEnd
	return u
}

// parseNamespace returns (name, range, fileScoped). p is currently AT
// `namespace`. After return, p is positioned past the `;` for file-
// scoped, or AT the `{` for block-scoped.
func (p *csParser) parseNamespace() (string, Range, bool) {
	p.skip() // "namespace"
	p.skipComments()
	start := p.pos
	for p.pos < len(p.tokens) && p.text() != ";" && p.text() != "{" {
		p.pos++
	}
	if start >= len(p.tokens) {
		return "", Range{}, false
	}
	nameStart := p.tokens[start].Start
	nameEnd := p.tokens[p.pos-1].End
	var name strings.Builder
	for i := start; i < p.pos; i++ {
		name.WriteString(p.tokens[i].Text)
	}
	r := Range{Start: nameStart, End: nameEnd, Line: p.tokens[start].Line}
	if p.text() == ";" {
		p.skip()
		return name.String(), r, true
	}
	// p.text() == "{" — block scope; leave caller to descend.
	return name.String(), r, false
}

func (p *csParser) atTypeKeyword() bool {
	tok := p.at()
	if tok.Kind != TIdent {
		return false
	}
	switch tok.Text {
	case "class", "interface", "struct", "enum", "record", "delegate":
		return true
	}
	return false
}

func (p *csParser) parseType() (TypeDecl, bool) {
	td := TypeDecl{Kind: p.text()}
	p.skip() // keyword
	p.skipComments()
	// `record class` / `record struct`
	if td.Kind == "record" && (p.text() == "class" || p.text() == "struct") {
		p.skip()
		p.skipComments()
	}
	// Skip generic params <T>
	for p.pos < len(p.tokens) && (p.at().Kind != TIdent || p.at().IsKeyword()) {
		if p.text() == "<" {
			p.skipAngleBrackets()
			continue
		}
		p.skip()
	}
	if p.pos >= len(p.tokens) {
		return TypeDecl{}, false
	}
	name := p.at()
	td.Name = name.Text
	td.NameRange = Range{Start: name.Start, End: name.End, Line: name.Line}
	p.skip()

	// Skip generics on the name, base list (`: Foo, Bar`), constraints
	// (`where T : class`), and primary-constructor parens for records.
	for p.pos < len(p.tokens) && p.text() != "{" && p.text() != ";" {
		switch p.text() {
		case "<":
			p.skipAngleBrackets()
		case "(":
			p.skipBalanced('(', ')')
		default:
			p.skip()
		}
	}
	// enum/delegate may use ; (delegate); records may use ; too.
	if p.text() == ";" {
		p.skip()
		return td, true
	}
	if p.text() != "{" {
		return td, true
	}
	bodyStart := p.at().Start
	p.skip() // {
	p.parseTypeBody(&td)
	bodyEnd := p.tokens[p.pos-1].End
	td.BodyRange = Range{Start: bodyStart, End: bodyEnd, Line: csLineFor(bodyStart, p.src)}
	return td, true
}

func (p *csParser) parseTypeBody(td *TypeDecl) {
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
		if depth != 1 {
			p.skip()
			continue
		}
		// Attribute on a member
		if tok.Text == "[" {
			p.skipBrackets()
			continue
		}
		// Nested type (direct child)
		if p.atTypeKeyword() {
			if nested, ok := p.parseType(); ok {
				td.Nested = append(td.Nested, nested)
			}
			continue
		}
		// Try to recognise a member.
		if tok.Kind == TIdent {
			members := p.tryParseMembers(td.Name)
			for _, m := range members {
				switch m.Kind {
				case "method", "constructor":
					td.Methods = append(td.Methods, m)
				case "field":
					td.Fields = append(td.Fields, m)
				case "property":
					td.Properties = append(td.Properties, m)
				}
			}
			continue
		}
		p.skip()
	}
}

// tryParseMembers handles a single declaration:
//
//	modifiers... type name ( ... ) { ... }     // method
//	modifiers... ClassName ( ... ) { ... }     // constructor
//	modifiers... type name { get; set; }       // property
//	modifiers... type name = expr;             // field
//	modifiers... type name1, name2, name3;     // field (multi)
func (p *csParser) tryParseMembers(className string) []MemberDecl {
	save := p.pos

	// Skip modifiers.
	for p.pos < len(p.tokens) && p.at().Kind == TIdent && isCSModifier(p.text()) {
		p.skip()
	}
	// Skip attributes (in case of inline).
	for p.text() == "[" {
		p.skipBrackets()
	}

	// Collect identifier tokens, skipping generics and type syntax,
	// until we find a parenthesis (method/constructor) or `{`/`;`/`=`/`,` (property/field).
	var idents []Token
	for p.pos < len(p.tokens) {
		tok := p.at()
		if tok.Text == "{" || tok.Text == "}" {
			break
		}
		if tok.Kind == TIdent && !isCSTypeKeyword(tok.Text) && !tok.IsKeyword() {
			idents = append(idents, tok)
			p.skip()
			continue
		}
		if tok.Kind == TIdent && isCSTypeKeyword(tok.Text) {
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
		if tok.Text == "." || tok.Text == "::" || tok.Text == "?" {
			p.skip()
			continue
		}
		// '(' → method or constructor
		if tok.Text == "(" && len(idents) > 0 {
			name := idents[len(idents)-1]
			kind := "method"
			if name.Text == className {
				kind = "constructor"
			}
			p.skipBalanced('(', ')')
			// Method body or `=> expr;` or `;` (abstract/interface)
			for p.text() == "where" || p.text() == ":" {
				// skip `where T : ...` / base call `: base(args)`
				p.skipUntil("{", ";")
			}
			if p.text() == "{" {
				p.skipBalanced('{', '}')
			} else if p.text() == "=>" {
				// expression-bodied; skip until ;
				for p.pos < len(p.tokens) && p.text() != ";" {
					p.skip()
				}
				if p.text() == ";" {
					p.skip()
				}
			} else if p.text() == ";" {
				p.skip()
			}
			return []MemberDecl{{
				Name: name.Text, Kind: kind,
				NameRange: Range{Start: name.Start, End: name.End, Line: name.Line},
			}}
		}
		// '{' → property (already broken out above, but we need to catch
		// the case where we see `{` right after the name)
		// Actually '{' is broken out at top; let me re-handle:
		// property: type name {
		// We need to detect `{` here. But the loop breaks on '{'. Let me
		// add a specific check just before breaking.
		// '=' or ';' or ',' → field
		if (tok.Text == ";" || tok.Text == "=" || tok.Text == ",") && len(idents) > 0 {
			return p.parseFieldDeclarators(idents)
		}
		// Anything else — give up
		break
	}

	// If we collected idents and the next non-token is '{', it might be a property.
	if len(idents) > 0 && p.text() == "{" {
		name := idents[len(idents)-1]
		p.skipBalanced('{', '}')
		// optional initializer: = expr;
		if p.text() == "=" {
			for p.pos < len(p.tokens) && p.text() != ";" {
				p.skip()
			}
		}
		if p.text() == ";" {
			p.skip()
		}
		return []MemberDecl{{
			Name: name.Text, Kind: "property",
			NameRange: Range{Start: name.Start, End: name.End, Line: name.Line},
		}}
	}

	p.pos = save + 1
	return nil
}

func (p *csParser) parseFieldDeclarators(idents []Token) []MemberDecl {
	if len(idents) == 0 {
		return nil
	}
	var out []MemberDecl
	name := idents[len(idents)-1]
	out = append(out, MemberDecl{
		Name: name.Text, Kind: "field",
		NameRange: Range{Start: name.Start, End: name.End, Line: name.Line},
	})
	for p.pos < len(p.tokens) {
		tok := p.at()
		if tok.Text == ";" {
			p.skip()
			return out
		}
		if tok.Text == "=" {
			p.skip()
			p.skipFieldInit()
			continue
		}
		if tok.Text == "," {
			p.skip()
			p.skipComments()
			if p.at().Kind == TIdent && !p.at().IsKeyword() {
				n := p.at()
				out = append(out, MemberDecl{
					Name: n.Text, Kind: "field",
					NameRange: Range{Start: n.Start, End: n.End, Line: n.Line},
				})
				p.skip()
			}
			continue
		}
		p.skip()
	}
	return out
}

func (p *csParser) skipFieldInit() {
	depth := 0
	for p.pos < len(p.tokens) {
		t := p.text()
		switch t {
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

func (p *csParser) skipBrackets() {
	if p.text() != "[" {
		return
	}
	depth := 0
	for p.pos < len(p.tokens) {
		t := p.text()
		if t == "[" {
			depth++
		}
		if t == "]" {
			depth--
			if depth <= 0 {
				p.skip()
				return
			}
		}
		p.skip()
	}
}

func (p *csParser) skipBalanced(open, close byte) {
	if p.pos >= len(p.tokens) || p.at().Text[0] != open {
		return
	}
	depth := 1
	p.skip()
	for p.pos < len(p.tokens) && depth > 0 {
		t := p.text()
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

func (p *csParser) skipAngleBrackets() {
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

func (p *csParser) skipUntil(targets ...string) {
	for p.pos < len(p.tokens) {
		for _, t := range targets {
			if p.text() == t {
				return
			}
		}
		p.skip()
	}
}

func (p *csParser) peekText(offset int) string {
	idx := p.pos + offset
	if idx < 0 || idx >= len(p.tokens) {
		return ""
	}
	return p.tokens[idx].Text
}

func isCSModifier(s string) bool {
	switch s {
	case "public", "private", "protected", "internal", "static", "readonly",
		"const", "virtual", "override", "abstract", "sealed", "partial",
		"new", "async", "extern", "unsafe", "volatile", "ref", "out",
		"in", "params", "required", "scoped", "file", "init":
		return true
	}
	return false
}

func isCSTypeKeyword(s string) bool {
	switch s {
	case "void", "int", "long", "short", "byte", "sbyte", "uint", "ulong",
		"ushort", "float", "double", "decimal", "bool", "char", "string",
		"object", "dynamic", "var":
		return true
	}
	return false
}

func csLineFor(off int, src []byte) int {
	line := 1
	for i := 0; i < off && i < len(src); i++ {
		if src[i] == '\n' {
			line++
		}
	}
	return line
}
