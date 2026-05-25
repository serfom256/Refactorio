package kotlin

import (
	"strings"

	"github.com/serfom256/refactorio/internal/lang/common"
)

type FileSymbols struct {
	Package      string
	PackageRange Range
	Imports      []ImportSpec
	Declarations []Declaration
}

type Range = common.Range

type ImportSpec struct {
	Path      string
	Alias     string // for `import com.foo.Bar as B`
	Range     Range
	PathRange Range
}

type Declaration struct {
	Name      string
	Kind      string // "class" | "interface" | "object" | "enum" | "fun" | "val" | "var" | "typealias"
	NameRange Range
	IsExport  bool
}

func ExtractSymbols(src []byte) (*FileSymbols, error) {
	tokens := Tokenize(src)
	p := &ktParser{tokens: tokens, src: src}
	return p.parse(), nil
}

type ktParser struct {
	tokens []Token
	pos    int
	src    []byte
}

func (p *ktParser) at() Token {
	if p.pos >= len(p.tokens) {
		return Token{Kind: TEOF}
	}
	return p.tokens[p.pos]
}

func (p *ktParser) text() string { return p.at().Text }
func (p *ktParser) skip()        { p.pos++ }

func (p *ktParser) skipComments() {
	for p.pos < len(p.tokens) && p.tokens[p.pos].Kind == TComment {
		p.pos++
	}
}

func (p *ktParser) parse() *FileSymbols {
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

		isExport := true // Kotlin: everything is public by default
		// Skip modifiers
		for p.at().Kind == TIdent && isModifier(p.text()) {
			if p.text() == "private" || p.text() == "internal" {
				isExport = false
			}
			p.skip()
			p.skipComments()
		}
		if p.at().Kind == TPunct && p.text() == "@" {
			p.skip()
			p.skipAnnotation()
			continue
		}

		if isDeclKeyword(p.text()) {
			if d, ok := p.parseDeclaration(isExport); ok {
				fs.Declarations = append(fs.Declarations, d)
			}
			continue
		}
		p.skip()
	}
	return fs
}

func (p *ktParser) parsePackage() (string, Range) {
	p.skip() // "package"
	p.skipComments()
	start := p.pos
	for p.pos < len(p.tokens) && p.text() != ";" && !isDeclKeyword(p.text()) && p.text() != "import" {
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
	return name.String(), Range{Start: nameStart, End: nameEnd, Line: p.tokens[start].Line}
}

func (p *ktParser) parseImport() (ImportSpec, bool) {
	imp := ImportSpec{Range: Range{Start: p.at().Start, Line: p.at().Line}}
	p.skip() // "import"
	p.skipComments()

	pathStart, pathEnd := -1, -1
	var path strings.Builder
	for p.pos < len(p.tokens) {
		tok := p.at()
		if tok.Text == ";" || (tok.Kind == TIdent && isTopLevel(tok.Text)) {
			break
		}
		if tok.Text == "as" {
			p.skip()
			p.skipComments()
			if p.at().Kind == TIdent {
				imp.Alias = p.text()
				p.skip()
			}
			break
		}
		if tok.Text == "*" {
			// Kotlin wildcard: import com.foo.*
			path.WriteString("*")
			p.skip()
			continue
		}
		if tok.Text == "." && p.pos+1 < len(p.tokens) && p.tokens[p.pos+1].Text == "*" {
			path.WriteString(".*")
			pathEnd = p.tokens[p.pos+1].End
			p.pos += 2
			continue
		}
		if pathStart < 0 {
			pathStart = tok.Start
		}
		pathEnd = tok.End
		path.WriteString(tok.Text)
		p.skip()
	}

	imp.Path = path.String()
	if pathStart >= 0 {
		imp.PathRange = Range{Start: pathStart, End: pathEnd, Line: lineFor(pathStart, p.src)}
	}
	imp.Range.End = pathEnd
	if imp.Path == "" {
		return ImportSpec{}, false
	}
	return imp, true
}

func (p *ktParser) parseDeclaration(isExport bool) (Declaration, bool) {
	d := Declaration{IsExport: isExport}
	tok := p.at()

	switch tok.Text {
	case "class", "interface", "object", "enum":
		d.Kind = tok.Text
		p.skip()
		p.skipComments()
		// `enum class` → kind stays "enum"
		if d.Kind == "enum" && p.text() == "class" {
			p.skip()
			p.skipComments()
		}
		if p.at().Kind == TIdent && !p.at().IsKeyword() {
			d.Name = p.text()
			d.NameRange = Range{Start: p.at().Start, End: p.at().End, Line: p.at().Line}
			p.skip()
		}
		p.skipToEndOfDecl()
		return d, d.Name != ""

	case "fun":
		d.Kind = "fun"
		p.skip()
		p.skipComments()
		// Skip generic params <T>
		if p.text() == "<" {
			p.skipAngleBrackets()
			p.skipComments()
		}
		// Extension function: Type.name — skip the receiver type.
		// Look ahead: if we see Ident.Ident(, the first is the receiver.
		if p.at().Kind == TIdent {
			name := p.text()
			nameRange := Range{Start: p.at().Start, End: p.at().End, Line: p.at().Line}
			p.skip()
			p.skipComments()
			if p.text() == "." {
				// Extension function — skip receiver, get actual name.
				p.skip()
				p.skipComments()
				if p.at().Kind == TIdent {
					name = p.text()
					nameRange = Range{Start: p.at().Start, End: p.at().End, Line: p.at().Line}
					p.skip()
				}
			}
			d.Name = name
			d.NameRange = nameRange
		}
		p.skipToEndOfDecl()
		return d, d.Name != ""

	case "val", "var":
		d.Kind = tok.Text
		p.skip()
		p.skipComments()
		if p.at().Kind == TIdent && !p.at().IsKeyword() {
			d.Name = p.text()
			d.NameRange = Range{Start: p.at().Start, End: p.at().End, Line: p.at().Line}
			p.skip()
		}
		p.skipToEndOfDecl()
		return d, d.Name != ""

	case "typealias":
		d.Kind = "typealias"
		p.skip()
		p.skipComments()
		if p.at().Kind == TIdent {
			d.Name = p.text()
			d.NameRange = Range{Start: p.at().Start, End: p.at().End, Line: p.at().Line}
			p.skip()
		}
		p.skipToEndOfDecl()
		return d, d.Name != ""
	}

	p.skip()
	return Declaration{}, false
}

func (p *ktParser) skipToEndOfDecl() {
	for p.pos < len(p.tokens) {
		if p.text() == "{" {
			p.skipBraces()
			return
		}
		if p.at().Kind == TIdent && isTopLevel(p.text()) {
			return
		}
		p.skip()
	}
}

func (p *ktParser) skipBraces() {
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

func (p *ktParser) skipAngleBrackets() {
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
		case ";", "{", "}":
			return
		}
		p.skip()
	}
}

func (p *ktParser) skipAnnotation() {
	if p.at().Kind == TIdent {
		p.skip()
	}
	for p.pos+1 < len(p.tokens) && p.text() == "." && p.tokens[p.pos+1].Kind == TIdent {
		p.pos += 2
	}
	if p.text() == "(" {
		depth := 1
		p.skip()
		for p.pos < len(p.tokens) && depth > 0 {
			if p.text() == "(" {
				depth++
			}
			if p.text() == ")" {
				depth--
			}
			p.skip()
		}
	}
}

func isModifier(s string) bool {
	switch s {
	case "public", "private", "protected", "internal", "open", "final",
		"abstract", "sealed", "data", "inner", "companion", "inline",
		"external", "override", "lateinit", "const", "suspend",
		"tailrec", "operator", "infix", "expect", "actual", "annotation":
		return true
	}
	return false
}

func isDeclKeyword(s string) bool {
	switch s {
	case "class", "interface", "object", "enum", "fun", "val", "var", "typealias":
		return true
	}
	return false
}

func isTopLevel(s string) bool {
	switch s {
	case "class", "interface", "object", "enum", "fun", "val", "var",
		"typealias", "package", "import":
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
