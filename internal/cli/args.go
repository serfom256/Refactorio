package cli

import (
	"fmt"
	"strings"
)

type Args struct {
	Subcommand string
	pairs      []pair
	index      map[string]int
}

type pair struct {
	key   string
	value string
}

func Parse(argv []string) (*Args, error) {
	if len(argv) == 0 {
		return nil, fmt.Errorf("missing subcommand")
	}
	sub := argv[0]
	if strings.Contains(sub, "=") {
		return nil, fmt.Errorf("first argument must be a subcommand, got %q", sub)
	}
	a := &Args{Subcommand: sub, index: map[string]int{}}
	for _, raw := range argv[1:] {
		eq := strings.IndexByte(raw, '=')
		if eq <= 0 {
			return nil, fmt.Errorf("argument %q is not in key=value form", raw)
		}
		k := raw[:eq]
		v := raw[eq+1:]
		if _, dup := a.index[k]; dup {
			return nil, fmt.Errorf("duplicate key %q", k)
		}
		a.index[k] = len(a.pairs)
		a.pairs = append(a.pairs, pair{k, v})
	}
	return a, nil
}

func (a *Args) Get(key string) (string, bool) {
	i, ok := a.index[key]
	if !ok {
		return "", false
	}
	return a.pairs[i].value, true
}

func (a *Args) Require(key string) (string, error) {
	v, ok := a.Get(key)
	if !ok {
		return "", fmt.Errorf("missing required argument %q", key)
	}
	if v == "" {
		return "", fmt.Errorf("argument %q has empty value", key)
	}
	return v, nil
}

func (a *Args) Bool(key string, def bool) (bool, error) {
	v, ok := a.Get(key)
	if !ok {
		return def, nil
	}
	switch strings.ToLower(v) {
	case "true":
		return true, nil
	case "false":
		return false, nil
	default:
		return false, fmt.Errorf("argument %q must be true or false, got %q", key, v)
	}
}

func (a *Args) Keys() []string {
	out := make([]string, len(a.pairs))
	for i, p := range a.pairs {
		out[i] = p.key
	}
	return out
}

func (a *Args) RawPairs() []string {
	out := make([]string, len(a.pairs))
	for i, p := range a.pairs {
		out[i] = p.key + "=" + p.value
	}
	return out
}

func (a *Args) Unknown(allowed ...string) []string {
	allow := map[string]struct{}{}
	for _, k := range allowed {
		allow[k] = struct{}{}
	}
	var out []string
	for _, p := range a.pairs {
		if _, ok := allow[p.key]; !ok {
			out = append(out, p.key)
		}
	}
	return out
}
