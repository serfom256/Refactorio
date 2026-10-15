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

var Version = "dev"

const maxLogBytes = 8 << 20

type Event struct {
	Time    time.Time `json:"ts"`
	Version string    `json:"version,omitempty"`
	Command string    `json:"command"`
	Args    []string  `json:"args,omitempty"`
	Lang    string    `json:"lang,omitempty"`
	Project string    `json:"project,omitempty"`
	DryRun  bool      `json:"dry_run,omitempty"`
	OK      bool      `json:"ok"`
	ErrKind string    `json:"err_kind,omitempty"`
	Millis  int64     `json:"ms"`

	Files        int `json:"files,omitempty"`
	Edits        int `json:"edits,omitempty"`
	LinesChanged int `json:"lines_changed,omitempty"`
	Moves        int `json:"moves,omitempty"`
	LinesMoved   int `json:"lines_moved,omitempty"`
	Deletes      int `json:"deletes,omitempty"`
	LinesDeleted int `json:"lines_deleted,omitempty"`
}

var (
	mu      sync.Mutex
	pending Event
)

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

func NoteContext(lang string, dryRun bool) {
	mu.Lock()
	defer mu.Unlock()
	pending.Lang = lang
	pending.DryRun = dryRun
}

func Reset() {
	mu.Lock()
	defer mu.Unlock()
	pending = Event{}
}

func Disabled() bool {
	v := strings.TrimSpace(os.Getenv("REFACTORIO_NO_STATS"))
	return v != "" && v != "0" && v != "false"
}

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

func ProjectRoot(start string) string {
	return history.FindProjectRoot(start)
}

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

var errKinds = []struct{ fragment, kind string }{
	{"no files found in package", "package not found"},
	{"no files declare package", "package not found"},
	{"no types found in package", "package not found"},
	{"no project root found", "project not detected"},
	{"refusing to delete", "delete blocked by references"},
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
