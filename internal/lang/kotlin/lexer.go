package kotlin

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
	TRawStr TokenKind = common.TLangSpecific + iota
	TChar
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
	_, ok := ktKeywords[t.Text]
	return ok
}

var ktKeywords = map[string]struct{}{
	"as": {}, "break": {}, "class": {}, "continue": {}, "do": {},
	"else": {}, "false": {}, "for": {}, "fun": {}, "if": {},
	"in": {}, "interface": {}, "is": {}, "null": {}, "object": {},
	"package": {}, "return": {}, "super": {}, "this": {}, "throw": {},
	"true": {}, "try": {}, "typealias": {}, "typeof": {}, "val": {},
	"var": {}, "when": {}, "while": {},
	"by": {}, "catch": {}, "constructor": {}, "delegate": {},
	"dynamic": {}, "field": {}, "file": {}, "finally": {}, "get": {},
	"import": {}, "init": {}, "param": {}, "property": {}, "receiver": {},
	"set": {}, "setparam": {}, "where": {},
	"abstract": {}, "actual": {}, "annotation": {}, "companion": {},
	"const": {}, "crossinline": {}, "data": {}, "enum": {},
	"expect": {}, "external": {}, "final": {}, "infix": {}, "inline": {},
	"inner": {}, "internal": {}, "lateinit": {}, "noinline": {},
	"open": {}, "operator": {}, "out": {}, "override": {}, "private": {},
	"protected": {}, "public": {}, "reified": {}, "sealed": {},
	"suspend": {}, "tailrec": {}, "vararg": {},
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

func (l *lexer) next(prevTokens *[]Token) (Token, bool) {
	start := l.pos
	c := l.src[l.pos]

	switch {
	case c == '/' && l.peek(1) == '/':
		return l.lineComment(start)
	case c == '/' && l.peek(1) == '*':
		return l.blockComment(start)
	case c == '"' && l.peek(1) == '"' && l.peek(2) == '"':
		return l.rawString(start, prevTokens)
	case c == '"':
		return l.interpString(start, prevTokens)
	case c == '\'':
		return l.charLiteral(start)
	case c == '`':
		return l.backtickIdent(start)
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
	depth := 1
	for l.pos+1 < len(l.src) && depth > 0 {
		if l.src[l.pos] == '/' && l.src[l.pos+1] == '*' {
			depth++
			l.pos += 2
			continue
		}
		if l.src[l.pos] == '*' && l.src[l.pos+1] == '/' {
			depth--
			l.pos += 2
			continue
		}
		l.pos++
	}
	return l.emit(TComment, start), true
}

func (l *lexer) interpString(start int, prevTokens *[]Token) (Token, bool) {
	l.pos++
	fragStart := start
	for l.pos < len(l.src) {
		c := l.src[l.pos]
		if c == '\\' && l.pos+1 < len(l.src) {
			l.pos += 2
			continue
		}
		if c == '"' {
			l.pos++
			return Token{Kind: TString, Start: fragStart, End: l.pos, Text: string(l.src[fragStart:l.pos]), Line: l.lineFor(fragStart)}, true
		}
		if c == '$' && l.pos+1 < len(l.src) {
			next := l.src[l.pos+1]
			if next == '{' {
				*prevTokens = append(*prevTokens, Token{Kind: TString, Start: fragStart, End: l.pos, Text: string(l.src[fragStart:l.pos]), Line: l.lineFor(fragStart)})
				l.pos += 2
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
					inner, ok := l.next(prevTokens)
					if ok {
						*prevTokens = append(*prevTokens, inner)
					}
				}
				fragStart = l.pos
				continue
			}
			if isIdentStart(rune(next)) {
				*prevTokens = append(*prevTokens, Token{Kind: TString, Start: fragStart, End: l.pos, Text: string(l.src[fragStart:l.pos]), Line: l.lineFor(fragStart)})
				l.pos++
				idStart := l.pos
				for l.pos < len(l.src) {
					r, w := utf8.DecodeRune(l.src[l.pos:])
					if !isIdentContinue(r) {
						break
					}
					l.pos += w
				}
				*prevTokens = append(*prevTokens, Token{Kind: TIdent, Start: idStart, End: l.pos, Text: string(l.src[idStart:l.pos]), Line: l.lineFor(idStart)})
				fragStart = l.pos
				continue
			}
		}
		if c == '\n' {
			return Token{Kind: TString, Start: fragStart, End: l.pos, Text: string(l.src[fragStart:l.pos]), Line: l.lineFor(fragStart)}, true
		}
		l.pos++
	}
	return Token{Kind: TString, Start: fragStart, End: l.pos, Text: string(l.src[fragStart:l.pos]), Line: l.lineFor(fragStart)}, true
}

func (l *lexer) rawString(start int, prevTokens *[]Token) (Token, bool) {
	l.pos += 3
	fragStart := start
	for l.pos+2 < len(l.src) {
		c := l.src[l.pos]
		if c == '"' && l.src[l.pos+1] == '"' && l.src[l.pos+2] == '"' {
			l.pos += 3
			return Token{Kind: TRawStr, Start: fragStart, End: l.pos, Text: string(l.src[fragStart:l.pos]), Line: l.lineFor(fragStart)}, true
		}
		if c == '$' && l.pos+1 < len(l.src) {
			next := l.src[l.pos+1]
			if next == '{' {
				*prevTokens = append(*prevTokens, Token{Kind: TRawStr, Start: fragStart, End: l.pos, Text: string(l.src[fragStart:l.pos]), Line: l.lineFor(fragStart)})
				l.pos += 2
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
					inner, ok := l.next(prevTokens)
					if ok {
						*prevTokens = append(*prevTokens, inner)
					}
				}
				fragStart = l.pos
				continue
			}
			if isIdentStart(rune(next)) {
				*prevTokens = append(*prevTokens, Token{Kind: TRawStr, Start: fragStart, End: l.pos, Text: string(l.src[fragStart:l.pos]), Line: l.lineFor(fragStart)})
				l.pos++
				idStart := l.pos
				for l.pos < len(l.src) {
					r, w := utf8.DecodeRune(l.src[l.pos:])
					if !isIdentContinue(r) {
						break
					}
					l.pos += w
				}
				*prevTokens = append(*prevTokens, Token{Kind: TIdent, Start: idStart, End: l.pos, Text: string(l.src[idStart:l.pos]), Line: l.lineFor(idStart)})
				fragStart = l.pos
				continue
			}
		}
		l.pos++
	}
	l.pos = len(l.src)
	return Token{Kind: TRawStr, Start: fragStart, End: l.pos, Text: string(l.src[fragStart:l.pos]), Line: l.lineFor(fragStart)}, true
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

func (l *lexer) backtickIdent(start int) (Token, bool) {
	l.pos++
	for l.pos < len(l.src) && l.src[l.pos] != '`' && l.src[l.pos] != '\n' {
		l.pos++
	}
	if l.pos < len(l.src) && l.src[l.pos] == '`' {
		l.pos++
	}
	return l.emit(TIdent, start), true
}

func (l *lexer) number(start int) (Token, bool) {
	if l.src[l.pos] == '0' && l.pos+1 < len(l.src) {
		switch l.src[l.pos+1] {
		case 'x', 'X':
			l.pos += 2
			for l.pos < len(l.src) && (isHex(l.src[l.pos]) || l.src[l.pos] == '_') {
				l.pos++
			}
			l.consumeIntSuffix()
			return l.emit(TInt, start), true
		case 'b', 'B':
			l.pos += 2
			for l.pos < len(l.src) && (l.src[l.pos] == '0' || l.src[l.pos] == '1' || l.src[l.pos] == '_') {
				l.pos++
			}
			l.consumeIntSuffix()
			return l.emit(TInt, start), true
		}
	}
	for l.pos < len(l.src) && (isDigit(l.src[l.pos]) || l.src[l.pos] == '_') {
		l.pos++
	}
	kind := TInt
	if l.pos < len(l.src) && l.src[l.pos] == '.' {
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
	if l.pos < len(l.src) {
		switch l.src[l.pos] {
		case 'f', 'F':
			kind = TFloat
			l.pos++
		case 'L', 'u', 'U':
			l.consumeIntSuffix()
		}
	}
	return l.emit(kind, start), true
}

func (l *lexer) consumeIntSuffix() {
	for l.pos < len(l.src) && (l.src[l.pos] == 'L' || l.src[l.pos] == 'u' || l.src[l.pos] == 'U') {
		l.pos++
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
	[]byte("!=="), []byte("==="), []byte(".."),
	[]byte("=="), []byte("!="), []byte("<="), []byte(">="), []byte("&&"),
	[]byte("||"), []byte("++"), []byte("--"), []byte("+="), []byte("-="),
	[]byte("*="), []byte("/="), []byte("%="), []byte("?."), []byte("?:"),
	[]byte("!!"), []byte("->"), []byte("::"),
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
	return true
}
