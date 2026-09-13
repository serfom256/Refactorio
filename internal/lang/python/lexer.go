// Package python provides Python 3 lexing and symbol extraction.
package python

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
	TTripleStr    TokenKind = common.TLangSpecific + iota // """...""" or '''...'''
	TInterpolated                                         // f"..." (with or without {expr})
	TNewline                                              // logical newline (for indent tracking)
)

type Token struct {
	Kind   TokenKind
	Start  int
	End    int
	Text   string
	Line   int
	Column int // 1-based column of Start within its line
}

func (t Token) IsKeyword() bool {
	if t.Kind != TIdent {
		return false
	}
	_, ok := pyKeywords[t.Text]
	return ok
}

var pyKeywords = map[string]struct{}{
	"False": {}, "None": {}, "True": {}, "and": {}, "as": {},
	"assert": {}, "async": {}, "await": {}, "break": {}, "class": {},
	"continue": {}, "def": {}, "del": {}, "elif": {}, "else": {},
	"except": {}, "finally": {}, "for": {}, "from": {}, "global": {},
	"if": {}, "import": {}, "in": {}, "is": {}, "lambda": {},
	"nonlocal": {}, "not": {}, "or": {}, "pass": {}, "raise": {},
	"return": {}, "try": {}, "while": {}, "with": {}, "yield": {},
	// soft / contextual
	"match": {}, "case": {}, "type": {},
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
		l.skipNonNewlineWS()
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

func (l *lexer) columnFor(off int) int {
	line := l.lineFor(off)
	return off - l.lineStart[line-1] + 1
}

// skipNonNewlineWS skips spaces, tabs, and \r but NOT \n. Newlines
// are emitted as TNewline so the symbol extractor can use them
// (combined with column info) for indentation tracking.
func (l *lexer) skipNonNewlineWS() {
	for l.pos < len(l.src) {
		c := l.src[l.pos]
		if c == ' ' || c == '\t' || c == '\r' || c == '\f' {
			l.pos++
			continue
		}
		// Backslash + newline = explicit line continuation; skip both.
		if c == '\\' && l.pos+1 < len(l.src) && l.src[l.pos+1] == '\n' {
			l.pos += 2
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

	if c == '\n' {
		l.pos++
		return l.emit(TNewline, start), true
	}
	if c == '#' {
		return l.lineComment(start)
	}
	// String prefixes: r, R, b, B, f, F, u, U and combinations rb, br, fb, etc.
	if isStringPrefix(c) && l.isStringStart(1) {
		return l.prefixedString(start, prev)
	}
	if (l.peek(0) == 'r' || l.peek(0) == 'R') && l.peek(1) != 0 && isStringPrefix(l.peek(1)) && l.isStringStart(2) {
		return l.prefixedString(start, prev)
	}

	// Triple-quoted strings.
	if c == '"' && l.peek(1) == '"' && l.peek(2) == '"' {
		return l.tripleString(start, '"', false, prev)
	}
	if c == '\'' && l.peek(1) == '\'' && l.peek(2) == '\'' {
		return l.tripleString(start, '\'', false, prev)
	}
	if c == '"' || c == '\'' {
		return l.singleString(start, c, false, false, prev)
	}
	if isDigit(c) {
		return l.number(start)
	}
	if c == '.' && l.pos+1 < len(l.src) && isDigit(l.src[l.pos+1]) {
		return l.number(start)
	}
	if isIdentStart(rune(c)) || c >= 0x80 {
		return l.identifier(start)
	}
	return l.operatorOrPunct(start)
}

// isStringPrefix reports whether the byte is a recognised string
// literal prefix character (r/R/b/B/f/F/u/U).
func isStringPrefix(b byte) bool {
	switch b {
	case 'r', 'R', 'b', 'B', 'f', 'F', 'u', 'U':
		return true
	}
	return false
}

// isStringStart reports whether the byte at pos+off is " or '.
func (l *lexer) isStringStart(off int) bool {
	c := l.peek(off)
	return c == '"' || c == '\''
}

// prefixedString handles r"...", b"...", f"..." and combinations like
// rb"..." or fr"...". It dispatches to single- or triple-quoted logic.
func (l *lexer) prefixedString(start int, prev *[]Token) (Token, bool) {
	prefixStart := l.pos
	isRaw, isFStr := false, false
	for isStringPrefix(l.peek(0)) {
		c := l.src[l.pos]
		switch c {
		case 'r', 'R':
			isRaw = true
		case 'f', 'F':
			isFStr = true
		}
		l.pos++
	}
	if l.pos >= len(l.src) {
		l.pos = prefixStart
		return l.identifier(start)
	}
	quote := l.src[l.pos]
	if quote != '"' && quote != '\'' {
		// Wasn't actually a string prefix — treat as identifier.
		l.pos = prefixStart
		return l.identifier(start)
	}
	if l.peek(1) == quote && l.peek(2) == quote {
		return l.tripleString(start, quote, isFStr, prev)
	}
	return l.singleString(start, quote, isRaw, isFStr, prev)
}

func (l *lexer) lineComment(start int) (Token, bool) {
	for l.pos < len(l.src) && l.src[l.pos] != '\n' {
		l.pos++
	}
	return l.emit(TComment, start), true
}

func (l *lexer) singleString(start int, quote byte, isRaw, isFStr bool, prev *[]Token) (Token, bool) {
	// Skip the opening quote (after any prefix already consumed).
	if l.src[l.pos] != quote {
		// Skip prefix to the quote.
		for l.pos < len(l.src) && l.src[l.pos] != quote {
			l.pos++
		}
	}
	if l.pos >= len(l.src) {
		return l.emit(TString, start), true
	}
	l.pos++ // opening quote

	fragStart := start
	for l.pos < len(l.src) {
		c := l.src[l.pos]
		if !isRaw && c == '\\' && l.pos+1 < len(l.src) {
			l.pos += 2
			continue
		}
		if c == quote {
			l.pos++
			kind := TString
			if isFStr {
				kind = TInterpolated
			}
			return Token{Kind: kind, Start: fragStart, End: l.pos, Text: string(l.src[fragStart:l.pos]), Line: l.lineFor(fragStart), Column: l.columnFor(fragStart)}, true
		}
		if c == '\n' {
			// Unterminated single-line string.
			kind := TString
			if isFStr {
				kind = TInterpolated
			}
			return l.emit(kind, start), true
		}
		if isFStr {
			if c == '{' && l.peek(1) == '{' {
				l.pos += 2 // escaped {{
				continue
			}
			if c == '}' && l.peek(1) == '}' {
				l.pos += 2
				continue
			}
			if c == '{' {
				// Emit fragment, lex code until matching }.
				*prev = append(*prev, Token{Kind: TInterpolated, Start: fragStart, End: l.pos, Text: string(l.src[fragStart:l.pos]), Line: l.lineFor(fragStart), Column: l.columnFor(fragStart)})
				l.pos++
				depth := 1
				for depth > 0 && l.pos < len(l.src) {
					l.skipNonNewlineWS()
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
		}
		l.pos++
	}
	kind := TString
	if isFStr {
		kind = TInterpolated
	}
	return Token{Kind: kind, Start: fragStart, End: l.pos, Text: string(l.src[fragStart:l.pos]), Line: l.lineFor(fragStart), Column: l.columnFor(fragStart)}, true
}

func (l *lexer) tripleString(start int, quote byte, isFStr bool, prev *[]Token) (Token, bool) {
	// Advance to the opening triple if not already there.
	for l.pos < len(l.src) && l.src[l.pos] != quote {
		l.pos++
	}
	if l.pos+2 >= len(l.src) {
		return l.emit(TTripleStr, start), true
	}
	l.pos += 3 // skip opening triple
	for l.pos+2 < len(l.src) {
		if l.src[l.pos] == quote && l.src[l.pos+1] == quote && l.src[l.pos+2] == quote {
			l.pos += 3
			kind := TTripleStr
			if isFStr {
				kind = TInterpolated
			}
			return l.emit(kind, start), true
		}
		if l.src[l.pos] == '\\' && l.pos+1 < len(l.src) {
			l.pos += 2
			continue
		}
		// Triple-quoted f-strings can contain {expr} — same handling.
		if isFStr {
			c := l.src[l.pos]
			if c == '{' && l.peek(1) != '{' {
				// Lex inner expression.
				l.pos++
				depth := 1
				for depth > 0 && l.pos < len(l.src) {
					l.skipNonNewlineWS()
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
				continue
			}
		}
		l.pos++
	}
	l.pos = len(l.src)
	kind := TTripleStr
	if isFStr {
		kind = TInterpolated
	}
	return l.emit(kind, start), true
}

func (l *lexer) number(start int) (Token, bool) {
	if l.src[l.pos] == '0' && l.pos+1 < len(l.src) {
		switch l.src[l.pos+1] {
		case 'x', 'X':
			l.pos += 2
			for l.pos < len(l.src) && (isHex(l.src[l.pos]) || l.src[l.pos] == '_') {
				l.pos++
			}
			return l.emit(TInt, start), true
		case 'b', 'B':
			l.pos += 2
			for l.pos < len(l.src) && (l.src[l.pos] == '0' || l.src[l.pos] == '1' || l.src[l.pos] == '_') {
				l.pos++
			}
			return l.emit(TInt, start), true
		case 'o', 'O':
			l.pos += 2
			for l.pos < len(l.src) && ((l.src[l.pos] >= '0' && l.src[l.pos] <= '7') || l.src[l.pos] == '_') {
				l.pos++
			}
			return l.emit(TInt, start), true
		}
	}
	for l.pos < len(l.src) && (isDigit(l.src[l.pos]) || l.src[l.pos] == '_') {
		l.pos++
	}
	kind := TInt
	if l.pos < len(l.src) && l.src[l.pos] == '.' {
		// Don't consume `..` operator (very rare in Python but be safe).
		if l.peek(1) != '.' {
			kind = TFloat
			l.pos++
			for l.pos < len(l.src) && (isDigit(l.src[l.pos]) || l.src[l.pos] == '_') {
				l.pos++
			}
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
	if l.pos < len(l.src) && (l.src[l.pos] == 'j' || l.src[l.pos] == 'J') {
		l.pos++
	}
	return l.emit(kind, start), true
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
	[]byte("**="), []byte("//="), []byte(">>="), []byte("<<="),
	[]byte(":="), []byte("->"),
	[]byte("=="), []byte("!="), []byte("<="), []byte(">="),
	[]byte("+="), []byte("-="), []byte("*="), []byte("/="), []byte("%="),
	[]byte("&="), []byte("|="), []byte("^="), []byte("@="),
	[]byte("**"), []byte("//"), []byte("<<"), []byte(">>"),
}

func isOpByte(b byte) bool {
	switch b {
	case '=', '+', '-', '*', '/', '%', '&', '|', '^', '!', '<', '>', '~', '@':
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
	return Token{
		Kind:   kind,
		Start:  start,
		End:    l.pos,
		Text:   string(l.src[start:l.pos]),
		Line:   l.lineFor(start),
		Column: l.columnFor(start),
	}
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
