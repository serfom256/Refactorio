package common

type Range struct {
	Start int
	End   int
	Line  int
}

type TokenKind int

const (
	TIdent TokenKind = iota
	TPunct
	TOp
	TInt
	TFloat
	TString
	TComment
	TEOF

	TLangSpecific
)
