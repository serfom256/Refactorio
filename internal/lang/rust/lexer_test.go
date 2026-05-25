package rust

import "testing"

func TestTokenize_Identifiers(t *testing.T) {
	tokens := Tokenize([]byte("fn greet(name: &str) -> String { name.to_string() }"))
	idents := filterKind(tokens, TIdent)
	want := []string{"fn", "greet", "name", "str", "String", "name", "to_string"}
	if len(idents) != len(want) {
		t.Fatalf("got %d idents: %v", len(idents), texts(idents))
	}
}

func TestTokenize_StringNotIdent(t *testing.T) {
	tokens := Tokenize([]byte(`let s = "fizzBuzz";`))
	for _, tok := range tokens {
		if tok.Kind == TIdent && tok.Text == "fizzBuzz" {
			t.Errorf("fizzBuzz inside string should not be identifier")
		}
	}
}

func TestTokenize_RawString(t *testing.T) {
	tokens := Tokenize([]byte(`let s = r#"fizzBuzz"#;`))
	for _, tok := range tokens {
		if tok.Kind == TIdent && tok.Text == "fizzBuzz" {
			t.Errorf("fizzBuzz inside raw string should not be identifier")
		}
	}
	raws := filterKind(tokens, TRawStr)
	if len(raws) != 1 || raws[0].Text != `r#"fizzBuzz"#` {
		t.Errorf("raw string: %v", texts(raws))
	}
}

func TestTokenize_DoubleHashRawString(t *testing.T) {
	tokens := Tokenize([]byte(`let s = r##"has "# inside"##;`))
	raws := filterKind(tokens, TRawStr)
	if len(raws) != 1 {
		t.Errorf("raw strings: %v", texts(raws))
	}
}

func TestTokenize_CharLiteral(t *testing.T) {
	tokens := Tokenize([]byte(`let c = '\n'; let d = 'x';`))
	chars := filterKind(tokens, TChar)
	if len(chars) != 2 {
		t.Errorf("char literals: %v", texts(chars))
	}
}

func TestTokenize_Lifetime(t *testing.T) {
	tokens := Tokenize([]byte("fn foo<'a>(x: &'a str) -> &'a str { x }"))
	lifetimes := filterKind(tokens, TLifetime)
	if len(lifetimes) != 3 {
		t.Errorf("lifetimes = %d: %v", len(lifetimes), texts(lifetimes))
	}
	for _, lt := range lifetimes {
		if lt.Text != "'a" {
			t.Errorf("lifetime = %q, want 'a", lt.Text)
		}
	}
}

func TestTokenize_LifetimeVsChar(t *testing.T) {
	// 'a' is a char, 'a is a lifetime — must not confuse.
	tokens := Tokenize([]byte("let c = 'a'; fn f<'a>() {}"))
	chars := filterKind(tokens, TChar)
	lifetimes := filterKind(tokens, TLifetime)
	if len(chars) != 1 || chars[0].Text != "'a'" {
		t.Errorf("chars: %v", texts(chars))
	}
	if len(lifetimes) != 1 || lifetimes[0].Text != "'a" {
		t.Errorf("lifetimes: %v", texts(lifetimes))
	}
}

func TestTokenize_Comment(t *testing.T) {
	tokens := Tokenize([]byte("// fizzBuzz\n/* fizzBuzz */\nlet x = 1;"))
	for _, tok := range tokens {
		if tok.Kind == TIdent && tok.Text == "fizzBuzz" {
			t.Errorf("fizzBuzz inside comment should not be identifier")
		}
	}
}

func TestTokenize_NestedBlockComment(t *testing.T) {
	tokens := Tokenize([]byte("/* outer /* inner */ still */ let x = 1;"))
	for _, tok := range tokens {
		if tok.Kind == TIdent && tok.Text == "still" {
			t.Errorf("'still' inside nested comment should not be identifier")
		}
	}
}

func TestTokenize_PathSeparator(t *testing.T) {
	tokens := Tokenize([]byte("use std::io::Read;"))
	ops := filterKind(tokens, TOp)
	count := 0
	for _, op := range ops {
		if op.Text == "::" {
			count++
		}
	}
	if count != 2 {
		t.Errorf(":: count = %d, want 2: %v", count, texts(ops))
	}
}

func TestTokenize_Arrow(t *testing.T) {
	tokens := Tokenize([]byte("fn f() -> i32 { 0 }"))
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

func TestTokenize_FatArrow(t *testing.T) {
	tokens := Tokenize([]byte("match x { 1 => true, _ => false }"))
	ops := filterKind(tokens, TOp)
	count := 0
	for _, op := range ops {
		if op.Text == "=>" {
			count++
		}
	}
	if count != 2 {
		t.Errorf("=> count = %d, want 2", count)
	}
}

func TestTokenize_RangeOps(t *testing.T) {
	tokens := Tokenize([]byte("let r = 0..10; let s = 0..=10;"))
	ops := filterKind(tokens, TOp)
	foundRange := false
	foundRangeInc := false
	for _, op := range ops {
		if op.Text == ".." {
			foundRange = true
		}
		if op.Text == "..=" {
			foundRangeInc = true
		}
	}
	if !foundRange || !foundRangeInc {
		t.Errorf("range ops: %v", texts(ops))
	}
}

func TestTokenize_ByteRanges(t *testing.T) {
	src := []byte("struct Foo { x: i32 }")
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
		{"foo_bar", true},
		{"foo123", false}, // actually valid in Rust
		{"123foo", false},
		{"foo-bar", false},
	}
	// foo123 IS valid — fix the test case
	cases[4].want = true
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
