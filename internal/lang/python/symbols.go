package python

import (
	"strings"

	"github.com/serfom256/refactorio/internal/lang/common"
)

type Range = common.Range

type FileSymbols struct {
	Imports      []ImportDecl
	Declarations []Declaration
}

type ImportDecl struct {
	Path      string
	Names     []ImportName
	IsFrom    bool
	Range     Range
	PathRange Range
}

type ImportName struct {
	Name  string
	Alias string
	Range Range
}

func (n ImportName) LocalName() string {
	if n.Alias != "" {
		return n.Alias
	}
	return n.Name
}

type Declaration struct {
	Name      string
	Kind      string
	NameRange Range
	BodyRange Range
	Indent    int
	Members   []Declaration
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
		if p.at().Kind == TOp && p.text() == "@" {
			p.skipDecorator()
			continue
		}
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
					p.skip()
					if d, ok := p.parseFunction(1, true); ok {
						fs.Declarations = append(fs.Declarations, d)
					}
					continue
				}
			}
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
	p.skip()
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

func (p *pyParser) looksLikeAssignment() bool {
	i := p.pos + 1
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

func (p *pyParser) parseImport() (ImportDecl, bool) {
	imp := ImportDecl{Range: Range{Start: p.at().Start, Line: p.at().Line}}
	p.skip()

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

func (p *pyParser) parseFromImport() (ImportDecl, bool) {
	imp := ImportDecl{
		Range:  Range{Start: p.at().Start, Line: p.at().Line},
		IsFrom: true,
	}
	p.skip()

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
	p.skip()

	parenthesised := false
	if p.text() == "(" {
		parenthesised = true
		p.skip()
	}

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
	p.skip()
	if p.at().Kind != TIdent {
		return Declaration{}, false
	}
	name := p.at()
	d.Name = name.Text
	d.NameRange = Range{Start: name.Start, End: name.End, Line: name.Line}
	p.skip()

	if p.text() == "(" {
		p.skipBalanced('(', ')')
	}
	if p.text() == ":" {
		p.skip()
	}
	bodyStart := p.pos
	p.skipToLineEnd()
	p.skipBlanks()

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
	p.skip()
	if p.at().Kind != TIdent {
		return Declaration{}, false
	}
	name := p.at()
	d.Name = name.Text
	d.NameRange = Range{Start: name.Start, End: name.End, Line: name.Line}
	p.skip()
	if p.text() == "(" {
		p.skipBalanced('(', ')')
	}
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
	bodyEnd := p.skipBlockBody(indent)
	d.BodyRange = Range{Start: bodyHeader, End: bodyEnd, Line: pyLineFor(bodyHeader, p.src)}
	return d, true
}

func (p *pyParser) parseBlock(parentIndent int) ([]Declaration, int) {
	var members []Declaration
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
