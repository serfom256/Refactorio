package python

import (
	"strings"

	"github.com/serfom256/refactorio/internal/lang/common"
)

type Range = common.Range

// FileSymbols is the per-file structural data for Python source.
type FileSymbols struct {
	Imports      []ImportDecl
	Declarations []Declaration
}

type ImportDecl struct {
	// Path is the module path: "foo.bar" for `import foo.bar` or
	// `from foo.bar import X`. For relative imports, leading dots are
	// kept: ".sibling", "..parent.mod".
	Path string
	// Names are the imported symbols for `from x import a, b`. Empty
	// for `import foo` (whole module).
	Names []ImportName
	// IsFrom distinguishes `from x import y` (true) from `import x` (false).
	IsFrom    bool
	Range     Range
	PathRange Range
}

type ImportName struct {
	Name  string
	Alias string
	Range Range
}

// LocalName returns the name a binding is exposed as in code.
func (n ImportName) LocalName() string {
	if n.Alias != "" {
		return n.Alias
	}
	return n.Name
}

// Declaration is a top-level class, function, or assignment.
type Declaration struct {
	Name      string
	Kind      string // "class" | "function" | "async_function" | "variable"
	NameRange Range
	// BodyRange covers the indented body for class/function. Empty for
	// variable bindings.
	BodyRange Range
	// Indent is the column of the declaration's first token (1-based).
	// Top-level declarations have Indent == 1.
	Indent int
	// Members are nested declarations directly inside a class body.
	Members []Declaration
}

func ExtractSymbols(src []byte) (*FileSymbols, error) {
	tokens := Tokenize(src)
	p := &pyParser{tokens: tokens, src: src}
	return p.parse(), nil
}

type pyParser struct {
	tokens []Token
	pos    int
	src    []byte
}

func (p *pyParser) at() Token {
	if p.pos >= len(p.tokens) {
		return Token{Kind: TEOF}
	}
	return p.tokens[p.pos]
}

func (p *pyParser) text() string { return p.at().Text }
func (p *pyParser) skip()        { p.pos++ }

func (p *pyParser) skipBlanks() {
	for p.pos < len(p.tokens) && (p.at().Kind == TComment || p.at().Kind == TNewline) {
		p.pos++
	}
}

// atStatementStart reports whether the current token is the first
// significant token on its line at top level (column 1).
func (p *pyParser) atColumn(col int) bool {
	if p.pos >= len(p.tokens) {
		return false
	}
	return p.tokens[p.pos].Column == col
}

func (p *pyParser) parse() *FileSymbols {
	fs := &FileSymbols{}
	for p.pos < len(p.tokens) {
		p.skipBlanks()
		if p.pos >= len(p.tokens) {
			break
		}
		// Skip decorators: `@name` or `@name(args)` at top level.
		if p.at().Kind == TOp && p.text() == "@" {
			p.skipDecorator()
			continue
		}
		// Only consider top-level (column 1) statements as declarations.
		// Indented code lives inside a class/function body.
		if !p.atColumn(1) {
			p.skip()
			continue
		}
		tok := p.at()
		if tok.Kind == TIdent {
			switch tok.Text {
			case "import":
				if imp, ok := p.parseImport(); ok {
					fs.Imports = append(fs.Imports, imp)
				}
				continue
			case "from":
				if imp, ok := p.parseFromImport(); ok {
					fs.Imports = append(fs.Imports, imp)
				}
				continue
			case "class":
				if d, ok := p.parseClass(1); ok {
					fs.Declarations = append(fs.Declarations, d)
				}
				continue
			case "def":
				if d, ok := p.parseFunction(1, false); ok {
					fs.Declarations = append(fs.Declarations, d)
				}
				continue
			case "async":
				if p.peekText(1) == "def" {
					p.skip() // skip "async"
					if d, ok := p.parseFunction(1, true); ok {
						fs.Declarations = append(fs.Declarations, d)
					}
					continue
				}
			}
			// Top-level assignment: NAME = ... or NAME: TYPE = ...
			if !tok.IsKeyword() && p.looksLikeAssignment() {
				d := Declaration{
					Name: tok.Text, Kind: "variable",
					NameRange: Range{Start: tok.Start, End: tok.End, Line: tok.Line},
					Indent:    1,
				}
				fs.Declarations = append(fs.Declarations, d)
				p.skipToLineEnd()
				continue
			}
		}
		p.skip()
	}
	return fs
}

func (p *pyParser) peekText(off int) string {
	idx := p.pos + off
	for idx < len(p.tokens) && (p.tokens[idx].Kind == TComment || p.tokens[idx].Kind == TNewline) {
		idx++
		off++
	}
	if idx >= len(p.tokens) {
		return ""
	}
	return p.tokens[idx].Text
}

func (p *pyParser) skipDecorator() {
	// Consume the @, then everything until newline (or until the end
	// of the call expression if multi-line — we keep it simple).
	p.skip() // @
	depth := 0
	for p.pos < len(p.tokens) {
		t := p.at()
		if t.Kind == TNewline && depth == 0 {
			p.skip()
			return
		}
		if t.Text == "(" || t.Text == "[" || t.Text == "{" {
			depth++
		}
		if t.Text == ")" || t.Text == "]" || t.Text == "}" {
			depth--
		}
		p.skip()
	}
}

// looksLikeAssignment checks if the current ident is followed (after
// optional `: type_annotation`) by `=` (but not `==`).
func (p *pyParser) looksLikeAssignment() bool {
	i := p.pos + 1
	// Skip type annotation.
	if i < len(p.tokens) && p.tokens[i].Text == ":" {
		i++
		depth := 0
		for i < len(p.tokens) {
			t := p.tokens[i]
			if depth == 0 && (t.Text == "=" || t.Kind == TNewline) {
				break
			}
			switch t.Text {
			case "(", "[", "{":
				depth++
			case ")", "]", "}":
				depth--
			}
			i++
		}
	}
	if i >= len(p.tokens) {
		return false
	}
	return p.tokens[i].Text == "="
}

func (p *pyParser) skipToLineEnd() {
	depth := 0
	for p.pos < len(p.tokens) {
		t := p.at()
		if t.Kind == TNewline && depth == 0 {
			p.skip()
			return
		}
		switch t.Text {
		case "(", "[", "{":
			depth++
		case ")", "]", "}":
			depth--
		}
		p.skip()
	}
}

// parseImport handles `import foo`, `import foo.bar`, `import foo as f`,
// `import foo, bar`.
func (p *pyParser) parseImport() (ImportDecl, bool) {
	imp := ImportDecl{Range: Range{Start: p.at().Start, Line: p.at().Line}}
	p.skip() // "import"

	// Collect path tokens.
	pathStart, pathEnd := -1, -1
	var path strings.Builder
	for p.pos < len(p.tokens) {
		t := p.at()
		if t.Kind == TNewline || t.Text == "," || t.Text == ";" {
			break
		}
		if t.Text == "as" {
			p.skip()
			if p.at().Kind == TIdent {
				imp.Names = append(imp.Names, ImportName{
					Name:  path.String(),
					Alias: p.text(),
					Range: Range{Start: p.at().Start, End: p.at().End, Line: p.at().Line},
				})
				p.skip()
			}
			break
		}
		if pathStart < 0 {
			pathStart = t.Start
		}
		pathEnd = t.End
		path.WriteString(t.Text)
		p.skip()
	}
	stmtEnd := pathEnd
	if p.pos < len(p.tokens) && p.at().Kind == TNewline {
		p.skip()
	}
	imp.Path = path.String()
	if pathStart >= 0 {
		imp.PathRange = Range{Start: pathStart, End: pathEnd, Line: pyLineFor(pathStart, p.src)}
	}
	imp.Range.End = stmtEnd
	return imp, imp.Path != ""
}

// parseFromImport handles `from x import a`, `from x import a, b`,
// `from x import a as b`, `from . import x`, `from x import *`.
func (p *pyParser) parseFromImport() (ImportDecl, bool) {
	imp := ImportDecl{
		Range:  Range{Start: p.at().Start, Line: p.at().Line},
		IsFrom: true,
	}
	p.skip() // "from"

	// Collect path tokens up to "import".
	pathStart, pathEnd := -1, -1
	var path strings.Builder
	for p.pos < len(p.tokens) {
		t := p.at()
		if t.Text == "import" {
			break
		}
		if t.Kind == TNewline {
			return ImportDecl{}, false
		}
		if pathStart < 0 {
			pathStart = t.Start
		}
		pathEnd = t.End
		path.WriteString(t.Text)
		p.skip()
	}
	imp.Path = strings.TrimSpace(path.String())
	if pathStart >= 0 {
		imp.PathRange = Range{Start: pathStart, End: pathEnd, Line: pyLineFor(pathStart, p.src)}
	}

	if p.pos >= len(p.tokens) || p.text() != "import" {
		return imp, imp.Path != ""
	}
	p.skip() // "import"

	// Optional parenthesised name list: `from x import (a, b)`.
	parenthesised := false
	if p.text() == "(" {
		parenthesised = true
		p.skip()
	}

	// Helper: skip comments, and skip newlines ONLY if we're inside `()`.
	skipBetween := func() {
		for p.pos < len(p.tokens) {
			if p.tokens[p.pos].Kind == TComment {
				p.pos++
				continue
			}
			if parenthesised && p.tokens[p.pos].Kind == TNewline {
				p.pos++
				continue
			}
			break
		}
	}

	for p.pos < len(p.tokens) {
		skipBetween()
		if p.pos >= len(p.tokens) {
			break
		}
		t := p.at()
		if t.Kind == TNewline || t.Text == ")" || t.Text == ";" {
			break
		}
		if t.Text == "*" {
			imp.Names = append(imp.Names, ImportName{
				Name:  "*",
				Range: Range{Start: t.Start, End: t.End, Line: t.Line},
			})
			p.skip()
			continue
		}
		if t.Kind != TIdent {
			p.skip()
			continue
		}
		name := ImportName{
			Name:  t.Text,
			Range: Range{Start: t.Start, End: t.End, Line: t.Line},
		}
		p.skip()
		skipBetween()
		if p.text() == "as" {
			p.skip()
			skipBetween()
			if p.at().Kind == TIdent {
				name.Alias = p.text()
				p.skip()
			}
		}
		imp.Names = append(imp.Names, name)
		skipBetween()
		if p.text() == "," {
			p.skip()
		}
	}
	if p.text() == ")" {
		p.skip()
	}
	imp.Range.End = p.at().End
	if p.at().Kind == TNewline {
		p.skip()
	}
	return imp, imp.Path != ""
}

func (p *pyParser) parseClass(indent int) (Declaration, bool) {
	d := Declaration{Kind: "class", Indent: indent}
	bodyHeader := p.at().Start
	p.skip() // "class"
	if p.at().Kind != TIdent {
		return Declaration{}, false
	}
	name := p.at()
	d.Name = name.Text
	d.NameRange = Range{Start: name.Start, End: name.End, Line: name.Line}
	p.skip()

	// Skip optional base list: `(Foo, Bar, metaclass=Baz)`.
	if p.text() == "(" {
		p.skipBalanced('(', ')')
	}
	// Expect ':' starting the body.
	if p.text() == ":" {
		p.skip()
	}
	// Skip until first non-blank token at indent > class indent.
	bodyStart := p.pos
	p.skipToLineEnd()
	p.skipBlanks()

	// Parse members at indent > class indent.
	members, bodyEnd := p.parseBlock(indent)
	d.Members = members
	d.BodyRange = Range{Start: bodyHeader, End: bodyEnd, Line: pyLineFor(bodyHeader, p.src)}
	_ = bodyStart
	return d, true
}

func (p *pyParser) parseFunction(indent int, async bool) (Declaration, bool) {
	d := Declaration{Indent: indent}
	if async {
		d.Kind = "async_function"
	} else {
		d.Kind = "function"
	}
	bodyHeader := p.at().Start
	p.skip() // "def"
	if p.at().Kind != TIdent {
		return Declaration{}, false
	}
	name := p.at()
	d.Name = name.Text
	d.NameRange = Range{Start: name.Start, End: name.End, Line: name.Line}
	p.skip()
	// Skip parameter list.
	if p.text() == "(" {
		p.skipBalanced('(', ')')
	}
	// Skip return type annotation: `-> Type`.
	if p.text() == "->" {
		p.skip()
		for p.pos < len(p.tokens) && p.text() != ":" && p.at().Kind != TNewline {
			p.skip()
		}
	}
	if p.text() == ":" {
		p.skip()
	}
	p.skipToLineEnd()
	p.skipBlanks()
	// Skip the body (we don't recurse — for v1 we report top-level only).
	bodyEnd := p.skipBlockBody(indent)
	d.BodyRange = Range{Start: bodyHeader, End: bodyEnd, Line: pyLineFor(bodyHeader, p.src)}
	return d, true
}

// parseBlock parses the indented block of a class. Returns the
// declarations and the byte offset where the block ends (start of the
// next less-indented token).
func (p *pyParser) parseBlock(parentIndent int) ([]Declaration, int) {
	var members []Declaration
	end := p.at().Start
	for p.pos < len(p.tokens) {
		p.skipBlanks()
		if p.pos >= len(p.tokens) {
			break
		}
		t := p.at()
		// Block ends when we see a token at indent <= parentIndent.
		if t.Column <= parentIndent {
			break
		}
		end = t.End
		if t.Kind == TOp && t.Text == "@" {
			p.skipDecorator()
			continue
		}
		memberIndent := t.Column
		if t.Kind == TIdent {
			switch t.Text {
			case "class":
				if d, ok := p.parseClass(memberIndent); ok {
					members = append(members, d)
				}
				continue
			case "def":
				if d, ok := p.parseFunction(memberIndent, false); ok {
					members = append(members, d)
				}
				continue
			case "async":
				if p.peekText(1) == "def" {
					p.skip()
					if d, ok := p.parseFunction(memberIndent, true); ok {
						members = append(members, d)
					}
					continue
				}
			}
			// Class-level assignment: `attr = ...` or `attr: type = ...`.
			if !t.IsKeyword() && p.looksLikeAssignment() {
				members = append(members, Declaration{
					Name: t.Text, Kind: "variable",
					NameRange: Range{Start: t.Start, End: t.End, Line: t.Line},
					Indent:    memberIndent,
				})
			}
		}
		p.skipToLineEnd()
	}
	return members, end
}

// skipBlockBody walks past an indented block, returning the end offset.
func (p *pyParser) skipBlockBody(parentIndent int) int {
	end := p.at().Start
	for p.pos < len(p.tokens) {
		p.skipBlanks()
		if p.pos >= len(p.tokens) {
			break
		}
		t := p.at()
		if t.Column <= parentIndent {
			break
		}
		end = t.End
		p.skipToLineEnd()
	}
	return end
}

func (p *pyParser) skipBalanced(open, close byte) {
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

func pyLineFor(off int, src []byte) int {
	line := 1
	for i := 0; i < off && i < len(src); i++ {
		if src[i] == '\n' {
			line++
		}
	}
	return line
}
