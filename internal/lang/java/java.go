// Package java provides Java-specific parsing and validation for the
// refactoring ops. Parsing uses a hand-rolled lexer (lexer.go) and
// token-stream scanner (symbols.go) — no tree-sitter, no cgo.
package java

import "unicode"

// IsValidIdentifier follows JLS §3.8: first character must be a Java
// letter (Unicode letter, '_', or '$'); subsequent characters may also
// be digits.
func IsValidIdentifier(s string) bool {
	if s == "" {
		return false
	}
	for i, r := range s {
		if i == 0 {
			if !isJavaLetterUnicode(r) {
				return false
			}
			continue
		}
		if !isJavaLetterUnicode(r) && !unicode.IsDigit(r) {
			return false
		}
	}
	return true
}

func isJavaLetterUnicode(r rune) bool {
	return unicode.IsLetter(r) || r == '_' || r == '$'
}
