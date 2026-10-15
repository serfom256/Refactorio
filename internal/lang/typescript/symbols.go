package typescript

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/serfom256/refactorio/internal/lang/common"
)

type FileSymbols struct {
	Imports      []ImportDecl
	Exports      []ExportDecl
	Declarations []Declaration
}

type Range = common.Range

type ImportDecl struct {
	Names      []ImportName
	Path       string
	Range      Range
	PathRange  Range
	IsTypeOnly bool
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

type ExportDecl struct {
	Names     []ExportName
	From      string
	FromRange Range
	Range     Range
}

type ExportName struct {
	Name  string
	Alias string
	Range Range
}

type Declaration struct {
	Name      string
	Kind      string
	NameRange Range
	IsExport  bool
	IsDefault bool
}

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

			if tok.Text == "{" || tok.Text == "*" {
				if exp, ok := p.parseExportBraces(); ok {
					fs.Exports = append(fs.Exports, exp)
				}
				continue
			}
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
				p.skipToSemiOrNewline()
				continue
			}
			if tok.Text == "type" && p.peekText(1) == "{" {
				p.skip()
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

func (p *tsParser) parseImport() (ImportDecl, bool) {
	imp := ImportDecl{Range: Range{Start: p.at().Start, Line: p.at().Line}}
	p.skip()
	p.skipComments()

	if p.text() == "type" && p.peekText(1) != "(" {
		imp.IsTypeOnly = true
		p.skip()
		p.skipComments()
	}

	if p.at().Kind == TString {
		imp.Path, imp.PathRange = p.extractStringPath()
		imp.Range.End = p.at().Start
		p.skipToSemiOrNewline()
		return imp, imp.Path != ""
	}

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

	if p.text() == "{" {
		imp.Names = p.parseNamedBindings()
		imp.Path, imp.PathRange = p.consumeFromClause()
		imp.Range.End = p.at().Start
		p.skipToSemiOrNewline()
		return imp, imp.Path != ""
	}

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
	p.skip()
	var names []ImportName
	for p.pos < len(p.tokens) && p.text() != "}" {
		p.skipComments()
		if p.text() == "}" || p.pos >= len(p.tokens) {
			break
		}
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

func (p *tsParser) parseDeclaration(isExport, isDefault bool) (Declaration, bool) {
	d := Declaration{IsExport: isExport, IsDefault: isDefault}

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
		p.skipToMatchingBrace()
		return d, d.Name != ""

	case "function":
		d.Kind = "function"
		p.skip()
		p.skipComments()
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
			return true
		default:
			return false
		}
	}
	return true
}

func ResolveImportPath(importPath, fromFile string) string {
	if !strings.HasPrefix(importPath, ".") {
		return ""
	}
	dir := filepath.Dir(fromFile)
	resolved := filepath.Join(dir, importPath)

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
