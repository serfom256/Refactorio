package golang

import "testing"

func TestTokenize_Identifiers(t *testing.T) {
	tokens := Tokenize([]byte("package main\nfunc Hello() string { return \"hi\" }"))
	idents := filterKind(tokens, TIdent)
	want := []string{"package", "main", "func", "Hello", "string", "return"}
	if len(idents) != len(want) {
		t.Fatalf("got %d idents: %v", len(idents), texts(idents))
	}
	for i, w := range want {
		if idents[i].Text != w {
			t.Errorf("[%d] = %q, want %q", i, idents[i].Text, w)
		}
	}
}

func TestTokenize_StringNotIdent(t *testing.T) {
	tokens := Tokenize([]byte(`var s = "fizzBuzz"`))
	for _, tok := range tokens {
		if tok.Kind == TIdent && tok.Text == "fizzBuzz" {
			t.Errorf("fizzBuzz inside string should not be identifier")
		}
	}
}

func TestTokenize_RawString(t *testing.T) {
	tokens := Tokenize([]byte("var s = `fizzBuzz\nmultiline`"))
	for _, tok := range tokens {
		if tok.Kind == TIdent && tok.Text == "fizzBuzz" {
			t.Errorf("fizzBuzz inside raw string should not be identifier")
		}
	}
	raws := filterKind(tokens, TRawStr)
	if len(raws) != 1 {
		t.Errorf("raw strings: %v", texts(raws))
	}
}

func TestTokenize_RuneLiteral(t *testing.T) {
	tokens := Tokenize([]byte(`var c = '\n'`))
	runes := filterKind(tokens, TRune)
	if len(runes) != 1 || runes[0].Text != `'\n'` {
		t.Errorf("rune: %v", texts(runes))
	}
}

func TestTokenize_Comment(t *testing.T) {
	tokens := Tokenize([]byte("// fizzBuzz\n/* fizzBuzz */\nvar x = 1"))
	for _, tok := range tokens {
		if tok.Kind == TIdent && tok.Text == "fizzBuzz" {
			t.Errorf("fizzBuzz inside comment should not be identifier")
		}
	}
}

func TestTokenize_ChannelOp(t *testing.T) {
	tokens := Tokenize([]byte("ch <- 1"))
	ops := filterKind(tokens, TOp)
	found := false
	for _, op := range ops {
		if op.Text == "<-" {
			found = true
		}
	}
	if !found {
		t.Errorf("<- not found: %v", texts(ops))
	}
}

func TestTokenize_ShortVarDecl(t *testing.T) {
	tokens := Tokenize([]byte("x := 1"))
	ops := filterKind(tokens, TOp)
	found := false
	for _, op := range ops {
		if op.Text == ":=" {
			found = true
		}
	}
	if !found {
		t.Errorf(":= not found: %v", texts(ops))
	}
}

func TestTokenize_ByteRanges(t *testing.T) {
	src := []byte("func Foo() {}")
	tokens := Tokenize(src)
	for _, tok := range tokens {
		if string(src[tok.Start:tok.End]) != tok.Text {
			t.Errorf("[%d:%d] = %q, Text = %q", tok.Start, tok.End, src[tok.Start:tok.End], tok.Text)
		}
	}
}

func TestTokenize_Imaginary(t *testing.T) {
	tokens := Tokenize([]byte("var c = 1.5i"))
	floats := filterKind(tokens, TFloat)
	if len(floats) != 1 || floats[0].Text != "1.5i" {
		t.Errorf("imaginary: %v", texts(floats))
	}
}

func TestIsValidIdentifier(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{"", false},
		{"foo", true},
		{"_foo", true},
		{"foo123", true},
		{"123foo", false},
		{"foo-bar", false},
		{"foo bar", false},
		{"$foo", false}, // $ not valid in Go
	}
	for _, tc := range cases {
		if got := IsValidIdentifier(tc.in); got != tc.want {
			t.Errorf("IsValidIdentifier(%q) = %v, want %v", tc.in, got, tc.want)
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
