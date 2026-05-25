package cli

import (
	"fmt"
	"io"
	"sort"
	"strings"
)

// Handler executes a parsed subcommand. Implementations live next to
// their operation packages (e.g. internal/op/rename/cli.go) and register
// themselves at process start.
type Handler func(args *Args, stdout, stderr io.Writer) error

var handlers = map[string]Handler{}

// RegisterHandler binds a Handler to a subcommand name. Panics on
// duplicate registration so misconfiguration is caught at startup.
func RegisterHandler(name string, h Handler) {
	if _, dup := handlers[name]; dup {
		panic(fmt.Sprintf("cli: duplicate handler for %q", name))
	}
	handlers[name] = h
}

// Dispatch parses argv and invokes the registered handler.
// argv must NOT include the program name (caller passes os.Args[1:]).
func Dispatch(argv []string, stdout, stderr io.Writer) error {
	args, err := Parse(argv)
	if err != nil {
		return fmt.Errorf("%w\n\n%s", err, Usage())
	}
	h, ok := handlers[args.Subcommand]
	if !ok {
		return fmt.Errorf("unknown subcommand %q\n\n%s", args.Subcommand, Usage())
	}
	return h(args, stdout, stderr)
}

// Usage prints a list of registered subcommands. We do not document
// per-subcommand args here yet; each handler is expected to surface
// argument errors with enough context to be self-documenting.
func Usage() string {
	names := make([]string, 0, len(handlers))
	for n := range handlers {
		names = append(names, n)
	}
	sort.Strings(names)
	var b strings.Builder
	b.WriteString("usage: refactorio <subcommand> key=value ...\n\nsubcommands:\n")
	for _, n := range names {
		b.WriteString("  ")
		b.WriteString(n)
		b.WriteString("\n")
	}
	return b.String()
}
