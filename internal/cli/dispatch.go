package cli

import (
	"fmt"
	"io"
	"sort"
	"strings"
	"time"
)

type Handler func(args *Args, stdout, stderr io.Writer) error

type Command struct {
	Name    string
	Handler Handler
	Summary string
	Usage   string
	Help    string
}

var commands = map[string]Command{}

func RegisterCommand(c Command) {
	if _, dup := commands[c.Name]; dup {
		panic(fmt.Sprintf("cli: duplicate command %q", c.Name))
	}
	commands[c.Name] = c
}

func RegisterHandler(name string, h Handler) {
	RegisterCommand(Command{Name: name, Handler: h})
}

func Dispatch(argv []string, stdout, stderr io.Writer) error {
	if len(argv) == 0 {
		_, _ = io.WriteString(stdout, Usage())
		return nil
	}

	first := argv[0]

	if first == "help" || first == "--help" || first == "-h" {
		if len(argv) > 1 {
			_, _ = io.WriteString(stdout, commandHelp(argv[1]))
		} else {
			_, _ = io.WriteString(stdout, Usage())
		}
		return nil
	}

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
	start := time.Now()
	err = c.Handler(args, stdout, stderr)
	if observer != nil {
		observer(args, time.Since(start), err)
	}
	return err
}

type Observer func(args *Args, dur time.Duration, err error)

var observer Observer

func SetObserver(o Observer) { observer = o }

func Usage() string {
	names := make([]string, 0, len(commands))
	for n := range commands {
		names = append(names, n)
	}
	sort.Strings(names)

	width := 0
	for _, n := range names {
		if len(n) > width {
			width = len(n)
		}
	}

	var b strings.Builder
	b.WriteString("refactorio — cross-file refactoring for Java, Kotlin, Go, TypeScript, JavaScript, Rust, C#, Python\n\n")
	b.WriteString("usage: refactorio <command> key=value ...\n\n")
	b.WriteString("commands:\n")
	for _, n := range names {
		c := commands[n]
		b.WriteString(fmt.Sprintf("  %-*s  %s\n", width, n, c.Summary))
	}
	b.WriteString("\nRun 'refactorio <command> --help' for detailed usage of a command.\n")
	return b.String()
}

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
