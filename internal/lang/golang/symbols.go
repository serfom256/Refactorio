package golang

import (
	"strings"

	"github.com/serfom256/refactorio/internal/lang/common"
)

type FileSymbols struct {
	Package      string
	PackageRange Range
	Imports      []ImportDecl
	Declarations []Declaration
}

type Range = common.Range

type ImportDecl struct {
	Path      string
	Alias     string
	Range     Range
	PathRange Range
}

func (d ImportDecl) PackageName() string {
	if d.Alias != "" {
		return d.Alias
	}
	parts := strings.Split(d.Path, "/")
	return parts[len(parts)-1]
}

type Declaration struct {
	Name      string
	Kind      string
	NameRange Range
	IsExport  bool
	Receiver  string
}

func ExtractSymbols(src []byte) (*FileSymbols, error) {
	tokens := Tokenize(src)
	p := &goParser{tokens: tokens, src: src}
	return p.parse(), nil
}

type goParser struct {
	tokens []Token
	pos    int
	src    []byte
}

func (p *goParser) at() Token {
	if p.pos >= len(p.tokens) {
		return Token{Kind: TEOF}
	}
	return p.tokens[p.pos]
}

func (p *goParser) text() string { return p.at().Text }
func (p *goParser) skip()        { p.pos++ }

func (p *goParser) skipComments() {
	for p.pos < len(p.tokens) && p.tokens[p.pos].Kind == TComment {
		p.pos++
	}
}

func (p *goParser) parse() *FileSymbols {
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
			fs.Imports = append(fs.Imports, p.parseImports()...)
			continue
		}
		if tok.Kind == TIdent && tok.Text == "func" {
			if d, ok := p.parseFunc(); ok {
				fs.Declarations = append(fs.Declarations, d)
			}
			continue
		}
		if tok.Kind == TIdent && tok.Text == "type" {
			fs.Declarations = append(fs.Declarations, p.parseTypeDecls()...)
			continue
		}
		if tok.Kind == TIdent && (tok.Text == "var" || tok.Text == "const") {
			fs.Declarations = append(fs.Declarations, p.parseVarConst(tok.Text)...)
			continue
		}
		p.skip()
	}
	return fs
}

func (p *goParser) parsePackage() (string, Range) {
	p.skip()
	p.skipComments()
	if p.at().Kind != TIdent {
		return "", Range{}
	}
	tok := p.at()
	p.skip()
	return tok.Text, Range{Start: tok.Start, End: tok.End, Line: tok.Line}
}

func (p *goParser) parseImports() []ImportDecl {
	p.skip()
	p.skipComments()

	if p.text() == "(" {
		return p.parseGroupedImports()
	}
	if imp, ok := p.parseSingleImport(); ok {
		return []ImportDecl{imp}
	}
	return nil
}

func (p *goParser) parseGroupedImports() []ImportDecl {
	p.skip()
	var out []ImportDecl
	for p.pos < len(p.tokens) && p.text() != ")" {
		p.skipComments()
		if p.text() == ")" {
			break
		}
		if imp, ok := p.parseSingleImport(); ok {
			out = append(out, imp)
		}
	}
	if p.text() == ")" {
		p.skip()
	}
	return out
}

func (p *goParser) parseSingleImport() (ImportDecl, bool) {
	imp := ImportDecl{Range: Range{Start: p.at().Start, Line: p.at().Line}}

	if p.at().Kind == TIdent || p.text() == "." {
		imp.Alias = p.text()
		p.skip()
		p.skipComments()
	}

	if p.at().Kind == TString || p.at().Kind == TRawStr {
		tok := p.at()
		imp.Path = unquote(tok.Text)
		imp.PathRange = Range{Start: tok.Start + 1, End: tok.End - 1, Line: tok.Line}
		imp.Range.End = tok.End
		p.skip()
		return imp, imp.Path != ""
	}
	return ImportDecl{}, false
}

func (p *goParser) parseFunc() (Declaration, bool) {
	p.skip()
	p.skipComments()

	d := Declaration{Kind: "func"}

	if p.text() == "(" {
		d.Kind = "method"
		d.Receiver = p.parseReceiverType()
		p.skipComments()
	}

	if p.at().Kind != TIdent {
		p.skipToMatchingBrace()
		return Declaration{}, false
	}
	nameTok := p.at()
	d.Name = nameTok.Text
	d.NameRange = Range{Start: nameTok.Start, End: nameTok.End, Line: nameTok.Line}
	d.IsExport = isExported(d.Name)
	p.skip()

	p.skipToMatchingBrace()
	return d, true
}

func (p *goParser) parseReceiverType() string {
	p.skip()
	var typeName string
	for p.pos < len(p.tokens) && p.text() != ")" {
		if p.at().Kind == TIdent && !p.at().IsKeyword() {
			typeName = p.text()
		}
		p.skip()
	}
	if p.text() == ")" {
		p.skip()
	}
	return typeName
}

func (p *goParser) parseTypeDecls() []Declaration {
	p.skip()
	p.skipComments()

	if p.text() == "(" {
		return p.parseGroupedTypeDecls()
	}
	if d, ok := p.parseSingleTypeDecl(); ok {
		return []Declaration{d}
	}
	return nil
}

func (p *goParser) parseGroupedTypeDecls() []Declaration {
	p.skip()
	var out []Declaration
	for p.pos < len(p.tokens) && p.text() != ")" {
		p.skipComments()
		if p.text() == ")" {
			break
		}
		if d, ok := p.parseSingleTypeDecl(); ok {
			out = append(out, d)
		} else {
			p.skip()
		}
	}
	if p.text() == ")" {
		p.skip()
	}
	return out
}

func (p *goParser) parseSingleTypeDecl() (Declaration, bool) {
	if p.at().Kind != TIdent || p.at().IsKeyword() {
		return Declaration{}, false
	}
	tok := p.at()
	d := Declaration{
		Name:      tok.Text,
		Kind:      "type",
		NameRange: Range{Start: tok.Start, End: tok.End, Line: tok.Line},
		IsExport:  isExported(tok.Text),
	}
	p.skip()

	for p.pos < len(p.tokens) {
		if p.text() == "{" {
			p.skipBraces()
			break
		}
		if p.text() == ")" || (p.at().Kind == TIdent && isTopLevel(p.text())) {
			break
		}
		p.skip()
	}
	return d, true
}

func (p *goParser) parseVarConst(kind string) []Declaration {
	p.skip()
	p.skipComments()

	if p.text() == "(" {
		return p.parseGroupedVarConst(kind)
	}
	if d, ok := p.parseSingleVarConst(kind); ok {
		return []Declaration{d}
	}
	return nil
}

func (p *goParser) parseGroupedVarConst(kind string) []Declaration {
	p.skip()
	var out []Declaration
	for p.pos < len(p.tokens) && p.text() != ")" {
		p.skipComments()
		if p.text() == ")" {
			break
		}
		if d, ok := p.parseSingleVarConst(kind); ok {
			out = append(out, d)
		} else {
			p.skip()
		}
	}
	if p.text() == ")" {
		p.skip()
	}
	return out
}

func (p *goParser) parseSingleVarConst(kind string) (Declaration, bool) {
	if p.at().Kind != TIdent || p.at().IsKeyword() {
		return Declaration{}, false
	}
	tok := p.at()
	d := Declaration{
		Name:      tok.Text,
		Kind:      kind,
		NameRange: Range{Start: tok.Start, End: tok.End, Line: tok.Line},
		IsExport:  isExported(tok.Text),
	}
	p.skip()
	startLine := tok.Line
	for p.pos < len(p.tokens) {
		if p.text() == ")" || (p.at().Kind == TIdent && isTopLevel(p.text())) {
			break
		}
		if p.at().Line > startLine && p.at().Kind == TIdent {
			break
		}
		if p.text() == "{" {
			p.skipBraces()
			continue
		}
		p.skip()
	}
	return d, true
}

func (p *goParser) skipToMatchingBrace() {
	for p.pos < len(p.tokens) && p.text() != "{" {
		if p.at().Kind == TIdent && isTopLevel(p.text()) {
			return
		}
		p.skip()
	}
	if p.pos < len(p.tokens) {
		p.skipBraces()
	}
}

func (p *goParser) skipBraces() {
	if p.text() != "{" {
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

func isTopLevel(s string) bool {
	switch s {
	case "func", "type", "var", "const", "package", "import":
		return true
	}
	return false
}

func isExported(name string) bool {
	if name == "" {
		return false
	}
	return name[0] >= 'A' && name[0] <= 'Z'
}

func unquote(s string) string {
	if len(s) >= 2 && (s[0] == '"' || s[0] == '`') {
		return s[1 : len(s)-1]
	}
	return s
}
