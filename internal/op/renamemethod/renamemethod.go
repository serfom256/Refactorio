package renamemethod

import (
	"fmt"
	"os"
	"strings"

	"github.com/serfom256/refactorio/internal/edit"
	"github.com/serfom256/refactorio/internal/index"
	"github.com/serfom256/refactorio/internal/lang/java"
)

type Request struct {
	ClassFQN string
	Method   string
	NewName  string
	Index    *index.Index
}

func Plan(req Request) (*edit.Set, error) {
	if req.Index == nil {
		return nil, fmt.Errorf("nil index")
	}
	if !java.IsValidIdentifier(req.NewName) {
		return nil, fmt.Errorf("%q is not a valid Java identifier", req.NewName)
	}
	if req.Method == req.NewName {
		return nil, fmt.Errorf("method is already named %q", req.NewName)
	}
	target := req.Index.LookupFQN(req.ClassFQN)
	if target == nil {
		return nil, fmt.Errorf("no class %q in project", req.ClassFQN)
	}
	var declMethods []java.MemberDecl
	for _, m := range target.Methods {
		if m.Kind == "method" && m.Name == req.Method {
			declMethods = append(declMethods, m)
		}
	}
	if len(declMethods) == 0 {
		return nil, fmt.Errorf("no method %s.%s found", req.ClassFQN, req.Method)
	}

	set := edit.NewSet()
	for _, m := range declMethods {
		set.Add(edit.Edit{
			File: target.File, Start: m.NameRange.Start, End: m.NameRange.End,
			NewText: req.NewName, Why: fmt.Sprintf("rename method %s.%s -> %s (declaration)", target.Name, req.Method, req.NewName),
		})
	}

	for _, path := range req.Index.FilesImporting(target.FQN) {
		fe := req.Index.File(path)
		if fe == nil || fe.Symbols == nil {
			continue
		}
		fileResolves := resolvesTarget(fe, target)
		hasStaticMember := hasStaticMemberImport(fe, target.FQN, req.Method)
		if !fileResolves && !hasStaticMember {
			continue
		}
		if err := planFile(fe, target, req.Method, req.NewName, fileResolves, hasStaticMember, set); err != nil {
			return nil, err
		}
	}

	// Rewrite static imports.
	for _, path := range req.Index.FilesImporting(target.FQN) {
		fe := req.Index.File(path)
		if fe == nil || fe.Symbols == nil {
			continue
		}
		for _, imp := range fe.Symbols.Imports {
			if !imp.IsStatic || imp.IsWildcard {
				continue
			}
			if imp.Path == target.FQN+"."+req.Method {
				set.Add(edit.Edit{
					File: fe.Path, Start: imp.PathRange.Start, End: imp.PathRange.End,
					NewText: target.FQN + "." + req.NewName,
					Why: fmt.Sprintf("update static import %s.%s -> %s", target.FQN, req.Method, req.NewName),
				})
			}
		}
	}

	return set, nil
}

func planFile(
	fe *index.FileEntry,
	target *index.TypeDecl,
	oldName, newName string,
	fileResolves, hasStaticMember bool,
	set *edit.Set,
) error {
	src, err := os.ReadFile(fe.Path)
	if err != nil {
		return err
	}
	tokens := java.Tokenize(src)

	varTypes := scanVarTypes(tokens)

	var targetBody *bodyRange
	if fe.Path == target.File {
		targetBody = &bodyRange{Start: target.BodyRange.Start, End: target.BodyRange.End}
	}

	// Scan for method call patterns: <receiver>.<name>( or bare <name>(
	for i, tok := range tokens {
		if tok.Kind != java.TIdent || tok.Text != oldName {
			continue
		}
		// Skip declaration edits (already emitted from target.Methods).
		if fe.Path == target.File && isDeclRange(tok, target) {
			continue
		}
		// Check if followed by '('
		next := nextNonComment(tokens, i+1)
		if next == nil || next.Text != "(" {
			continue
		}
		// Check for receiver: token before this one should be '.'
		prev := prevNonComment(tokens, i-1)
		if prev != nil && prev.Text == "." {
			// obj.method( or Class.method( or this.method(
			receiver := prevNonComment(tokens, indexOfToken(tokens, prev)-1)
			if receiver == nil {
				continue
			}
			conf, ok := classifyReceiver(receiver, target, varTypes, targetBody, fileResolves)
			if !ok {
				continue
			}
			set.Add(edit.Edit{
				File: fe.Path, Start: tok.Start, End: tok.End,
				NewText: newName, Why: fmt.Sprintf("rename call %s.%s -> %s [%s]", target.Name, oldName, newName, conf),
			})
		} else {
			// Bare call: method(
			if targetBody != nil && targetBody.contains(tok.Start) {
				set.Add(edit.Edit{
					File: fe.Path, Start: tok.Start, End: tok.End,
					NewText: newName, Why: fmt.Sprintf("rename call %s.%s -> %s [HIGH (self-reference)]", target.Name, oldName, newName),
				})
			} else if hasStaticMember {
				set.Add(edit.Edit{
					File: fe.Path, Start: tok.Start, End: tok.End,
					NewText: newName, Why: fmt.Sprintf("rename call %s.%s -> %s [HIGH (static-import)]", target.Name, oldName, newName),
				})
			}
		}
	}
	return nil
}

type bodyRange struct{ Start, End int }

func (r *bodyRange) contains(off int) bool {
	return r != nil && off >= r.Start && off < r.End
}

func isDeclRange(tok java.Token, target *index.TypeDecl) bool {
	for _, m := range target.Methods {
		if m.Kind == "method" && tok.Start == m.NameRange.Start {
			return true
		}
	}
	return false
}

func classifyReceiver(receiver *java.Token, target *index.TypeDecl, varTypes map[string]string, targetBody *bodyRange, fileResolves bool) (string, bool) {
	switch receiver.Text {
	case "this":
		if targetBody != nil && targetBody.contains(receiver.Start) {
			return "HIGH (this)", true
		}
		return "", false
	case "super":
		return "", false
	}
	if receiver.Kind == java.TIdent {
		if receiver.Text == target.Name && fileResolves {
			return "HIGH (static)", true
		}
		if t, ok := varTypes[receiver.Text]; ok {
			if t == target.Name {
				return "HIGH (receiver-typed)", true
			}
			return "", false
		}
		if fileResolves {
			return "MEDIUM (unknown receiver)", true
		}
	}
	if fileResolves {
		return "MEDIUM (complex receiver)", true
	}
	return "", false
}

// scanVarTypes builds a name→type map from variable declarations.
func scanVarTypes(tokens []java.Token) map[string]string {
	out := map[string]string{}
	for i := 0; i < len(tokens); i++ {
		tok := tokens[i]
		if tok.Kind != java.TIdent || tok.IsKeyword() {
			continue
		}
		// Pattern: <TypeIdent> <name> [= | ; | , | )]
		// TypeIdent must be PascalCase (start with uppercase) for us to
		// consider it a type — avoids treating `int x` where int is keyword.
		if !isUpperStart(tok.Text) {
			continue
		}
		next := nextNonComment(tokens, i+1)
		if next == nil || next.Kind != java.TIdent {
			continue
		}
		after := nextNonComment(tokens, indexOfToken(tokens, next)+1)
		if after == nil {
			continue
		}
		if after.Text == "=" || after.Text == ";" || after.Text == "," || after.Text == ")" {
			out[next.Text] = tok.Text
		}
	}
	return out
}

func isUpperStart(s string) bool {
	return len(s) > 0 && s[0] >= 'A' && s[0] <= 'Z'
}

func nextNonComment(tokens []java.Token, from int) *java.Token {
	for from < len(tokens) {
		if tokens[from].Kind != java.TComment {
			return &tokens[from]
		}
		from++
	}
	return nil
}

func prevNonComment(tokens []java.Token, from int) *java.Token {
	for from >= 0 {
		if tokens[from].Kind != java.TComment {
			return &tokens[from]
		}
		from--
	}
	return nil
}

func indexOfToken(tokens []java.Token, t *java.Token) int {
	for i := range tokens {
		if &tokens[i] == t {
			return i
		}
	}
	return -1
}

func resolvesTarget(fe *index.FileEntry, target *index.TypeDecl) bool {
	simple := target.Name
	for _, imp := range fe.Symbols.Imports {
		if imp.IsStatic || imp.IsWildcard {
			continue
		}
		if endsWithDotName(imp.Path, simple) {
			return imp.Path == target.FQN
		}
	}
	if fe.Symbols.Package == target.Package {
		return true
	}
	for _, imp := range fe.Symbols.Imports {
		if !imp.IsStatic && imp.IsWildcard && imp.Path == target.Package {
			return true
		}
	}
	return false
}

func hasStaticMemberImport(fe *index.FileEntry, classFQN, method string) bool {
	for _, imp := range fe.Symbols.Imports {
		if !imp.IsStatic {
			continue
		}
		if imp.IsWildcard && imp.Path == classFQN {
			return true
		}
		if imp.Path == classFQN+"."+method {
			return true
		}
	}
	return false
}

func endsWithDotName(path, name string) bool {
	if path == name {
		return true
	}
	return len(path) > len(name)+1 &&
		path[len(path)-len(name)-1] == '.' &&
		path[len(path)-len(name):] == name
}

var _ = strings.HasPrefix // keep strings imported for endsWithDotName
