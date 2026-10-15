package unified

type tokenContext int

const (
	ctxUnknown tokenContext = iota
	ctxType
	ctxCall
	ctxField
	ctxLocalDecl
	ctxDecl
)

func classifyToken(tokens interface {
	Len() int
	Get(int) tokenInfo
}, i int) tokenContext {
	if i < 0 || i >= tokens.Len() {
		return ctxUnknown
	}

	next := nextNonCommentInfo(tokens, i+1)
	prev := prevNonCommentInfo(tokens, i-1)

	if next != nil && next.text == "(" {
		if prev != nil && isDeclKeywordGeneric(prev.text) {
			return ctxDecl
		}
		return ctxCall
	}

	if prev != nil && prev.text == "." {
		if next != nil && next.text == "(" {
			return ctxCall
		}
		return ctxField
	}

	if prev != nil && prev.text == "new" {
		return ctxType
	}

	if prev != nil && (prev.text == "extends" || prev.text == "implements" || prev.text == ":" || prev.text == "<" || prev.text == ",") {
		return ctxType
	}

	if next != nil && next.isIdent && !next.isKeyword {
		return ctxType
	}

	if prev != nil && prev.text == ":=" {
		return ctxLocalDecl
	}

	if prev != nil && prev.text == "=" {
		return ctxLocalDecl
	}

	return ctxUnknown
}

type tokenInfo struct {
	text      string
	isIdent   bool
	isKeyword bool
	isComment bool
	start     int
	end       int
}

type tokenList struct {
	infos []tokenInfo
}

func (tl *tokenList) Len() int            { return len(tl.infos) }
func (tl *tokenList) Get(i int) tokenInfo { return tl.infos[i] }

func nextNonCommentInfo(tokens interface {
	Len() int
	Get(int) tokenInfo
}, from int) *tokenInfo {
	for from < tokens.Len() {
		t := tokens.Get(from)
		if !t.isComment {
			return &t
		}
		from++
	}
	return nil
}

func prevNonCommentInfo(tokens interface {
	Len() int
	Get(int) tokenInfo
}, from int) *tokenInfo {
	for from >= 0 {
		t := tokens.Get(from)
		if !t.isComment {
			return &t
		}
		from--
	}
	return nil
}

func isDeclKeywordGeneric(s string) bool {
	switch s {
	case "class", "interface", "struct", "enum", "type", "func",
		"fun", "function", "object", "typealias", "record":
		return true
	}
	return false
}
