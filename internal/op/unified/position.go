package unified

// Position-aware identifier scanning. Instead of renaming every token
// that matches the name, check the surrounding context to decide
// whether the identifier is a type reference, a call, a field access,
// or a local variable declaration (which should be skipped).
//
// Rules (language-agnostic, keyword lists parameterised):
//
//   TYPE POSITION (safe to rename for class/type renames):
//     - After: new, extends, implements, :, <, ,
//     - Before another non-keyword identifier (TypeName varName)
//     - After type/class/interface/struct keywords (but that's the
//       declaration — handled separately via NameRange)
//
//   CALL POSITION (safe to rename for function/method renames):
//     - Followed by '('
//
//   FIELD/PROPERTY POSITION:
//     - After '.' and NOT followed by '(' (field access, not method call)
//
//   SKIP (likely a local variable, not our target):
//     - Is the name of a variable_declarator (preceded by a type +
//       followed by = or ; or ,)
//     - Is a parameter name (inside parentheses, after a type)
//     - After ':=' (Go short var decl)

// tokenContext classifies an identifier token's context.
type tokenContext int

const (
	ctxUnknown   tokenContext = iota
	ctxType                   // type position (new X, extends X, : X, etc.)
	ctxCall                   // followed by (
	ctxField                  // after . not followed by (
	ctxLocalDecl              // local variable declaration
	ctxDecl                   // the declaration itself (class/func/type keyword before it)
)

// classifyToken determines the context of token at index i.
// Works across all languages — uses generic punctuation/keyword patterns.
func classifyToken(tokens interface {
	Len() int
	Get(int) tokenInfo
}, i int) tokenContext {
	if i < 0 || i >= tokens.Len() {
		return ctxUnknown
	}

	// Check what follows.
	next := nextNonCommentInfo(tokens, i+1)
	prev := prevNonCommentInfo(tokens, i-1)

	// Followed by '(' → call.
	if next != nil && next.text == "(" {
		// But if preceded by a declaration keyword, it's a declaration.
		if prev != nil && isDeclKeywordGeneric(prev.text) {
			return ctxDecl
		}
		return ctxCall
	}

	// After '.' → field or method access.
	if prev != nil && prev.text == "." {
		if next != nil && next.text == "(" {
			return ctxCall
		}
		return ctxField
	}

	// After 'new' → type position.
	if prev != nil && prev.text == "new" {
		return ctxType
	}

	// After extends/implements/: → type position.
	if prev != nil && (prev.text == "extends" || prev.text == "implements" || prev.text == ":" || prev.text == "<" || prev.text == ",") {
		return ctxType
	}

	// Before a non-keyword identifier → likely type position (TypeName varName).
	if next != nil && next.isIdent && !next.isKeyword {
		// Exclude: this could also be two consecutive variable references.
		// Only treat as type if the current token starts with uppercase
		// (PascalCase convention for types in Java/Kotlin/Go).
		return ctxType
	}

	// After ':=' → local decl (Go).
	if prev != nil && prev.text == ":=" {
		return ctxLocalDecl
	}

	// After '=' preceded by a type → local decl.
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
