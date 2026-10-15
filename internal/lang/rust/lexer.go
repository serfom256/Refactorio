package rust

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
	TLifetime
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
	_, ok := rustKeywords[t.Text]
	return ok
}

var rustKeywords = map[string]struct{}{
	"as": {}, "async": {}, "await": {}, "break": {}, "const": {},
	"continue": {}, "crate": {}, "dyn": {}, "else": {}, "enum": {},
	"extern": {}, "false": {}, "fn": {}, "for": {}, "if": {},
	"impl": {}, "in": {}, "let": {}, "loop": {}, "match": {},
	"mod": {}, "move": {}, "mut": {}, "pub": {}, "ref": {},
	"return": {}, "self": {}, "Self": {}, "static": {}, "struct": {},
	"super": {}, "trait": {}, "true": {}, "type": {}, "unsafe": {},
	"use": {}, "where": {}, "while": {}, "yield": {},
	"abstract": {}, "become": {}, "box": {}, "do": {}, "final": {},
	"macro": {}, "override": {}, "priv": {}, "try": {}, "typeof": {},
	"unsized": {}, "virtual": {},
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
		t, ok := l.next()
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

func (l *lexer) next() (Token, bool) {
	start := l.pos
	c := l.src[l.pos]

	switch {
	case c == '/' && l.peek(1) == '/':
		return l.lineComment(start)
	case c == '/' && l.peek(1) == '*':
		return l.blockComment(start)
	case c == 'r' && (l.peek(1) == '"' || l.peek(1) == '#'):
		return l.rawString(start)
	case c == 'b' && l.peek(1) == '"':
		return l.byteString(start)
	case c == 'b' && l.peek(1) == '\'':
		l.pos++
		return l.charLiteral(start)
	case c == '"':
		return l.stringLiteral(start)
	case c == '\'' && l.isLifetime():
		return l.lifetime(start)
	case c == '\'':
		return l.charLiteral(start)
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

func (l *lexer) isLifetime() bool {
	if l.pos+1 >= len(l.src) {
		return false
	}
	next := l.src[l.pos+1]
	if !isIdentStart(rune(next)) {
		return false
	}
	i := l.pos + 2
	for i < len(l.src) && isIdentContinue(rune(l.src[i])) {
		i++
	}
	if i < len(l.src) && l.src[i] == '\'' {
		return false
	}
	return true
}

func (l *lexer) lifetime(start int) (Token, bool) {
	l.pos++
	for l.pos < len(l.src) && isIdentContinue(rune(l.src[l.pos])) {
		l.pos++
	}
	return l.emit(TLifetime, start), true
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
		l.pos++
	}
	return l.emit(TString, start), true
}

func (l *lexer) byteString(start int) (Token, bool) {
	l.pos++
	return l.stringLiteral(start)
}

func (l *lexer) rawString(start int) (Token, bool) {
	l.pos++
	hashes := 0
	for l.pos < len(l.src) && l.src[l.pos] == '#' {
		hashes++
		l.pos++
	}
	if l.pos >= len(l.src) || l.src[l.pos] != '"' {
		l.pos = start
		return l.identifier(start)
	}
	l.pos++
	for l.pos < len(l.src) {
		if l.src[l.pos] == '"' {
			match := true
			for j := 0; j < hashes; j++ {
				if l.pos+1+j >= len(l.src) || l.src[l.pos+1+j] != '#' {
					match = false
					break
				}
			}
			if match {
				l.pos += 1 + hashes
				return l.emit(TRawStr, start), true
			}
		}
		l.pos++
	}
	return l.emit(TRawStr, start), true
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
		case 'o', 'O':
			l.pos += 2
			for l.pos < len(l.src) && ((l.src[l.pos] >= '0' && l.src[l.pos] <= '7') || l.src[l.pos] == '_') {
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
	if l.pos < len(l.src) && (l.src[l.pos] == 'i' || l.src[l.pos] == 'u' || l.src[l.pos] == 'f') {
		for l.pos < len(l.src) && isIdentContinue(rune(l.src[l.pos])) {
			l.pos++
		}
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
	[]byte("<<="), []byte(">>="),
	[]byte("..="), []byte("..."),
	[]byte("=="), []byte("!="), []byte("<="), []byte(">="), []byte("&&"),
	[]byte("||"), []byte("+="), []byte("-="), []byte("*="), []byte("/="),
	[]byte("%="), []byte("&="), []byte("|="), []byte("^="), []byte("<<"),
	[]byte(">>"), []byte("::"), []byte("->"), []byte("=>"), []byte(".."),
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
