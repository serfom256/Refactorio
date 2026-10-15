package ktrename

import (
	"fmt"
	"os"
	"strings"
	"sync"

	"github.com/serfom256/refactorio/internal/edit"
	"github.com/serfom256/refactorio/internal/lang/kotlin"
	"github.com/serfom256/refactorio/internal/project"
)

type KtIndex struct {
	files   map[string]*KtFileEntry
	byFQN   map[string]*KtTypeDecl
	byPkg   map[string][]string
	impBy   map[string][]string
	wildImp map[string][]string
}

type KtFileEntry struct {
	Path    string
	Symbols *kotlin.FileSymbols
	Err     error
}

type KtTypeDecl struct {
	kotlin.Declaration
	File    string
	Package string
	FQN     string
}

func BuildIndex(p *project.Project) (*KtIndex, error) {
	files, err := p.SourceFiles(".kt", ".kts")
	if err != nil {
		return nil, err
	}
	idx := &KtIndex{
		files:   map[string]*KtFileEntry{},
		byFQN:   map[string]*KtTypeDecl{},
		byPkg:   map[string][]string{},
		impBy:   map[string][]string{},
		wildImp: map[string][]string{},
	}

	type result struct {
		path  string
		entry *KtFileEntry
	}
	in := make(chan string, len(files))
	out := make(chan result, len(files))
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for p := range in {
				e := &KtFileEntry{Path: p}
				src, err := os.ReadFile(p)
				if err != nil {
					e.Err = err
				} else {
					fs, err := kotlin.ExtractSymbols(src)
					if err != nil {
						e.Err = err
					} else {
						e.Symbols = fs
					}
				}
				out <- result{p, e}
			}
		}()
	}
	for _, f := range files {
		in <- f
	}
	close(in)
	go func() { wg.Wait(); close(out) }()
	for r := range out {
		idx.files[r.path] = r.entry
	}
	idx.buildLookups()
	return idx, nil
}

func (idx *KtIndex) buildLookups() {
	for path, fe := range idx.files {
		if fe.Symbols == nil {
			continue
		}
		pkg := fe.Symbols.Package
		if pkg != "" {
			idx.byPkg[pkg] = append(idx.byPkg[pkg], path)
		}
		for _, d := range fe.Symbols.Declarations {
			fqn := d.Name
			if pkg != "" {
				fqn = pkg + "." + d.Name
			}
			idx.byFQN[fqn] = &KtTypeDecl{Declaration: d, File: path, Package: pkg, FQN: fqn}
		}
		for _, imp := range fe.Symbols.Imports {
			if strings.HasSuffix(imp.Path, ".*") {
				pkg := strings.TrimSuffix(imp.Path, ".*")
				idx.wildImp[pkg] = append(idx.wildImp[pkg], path)
			} else {
				idx.impBy[imp.Path] = append(idx.impBy[imp.Path], path)
			}
		}
	}
}

func (idx *KtIndex) File(path string) *KtFileEntry {
	return idx.files[path]
}

func (idx *KtIndex) FilesInPkg(pkg string) []string {
	return idx.byPkg[pkg]
}

func (idx *KtIndex) FilesImporting(fqn string) []string {
	seen := map[string]struct{}{}
	for _, p := range idx.impBy[fqn] {
		seen[p] = struct{}{}
	}
	pkg := fqn[:max(0, strings.LastIndexByte(fqn, '.'))]
	for _, p := range idx.wildImp[pkg] {
		seen[p] = struct{}{}
	}
	for _, p := range idx.byPkg[pkg] {
		seen[p] = struct{}{}
	}
	if td := idx.byFQN[fqn]; td != nil {
		seen[td.File] = struct{}{}
	}
	out := make([]string, 0, len(seen))
	for p := range seen {
		out = append(out, p)
	}
	return out
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

type RenameRequest struct {
	FQN     string
	NewName string
	Index   *KtIndex
}

func PlanRename(req RenameRequest) (*edit.Set, error) {
	if !kotlin.IsValidIdentifier(req.NewName) {
		return nil, fmt.Errorf("%q is not a valid Kotlin identifier", req.NewName)
	}
	target := req.Index.byFQN[req.FQN]
	if target == nil {
		return nil, fmt.Errorf("no declaration %q in project", req.FQN)
	}
	oldName := target.Name
	if oldName == req.NewName {
		return nil, fmt.Errorf("symbol is already named %q", req.NewName)
	}

	set := edit.NewSet()

	set.Add(edit.Edit{
		File: target.File, Start: target.NameRange.Start, End: target.NameRange.End,
		NewText: req.NewName, Why: fmt.Sprintf("rename declaration %s -> %s", oldName, req.NewName),
	})

	for _, path := range req.Index.FilesImporting(req.FQN) {
		fe := req.Index.files[path]
		if fe == nil || fe.Symbols == nil {
			continue
		}
		if err := planKtFile(fe, target, req.NewName, set); err != nil {
			return nil, err
		}
	}

	return set, nil
}

func planKtFile(fe *KtFileEntry, target *KtTypeDecl, newName string, set *edit.Set) error {
	src, err := os.ReadFile(fe.Path)
	if err != nil {
		return err
	}
	tokens := kotlin.Tokenize(src)
	oldName := target.Name

	for _, imp := range fe.Symbols.Imports {
		if imp.Path == target.FQN {
			newFQN := target.Package + "." + newName
			set.Add(edit.Edit{
				File: fe.Path, Start: imp.PathRange.Start, End: imp.PathRange.End,
				NewText: newFQN, Why: fmt.Sprintf("update import %s", target.FQN),
			})
		}
	}

	for i, tok := range tokens {
		if tok.Kind != kotlin.TIdent || tok.Text != oldName {
			continue
		}
		if tok.Start == target.NameRange.Start && tok.End == target.NameRange.End && fe.Path == target.File {
			continue
		}
		if inImportRange(tok, fe.Symbols) {
			continue
		}
		if isLocalDeclKt(tokens, i) {
			continue
		}
		set.Add(edit.Edit{
			File: fe.Path, Start: tok.Start, End: tok.End,
			NewText: newName, Why: fmt.Sprintf("rename %s -> %s", oldName, newName),
		})
	}

	return nil
}

func inImportRange(tok kotlin.Token, syms *kotlin.FileSymbols) bool {
	for _, imp := range syms.Imports {
		if tok.Start >= imp.Range.Start && tok.End <= imp.Range.End {
			return true
		}
	}
	return false
}

func isLocalDeclKt(tokens []kotlin.Token, i int) bool {
	if i <= 0 {
		return false
	}
	prev := prevNonCommentKt(tokens, i-1)
	if prev != nil && (prev.Text == "val" || prev.Text == "var") {
		return true
	}
	if prev != nil && prev.Kind == kotlin.TIdent && !prev.IsKeyword() && isUpperStartKt(prev.Text) {
		next := nextNonCommentKt(tokens, i+1)
		if next != nil && (next.Text == "," || next.Text == ")" || next.Text == ":") {
			return true
		}
	}
	return false
}

func prevNonCommentKt(tokens []kotlin.Token, from int) *kotlin.Token {
	for from >= 0 {
		if tokens[from].Kind != kotlin.TComment {
			return &tokens[from]
		}
		from--
	}
	return nil
}

func nextNonCommentKt(tokens []kotlin.Token, from int) *kotlin.Token {
	for from < len(tokens) {
		if tokens[from].Kind != kotlin.TComment {
			return &tokens[from]
		}
		from++
	}
	return nil
}

func isUpperStartKt(s string) bool {
	return len(s) > 0 && s[0] >= 'A' && s[0] <= 'Z'
}
