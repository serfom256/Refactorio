package cli

import (
	"fmt"
	"io"
	"sort"
	"strings"
)

// Handler executes a parsed subcommand. Implementations live next to
// their operation packages and register themselves at process start.
type Handler func(args *Args, stdout, stderr io.Writer) error

// Command bundles a handler with its help metadata.
type Command struct {
	Name    string
	Handler Handler
	// Summary is a one-line description shown in the top-level command list.
	Summary string
	// Usage is the usage line, e.g. "rename in=<file> name=<sym> to=<new>".
	Usage string
	// Help is the full multi-line help body shown for `<cmd> --help`.
	Help string
}

var commands = map[string]Command{}

// RegisterCommand binds a Command to its name. Panics on duplicate.
func RegisterCommand(c Command) {
	if _, dup := commands[c.Name]; dup {
		panic(fmt.Sprintf("cli: duplicate command %q", c.Name))
	}
	commands[c.Name] = c
}

// RegisterHandler is the minimal registration (no help). Kept for
// backwards compatibility with handlers that don't supply help text.
func RegisterHandler(name string, h Handler) {
	RegisterCommand(Command{Name: name, Handler: h})
}

// Dispatch parses argv and invokes the registered handler. Handles
// `--help` / `-h` / `help` forms before argument parsing.
//
// argv must NOT include the program name (caller passes os.Args[1:]).
func Dispatch(argv []string, stdout, stderr io.Writer) error {
	if len(argv) == 0 {
		_, _ = io.WriteString(stdout, Usage())
		return nil
	}

	first := argv[0]

	// `refactorio help [cmd]` or `refactorio --help`
	if first == "help" || first == "--help" || first == "-h" {
		if len(argv) > 1 {
			_, _ = io.WriteString(stdout, commandHelp(argv[1]))
		} else {
			_, _ = io.WriteString(stdout, Usage())
		}
		return nil
	}

	// `refactorio <cmd> --help`
	for _, a := range argv[1:] {
		if a == "--help" || a == "-h" {
			_, _ = io.WriteString(stdout, commandHelp(first))
			return nil
		}
	}

	args, err := Parse(argv)
	if err != nil {
		return fmt.Errorf("%w\n\n%s", err, Usage())
	}
	c, ok := commands[args.Subcommand]
	if !ok {
		return fmt.Errorf("unknown subcommand %q\n\n%s", args.Subcommand, Usage())
	}
	return c.Handler(args, stdout, stderr)
}

// Usage returns the top-level command list with one-line summaries.
func Usage() string {
	names := make([]string, 0, len(commands))
	for n := range commands {
		names = append(names, n)
	}
	sort.Strings(names)

	// Compute column width for alignment.
	width := 0
	for _, n := range names {
		if len(n) > width {
			width = len(n)
		}
	}

	var b strings.Builder
	b.WriteString("refactorio — cross-file refactoring for Java, Kotlin, Go, TypeScript, JavaScript, Rust\n\n")
	b.WriteString("usage: refactorio <command> key=value ...\n\n")
	b.WriteString("commands:\n")
	for _, n := range names {
		c := commands[n]
		b.WriteString(fmt.Sprintf("  %-*s  %s\n", width, n, c.Summary))
	}
	b.WriteString("\nRun 'refactorio <command> --help' for detailed usage of a command.\n")
	return b.String()
}

// commandHelp returns the detailed help for one command.
func commandHelp(name string) string {
	c, ok := commands[name]
	if !ok {
		return fmt.Sprintf("unknown command %q\n\n%s", name, Usage())
	}
	var b strings.Builder
	if c.Summary != "" {
		b.WriteString(c.Summary + "\n\n")
	}
	if c.Usage != "" {
		b.WriteString("usage: refactorio " + c.Usage + "\n")
	}
	if c.Help != "" {
		b.WriteString("\n" + strings.TrimRight(c.Help, "\n") + "\n")
	}
	return b.String()
}
