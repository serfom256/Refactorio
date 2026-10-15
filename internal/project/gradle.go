package project

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

func loadGradle(root string) (*Project, error) {
	p := &Project{Root: root, Kind: KindGradle}

	includes, err := parseGradleSettings(root)
	if err != nil {
		return nil, err
	}

	rootMod := &Module{
		Name:        ":",
		Path:        root,
		SourceRoots: standardSourceRoots(root),
	}
	p.Modules = append(p.Modules, rootMod)

	for _, inc := range includes {
		rel := strings.ReplaceAll(strings.TrimPrefix(inc, ":"), ":", string(filepath.Separator))
		modPath := filepath.Join(root, rel)
		if _, err := os.Stat(modPath); err != nil {
			continue
		}
		p.Modules = append(p.Modules, &Module{
			Name:        ":" + inc,
			Path:        modPath,
			SourceRoots: standardSourceRoots(modPath),
		})
	}
	return p, nil
}

var gradleIncludeArg = regexp.MustCompile(`['"]([:.\w\-]+)['"]`)

func includeArgLists(src string) []string {
	var out []string
	for i := 0; i+len("include") <= len(src); i++ {
		if src[i] != 'i' || !strings.HasPrefix(src[i:], "include") {
			continue
		}
		if i > 0 && isIdentByte(src[i-1]) {
			continue
		}
		j := i + len("include")
		for j < len(src) && (src[j] == ' ' || src[j] == '\t') {
			j++
		}
		if j >= len(src) {
			break
		}
		switch {
		case src[j] == '(':
			if end := matchParen(src, j); end > 0 {
				out = append(out, src[j+1:end])
				i = end
			}
		case src[j] == '\'' || src[j] == '"':
			end := j
			for end < len(src) && src[end] != '\n' {
				end++
			}
			out = append(out, src[j:end])
			i = end
		}
	}
	return out
}

func matchParen(src string, open int) int {
	depth := 0
	for i := open; i < len(src); i++ {
		switch c := src[i]; c {
		case '\'', '"':
			i = skipString(src, i)
		case '(':
			depth++
		case ')':
			if depth--; depth == 0 {
				return i
			}
		}
	}
	return -1
}

func skipString(src string, i int) int {
	quote := src[i]
	for j := i + 1; j < len(src); j++ {
		if src[j] == '\\' {
			j++
			continue
		}
		if src[j] == quote {
			return j
		}
	}
	return len(src) - 1
}

func isIdentByte(b byte) bool {
	return b == '_' || b == '.' ||
		(b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') || (b >= '0' && b <= '9')
}

func parseGradleSettings(root string) ([]string, error) {
	var bytes []byte
	for _, name := range []string{"settings.gradle.kts", "settings.gradle"} {
		b, err := os.ReadFile(filepath.Join(root, name))
		if err == nil {
			bytes = b
			break
		}
	}
	if bytes == nil {
		return nil, nil
	}
	stripped := stripGroovyKotlinComments(string(bytes))

	var includes []string
	seen := map[string]struct{}{}
	for _, argList := range includeArgLists(stripped) {
		for _, arg := range gradleIncludeArg.FindAllStringSubmatch(argList, -1) {
			path := strings.TrimPrefix(arg[1], ":")
			if path == "" {
				continue
			}
			if _, dup := seen[path]; dup {
				continue
			}
			seen[path] = struct{}{}
			includes = append(includes, path)
		}
	}
	return includes, nil
}

func stripGroovyKotlinComments(s string) string {
	var out strings.Builder
	out.Grow(len(s))
	i := 0
	for i < len(s) {
		if i+1 < len(s) && s[i] == '/' && s[i+1] == '/' {
			j := i + 2
			for j < len(s) && s[j] != '\n' {
				j++
			}
			i = j
			continue
		}
		if i+1 < len(s) && s[i] == '/' && s[i+1] == '*' {
			j := i + 2
			for j+1 < len(s) && !(s[j] == '*' && s[j+1] == '/') {
				j++
			}
			if j+1 < len(s) {
				i = j + 2
			} else {
				i = len(s)
			}
			continue
		}
		out.WriteByte(s[i])
		i++
	}
	return out.String()
}
