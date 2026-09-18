package stats

import (
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/serfom256/refactorio/internal/cli"
)

func init() {
	cli.RegisterCommand(cli.Command{
		Name:    "stats",
		Handler: runStats,
		Summary: "show how Refactorio has been used on this project",
		Usage:   "stats [scope=project|all] [since=<7d|30d|YYYY-MM-DD>] [format=text|json] [reset=true]",
		Help: `Reports which commands have been run, which arguments they were
given, how often they succeeded, and how much code was rewritten.

Everything is recorded locally in ~/.refactorio/stats.jsonl and is never
transmitted anywhere. Symbol names, the paths inside your project and raw
error text are deliberately not recorded — only argument names, a classified
error kind, and counts. Set REFACTORIO_NO_STATS=1 to turn recording off.

arguments:
  scope=project   only this project (default); scope=all covers every project
  since=7d        limit to a window: 7d, 30d, 12h, or a YYYY-MM-DD date
  format=text     text (default) or json
  reset=true      delete the recorded history and exit

"lines" counts distinct source lines rewritten, and only for runs that were
actually applied — a dry run changes nothing, so it contributes usage counts
but no lines.`,
	})
}

func runStats(args *cli.Args, stdout, stderr io.Writer) error {
	if unknown := args.Unknown("scope", "since", "format", "reset", "root"); len(unknown) > 0 {
		return fmt.Errorf("unknown arguments: %v", unknown)
	}

	reset, err := args.Bool("reset", false)
	if err != nil {
		return err
	}
	if reset {
		path, err := LogPath()
		if err != nil {
			return err
		}
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return err
		}
		fmt.Fprintln(stdout, "statistics cleared")
		return nil
	}

	scope, _ := args.Get("scope")
	if scope == "" {
		scope = "project"
	}
	if scope != "project" && scope != "all" {
		return fmt.Errorf("scope must be \"project\" or \"all\", got %q", scope)
	}

	project := ""
	if scope == "project" {
		root, _ := args.Get("root")
		if root == "" {
			root, _ = os.Getwd()
		}
		project = ProjectRoot(root)
	}

	var since time.Time
	if raw, ok := args.Get("since"); ok && raw != "" {
		since, err = parseSince(raw, time.Now())
		if err != nil {
			return err
		}
	}

	evs, err := Load()
	if err != nil {
		return err
	}
	summary := Summarise(Filter(evs, project, since), project, since)

	format, _ := args.Get("format")
	switch format {
	case "", "text":
		return FormatText(stdout, summary)
	case "json":
		return FormatJSON(stdout, summary)
	default:
		return fmt.Errorf("format must be \"text\" or \"json\", got %q", format)
	}
}

// parseSince accepts a relative window (7d, 12h) or an absolute YYYY-MM-DD.
func parseSince(raw string, now time.Time) (time.Time, error) {
	if t, err := time.Parse("2006-01-02", raw); err == nil {
		return t, nil
	}
	if len(raw) > 1 {
		unit := raw[len(raw)-1]
		if n, err := strconv.Atoi(raw[:len(raw)-1]); err == nil && n > 0 {
			switch unit {
			case 'd':
				return now.AddDate(0, 0, -n), nil
			case 'h':
				return now.Add(-time.Duration(n) * time.Hour), nil
			case 'w':
				return now.AddDate(0, 0, -7*n), nil
			}
		}
	}
	return time.Time{}, fmt.Errorf("cannot parse since=%q: use 7d, 12h, 2w or YYYY-MM-DD", strings.TrimSpace(raw))
}
