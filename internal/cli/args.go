// Package cli parses refactorio's key=value command-line arguments.
//
// Form: `refactorio <subcommand> key=value key2=value2 ...`.
// Each key is unique; duplicate keys are an error so accidental overrides
// fail loudly instead of silently picking the last one.
package cli

import (
	"fmt"
	"strings"
)

// Args is the parsed command line: a subcommand name and an ordered set of
// key=value pairs. Order is preserved so error messages can point at the
// originally-typed argument.
type Args struct {
	Subcommand string
	pairs      []pair
	index      map[string]int // key -> position in pairs
}

type pair struct {
	key   string
	value string
}

// Parse turns the argv tail (everything after the program name) into Args.
// Returns a usage-style error if the input is structurally wrong.
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

// Get returns the value for a key and whether it was present.
func (a *Args) Get(key string) (string, bool) {
	i, ok := a.index[key]
	if !ok {
		return "", false
	}
	return a.pairs[i].value, true
}

// Require returns the value for a key or an error naming the missing key.
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

// Bool returns the value of a boolean key (true/false), or def if absent.
// Any non-true/false value is an error so typos like dry-run=yes don't get
// silently treated as false.
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

// Keys returns the argument names that were supplied, in order and without
// their values. Statistics record which options get used without recording
// what they were pointed at.
func (a *Args) Keys() []string {
	out := make([]string, len(a.pairs))
	for i, p := range a.pairs {
		out[i] = p.key
	}
	return out
}

// RawPairs returns all key=value pairs as strings for display.
func (a *Args) RawPairs() []string {
	out := make([]string, len(a.pairs))
	for i, p := range a.pairs {
		out[i] = p.key + "=" + p.value
	}
	return out
}

// Unknown returns the keys not listed in allowed. Operations call this to
// reject typos rather than silently ignoring them.
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
