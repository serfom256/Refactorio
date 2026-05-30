package cli

import (
	"bytes"
	"io"
	"strings"
	"testing"
)

func setupTestCommand() func() {
	RegisterCommand(Command{
		Name:    "testcmd",
		Summary: "a test command",
		Usage:   "testcmd in=<file>",
		Help:    "detailed help for testcmd",
		Handler: func(a *Args, stdout, stderr io.Writer) error {
			_, _ = io.WriteString(stdout, "ran")
			return nil
		},
	})
	return func() { delete(commands, "testcmd") }
}

func TestDispatch_TopLevelHelp(t *testing.T) {
	cleanup := setupTestCommand()
	defer cleanup()

	for _, argv := range [][]string{{}, {"help"}, {"--help"}, {"-h"}} {
		var out bytes.Buffer
		if err := Dispatch(argv, &out, &out); err != nil {
			t.Fatalf("argv %v: %v", argv, err)
		}
		if !strings.Contains(out.String(), "testcmd") {
			t.Errorf("argv %v: overview missing command list:\n%s", argv, out.String())
		}
	}
}

func TestDispatch_CommandHelp(t *testing.T) {
	cleanup := setupTestCommand()
	defer cleanup()

	for _, argv := range [][]string{{"testcmd", "--help"}, {"testcmd", "-h"}, {"help", "testcmd"}} {
		var out bytes.Buffer
		if err := Dispatch(argv, &out, &out); err != nil {
			t.Fatalf("argv %v: %v", argv, err)
		}
		got := out.String()
		if !strings.Contains(got, "detailed help for testcmd") {
			t.Errorf("argv %v: missing detailed help:\n%s", argv, got)
		}
		if !strings.Contains(got, "usage: refactorio testcmd") {
			t.Errorf("argv %v: missing usage line:\n%s", argv, got)
		}
	}
}

func TestDispatch_HelpForUnknownCommand(t *testing.T) {
	var out bytes.Buffer
	if err := Dispatch([]string{"help", "nonexistent"}, &out, &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "unknown command") {
		t.Errorf("expected unknown command message:\n%s", out.String())
	}
}

func TestDispatch_RunsHandler(t *testing.T) {
	cleanup := setupTestCommand()
	defer cleanup()

	var out bytes.Buffer
	if err := Dispatch([]string{"testcmd", "in=foo"}, &out, &out); err != nil {
		t.Fatal(err)
	}
	if out.String() != "ran" {
		t.Errorf("handler not invoked, got: %q", out.String())
	}
}
