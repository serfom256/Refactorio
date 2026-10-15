package cleancomments

import (
	"path/filepath"
	"strings"

	"github.com/serfom256/refactorio/internal/lang/csharp"
	"github.com/serfom256/refactorio/internal/lang/golang"
	"github.com/serfom256/refactorio/internal/lang/java"
	"github.com/serfom256/refactorio/internal/lang/kotlin"
	"github.com/serfom256/refactorio/internal/lang/python"
	"github.com/serfom256/refactorio/internal/lang/rust"
	"github.com/serfom256/refactorio/internal/lang/typescript"
)

type class int

const (
	classOther class = iota
	classComment
	classString
)

type tok struct {
	Class class
	Start int
	End   int
	Text  string
	Line  int
}

func Lang(path string) string {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".java":
		return "java"
	case ".kt", ".kts":
		return "kotlin"
	case ".go":
		return "go"
	case ".ts", ".tsx", ".mts", ".cts":
		return "ts"
	case ".js", ".jsx", ".mjs", ".cjs":
		return "js"
	case ".rs":
		return "rust"
	case ".cs":
		return "csharp"
	case ".py":
		return "python"
	}
	return ""
}

func tokenize(path string, src []byte) ([]tok, string) {
	lang := Lang(path)
	var out []tok
	switch lang {
	case "java":
		for _, t := range java.Tokenize(src) {
			out = append(out, tok{classOf(t.Kind == java.TComment, t.Kind == java.TString), t.Start, t.End, t.Text, t.Line})
		}
	case "kotlin":
		for _, t := range kotlin.Tokenize(src) {
			out = append(out, tok{classOf(t.Kind == kotlin.TComment, t.Kind == kotlin.TString), t.Start, t.End, t.Text, t.Line})
		}
	case "go":
		for _, t := range golang.Tokenize(src) {
			out = append(out, tok{classOf(t.Kind == golang.TComment, t.Kind == golang.TString), t.Start, t.End, t.Text, t.Line})
		}
	case "ts", "js":
		for _, t := range typescript.Tokenize(src) {
			out = append(out, tok{classOf(t.Kind == typescript.TComment, t.Kind == typescript.TString), t.Start, t.End, t.Text, t.Line})
		}
	case "rust":
		for _, t := range rust.Tokenize(src) {
			out = append(out, tok{classOf(t.Kind == rust.TComment, t.Kind == rust.TString), t.Start, t.End, t.Text, t.Line})
		}
	case "csharp":
		for _, t := range csharp.Tokenize(src) {
			out = append(out, tok{classOf(t.Kind == csharp.TComment, t.Kind == csharp.TString), t.Start, t.End, t.Text, t.Line})
		}
	case "python":
		for _, t := range python.Tokenize(src) {
			isStr := t.Kind == python.TString || t.Kind == python.TTripleStr
			out = append(out, tok{classOf(t.Kind == python.TComment, isStr), t.Start, t.End, t.Text, t.Line})
		}
	default:
		return nil, ""
	}
	return out, lang
}

func classOf(isComment, isString bool) class {
	switch {
	case isComment:
		return classComment
	case isString:
		return classString
	default:
		return classOther
	}
}
