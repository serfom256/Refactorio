package python

import "testing"

func TestTokenize_Identifiers(t *testing.T) {
	tokens := Tokenize([]byte("def greet(name): return name"))
	idents := filterKind(tokens, TIdent)
	want := []string{"def", "greet", "name", "return", "name"}
	if len(idents) != len(want) {
		t.Fatalf("got %d: %v", len(idents), texts(idents))
	}
}

func TestTokenize_StringNotIdent(t *testing.T) {
	tokens := Tokenize([]byte(`x = "fizzBuzz"`))
	for _, tok := range tokens {
		if tok.Kind == TIdent && tok.Text == "fizzBuzz" {
			t.Errorf("fizzBuzz in string should not be ident")
		}
	}
}

func TestTokenize_SingleQuoteString(t *testing.T) {
	tokens := Tokenize([]byte(`x = 'fizzBuzz'`))
	for _, tok := range tokens {
		if tok.Kind == TIdent && tok.Text == "fizzBuzz" {
			t.Errorf("fizzBuzz in single-quote string should not be ident")
		}
	}
}

func TestTokenize_TripleQuotedString(t *testing.T) {
	src := []byte(`x = """
multi
line fizzBuzz
"""`)
	tokens := Tokenize(src)
	for _, tok := range tokens {
		if tok.Kind == TIdent && tok.Text == "fizzBuzz" {
			t.Errorf("fizzBuzz in triple string should not be ident")
		}
	}
	triples := filterKind(tokens, TTripleStr)
	if len(triples) != 1 {
		t.Errorf("triples: %v", texts(triples))
	}
}

func TestTokenize_TripleQuotedSingleString(t *testing.T) {
	src := []byte(`x = '''abc def'''`)
	tokens := Tokenize(src)
	triples := filterKind(tokens, TTripleStr)
	if len(triples) != 1 {
		t.Errorf("triples: %v", texts(triples))
	}
}

func TestTokenize_RawString(t *testing.T) {
	tokens := Tokenize([]byte(`x = r"\n no escape fizzBuzz"`))
	for _, tok := range tokens {
		if tok.Kind == TIdent && tok.Text == "fizzBuzz" {
			t.Errorf("fizzBuzz in raw string should not be ident")
		}
	}
}

func TestTokenize_ByteString(t *testing.T) {
	tokens := Tokenize([]byte(`x = b"fizzBuzz"`))
	for _, tok := range tokens {
		if tok.Kind == TIdent && tok.Text == "fizzBuzz" {
			t.Errorf("fizzBuzz in byte string should not be ident")
		}
	}
}

func TestTokenize_FString_NoInterpolation(t *testing.T) {
	tokens := Tokenize([]byte(`x = f"hello world"`))
	interps := filterKind(tokens, TInterpolated)
	if len(interps) == 0 {
		t.Errorf("expected f-string token")
	}
}

func TestTokenize_FString_WithInterpolation(t *testing.T) {
	src := []byte(`name = "world"
greeting = f"Hello {name}!"`)
	tokens := Tokenize(src)
	count := 0
	for _, tok := range tokens {
		if tok.Kind == TIdent && tok.Text == "name" {
			count++
		}
	}
	if count != 2 {
		t.Errorf("name count = %d, want 2 (decl + interp)", count)
	}
}

func TestTokenize_FString_EscapedBrace(t *testing.T) {
	tokens := Tokenize([]byte(`x = f"{{literal}}"`))
	interps := filterKind(tokens, TInterpolated)
	if len(interps) == 0 {
		t.Errorf("expected f-string")
	}
}

func TestTokenize_Comment(t *testing.T) {
	tokens := Tokenize([]byte("# fizzBuzz comment\nx = 1"))
	for _, tok := range tokens {
		if tok.Kind == TIdent && tok.Text == "fizzBuzz" {
			t.Errorf("fizzBuzz in comment should not be ident")
		}
	}
	comments := filterKind(tokens, TComment)
	if len(comments) != 1 {
		t.Errorf("comments: %v", texts(comments))
	}
}

func TestTokenize_Newline(t *testing.T) {
	tokens := Tokenize([]byte("a\nb\nc"))
	newlines := filterKind(tokens, TNewline)
	if len(newlines) != 2 {
		t.Errorf("newlines = %d, want 2", len(newlines))
	}
}

func TestTokenize_Column(t *testing.T) {
	tokens := Tokenize([]byte("    foo"))
	idents := filterKind(tokens, TIdent)
	if len(idents) != 1 || idents[0].Column != 5 {
		t.Errorf("column = %d, want 5 (1-based after 4 spaces)", idents[0].Column)
	}
}

func TestTokenize_WalrusOperator(t *testing.T) {
	tokens := Tokenize([]byte("if (n := 10) > 5: pass"))
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

func TestTokenize_PowerOp(t *testing.T) {
	tokens := Tokenize([]byte("x = 2 ** 10"))
	ops := filterKind(tokens, TOp)
	found := false
	for _, op := range ops {
		if op.Text == "**" {
			found = true
		}
	}
	if !found {
		t.Errorf("** not found: %v", texts(ops))
	}
}

func TestTokenize_FloorDiv(t *testing.T) {
	tokens := Tokenize([]byte("x = 10 // 3"))
	ops := filterKind(tokens, TOp)
	found := false
	for _, op := range ops {
		if op.Text == "//" {
			found = true
		}
	}
	if !found {
		t.Errorf("// not found: %v", texts(ops))
	}
}

func TestTokenize_Decorator(t *testing.T) {
	tokens := Tokenize([]byte("@property\ndef x(self): pass"))
	ops := filterKind(tokens, TOp)
	found := false
	for _, op := range ops {
		if op.Text == "@" {
			found = true
		}
	}
	if !found {
		t.Errorf("@ not found: %v", texts(ops))
	}
}

func TestTokenize_TypeHintArrow(t *testing.T) {
	tokens := Tokenize([]byte("def f(x: int) -> str: pass"))
	ops := filterKind(tokens, TOp)
	found := false
	for _, op := range ops {
		if op.Text == "->" {
			found = true
		}
	}
	if !found {
		t.Errorf("-> not found: %v", texts(ops))
	}
}

func TestTokenize_ByteRanges(t *testing.T) {
	src := []byte("class Foo: pass")
	tokens := Tokenize(src)
	for _, tok := range tokens {
		if string(src[tok.Start:tok.End]) != tok.Text {
			t.Errorf("[%d:%d] = %q, Text = %q", tok.Start, tok.End, src[tok.Start:tok.End], tok.Text)
		}
	}
}

func TestTokenize_LineContinuation(t *testing.T) {
	tokens := Tokenize([]byte("x = 1 + \\\n2"))
	newlines := filterKind(tokens, TNewline)
	if len(newlines) != 0 {
		t.Errorf("backslash-newline should be skipped: %v", texts(newlines))
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
		{"foo_bar", true},
		{"foo123", true},
		{"123foo", false},
		{"foo-bar", false},
		{"foo bar", false},
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
