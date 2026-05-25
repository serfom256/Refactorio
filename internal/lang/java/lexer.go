package java

import (
	"unicode/utf8"

	"github.com/serfom256/refactorio/internal/lang/common"
)

// TokenKind is an alias for the shared token kind type.
type TokenKind = common.TokenKind

// Common token kinds re-exported for use as java.TIdent etc.
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

// Java-specific token kinds.
const (
	TTextBlock TokenKind = common.TLangSpecific + iota
	TChar
)

// Token is one lexical unit. Start/End are byte offsets into the source
// (End is exclusive). Line is the 1-based line containing Start.
type Token struct {
	Kind  TokenKind
	Start int
	End   int
	Text  string
	Line  int
}

// IsKeyword reports whether this identifier token is a Java reserved
// word. We tokenize keywords as TIdent and let consumers distinguish
// — that mirrors how we will scan ("did we just pass a `class`
// keyword?") and keeps the token kind set small.
func (t Token) IsKeyword() bool {
	if t.Kind != TIdent {
		return false
	}
	_, ok := javaKeywords[t.Text]
	return ok
}

// javaKeywords lists Java reserved words. `record`, `sealed`,
// `non-sealed`, `permits`, `yield`, `var` are contextual but listed
// because every parser we write will care about them as control tokens.
var javaKeywords = map[string]struct{}{
	"abstract": {}, "assert": {}, "boolean": {}, "break": {}, "byte": {},
	"case": {}, "catch": {}, "char": {}, "class": {}, "const": {},
	"continue": {}, "default": {}, "do": {}, "double": {}, "else": {},
	"enum": {}, "extends": {}, "final": {}, "finally": {}, "float": {},
	"for": {}, "goto": {}, "if": {}, "implements": {}, "import": {},
	"instanceof": {}, "int": {}, "interface": {}, "long": {}, "native": {},
	"new": {}, "package": {}, "private": {}, "protected": {}, "public": {},
	"return": {}, "short": {}, "static": {}, "strictfp": {}, "super": {},
	"switch": {}, "synchronized": {}, "this": {}, "throw": {}, "throws": {},
	"transient": {}, "try": {}, "void": {}, "volatile": {}, "while": {},
	"true": {}, "false": {}, "null": {},
	// contextual
	"record": {}, "sealed": {}, "permits": {}, "yield": {}, "var": {},
}

// Tokenize lexes Java source. Whitespace is dropped; everything else
// becomes a Token. Lex errors (unterminated string, etc.) are emitted
// as best-effort tokens covering whatever was scanned so far —
// downstream tools then see structural breakage but the indexer does
// not need to fail hard on a single malformed file.
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
			// Skip the byte and continue — unrecognised input.
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
	// Binary search for the largest lineStart[i] <= off; index+1 is line.
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

func (l *lexer) next() (Token, bool) {
	start := l.pos
	c := l.src[l.pos]

	switch {
	case c == '/' && l.peek(1) == '/':
		return l.lineComment(start)
	case c == '/' && l.peek(1) == '*':
		return l.blockComment(start)
	case c == '"' && l.peek(1) == '"' && l.peek(2) == '"':
		return l.textBlock(start)
	case c == '"':
		return l.stringLiteral(start)
	case c == '\'':
		return l.charLiteral(start)
	case isDigit(c):
		return l.number(start)
	case c == '.' && l.pos+1 < len(l.src) && isDigit(l.src[l.pos+1]):
		// .5, .25e3, etc.
		return l.number(start)
	case isIdentStart(rune(c)) || c >= 0x80:
		return l.identifier(start)
	default:
		return l.operatorOrPunct(start)
	}
}

func (l *lexer) peek(off int) byte {
	if l.pos+off >= len(l.src) {
		return 0
	}
	return l.src[l.pos+off]
}

func (l *lexer) lineComment(start int) (Token, bool) {
	for l.pos < len(l.src) && l.src[l.pos] != '\n' {
		l.pos++
	}
	return l.emit(TComment, start), true
}

func (l *lexer) blockComment(start int) (Token, bool) {
	l.pos += 2 // skip /*
	for l.pos+1 < len(l.src) {
		if l.src[l.pos] == '*' && l.src[l.pos+1] == '/' {
			l.pos += 2
			return l.emit(TComment, start), true
		}
		l.pos++
	}
	l.pos = len(l.src) // unterminated; consume rest
	return l.emit(TComment, start), true
}

func (l *lexer) stringLiteral(start int) (Token, bool) {
	l.pos++ // opening "
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
			// Unterminated string — close at the line break to keep
			// downstream scanners on track.
			return l.emit(TString, start), true
		}
		l.pos++
	}
	return l.emit(TString, start), true
}

// textBlock handles Java 15+ `"""..."""`. The opening triple-quote is
// followed by a line terminator (per spec), but we accept anything and
// scan until the closing `"""`. Escapes inside text blocks follow the
// same rules as strings; we only care about positions so we don't
// interpret them.
func (l *lexer) textBlock(start int) (Token, bool) {
	l.pos += 3 // skip """
	for l.pos+2 < len(l.src) {
		if l.src[l.pos] == '\\' && l.pos+1 < len(l.src) {
			l.pos += 2
			continue
		}
		if l.src[l.pos] == '"' && l.src[l.pos+1] == '"' && l.src[l.pos+2] == '"' {
			l.pos += 3
			return l.emit(TTextBlock, start), true
		}
		l.pos++
	}
	l.pos = len(l.src)
	return l.emit(TTextBlock, start), true
}

func (l *lexer) charLiteral(start int) (Token, bool) {
	l.pos++ // opening '
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
	// Hex/binary/octal prefix.
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
	// Decimal int / float.
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
	// Float / int suffix.
	if l.pos < len(l.src) {
		switch l.src[l.pos] {
		case 'f', 'F', 'd', 'D':
			kind = TFloat
			l.pos++
		case 'l', 'L':
			l.pos++
		}
	}
	return l.emit(kind, start), true
}

func (l *lexer) consumeIntSuffix() {
	if l.pos < len(l.src) && (l.src[l.pos] == 'l' || l.src[l.pos] == 'L') {
		l.pos++
	}
}

func (l *lexer) identifier(start int) (Token, bool) {
	// First rune already validated by the caller.
	for l.pos < len(l.src) {
		r, w := utf8.DecodeRune(l.src[l.pos:])
		if !isIdentContinue(r) {
			break
		}
		l.pos += w
	}
	return l.emit(TIdent, start), true
}

// operatorOrPunct emits multi-char operators where possible, otherwise
// single-char punctuation. We greedy-match against the longest
// known operator prefix.
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

// multiCharOps must be ordered longest first.
var multiCharOps = [][]byte{
	[]byte(">>>="), []byte("..."), []byte(">>>"), []byte("<<="), []byte(">>="),
	[]byte("=="), []byte("!="), []byte("<="), []byte(">="), []byte("&&"),
	[]byte("||"), []byte("++"), []byte("--"), []byte("+="), []byte("-="),
	[]byte("*="), []byte("/="), []byte("%="), []byte("&="), []byte("|="),
	[]byte("^="), []byte("<<"), []byte(">>"), []byte("->"), []byte("::"),
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
	end := l.pos
	return Token{
		Kind:  kind,
		Start: start,
		End:   end,
		Text:  string(l.src[start:end]),
		Line:  l.lineFor(start),
	}
}

func isDigit(c byte) bool { return c >= '0' && c <= '9' }

func isHex(c byte) bool {
	return isDigit(c) || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')
}

func isIdentStart(r rune) bool {
	if r == '_' || r == '$' {
		return true
	}
	return isLetter(r)
}

func isIdentContinue(r rune) bool {
	return isIdentStart(r) || (r >= '0' && r <= '9')
}

// isLetter is the JLS §3.8 "Java letter" approximation: ASCII letter
// or any rune outside ASCII. We do not consult the full Unicode letter
// tables here; identifiers containing exotic Unicode-category letters
// will still be accepted because we accept any non-ASCII rune.
func isLetter(r rune) bool {
	if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') {
		return true
	}
	return r >= 0x80
}
