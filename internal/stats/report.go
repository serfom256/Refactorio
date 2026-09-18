package stats

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"time"
)

// Load reads every event in the log. Malformed lines are skipped rather than
// failing the read: a truncated final line from an interrupted write should
// not make the whole history unreadable.
func Load() ([]Event, error) {
	path, err := LogPath()
	if err != nil {
		return nil, err
	}
	f, err := os.Open(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var out []Event
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1<<20)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var ev Event
		if json.Unmarshal([]byte(line), &ev) != nil {
			continue
		}
		out = append(out, ev)
	}
	return out, sc.Err()
}

// Filter narrows events to a project (empty means all) and a time window
// (zero means unbounded).
func Filter(evs []Event, project string, since time.Time) []Event {
	var out []Event
	for _, ev := range evs {
		if project != "" && ev.Project != project {
			continue
		}
		if !since.IsZero() && ev.Time.Before(since) {
			continue
		}
		out = append(out, ev)
	}
	return out
}

// Summary is the aggregated view rendered by the stats command.
type Summary struct {
	Total int `json:"total"`
	// Since is nil when the report is unbounded. A zero time.Time would
	// serialise as year 1 rather than being omitted.
	Since    *time.Time    `json:"since,omitempty"`
	Project  string        `json:"project,omitempty"`
	Commands []CommandStat `json:"commands"`
	Langs    []CountStat   `json:"languages,omitempty"`
	Errors   []CountStat   `json:"errors,omitempty"`
	Args     []CountStat   `json:"arguments,omitempty"`
	Code     CodeStat      `json:"code"`
}

// CommandStat is per-command usage.
type CommandStat struct {
	Name    string `json:"name"`
	Runs    int    `json:"runs"`
	OK      int    `json:"ok"`
	Err     int    `json:"err"`
	DryRuns int    `json:"dry_runs"`
	// LinesChanged counts only applied runs; a dry run rewrites nothing.
	LinesChanged int `json:"lines_changed"`
}

// CountStat is a generic name/count pair.
type CountStat struct {
	Name  string `json:"name"`
	Count int    `json:"count"`
}

// CodeStat totals the code actually rewritten, excluding dry runs and failures.
type CodeStat struct {
	LinesChanged int `json:"lines_changed"`
	LinesMoved   int `json:"lines_moved"`
	LinesDeleted int `json:"lines_deleted"`
	Files        int `json:"files"`
	Edits        int `json:"edits"`
}

// Summarise aggregates events. Dry runs and failed runs are counted as usage
// but never as code changed — nothing reached disk.
func Summarise(evs []Event, project string, since time.Time) Summary {
	s := Summary{Total: len(evs), Project: project}
	if !since.IsZero() {
		s.Since = &since
	}

	byCmd := map[string]*CommandStat{}
	langs := map[string]int{}
	errs := map[string]int{}
	args := map[string]int{}

	for _, ev := range evs {
		c, ok := byCmd[ev.Command]
		if !ok {
			c = &CommandStat{Name: ev.Command}
			byCmd[ev.Command] = c
		}
		c.Runs++
		if ev.OK {
			c.OK++
		} else {
			c.Err++
			errs[ev.ErrKind]++
		}
		if ev.DryRun {
			c.DryRuns++
		}
		if ev.Lang != "" {
			langs[ev.Lang]++
		}
		for _, a := range ev.Args {
			args[a]++
		}
		if ev.OK && !ev.DryRun {
			c.LinesChanged += ev.LinesChanged
			s.Code.LinesChanged += ev.LinesChanged
			s.Code.LinesMoved += ev.LinesMoved
			s.Code.LinesDeleted += ev.LinesDeleted
			s.Code.Files += ev.Files
			s.Code.Edits += ev.Edits
		}
	}

	for _, c := range byCmd {
		s.Commands = append(s.Commands, *c)
	}
	sort.Slice(s.Commands, func(i, j int) bool {
		if s.Commands[i].Runs != s.Commands[j].Runs {
			return s.Commands[i].Runs > s.Commands[j].Runs
		}
		return s.Commands[i].Name < s.Commands[j].Name
	})
	s.Langs = topCounts(langs, 0)
	s.Errors = topCounts(errs, 0)
	s.Args = topCounts(args, 0)
	return s
}

func topCounts(m map[string]int, limit int) []CountStat {
	out := make([]CountStat, 0, len(m))
	for k, v := range m {
		if k == "" {
			continue
		}
		out = append(out, CountStat{Name: k, Count: v})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Count != out[j].Count {
			return out[i].Count > out[j].Count
		}
		return out[i].Name < out[j].Name
	})
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out
}

// FormatText renders a Summary for the terminal.
func FormatText(w io.Writer, s Summary) error {
	var b strings.Builder

	scope := "all projects"
	if s.Project != "" {
		scope = s.Project
	}
	window := ""
	if s.Since != nil {
		window = fmt.Sprintf(" since %s", s.Since.Format("2006-01-02"))
	}
	if s.Total == 0 {
		fmt.Fprintf(&b, "no recorded invocations for %s%s\n", scope, window)
		_, err := io.WriteString(w, b.String())
		return err
	}
	fmt.Fprintf(&b, "Refactorio usage — %s%s\n%d invocations\n\n", scope, window, s.Total)

	fmt.Fprintf(&b, "%-16s %5s %5s %5s %8s %9s\n", "command", "runs", "ok", "err", "dry-run", "lines")
	for _, c := range s.Commands {
		fmt.Fprintf(&b, "%-16s %5d %5d %5d %8d %9d\n",
			c.Name, c.Runs, c.OK, c.Err, c.DryRuns, c.LinesChanged)
	}

	if len(s.Langs) > 0 {
		b.WriteString("\nlanguage           runs\n")
		for _, l := range s.Langs {
			fmt.Fprintf(&b, "%-16s %6d\n", l.Name, l.Count)
		}
	}
	if len(s.Args) > 0 {
		b.WriteString("\nargument           uses\n")
		for _, a := range s.Args {
			fmt.Fprintf(&b, "%-16s %6d\n", a.Name, a.Count)
		}
	}
	if len(s.Errors) > 0 {
		b.WriteString("\nerror                     count\n")
		for _, e := range s.Errors {
			fmt.Fprintf(&b, "%-25s %5d\n", e.Name, e.Count)
		}
	}

	fmt.Fprintf(&b, "\ncode refactored (applied runs only)\n")
	fmt.Fprintf(&b, "  %d lines changed across %d files (%d edits)\n",
		s.Code.LinesChanged, s.Code.Files, s.Code.Edits)
	if s.Code.LinesMoved > 0 {
		fmt.Fprintf(&b, "  %d lines moved\n", s.Code.LinesMoved)
	}
	if s.Code.LinesDeleted > 0 {
		fmt.Fprintf(&b, "  %d lines deleted\n", s.Code.LinesDeleted)
	}

	_, err := io.WriteString(w, b.String())
	return err
}

// FormatJSON renders a Summary as JSON.
func FormatJSON(w io.Writer, s Summary) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(s)
}
