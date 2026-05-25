package unified

import (
	"github.com/serfom256/refactorio/internal/edit"
	"github.com/serfom256/refactorio/internal/index"
	"github.com/serfom256/refactorio/internal/lang/java"
)

// scanFQNReferences finds inline fully-qualified references like
// `com.openmapper.annotations.entity.Field` in the token stream and
// produces edits. Used for both rename (rewrite last segment) and
// move (rewrite package portion).
//
// A FQN chain is a sequence of TIdent tokens separated by "." puncts
// that, when concatenated, equals the target FQN.

// renameFQNReferences scans for inline FQN chains matching targetFQN
// and renames the last segment (the simple name) to newName.
func renameFQNReferences(path string, tokens []java.Token, targetFQN, newName string, fe *index.FileEntry, set *edit.Set) {
	chains := findFQNChains(tokens, targetFQN)
	for _, c := range chains {
		if fe != nil && fe.Symbols != nil && inJavaImportOrPkg(c.tokens[0], fe) {
			continue
		}
		lastTok := c.tokens[len(c.tokens)-1]
		set.Add(edit.Edit{
			File: path, Start: lastTok.Start, End: lastTok.End,
			NewText: newName, Why: "rename inline FQN reference",
		})
	}
}

func moveFQNReferences(path string, tokens []java.Token, oldFQN, newFQN string, fe *index.FileEntry, set *edit.Set) {
	chains := findFQNChains(tokens, oldFQN)
	for _, c := range chains {
		if fe != nil && fe.Symbols != nil && inJavaImportOrPkg(c.tokens[0], fe) {
			continue
		}
		set.Add(edit.Edit{
			File: path, Start: c.fullStart, End: c.fullEnd,
			NewText: newFQN, Why: "rewrite inline FQN for moved class",
		})
	}
}

func inJavaImportOrPkg(tok java.Token, fe *index.FileEntry) bool {
	for _, imp := range fe.Symbols.Imports {
		if tok.Start >= imp.Range.Start && tok.End <= imp.Range.End {
			return true
		}
	}
	r := fe.Symbols.PackageRange
	if r.End > 0 && tok.Start >= r.Start && tok.End <= r.End {
		return true
	}
	return false
}

type fqnChain struct {
	tokens    []java.Token // just the ident tokens (no dots)
	fullStart int          // byte offset of first ident
	fullEnd   int          // byte offset past last ident
}

// findFQNChains walks the token stream looking for ident.ident.ident
// sequences that match the target FQN when dots are inserted.
func findFQNChains(tokens []java.Token, targetFQN string) []fqnChain {
	var results []fqnChain
	i := 0
	for i < len(tokens) {
		if tokens[i].Kind != java.TIdent {
			i++
			continue
		}
		// Try to build a dotted chain starting here.
		chain, endIdx := buildChain(tokens, i)
		if chain == targetFQN {
			// Collect the ident tokens.
			var idents []java.Token
			for j := i; j < endIdx; j++ {
				if tokens[j].Kind == java.TIdent {
					idents = append(idents, tokens[j])
				}
			}
			results = append(results, fqnChain{
				tokens:    idents,
				fullStart: tokens[i].Start,
				fullEnd:   tokens[endIdx-1].End,
			})
			i = endIdx
			continue
		}
		i++
	}
	return results
}

// buildChain tries to parse an ident.ident.ident... chain starting at
// position i. Returns the concatenated dotted string and the index
// past the last consumed token.
func buildChain(tokens []java.Token, start int) (string, int) {
	if start >= len(tokens) || tokens[start].Kind != java.TIdent {
		return "", start
	}
	result := tokens[start].Text
	i := start + 1
	for i+1 < len(tokens) {
		// Expect "." then ident.
		if tokens[i].Kind == java.TPunct && tokens[i].Text == "." &&
			tokens[i+1].Kind == java.TIdent {
			result += "." + tokens[i+1].Text
			i += 2
			continue
		}
		break
	}
	return result, i
}
