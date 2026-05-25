package java

import (
	"testing"
)

func TestTokenize_Identifiers(t *testing.T) {
	tokens := Tokenize([]byte("public class Foo extends Bar"))
	idents := filterKind(tokens, TIdent)
	want := []string{"public", "class", "Foo", "extends", "Bar"}
	if len(idents) != len(want) {
		t.Fatalf("got %d idents, want %d: %v", len(idents), len(want), texts(idents))
	}
	for i, w := range want {
		if idents[i].Text != w {
			t.Errorf("[%d] = %q, want %q", i, idents[i].Text, w)
		}
	}
	if !idents[0].IsKeyword() {
		t.Errorf("public should be keyword")
	}
	if idents[2].IsKeyword() {
		t.Errorf("Foo should NOT be keyword")
	}
}

func TestTokenize_StringNotIdentifier(t *testing.T) {
	tokens := Tokenize([]byte(`String s = "fizzBuzz is overrated";`))
	for _, tok := range tokens {
		if tok.Kind == TIdent && tok.Text == "fizzBuzz" {
			t.Errorf("fizzBuzz inside string should not be an identifier token")
		}
	}
	strs := filterKind(tokens, TString)
	if len(strs) != 1 || strs[0].Text != `"fizzBuzz is overrated"` {
		t.Errorf("string token wrong: %v", strs)
	}
}

func TestTokenize_CommentNotIdentifier(t *testing.T) {
	tokens := Tokenize([]byte("// fizzBuzz line\n/* fizzBuzz block */\nint x;"))
	for _, tok := range tokens {
		if tok.Kind == TIdent && tok.Text == "fizzBuzz" {
			t.Errorf("fizzBuzz inside comment should not be an identifier")
		}
	}
	comments := filterKind(tokens, TComment)
	if len(comments) != 2 {
		t.Errorf("got %d comments, want 2", len(comments))
	}
}

func TestTokenize_TextBlock(t *testing.T) {
	src := "String s = \"\"\"\n    fizzBuzz\n    \"\"\";"
	tokens := Tokenize([]byte(src))
	for _, tok := range tokens {
		if tok.Kind == TIdent && tok.Text == "fizzBuzz" {
			t.Errorf("fizzBuzz inside text block should not be identifier")
		}
	}
	tbs := filterKind(tokens, TTextBlock)
	if len(tbs) != 1 {
		t.Errorf("text blocks: %v", tbs)
	}
}

func TestTokenize_CharLiteral(t *testing.T) {
	tokens := Tokenize([]byte(`char c = '\''; char d = 'x';`))
	chars := filterKind(tokens, TChar)
	if len(chars) != 2 {
		t.Errorf("char literals: %v", texts(chars))
	}
}

func TestTokenize_Numbers(t *testing.T) {
	tokens := Tokenize([]byte("int a = 0xFF; long b = 1_000L; double c = .5e3;"))
	ints := filterKind(tokens, TInt)
	floats := filterKind(tokens, TFloat)
	if len(ints) != 2 {
		t.Errorf("ints: %v", texts(ints))
	}
	if len(floats) != 1 || floats[0].Text != ".5e3" {
		t.Errorf("floats: %v", texts(floats))
	}
}

func TestTokenize_Punctuation(t *testing.T) {
	tokens := Tokenize([]byte("foo.bar();"))
	puncts := filterKind(tokens, TPunct)
	want := []string{".", "(", ")", ";"}
	if len(puncts) != len(want) {
		t.Fatalf("puncts = %v", texts(puncts))
	}
	for i, w := range want {
		if puncts[i].Text != w {
			t.Errorf("[%d] = %q, want %q", i, puncts[i].Text, w)
		}
	}
}

func TestTokenize_Operators(t *testing.T) {
	tokens := Tokenize([]byte("a >= b && c != d"))
	ops := filterKind(tokens, TOp)
	want := []string{">=", "&&", "!="}
	if len(ops) != len(want) {
		t.Fatalf("ops = %v", texts(ops))
	}
}

func TestTokenize_LineNumbers(t *testing.T) {
	tokens := Tokenize([]byte("a\nb\n\nc"))
	if tokens[0].Line != 1 || tokens[1].Line != 2 || tokens[2].Line != 4 {
		t.Errorf("lines: %d, %d, %d", tokens[0].Line, tokens[1].Line, tokens[2].Line)
	}
}

func TestTokenize_ByteRanges(t *testing.T) {
	src := []byte("class Foo {}")
	tokens := Tokenize(src)
	for _, tok := range tokens {
		if string(src[tok.Start:tok.End]) != tok.Text {
			t.Errorf("range [%d:%d] = %q, Text = %q", tok.Start, tok.End, src[tok.Start:tok.End], tok.Text)
		}
	}
}

func TestTokenize_EscapedString(t *testing.T) {
	tokens := Tokenize([]byte(`"hello \"world\""`))
	strs := filterKind(tokens, TString)
	if len(strs) != 1 || strs[0].Text != `"hello \"world\""` {
		t.Errorf("escaped string: %v", texts(strs))
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
