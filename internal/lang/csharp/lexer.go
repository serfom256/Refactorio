// Package csharp provides C# lexing and symbol extraction.
package csharp

import (
	"unicode/utf8"

	"github.com/serfom256/refactorio/internal/lang/common"
)

type TokenKind = common.TokenKind

const (
	TIdent   = common.TIdent
	TPunct   = common.TPunct
	TOp      = common.TOp
	TInt     = common.TInt
	TFloat   = common.TFloat
	TString  = common.TString
	TComment = common.TComment
	TEOF     = common.TEOF
)

const (
	TVerbatim     TokenKind = common.TLangSpecific + iota // @"..."
	TInterpolated                                         // $"..." or $@"..." or $"""..."""
	TRawStr                                               // """..."""
	TChar                                                 // 'x'
)

type Token struct {
	Kind  TokenKind
	Start int
	End   int
	Text  string
	Line  int
}

func (t Token) IsKeyword() bool {
	if t.Kind != TIdent {
		return false
	}
	_, ok := csKeywords[t.Text]
	return ok
}

var csKeywords = map[string]struct{}{
	"abstract": {}, "as": {}, "base": {}, "bool": {}, "break": {},
	"byte": {}, "case": {}, "catch": {}, "char": {}, "checked": {},
	"class": {}, "const": {}, "continue": {}, "decimal": {}, "default": {},
	"delegate": {}, "do": {}, "double": {}, "else": {}, "enum": {},
	"event": {}, "explicit": {}, "extern": {}, "false": {}, "finally": {},
	"fixed": {}, "float": {}, "for": {}, "foreach": {}, "goto": {},
	"if": {}, "implicit": {}, "in": {}, "int": {}, "interface": {},
	"internal": {}, "is": {}, "lock": {}, "long": {}, "namespace": {},
	"new": {}, "null": {}, "object": {}, "operator": {}, "out": {},
	"override": {}, "params": {}, "private": {}, "protected": {}, "public": {},
	"readonly": {}, "ref": {}, "return": {}, "sbyte": {}, "sealed": {},
	"short": {}, "sizeof": {}, "stackalloc": {}, "static": {}, "string": {},
	"struct": {}, "switch": {}, "this": {}, "throw": {}, "true": {},
	"try": {}, "typeof": {}, "uint": {}, "ulong": {}, "unchecked": {},
	"unsafe": {}, "ushort": {}, "using": {}, "virtual": {}, "void": {},
	"volatile": {}, "while": {},
	// contextual keywords
	"add": {}, "alias": {}, "ascending": {}, "async": {}, "await": {},
	"by": {}, "descending": {}, "dynamic": {}, "equals": {}, "from": {},
	"get": {}, "global": {}, "group": {}, "init": {}, "into": {},
	"join": {}, "let": {}, "nameof": {}, "notnull": {}, "on": {},
	"orderby": {}, "partial": {}, "record": {}, "remove": {}, "select": {},
	"set": {}, "unmanaged": {}, "value": {}, "var": {}, "when": {},
	"where": {}, "with": {}, "yield": {},
	"file": {}, "required": {}, "scoped": {},
}

func Tokenize(src []byte) []Token {
	l := &lexer{src: src}
	l.lineStart = []int{0}
	for i, b := range src {
		if b == '\n' {
			l.lineStart = append(l.lineStart, i+1)
		}
	}
	var out []Token
	for {
		l.skipWS()
		if l.pos >= len(l.src) {
			break
		}
		t, ok := l.next(&out)
		if !ok {
			l.pos++
			continue
		}
		out = append(out, t)
	}
	return out
}

type lexer struct {
	src       []byte
	pos       int
	lineStart []int
}

func (l *lexer) lineFor(off int) int {
	lo, hi := 0, len(l.lineStart)-1
	for lo <= hi {
		mid := (lo + hi) / 2
		if l.lineStart[mid] <= off {
			lo = mid + 1
		} else {
			hi = mid - 1
		}
	}
	if lo == 0 {
		return 1
	}
	return lo
}

func (l *lexer) skipWS() {
	for l.pos < len(l.src) {
		c := l.src[l.pos]
		if c == ' ' || c == '\t' || c == '\r' || c == '\n' || c == '\f' {
			l.pos++
			continue
		}
		break
	}
}

func (l *lexer) peek(off int) byte {
	if l.pos+off >= len(l.src) {
		return 0
	}
	return l.src[l.pos+off]
}

func (l *lexer) next(prev *[]Token) (Token, bool) {
	start := l.pos
	c := l.src[l.pos]

	switch {
	case c == '/' && l.peek(1) == '/':
		return l.lineComment(start)
	case c == '/' && l.peek(1) == '*':
		return l.blockComment(start)
	case c == '"' && l.peek(1) == '"' && l.peek(2) == '"':
		return l.rawString(start)
	case c == '@' && l.peek(1) == '"':
		return l.verbatimString(start)
	case c == '$' && l.peek(1) == '"' && l.peek(2) == '"' && l.peek(3) == '"':
		return l.interpolatedRaw(start)
	case c == '$' && l.peek(1) == '@' && l.peek(2) == '"':
		return l.interpolatedVerbatim(start)
	case c == '$' && l.peek(1) == '"':
		return l.interpolatedString(start, prev)
	case c == '"':
		return l.stringLiteral(start)
	case c == '\'':
		return l.charLiteral(start)
	case c == '@' && l.pos+1 < len(l.src) && isIdentStart(rune(l.src[l.pos+1])):
		// @-prefixed identifier: @class, @if (escape keywords)
		l.pos++
		return l.identifier(start)
	case isDigit(c):
		return l.number(start)
	case c == '.' && l.pos+1 < len(l.src) && isDigit(l.src[l.pos+1]):
		return l.number(start)
	case isIdentStart(rune(c)) || c >= 0x80:
		return l.identifier(start)
	default:
		return l.operatorOrPunct(start)
	}
}

func (l *lexer) lineComment(start int) (Token, bool) {
	for l.pos < len(l.src) && l.src[l.pos] != '\n' {
		l.pos++
	}
	return l.emit(TComment, start), true
}

func (l *lexer) blockComment(start int) (Token, bool) {
	l.pos += 2
	for l.pos+1 < len(l.src) {
		if l.src[l.pos] == '*' && l.src[l.pos+1] == '/' {
			l.pos += 2
			return l.emit(TComment, start), true
		}
		l.pos++
	}
	l.pos = len(l.src)
	return l.emit(TComment, start), true
}

func (l *lexer) stringLiteral(start int) (Token, bool) {
	l.pos++
	for l.pos < len(l.src) {
		c := l.src[l.pos]
		if c == '\\' && l.pos+1 < len(l.src) {
			l.pos += 2
			continue
		}
		if c == '"' {
			l.pos++
			return l.emit(TString, start), true
		}
		if c == '\n' {
			return l.emit(TString, start), true
		}
		l.pos++
	}
	return l.emit(TString, start), true
}

// verbatimString handles @"..." where "" is an escaped quote and \ is literal.
func (l *lexer) verbatimString(start int) (Token, bool) {
	l.pos += 2 // skip @"
	for l.pos < len(l.src) {
		if l.src[l.pos] == '"' {
			if l.pos+1 < len(l.src) && l.src[l.pos+1] == '"' {
				l.pos += 2
				continue
			}
			l.pos++
			return l.emit(TVerbatim, start), true
		}
		l.pos++
	}
	return l.emit(TVerbatim, start), true
}

// rawString handles """...""" (C# 11).
func (l *lexer) rawString(start int) (Token, bool) {
	// Count opening quotes to know how many closers are required.
	opens := 0
	for l.pos < len(l.src) && l.src[l.pos] == '"' {
		opens++
		l.pos++
	}
	for l.pos < len(l.src) {
		if l.src[l.pos] != '"' {
			l.pos++
			continue
		}
		// Count consecutive quotes.
		j := l.pos
		closes := 0
		for j < len(l.src) && l.src[j] == '"' {
			closes++
			j++
		}
		if closes >= opens {
			l.pos = j
			return l.emit(TRawStr, start), true
		}
		l.pos = j
	}
	return l.emit(TRawStr, start), true
}

// interpolatedString handles $"...{expr}..." — splits into fragments
// and lexes expressions inside braces.
func (l *lexer) interpolatedString(start int, prev *[]Token) (Token, bool) {
	l.pos += 2 // skip $"
	fragStart := start
	for l.pos < len(l.src) {
		c := l.src[l.pos]
		if c == '\\' && l.pos+1 < len(l.src) {
			l.pos += 2
			continue
		}
		if c == '"' {
			l.pos++
			return Token{Kind: TInterpolated, Start: fragStart, End: l.pos, Text: string(l.src[fragStart:l.pos]), Line: l.lineFor(fragStart)}, true
		}
		if c == '{' && l.peek(1) == '{' {
			// escaped { — keep both as literal text
			l.pos += 2
			continue
		}
		if c == '}' && l.peek(1) == '}' {
			l.pos += 2
			continue
		}
		if c == '{' {
			*prev = append(*prev, Token{Kind: TInterpolated, Start: fragStart, End: l.pos, Text: string(l.src[fragStart:l.pos]), Line: l.lineFor(fragStart)})
			l.pos++
			depth := 1
			for depth > 0 && l.pos < len(l.src) {
				l.skipWS()
				if l.pos >= len(l.src) {
					break
				}
				if l.src[l.pos] == '}' {
					depth--
					if depth == 0 {
						l.pos++
						break
					}
				}
				if l.src[l.pos] == '{' {
					depth++
				}
				inner, ok := l.next(prev)
				if ok {
					*prev = append(*prev, inner)
				}
			}
			fragStart = l.pos
			continue
		}
		if c == '\n' {
			return Token{Kind: TInterpolated, Start: fragStart, End: l.pos, Text: string(l.src[fragStart:l.pos]), Line: l.lineFor(fragStart)}, true
		}
		l.pos++
	}
	return Token{Kind: TInterpolated, Start: fragStart, End: l.pos, Text: string(l.src[fragStart:l.pos]), Line: l.lineFor(fragStart)}, true
}

// interpolatedVerbatim handles $@"...{expr}..." — verbatim + interpolation.
func (l *lexer) interpolatedVerbatim(start int) (Token, bool) {
	l.pos += 3 // skip $@"
	for l.pos < len(l.src) {
		if l.src[l.pos] == '"' {
			if l.pos+1 < len(l.src) && l.src[l.pos+1] == '"' {
				l.pos += 2
				continue
			}
			l.pos++
			return l.emit(TInterpolated, start), true
		}
		l.pos++
	}
	return l.emit(TInterpolated, start), true
}

// interpolatedRaw handles $"""..."""
func (l *lexer) interpolatedRaw(start int) (Token, bool) {
	l.pos++                           // $
	return l.rawString(l.pos - 1 + 1) // close-enough; whole token range tracked via start
}

func (l *lexer) charLiteral(start int) (Token, bool) {
	l.pos++
	for l.pos < len(l.src) {
		c := l.src[l.pos]
		if c == '\\' && l.pos+1 < len(l.src) {
			l.pos += 2
			continue
		}
		if c == '\'' {
			l.pos++
			return l.emit(TChar, start), true
		}
		if c == '\n' {
			return l.emit(TChar, start), true
		}
		l.pos++
	}
	return l.emit(TChar, start), true
}

func (l *lexer) number(start int) (Token, bool) {
	if l.src[l.pos] == '0' && l.pos+1 < len(l.src) {
		switch l.src[l.pos+1] {
		case 'x', 'X':
			l.pos += 2
			for l.pos < len(l.src) && (isHex(l.src[l.pos]) || l.src[l.pos] == '_') {
				l.pos++
			}
			l.consumeSuffix()
			return l.emit(TInt, start), true
		case 'b', 'B':
			l.pos += 2
			for l.pos < len(l.src) && (l.src[l.pos] == '0' || l.src[l.pos] == '1' || l.src[l.pos] == '_') {
				l.pos++
			}
			l.consumeSuffix()
			return l.emit(TInt, start), true
		}
	}
	for l.pos < len(l.src) && (isDigit(l.src[l.pos]) || l.src[l.pos] == '_') {
		l.pos++
	}
	kind := TInt
	if l.pos < len(l.src) && l.src[l.pos] == '.' && l.peek(1) != '.' {
		kind = TFloat
		l.pos++
		for l.pos < len(l.src) && (isDigit(l.src[l.pos]) || l.src[l.pos] == '_') {
			l.pos++
		}
	}
	if l.pos < len(l.src) && (l.src[l.pos] == 'e' || l.src[l.pos] == 'E') {
		kind = TFloat
		l.pos++
		if l.pos < len(l.src) && (l.src[l.pos] == '+' || l.src[l.pos] == '-') {
			l.pos++
		}
		for l.pos < len(l.src) && (isDigit(l.src[l.pos]) || l.src[l.pos] == '_') {
			l.pos++
		}
	}
	l.consumeSuffix()
	return l.emit(kind, start), true
}

func (l *lexer) consumeSuffix() {
	// f, d, m for float/double/decimal; u/l/ul/lu for integer.
	for l.pos < len(l.src) {
		c := l.src[l.pos]
		if c == 'f' || c == 'F' || c == 'd' || c == 'D' || c == 'm' || c == 'M' ||
			c == 'u' || c == 'U' || c == 'l' || c == 'L' {
			l.pos++
			continue
		}
		break
	}
}

func (l *lexer) identifier(start int) (Token, bool) {
	for l.pos < len(l.src) {
		r, w := utf8.DecodeRune(l.src[l.pos:])
		if !isIdentContinue(r) {
			break
		}
		l.pos += w
	}
	return l.emit(TIdent, start), true
}

func (l *lexer) operatorOrPunct(start int) (Token, bool) {
	rest := l.src[l.pos:]
	for _, op := range multiCharOps {
		if hasPrefixBytes(rest, op) {
			l.pos += len(op)
			return l.emit(TOp, start), true
		}
	}
	c := l.src[l.pos]
	l.pos++
	if isOpByte(c) {
		return l.emit(TOp, start), true
	}
	return l.emit(TPunct, start), true
}

var multiCharOps = [][]byte{
	[]byte("<<="), []byte(">>="), []byte("..."),
	[]byte("=="), []byte("!="), []byte("<="), []byte(">="), []byte("&&"),
	[]byte("||"), []byte("++"), []byte("--"), []byte("+="), []byte("-="),
	[]byte("*="), []byte("/="), []byte("%="), []byte("&="), []byte("|="),
	[]byte("^="), []byte("<<"), []byte(">>"), []byte("??"), []byte("??="),
	[]byte("?."), []byte("=>"), []byte("->"), []byte(".."), []byte("::"),
}

func isOpByte(b byte) bool {
	switch b {
	case '=', '+', '-', '*', '/', '%', '&', '|', '^', '!', '<', '>', '~':
		return true
	}
	return false
}

func hasPrefixBytes(s, p []byte) bool {
	if len(s) < len(p) {
		return false
	}
	for i := range p {
		if s[i] != p[i] {
			return false
		}
	}
	return true
}

func (l *lexer) emit(kind TokenKind, start int) Token {
	return Token{Kind: kind, Start: start, End: l.pos, Text: string(l.src[start:l.pos]), Line: l.lineFor(start)}
}

func isDigit(c byte) bool { return c >= '0' && c <= '9' }
func isHex(c byte) bool {
	return isDigit(c) || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')
}
func isIdentStart(r rune) bool { return r == '_' || isLetter(r) }
func isIdentContinue(r rune) bool {
	return isIdentStart(r) || (r >= '0' && r <= '9')
}
func isLetter(r rune) bool {
	if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') {
		return true
	}
	return r >= 0x80
}

func IsValidIdentifier(s string) bool {
	if s == "" {
		return false
	}
	// Allow leading @ to escape keywords.
	start := 0
	if s[0] == '@' && len(s) > 1 {
		start = 1
	}
	for i, r := range s[start:] {
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
	return true
}
