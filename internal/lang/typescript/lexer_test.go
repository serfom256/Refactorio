package typescript

import (
	"testing"
)

func TestTokenize_Identifiers(t *testing.T) {
	tokens := Tokenize([]byte("export class Foo extends Bar {}"))
	idents := filterKind(tokens, TIdent)
	want := []string{"export", "class", "Foo", "extends", "Bar"}
	if len(idents) != len(want) {
		t.Fatalf("got %d idents: %v", len(idents), texts(idents))
	}
	for i, w := range want {
		if idents[i].Text != w {
			t.Errorf("[%d] = %q, want %q", i, idents[i].Text, w)
		}
	}
}

func TestTokenize_StringNotIdentifier(t *testing.T) {
	tokens := Tokenize([]byte(`const s = "fizzBuzz is overrated";`))
	for _, tok := range tokens {
		if tok.Kind == TIdent && tok.Text == "fizzBuzz" {
			t.Errorf("fizzBuzz inside string should not be identifier")
		}
	}
}

func TestTokenize_SingleQuoteString(t *testing.T) {
	tokens := Tokenize([]byte(`const s = 'fizzBuzz';`))
	for _, tok := range tokens {
		if tok.Kind == TIdent && tok.Text == "fizzBuzz" {
			t.Errorf("fizzBuzz inside single-quote string should not be identifier")
		}
	}
}

func TestTokenize_TemplateLiteral_NoSubstitution(t *testing.T) {
	tokens := Tokenize([]byte("const s = `hello world`;"))
	for _, tok := range tokens {
		if tok.Kind == TIdent && tok.Text == "hello" {
			t.Errorf("hello inside template should not be identifier")
		}
	}
}

func TestTokenize_TemplateLiteral_WithSubstitution(t *testing.T) {
	src := "const name = 'x'; const s = `Hello ${name} bye`;"
	tokens := Tokenize([]byte(src))
	nameCount := 0
	for _, tok := range tokens {
		if tok.Kind == TIdent && tok.Text == "name" {
			nameCount++
		}
	}
	if nameCount != 2 {
		t.Errorf("name TIdent count = %d, want 2 (decl + template subst)", nameCount)
	}
}

func TestTokenize_TemplateLiteral_NestedBraces(t *testing.T) {
	src := "const s = `${obj.x > 0 ? {a: 1} : {b: 2}}`;"
	tokens := Tokenize([]byte(src))
	var found bool
	for _, tok := range tokens {
		if tok.Kind == TIdent && tok.Text == "obj" {
			found = true
		}
	}
	if !found {
		t.Errorf("obj not found in template substitution tokens: %v", texts(filterKind(tokens, TIdent)))
	}
}

func TestTokenize_Comment(t *testing.T) {
	tokens := Tokenize([]byte("// fizzBuzz\n/* fizzBuzz */\nconst x = 1;"))
	for _, tok := range tokens {
		if tok.Kind == TIdent && tok.Text == "fizzBuzz" {
			t.Errorf("fizzBuzz inside comment should not be identifier")
		}
	}
}

func TestTokenize_Regex(t *testing.T) {
	tokens := Tokenize([]byte("const re = /pattern/gi;"))
	regexes := filterKind(tokens, TRegex)
	if len(regexes) != 1 || regexes[0].Text != "/pattern/gi" {
		t.Errorf("regex: %v", texts(regexes))
	}
}

func TestTokenize_DivisionNotRegex(t *testing.T) {
	tokens := Tokenize([]byte("const x = a / b;"))
	regexes := filterKind(tokens, TRegex)
	if len(regexes) != 0 {
		t.Errorf("division misidentified as regex: %v", texts(regexes))
	}
}

func TestTokenize_OptionalChaining(t *testing.T) {
	tokens := Tokenize([]byte("x?.foo"))
	ops := filterKind(tokens, TOp)
	found := false
	for _, op := range ops {
		if op.Text == "?." {
			found = true
		}
	}
	if !found {
		t.Errorf("?. not found in ops: %v", texts(ops))
	}
}

func TestTokenize_ArrowFunction(t *testing.T) {
	tokens := Tokenize([]byte("const f = (x) => x + 1;"))
	ops := filterKind(tokens, TOp)
	found := false
	for _, op := range ops {
		if op.Text == "=>" {
			found = true
		}
	}
	if !found {
		t.Errorf("=> not found: %v", texts(ops))
	}
}

func TestTokenize_PrivateField(t *testing.T) {
	tokens := Tokenize([]byte("class C { #name = 'x'; }"))
	idents := filterKind(tokens, TIdent)
	found := false
	for _, id := range idents {
		if id.Text == "name" {
			found = true
		}
	}
	if !found {
		t.Errorf("#name: 'name' should be a separate TIdent after '#' punct")
	}
}

func TestTokenize_BigInt(t *testing.T) {
	tokens := Tokenize([]byte("const x = 123n;"))
	ints := filterKind(tokens, TInt)
	if len(ints) != 1 || ints[0].Text != "123n" {
		t.Errorf("bigint: %v", texts(ints))
	}
}

func TestTokenize_ByteRanges(t *testing.T) {
	src := []byte("class Foo {}")
	tokens := Tokenize(src)
	for _, tok := range tokens {
		if string(src[tok.Start:tok.End]) != tok.Text {
			t.Errorf("range mismatch: [%d:%d] = %q, Text = %q", tok.Start, tok.End, src[tok.Start:tok.End], tok.Text)
		}
	}
}

func filterKind(tokens []Token, k TokenKind) []Token {
	var out []Token
	for _, t := range tokens {
		if t.Kind == k {
			out = append(out, t)
		}
	}
	return out
}

func texts(tokens []Token) []string {
	out := make([]string, len(tokens))
	for i, t := range tokens {
		out[i] = t.Text
	}
	return out
}
