package renamefield

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
	Field    string
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
	if req.Field == req.NewName {
		return nil, fmt.Errorf("field is already named %q", req.NewName)
	}
	target := req.Index.LookupFQN(req.ClassFQN)
	if target == nil {
		return nil, fmt.Errorf("no class %q in project", req.ClassFQN)
	}
	var declFields []java.MemberDecl
	for _, f := range target.Fields {
		if f.Name == req.Field {
			declFields = append(declFields, f)
		}
	}
	if len(declFields) == 0 {
		return nil, fmt.Errorf("no field %s.%s found", req.ClassFQN, req.Field)
	}

	set := edit.NewSet()
	for _, f := range declFields {
		set.Add(edit.Edit{
			File: target.File, Start: f.NameRange.Start, End: f.NameRange.End,
			NewText: req.NewName, Why: fmt.Sprintf("rename field %s.%s -> %s (declaration)", target.Name, req.Field, req.NewName),
		})
	}

	for _, path := range req.Index.FilesImporting(target.FQN) {
		fe := req.Index.File(path)
		if fe == nil || fe.Symbols == nil {
			continue
		}
		fileResolves := resolvesTarget(fe, target)
		hasStaticField := hasStaticFieldImport(fe, target.FQN, req.Field)
		if !fileResolves && !hasStaticField {
			continue
		}
		if err := planFile(fe, target, req.Field, req.NewName, fileResolves, hasStaticField, set); err != nil {
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
			if imp.Path == target.FQN+"."+req.Field {
				set.Add(edit.Edit{
					File: fe.Path, Start: imp.PathRange.Start, End: imp.PathRange.End,
					NewText: target.FQN + "." + req.NewName,
					Why: fmt.Sprintf("update static import %s.%s -> %s", target.FQN, req.Field, req.NewName),
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
	fileResolves, hasStaticField bool,
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
	hasShadow := hasShadowingLocal(tokens, oldName, targetBody, target.Fields)

	for i, tok := range tokens {
		if tok.Kind != java.TIdent || tok.Text != oldName {
			continue
		}
		// Skip declaration edits already emitted.
		if fe.Path == target.File && isDeclRange(tok, target) {
			continue
		}

		// Pattern 1: obj.field (field access)
		prev := prevNonComment(tokens, i-1)
		if prev != nil && prev.Text == "." {
			receiver := prevNonComment(tokens, indexOf(tokens, prev)-1)
			if receiver == nil {
				continue
			}
			// Exclude method calls: field followed by (
			next := nextNonComment(tokens, i+1)
			if next != nil && next.Text == "(" {
				continue // This is a method call, not a field access.
			}
			conf, ok := classifyReceiver(receiver, target, varTypes, targetBody, fileResolves)
			if !ok {
				continue
			}
			set.Add(edit.Edit{
				File: fe.Path, Start: tok.Start, End: tok.End,
				NewText: newName, Why: fmt.Sprintf("rename field access %s.%s -> %s [%s]", target.Name, oldName, newName, conf),
			})
			continue
		}

		// Pattern 2: bare identifier inside target class body (implicit this).
		if targetBody != nil && targetBody.contains(tok.Start) && !hasShadow {
			// Skip if this is a declaration context.
			next := nextNonComment(tokens, i+1)
			if next != nil && next.Text == "(" {
				continue // method call, not field
			}
			set.Add(edit.Edit{
				File: fe.Path, Start: tok.Start, End: tok.End,
				NewText: newName, Why: fmt.Sprintf("rename bare reference %s -> %s [HIGH]", oldName, newName),
			})
		}
	}
	return nil
}

type bodyRange struct{ Start, End int }

func (r *bodyRange) contains(off int) bool {
	return r != nil && off >= r.Start && off < r.End
}

func isDeclRange(tok java.Token, target *index.TypeDecl) bool {
	for _, f := range target.Fields {
		if tok.Start == f.NameRange.Start {
			return true
		}
	}
	return false
}

func classifyReceiver(receiver *java.Token, target *index.TypeDecl, varTypes map[string]string, targetBody *bodyRange, fileResolves bool) (string, bool) {
	if receiver.Text == "this" {
		if targetBody != nil && targetBody.contains(receiver.Start) {
			return "HIGH (this)", true
		}
		return "", false
	}
	if receiver.Text == "super" {
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
	return "", false
}

func scanVarTypes(tokens []java.Token) map[string]string {
	out := map[string]string{}
	for i := 0; i < len(tokens); i++ {
		tok := tokens[i]
		if tok.Kind != java.TIdent || tok.IsKeyword() || !isUpperStart(tok.Text) {
			continue
		}
		next := nextNonComment(tokens, i+1)
		if next == nil || next.Kind != java.TIdent {
			continue
		}
		after := nextNonComment(tokens, indexOf(tokens, next)+1)
		if after != nil && (after.Text == "=" || after.Text == ";" || after.Text == "," || after.Text == ")") {
			out[next.Text] = tok.Text
		}
	}
	return out
}

func hasShadowingLocal(tokens []java.Token, fieldName string, targetBody *bodyRange, fieldDecl []java.MemberDecl) bool {
	// Conservative: if any local/param declares an identifier with the
	// same name as the field (NOT the field declaration itself), we
	// skip bare-identifier rewrites to avoid renaming shadowed references.
	for i, tok := range tokens {
		if tok.Kind != java.TIdent || tok.Text != fieldName {
			continue
		}
		// Skip the field declaration itself.
		isFieldDecl := false
		for _, f := range fieldDecl {
			if tok.Start == f.NameRange.Start && tok.End == f.NameRange.End {
				isFieldDecl = true
				break
			}
		}
		if isFieldDecl {
			continue
		}
		// Check if this looks like a local/param declaration.
		prev := prevNonComment(tokens, i-1)
		if prev == nil {
			continue
		}
		next := nextNonComment(tokens, i+1)
		if next == nil {
			continue
		}
		isLocalDecl := (prev.Kind == java.TIdent || prev.Text == "]") &&
			(next.Text == "=" || next.Text == ";" || next.Text == ",")
		isParam := (prev.Kind == java.TIdent) && (next.Text == ")" || next.Text == ",")
		if (isLocalDecl || isParam) && prev.Kind == java.TIdent && !isModifier(prev.Text) {
			return true
		}
	}
	return false
}

func isModifier(s string) bool {
	switch s {
	case "public", "private", "protected", "static", "final", "abstract",
		"native", "synchronized", "transient", "volatile":
		return true
	}
	return false
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

func indexOf(tokens []java.Token, t *java.Token) int {
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

func hasStaticFieldImport(fe *index.FileEntry, classFQN, field string) bool {
	for _, imp := range fe.Symbols.Imports {
		if !imp.IsStatic {
			continue
		}
		if imp.IsWildcard && imp.Path == classFQN {
			return true
		}
		if imp.Path == classFQN+"."+field {
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

var _ = strings.HasPrefix
