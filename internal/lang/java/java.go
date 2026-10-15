package java

import "unicode"

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
