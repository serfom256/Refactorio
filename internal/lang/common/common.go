// Package common defines types shared across all language backends.
package common

// Range is a byte range into source. Start is inclusive, End is
// exclusive. Line is the 1-based line of Start.
type Range struct {
	Start int
	End   int
	Line  int
}

// TokenKind classifies a lexical element. The common constants below
// are shared by all languages. Each language extends with its own
// constants starting from TLangSpecific.
type TokenKind int

const (
	TIdent   TokenKind = iota // identifier (may be keyword)
	TPunct                    // single-char punctuation
	TOp                       // operator
	TInt                      // integer literal
	TFloat                    // float literal
	TString                   // string literal
	TComment                  // comment
	TEOF                      // end of input

	// TLangSpecific is the first value available for language-specific
	// token kinds. Each language does:
	//   const ( TFoo = common.TLangSpecific + iota; TBar; ... )
	TLangSpecific
)
