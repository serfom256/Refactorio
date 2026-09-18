// Package stats records how Refactorio itself is used: which commands run,
// which arguments they are given, whether they succeed, and how much code
// they rewrite.
//
// Everything stays on this machine. The log is a plain JSONL file under
// ~/.refactorio/ and nothing is ever transmitted anywhere. Set
// REFACTORIO_NO_STATS=1 to turn recording off entirely.
//
// What is deliberately NOT recorded: symbol names, file paths within the
// project, and raw error text. Argument *names* and a classified error *kind*
// answer "what gets used and what fails" just as well, without turning the
// log into a record of the code being refactored. The project root is kept,
// since stats are scoped per project and the history store already records it.
package stats

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/serfom256/refactorio/internal/edit"
	"github.com/serfom256/refactorio/internal/history"
)

// Version is stamped onto each event. Overridden at build time if desired.
var Version = "dev"

// maxLogBytes bounds the log. On exceeding it the older half is dropped, so a
// long-lived machine cannot accumulate an unbounded file.
const maxLogBytes = 8 << 20

// Event is one Refactorio invocation.
type Event struct {
	Time    time.Time `json:"ts"`
	Version string    `json:"version,omitempty"`
	Command string    `json:"command"`
	// Args holds argument NAMES only, never their values.
	Args    []string `json:"args,omitempty"`
	Lang    string   `json:"lang,omitempty"`
	Project string   `json:"project,omitempty"`
	DryRun  bool     `json:"dry_run,omitempty"`
	OK      bool     `json:"ok"`
	// ErrKind is a classified error category, never the raw message.
	ErrKind string `json:"err_kind,omitempty"`
	Millis  int64  `json:"ms"`

	Files        int `json:"files,omitempty"`
	Edits        int `json:"edits,omitempty"`
	LinesChanged int `json:"lines_changed,omitempty"`
	Moves        int `json:"moves,omitempty"`
	LinesMoved   int `json:"lines_moved,omitempty"`
	Deletes      int `json:"deletes,omitempty"`
	LinesDeleted int `json:"lines_deleted,omitempty"`
}

// A refactorio process handles exactly one command, so the metrics an
// operation discovers deep in the call stack are collected here rather than
// threaded back through every handler signature.
var (
	mu      sync.Mutex
	pending Event
)

// Note attaches the size of a planned change to the invocation in flight.
// Call it before Apply, while the files still exist to be measured.
func Note(m edit.Metrics) {
	mu.Lock()
	defer mu.Unlock()
	pending.Files = m.Files
	pending.Edits = m.Edits
	pending.LinesChanged = m.LinesChanged
	pending.Moves = m.Moves
	pending.LinesMoved = m.LinesMoved
	pending.Deletes = m.Deletes
	pending.LinesDeleted = m.LinesDeleted
}

// NoteContext records the language and dry-run status of the invocation.
func NoteContext(lang string, dryRun bool) {
	mu.Lock()
	defer mu.Unlock()
	pending.Lang = lang
	pending.DryRun = dryRun
}

// Reset clears the pending invocation. Tests use it to isolate cases.
func Reset() {
	mu.Lock()
	defer mu.Unlock()
	pending = Event{}
}

// Disabled reports whether recording is turned off.
func Disabled() bool {
	v := strings.TrimSpace(os.Getenv("REFACTORIO_NO_STATS"))
	return v != "" && v != "0" && v != "false"
}

// Record finalises the pending invocation and appends it to the log. Errors
// are swallowed: statistics must never be the reason a refactor reports
// failure.
func Record(command string, argNames []string, project string, dur time.Duration, err error) {
	if Disabled() {
		return
	}
	mu.Lock()
	ev := pending
	mu.Unlock()

	ev.Time = time.Now()
	ev.Version = Version
	ev.Command = command
	ev.Args = argNames
	ev.Project = project
	ev.Millis = dur.Milliseconds()
	ev.OK = err == nil
	if err != nil {
		ev.ErrKind = classify(err)
	}
	_ = appendEvent(ev)
}

// ProjectRoot resolves the project a path belongs to, so statistics are
// scoped the same way the rollback history is.
func ProjectRoot(start string) string {
	return history.FindProjectRoot(start)
}

// LogPath returns the location of the stats log.
func LogPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".refactorio", "stats.jsonl"), nil
}

func appendEvent(ev Event) error {
	path, err := LogPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	prune(path)

	line, err := json.Marshal(ev)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.Write(append(line, '\n'))
	return err
}

// prune drops the older half of the log once it exceeds maxLogBytes.
func prune(path string) {
	info, err := os.Stat(path)
	if err != nil || info.Size() <= maxLogBytes {
		return
	}
	src, err := os.ReadFile(path)
	if err != nil {
		return
	}
	lines := strings.Split(string(src), "\n")
	keep := lines[len(lines)/2:]
	_ = os.WriteFile(path, []byte(strings.Join(keep, "\n")), 0o644)
}

// errKinds maps a recognisable fragment of an error message to a stable
// category. Matching on fragments keeps the raw message — which can embed
// symbol names and paths — out of the log.
var errKinds = []struct{ fragment, kind string }{
	{"no files found in package", "package not found"},
	{"not indexed", "file not indexed"},
	{"in project", "symbol not found"},
	{"no declaration", "symbol not found"},
	{"unsupported file type", "unsupported file type"},
	{"unknown arguments", "unknown arguments"},
	{"unknown subcommand", "unknown subcommand"},
	{"missing required", "missing argument"},
	{"required argument", "missing argument"},
	{"no such file", "file not found"},
	{"overlapping", "overlapping edits"},
	{"nothing to roll back", "nothing to roll back"},
	{"permission denied", "permission denied"},
}

func classify(err error) string {
	msg := strings.ToLower(err.Error())
	for _, k := range errKinds {
		if strings.Contains(msg, k.fragment) {
			return k.kind
		}
	}
	return "other"
}
