package kotlin

import "testing"

func TestTokenize_Identifiers(t *testing.T) {
	tokens := Tokenize([]byte("fun greet(): String { return \"hi\" }"))
	idents := filterKind(tokens, TIdent)
	want := []string{"fun", "greet", "String", "return"}
	if len(idents) != len(want) {
		t.Fatalf("got %d idents: %v", len(idents), texts(idents))
	}
}

func TestTokenize_StringTemplate_Dollar(t *testing.T) {
	src := []byte(`val name = "world"; val s = "Hello $name bye"`)
	tokens := Tokenize(src)
	count := 0
	for _, tok := range tokens {
		if tok.Kind == TIdent && tok.Text == "name" {
			count++
		}
	}
	if count != 2 {
		t.Errorf("name count = %d, want 2 (decl + template $name)", count)
	}
}

func TestTokenize_StringTemplate_Braces(t *testing.T) {
	src := []byte(`val x = 1; val s = "val=${x + 1}"`)
	tokens := Tokenize(src)
	found := false
	for _, tok := range tokens {
		if tok.Kind == TIdent && tok.Text == "x" {
			found = true
		}
	}
	if !found {
		t.Errorf("x not found inside ${} template")
	}
}

func TestTokenize_RawStringTemplate(t *testing.T) {
	src := []byte("val name = \"x\"\nval s = \"\"\"Hello $name\"\"\"")
	tokens := Tokenize(src)
	count := 0
	for _, tok := range tokens {
		if tok.Kind == TIdent && tok.Text == "name" {
			count++
		}
	}
	if count != 2 {
		t.Errorf("name count = %d, want 2", count)
	}
}

func TestTokenize_StringNotIdent(t *testing.T) {
	tokens := Tokenize([]byte(`val s = "fizzBuzz"`))
	for _, tok := range tokens {
		if tok.Kind == TIdent && tok.Text == "fizzBuzz" {
			t.Errorf("fizzBuzz inside string should not be identifier")
		}
	}
}

func TestTokenize_Comment(t *testing.T) {
	tokens := Tokenize([]byte("// fizzBuzz\n/* fizzBuzz */\nval x = 1"))
	for _, tok := range tokens {
		if tok.Kind == TIdent && tok.Text == "fizzBuzz" {
			t.Errorf("fizzBuzz inside comment should not be identifier")
		}
	}
}

func TestTokenize_NestedBlockComment(t *testing.T) {
	tokens := Tokenize([]byte("/* outer /* inner */ still comment */ val x = 1"))
	for _, tok := range tokens {
		if tok.Kind == TIdent && tok.Text == "still" {
			t.Errorf("'still' inside nested comment should not be identifier")
		}
	}
}

func TestTokenize_SafeCall(t *testing.T) {
	tokens := Tokenize([]byte("x?.foo()"))
	ops := filterKind(tokens, TOp)
	found := false
	for _, op := range ops {
		if op.Text == "?." {
			found = true
		}
	}
	if !found {
		t.Errorf("?. not found: %v", texts(ops))
	}
}

func TestTokenize_Elvis(t *testing.T) {
	tokens := Tokenize([]byte("x ?: y"))
	ops := filterKind(tokens, TOp)
	found := false
	for _, op := range ops {
		if op.Text == "?:" {
			found = true
		}
	}
	if !found {
		t.Errorf("?: not found: %v", texts(ops))
	}
}

func TestTokenize_BacktickIdent(t *testing.T) {
	tokens := Tokenize([]byte("fun `is`() {}"))
	idents := filterKind(tokens, TIdent)
	found := false
	for _, id := range idents {
		if id.Text == "`is`" {
			found = true
		}
	}
	if !found {
		t.Errorf("backtick ident not found: %v", texts(idents))
	}
}

func TestTokenize_ByteRanges(t *testing.T) {
	src := []byte("class Foo {}")
	tokens := Tokenize(src)
	for _, tok := range tokens {
		if string(src[tok.Start:tok.End]) != tok.Text {
			t.Errorf("[%d:%d] = %q, Text = %q", tok.Start, tok.End, src[tok.Start:tok.End], tok.Text)
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
