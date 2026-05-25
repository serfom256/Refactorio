// Package history provides undo support by snapshotting file contents
// before each refactoring operation. Snapshots are stored in
// ~/.refactorio/history/ as gob-encoded files. The stack holds at most
// MaxEntries changesets; oldest are pruned on save.
package history

import (
	"encoding/gob"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/serfom256/refactorio/internal/edit"
)

const MaxEntries = 10

// Changeset is the undo record for one refactoring operation.
type Changeset struct {
	ID        int
	Timestamp time.Time
	Command   string              // the subcommand + args for display
	Originals map[string][]byte   // abs path → original file content
	Moves     []edit.Move         // applied moves (rollback reverses them)
	Deletes   []DeletedFile       // files that were deleted (rollback recreates them)
}

type DeletedFile struct {
	Path    string
	Content []byte
}

// histDir returns ~/.refactorio/history/, creating it if needed.
func histDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(home, ".refactorio", "history")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	return dir, nil
}

// Snapshot captures the current state of all files that will be
// affected by the edit set. Call this BEFORE set.Apply().
func Snapshot(set *edit.Set, command string) (*Changeset, error) {
	cs := &Changeset{
		Timestamp: time.Now(),
		Command:   command,
		Originals: map[string][]byte{},
	}

	// Capture original content of edited files.
	for _, f := range set.Files() {
		src, err := os.ReadFile(f)
		if err != nil {
			continue // file might not exist yet (insertion)
		}
		cs.Originals[f] = src
	}

	// Capture files that will be moved (we need the source content).
	for _, m := range set.Moves() {
		if _, ok := cs.Originals[m.From]; !ok {
			src, err := os.ReadFile(m.From)
			if err == nil {
				cs.Originals[m.From] = src
			}
		}
		cs.Moves = append(cs.Moves, m)
	}

	// Capture files that will be deleted.
	for _, d := range set.Deletes() {
		src, err := os.ReadFile(d.Path)
		if err == nil {
			cs.Deletes = append(cs.Deletes, DeletedFile{Path: d.Path, Content: src})
		}
	}

	return cs, nil
}

// Save writes a changeset to ~/.refactorio/history/ and prunes old
// entries beyond MaxEntries.
func Save(cs *Changeset) error {
	dir, err := histDir()
	if err != nil {
		return err
	}

	// Assign next ID.
	entries, _ := listEntries(dir)
	nextID := 1
	if len(entries) > 0 {
		nextID = entries[len(entries)-1] + 1
	}
	cs.ID = nextID

	// Write.
	path := filepath.Join(dir, fmt.Sprintf("%04d.gob", cs.ID))
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	if err := gob.NewEncoder(f).Encode(cs); err != nil {
		return err
	}

	// Prune old entries.
	entries = append(entries, nextID)
	if len(entries) > MaxEntries {
		for _, old := range entries[:len(entries)-MaxEntries] {
			_ = os.Remove(filepath.Join(dir, fmt.Sprintf("%04d.gob", old)))
		}
	}
	return nil
}

// Rollback restores the most recent changeset. Returns the changeset
// description for display. Returns an error if no history exists.
func Rollback() (string, error) {
	dir, err := histDir()
	if err != nil {
		return "", err
	}
	entries, err := listEntries(dir)
	if err != nil || len(entries) == 0 {
		return "", fmt.Errorf("no history to roll back")
	}

	latest := entries[len(entries)-1]
	path := filepath.Join(dir, fmt.Sprintf("%04d.gob", latest))

	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	var cs Changeset
	if err := gob.NewDecoder(f).Decode(&cs); err != nil {
		return "", fmt.Errorf("corrupt history entry %d: %w", latest, err)
	}

	// 1. Reverse moves (rename To → From).
	for i := len(cs.Moves) - 1; i >= 0; i-- {
		m := cs.Moves[i]
		if err := os.MkdirAll(filepath.Dir(m.From), 0o755); err != nil {
			return "", fmt.Errorf("rollback move mkdir: %w", err)
		}
		if err := os.Rename(m.To, m.From); err != nil {
			// The file might already be at From if a previous rollback
			// partially succeeded. Skip if the source (To) doesn't exist.
			if !os.IsNotExist(err) {
				return "", fmt.Errorf("rollback move %s -> %s: %w", m.To, m.From, err)
			}
		}
	}

	// 2. Restore deleted files.
	for _, d := range cs.Deletes {
		if err := os.MkdirAll(filepath.Dir(d.Path), 0o755); err != nil {
			return "", fmt.Errorf("rollback delete mkdir: %w", err)
		}
		if err := os.WriteFile(d.Path, d.Content, 0o644); err != nil {
			return "", fmt.Errorf("rollback restore %s: %w", d.Path, err)
		}
	}

	// 3. Restore original file contents.
	for path, content := range cs.Originals {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return "", fmt.Errorf("rollback mkdir: %w", err)
		}
		if err := os.WriteFile(path, content, 0o644); err != nil {
			return "", fmt.Errorf("rollback write %s: %w", path, err)
		}
	}

	// Remove the history entry.
	_ = os.Remove(filepath.Join(dir, fmt.Sprintf("%04d.gob", latest)))

	desc := fmt.Sprintf("rolled back: %s (from %s)", cs.Command, cs.Timestamp.Format("2006-01-02 15:04:05"))
	return desc, nil
}

// List returns descriptions of all stored changesets (newest last).
func List() ([]string, error) {
	dir, err := histDir()
	if err != nil {
		return nil, err
	}
	entries, err := listEntries(dir)
	if err != nil || len(entries) == 0 {
		return nil, nil
	}
	var out []string
	for _, id := range entries {
		path := filepath.Join(dir, fmt.Sprintf("%04d.gob", id))
		f, err := os.Open(path)
		if err != nil {
			continue
		}
		var cs Changeset
		if err := gob.NewDecoder(f).Decode(&cs); err != nil {
			f.Close()
			continue
		}
		f.Close()
		out = append(out, fmt.Sprintf("#%d  %s  %s  (%d files)",
			cs.ID, cs.Timestamp.Format("2006-01-02 15:04:05"), cs.Command, len(cs.Originals)))
	}
	return out, nil
}

func listEntries(dir string) ([]int, error) {
	dirEntries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var ids []int
	for _, e := range dirEntries {
		name := e.Name()
		if !strings.HasSuffix(name, ".gob") {
			continue
		}
		id, err := strconv.Atoi(strings.TrimSuffix(name, ".gob"))
		if err != nil {
			continue
		}
		ids = append(ids, id)
	}
	sort.Ints(ids)
	return ids, nil
}
