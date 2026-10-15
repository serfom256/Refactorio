package stats

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/serfom256/refactorio/internal/edit"
)

func metrics(edits, files, lines int) edit.Metrics {
	return edit.Metrics{Edits: edits, Files: files, LinesChanged: lines}
}

func TestClassify_NeverLeaksTheRawMessage(t *testing.T) {
	err := errors.New(`no class "com.acme.secret.BillingKey" in project /Users/me/work/acme`)
	kind := classify(err)
	if kind != "symbol not found" {
		t.Errorf("kind = %q, want %q", kind, "symbol not found")
	}
	for _, leak := range []string{"BillingKey", "acme", "/Users"} {
		if strings.Contains(kind, leak) {
			t.Errorf("kind %q leaked %q", kind, leak)
		}
	}
}

func TestClassify_UnknownFallsBackToOther(t *testing.T) {
	if got := classify(errors.New("something entirely new")); got != "other" {
		t.Errorf("got %q, want other", got)
	}
}

func TestClassify_PackageNotFound(t *testing.T) {
	err := errors.New(`no files found in package "javax.persistence"`)
	if got := classify(err); got != "package not found" {
		t.Errorf("got %q, want %q", got, "package not found")
	}
}

func TestSummarise_DryRunsCountAsUsageButNotAsLines(t *testing.T) {
	evs := []Event{
		{Command: "rename", OK: true, DryRun: true, LinesChanged: 40, Files: 5},
		{Command: "rename", OK: true, LinesChanged: 40, Files: 5, Edits: 60},
		{Command: "rename", OK: false, ErrKind: "symbol not found", LinesChanged: 99},
	}
	s := Summarise(evs, "", time.Time{})

	if s.Total != 3 {
		t.Errorf("Total = %d, want 3", s.Total)
	}
	if len(s.Commands) != 1 {
		t.Fatalf("Commands = %v, want 1", s.Commands)
	}
	c := s.Commands[0]
	if c.Runs != 3 || c.OK != 2 || c.Err != 1 || c.DryRuns != 1 {
		t.Errorf("got runs=%d ok=%d err=%d dry=%d, want 3/2/1/1", c.Runs, c.OK, c.Err, c.DryRuns)
	}
	if s.Code.LinesChanged != 40 {
		t.Errorf("LinesChanged = %d, want 40", s.Code.LinesChanged)
	}
	if s.Code.Edits != 60 {
		t.Errorf("Edits = %d, want 60", s.Code.Edits)
	}
	if len(s.Errors) != 1 || s.Errors[0].Name != "symbol not found" {
		t.Errorf("Errors = %v", s.Errors)
	}
}

func TestSummarise_RanksCommandsByRuns(t *testing.T) {
	evs := []Event{
		{Command: "usages", OK: true},
		{Command: "rename", OK: true},
		{Command: "rename", OK: true},
		{Command: "rename", OK: true},
		{Command: "usages", OK: true},
	}
	s := Summarise(evs, "", time.Time{})
	if s.Commands[0].Name != "rename" || s.Commands[0].Runs != 3 {
		t.Errorf("first = %+v, want rename with 3 runs", s.Commands[0])
	}
}

func TestFilter_ByProjectAndWindow(t *testing.T) {
	now := time.Now()
	evs := []Event{
		{Command: "rename", Project: "/a", Time: now.Add(-48 * time.Hour)},
		{Command: "rename", Project: "/a", Time: now.Add(-1 * time.Hour)},
		{Command: "rename", Project: "/b", Time: now.Add(-1 * time.Hour)},
	}
	if got := Filter(evs, "/a", time.Time{}); len(got) != 2 {
		t.Errorf("project filter kept %d, want 2", len(got))
	}
	if got := Filter(evs, "", now.Add(-24*time.Hour)); len(got) != 2 {
		t.Errorf("window filter kept %d, want 2", len(got))
	}
	if got := Filter(evs, "/a", now.Add(-24*time.Hour)); len(got) != 1 {
		t.Errorf("combined filter kept %d, want 1", len(got))
	}
}

func TestParseSince(t *testing.T) {
	now := time.Date(2026, 9, 13, 12, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		in   string
		want time.Time
	}{
		{"7d", now.AddDate(0, 0, -7)},
		{"2w", now.AddDate(0, 0, -14)},
		{"12h", now.Add(-12 * time.Hour)},
		{"2026-01-02", time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)},
	} {
		got, err := parseSince(tc.in, now)
		if err != nil {
			t.Errorf("parseSince(%q) errored: %v", tc.in, err)
			continue
		}
		if !got.Equal(tc.want) {
			t.Errorf("parseSince(%q) = %v, want %v", tc.in, got, tc.want)
		}
	}
	if _, err := parseSince("soon", now); err == nil {
		t.Error("parseSince(\"soon\") should fail")
	}
}

func TestDisabled(t *testing.T) {
	for _, tc := range []struct {
		val  string
		want bool
	}{
		{"", false}, {"0", false}, {"false", false},
		{"1", true}, {"yes", true},
	} {
		t.Setenv("REFACTORIO_NO_STATS", tc.val)
		if got := Disabled(); got != tc.want {
			t.Errorf("REFACTORIO_NO_STATS=%q: Disabled() = %v, want %v", tc.val, got, tc.want)
		}
	}
}

func TestRecord_NoOpWhenDisabled(t *testing.T) {
	t.Setenv("REFACTORIO_NO_STATS", "1")
	t.Setenv("HOME", t.TempDir())
	Reset()
	Record("rename", []string{"in", "name"}, "/proj", time.Second, nil)
	if _, err := Load(); err != nil {
		t.Errorf("Load after disabled Record: %v", err)
	}
	evs, _ := Load()
	if len(evs) != 0 {
		t.Errorf("disabled Record wrote %d events", len(evs))
	}
}

func TestRecordAndLoad_RoundTrip(t *testing.T) {
	t.Setenv("REFACTORIO_NO_STATS", "")
	t.Setenv("HOME", t.TempDir())
	Reset()

	NoteContext("java", false)
	Note(metrics(12, 3, 40))
	Record("rename", []string{"in", "name", "to"}, "/proj", 250*time.Millisecond, nil)

	evs, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(evs) != 1 {
		t.Fatalf("got %d events, want 1", len(evs))
	}
	ev := evs[0]
	if ev.Command != "rename" || ev.Lang != "java" || !ev.OK {
		t.Errorf("unexpected event: %+v", ev)
	}
	if ev.LinesChanged != 40 || ev.Edits != 12 || ev.Files != 3 {
		t.Errorf("metrics not recorded: %+v", ev)
	}
	if strings.Join(ev.Args, ",") != "in,name,to" {
		t.Errorf("args = %v", ev.Args)
	}
}

func TestClassify_CoversTheCommonFailures(t *testing.T) {
	for _, tc := range []struct{ msg, want string }{
		{`no files declare package "com.foo", and nothing references it`, "package not found"},
		{`no types found in package "com.foo"`, "package not found"},
		{"no project root found (no settings.gradle / pom.xml / build.gradle above this path)", "project not detected"},
		{"refusing to delete: 2 file(s) still reference it — fix those, or re-run with force=true", "delete blocked by references"},
		{"file /x/y/Z.java not indexed", "file not indexed"},
	} {
		if got := classify(errors.New(tc.msg)); got != tc.want {
			t.Errorf("classify(%q) = %q, want %q", tc.msg, got, tc.want)
		}
	}
}
