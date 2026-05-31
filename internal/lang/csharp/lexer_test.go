package csharp

import "testing"

func TestTokenize_Identifiers(t *testing.T) {
	tokens := Tokenize([]byte("public class Foo : Bar { }"))
	idents := filterKind(tokens, TIdent)
	want := []string{"public", "class", "Foo", "Bar"}
	if len(idents) != len(want) {
		t.Fatalf("got %v", texts(idents))
	}
}

func TestTokenize_StringNotIdent(t *testing.T) {
	tokens := Tokenize([]byte(`var s = "fizzBuzz";`))
	for _, tok := range tokens {
		if tok.Kind == TIdent && tok.Text == "fizzBuzz" {
			t.Errorf("fizzBuzz in string should not be ident")
		}
	}
}

func TestTokenize_VerbatimString(t *testing.T) {
	tokens := Tokenize([]byte(`var path = @"C:\fizzBuzz\path";`))
	for _, tok := range tokens {
		if tok.Kind == TIdent && tok.Text == "fizzBuzz" {
			t.Errorf("fizzBuzz in verbatim string should not be ident")
		}
	}
	verbs := filterKind(tokens, TVerbatim)
	if len(verbs) != 1 || verbs[0].Text != `@"C:\fizzBuzz\path"` {
		t.Errorf("verbatim: %v", texts(verbs))
	}
}

func TestTokenize_VerbatimEscapedQuote(t *testing.T) {
	tokens := Tokenize([]byte(`var s = @"He said ""hi""";`))
	verbs := filterKind(tokens, TVerbatim)
	if len(verbs) != 1 {
		t.Errorf("verbatim escape: %v", texts(verbs))
	}
}

func TestTokenize_InterpolatedString(t *testing.T) {
	src := []byte(`var name = "x"; var s = $"Hello {name} bye";`)
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

func TestTokenize_InterpolatedEscapedBrace(t *testing.T) {
	// {{ and }} are literal { and } inside $"..."
	tokens := Tokenize([]byte(`var s = $"literal {{ brace }}";`))
	interps := filterKind(tokens, TInterpolated)
	if len(interps) == 0 {
		t.Errorf("expected interpolated token")
	}
}

func TestTokenize_RawString(t *testing.T) {
	tokens := Tokenize([]byte(`var s = """has "quote" inside""";`))
	raws := filterKind(tokens, TRawStr)
	if len(raws) != 1 {
		t.Errorf("raw string: %v", texts(raws))
	}
}

func TestTokenize_CharLiteral(t *testing.T) {
	tokens := Tokenize([]byte(`char c = '\n'; char d = 'x';`))
	chars := filterKind(tokens, TChar)
	if len(chars) != 2 {
		t.Errorf("chars: %v", texts(chars))
	}
}

func TestTokenize_Comment(t *testing.T) {
	tokens := Tokenize([]byte("// fizzBuzz\n/* fizzBuzz */\nvar x = 1;"))
	for _, tok := range tokens {
		if tok.Kind == TIdent && tok.Text == "fizzBuzz" {
			t.Errorf("fizzBuzz in comment should not be ident")
		}
	}
}

func TestTokenize_NullConditional(t *testing.T) {
	tokens := Tokenize([]byte("var y = x?.Foo;"))
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

func TestTokenize_NullCoalescing(t *testing.T) {
	tokens := Tokenize([]byte("var y = x ?? 0;"))
	ops := filterKind(tokens, TOp)
	found := false
	for _, op := range ops {
		if op.Text == "??" {
			found = true
		}
	}
	if !found {
		t.Errorf("?? not found: %v", texts(ops))
	}
}

func TestTokenize_LambdaArrow(t *testing.T) {
	tokens := Tokenize([]byte("Func<int,int> f = x => x + 1;"))
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

func TestTokenize_NumericSuffix(t *testing.T) {
	tokens := Tokenize([]byte("var x = 1.5m; var y = 100L; var z = 0xFFu;"))
	all := filterKind(tokens, TInt)
	all = append(all, filterKind(tokens, TFloat)...)
	if len(all) != 3 {
		t.Errorf("numeric count = %d, want 3 (%v)", len(all), texts(all))
	}
}

func TestTokenize_AtIdentifier(t *testing.T) {
	// @class is an identifier (escape keyword)
	tokens := Tokenize([]byte("var @class = 1;"))
	idents := filterKind(tokens, TIdent)
	found := false
	for _, id := range idents {
		if id.Text == "@class" {
			found = true
		}
	}
	if !found {
		t.Errorf("@class not found: %v", texts(idents))
	}
}

func TestTokenize_ByteRanges(t *testing.T) {
	src := []byte("class Foo { }")
	tokens := Tokenize(src)
	for _, tok := range tokens {
		if string(src[tok.Start:tok.End]) != tok.Text {
			t.Errorf("[%d:%d] = %q, Text = %q", tok.Start, tok.End, src[tok.Start:tok.End], tok.Text)
		}
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
		{"@class", true}, // escaped keyword
		{"foo123", true},
		{"123foo", false},
		{"foo-bar", false},
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
