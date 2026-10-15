package history

import (
	"crypto/sha256"
	"encoding/gob"
	"encoding/hex"
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

type Changeset struct {
	ID          int
	Timestamp   time.Time
	Command     string
	ProjectRoot string
	Originals   map[string][]byte
	Moves       []edit.Move
	Deletes     []DeletedFile
}

type DeletedFile struct {
	Path    string
	Content []byte
}

func FindProjectRoot(start string) string {
	abs, err := filepath.Abs(start)
	if err != nil {
		return start
	}
	info, err := os.Stat(abs)
	if err == nil && !info.IsDir() {
		abs = filepath.Dir(abs)
	}
	markers := []string{
		"go.mod", "Cargo.toml",
		"settings.gradle", "settings.gradle.kts",
		"pom.xml",
		"build.gradle", "build.gradle.kts",
		"package.json", "tsconfig.json",
	}
	dir := abs
	for {
		for _, m := range markers {
			if _, err := os.Stat(filepath.Join(dir, m)); err == nil {
				return dir
			}
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return abs
		}
		dir = parent
	}
}

func projectDir(projectRoot string) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	h := sha256.Sum256([]byte(projectRoot))
	hash := hex.EncodeToString(h[:])[:12]
	dir := filepath.Join(home, ".refactorio", "projects", hash, "history")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	pathFile := filepath.Join(filepath.Dir(dir), "path.txt")
	if _, err := os.Stat(pathFile); err != nil {
		_ = os.WriteFile(pathFile, []byte(projectRoot+"\n"), 0o644)
	}
	return dir, nil
}

func Snapshot(set *edit.Set, command, projectRoot string) (*Changeset, error) {
	cs := &Changeset{
		Timestamp:   time.Now(),
		Command:     command,
		ProjectRoot: projectRoot,
		Originals:   map[string][]byte{},
	}
	for _, f := range set.Files() {
		src, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		cs.Originals[f] = src
	}
	for _, m := range set.Moves() {
		if _, ok := cs.Originals[m.From]; !ok {
			if src, err := os.ReadFile(m.From); err == nil {
				cs.Originals[m.From] = src
			}
		}
		cs.Moves = append(cs.Moves, m)
	}
	for _, d := range set.Deletes() {
		if src, err := os.ReadFile(d.Path); err == nil {
			cs.Deletes = append(cs.Deletes, DeletedFile{Path: d.Path, Content: src})
		}
	}
	return cs, nil
}

func Save(cs *Changeset) error {
	dir, err := projectDir(cs.ProjectRoot)
	if err != nil {
		return err
	}
	entries, _ := listEntries(dir)
	nextID := 1
	if len(entries) > 0 {
		nextID = entries[len(entries)-1] + 1
	}
	cs.ID = nextID

	path := filepath.Join(dir, fmt.Sprintf("%04d.gob", cs.ID))
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	if err := gob.NewEncoder(f).Encode(cs); err != nil {
		return err
	}

	entries = append(entries, nextID)
	if len(entries) > MaxEntries {
		for _, old := range entries[:len(entries)-MaxEntries] {
			_ = os.Remove(filepath.Join(dir, fmt.Sprintf("%04d.gob", old)))
		}
	}
	return nil
}

func Rollback(projectRoot string) (string, error) {
	dir, err := projectDir(projectRoot)
	if err != nil {
		return "", err
	}
	entries, err := listEntries(dir)
	if err != nil || len(entries) == 0 {
		return "", fmt.Errorf("no history to roll back for %s", projectRoot)
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

	for i := len(cs.Moves) - 1; i >= 0; i-- {
		m := cs.Moves[i]
		if err := os.MkdirAll(filepath.Dir(m.From), 0o755); err != nil {
			return "", fmt.Errorf("rollback move mkdir: %w", err)
		}
		if err := os.Rename(m.To, m.From); err != nil && !os.IsNotExist(err) {
			return "", fmt.Errorf("rollback move %s -> %s: %w", m.To, m.From, err)
		}
	}
	for _, d := range cs.Deletes {
		if err := os.MkdirAll(filepath.Dir(d.Path), 0o755); err != nil {
			return "", fmt.Errorf("rollback delete mkdir: %w", err)
		}
		if err := os.WriteFile(d.Path, d.Content, 0o644); err != nil {
			return "", fmt.Errorf("rollback restore %s: %w", d.Path, err)
		}
	}
	for path, content := range cs.Originals {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return "", fmt.Errorf("rollback mkdir: %w", err)
		}
		if err := os.WriteFile(path, content, 0o644); err != nil {
			return "", fmt.Errorf("rollback write %s: %w", path, err)
		}
	}

	_ = os.Remove(path)

	desc := fmt.Sprintf("rolled back: %s (from %s)", cs.Command, cs.Timestamp.Format("2006-01-02 15:04:05"))
	return desc, nil
}

func List(projectRoot string) ([]string, error) {
	dir, err := projectDir(projectRoot)
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
