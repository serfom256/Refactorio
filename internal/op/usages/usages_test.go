package usages

import (
	"bytes"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/serfom256/refactorio/internal/edit"
)

func stubRename(edits []edit.Edit) RenameFn {
	return func(inFile, name, newName, root string) (*edit.Set, error) {
		s := edit.NewSet()
		for _, e := range edits {
			s.Add(e)
		}
		return s, nil
	}
}

func TestFind_OrdersDeclarationFirst(t *testing.T) {
	tmp := t.TempDir() + "/Foo.cs"
	src := "namespace Demo;\npublic class Foo {\n    public Foo make() { return new Foo(); }\n}\n"
	if err := writeFile(t, tmp, src); err != nil {
		t.Fatal(err)
	}
	probe := stubRename([]edit.Edit{
		{File: tmp, Start: 75, End: 78, NewText: "Bar", Why: "rename type Foo -> Bar"},
		{File: tmp, Start: 29, End: 32, NewText: "Bar", Why: "rename type Foo -> Bar (declaration)"},
	})
	res, err := Find(tmp, "Foo", "", probe)
	if err != nil {
		t.Fatal(err)
	}
	if res.Total != 2 {
		t.Fatalf("total = %d, want 2", res.Total)
	}
	if res.Findings[0].Category != CatDeclaration {
		t.Errorf("first finding should be declaration, got %+v", res.Findings[0])
	}
	if res.Findings[1].Category != CatUsage {
		t.Errorf("second finding should be usage, got %+v", res.Findings[1])
	}
}

func TestFind_OverloadsAtBottom(t *testing.T) {
	tmp := t.TempDir() + "/Foo.cs"
	src := "class Foo {\n  void Bar() {}\n  void Bar(int x) {}\n  void Use() { Bar(); }\n}\n"
	if err := writeFile(t, tmp, src); err != nil {
		t.Fatal(err)
	}
	probe := stubRename([]edit.Edit{
		{File: tmp, Start: 19, End: 22, NewText: "Baz", Why: "rename method Foo.Bar -> Baz (declaration)"},
		{File: tmp, Start: 35, End: 38, NewText: "Baz", Why: "rename method Foo.Bar -> Baz (declaration)"},
		{File: tmp, Start: 64, End: 67, NewText: "Baz", Why: "rename call Foo.Bar -> Baz"},
	})
	res, _ := Find(tmp, "Bar", "", probe)
	if res.Findings[0].Category != CatDeclaration {
		t.Errorf("[0] = %v", res.Findings[0])
	}
	if res.Findings[1].Category != CatUsage {
		t.Errorf("[1] = %v", res.Findings[1])
	}
	if res.Findings[2].Category != CatOverload {
		t.Errorf("[2] = %v, want overload at the bottom", res.Findings[2])
	}
}

func TestFind_FiltersImportRewrites(t *testing.T) {
	tmp := t.TempDir() + "/Foo.cs"
	src := "using System;\nclass Foo {}\n"
	if err := writeFile(t, tmp, src); err != nil {
		t.Fatal(err)
	}
	probe := stubRename([]edit.Edit{
		{File: tmp, Start: 21, End: 24, NewText: "Bar", Why: "rename type Foo -> Bar (declaration)"},
		{File: tmp, Start: 0, End: 5, NewText: "x", Why: "update import for Foo"},
		{File: tmp, Start: 0, End: 1, NewText: "y", Why: "rename file"},
		{File: tmp, Start: 0, End: 1, NewText: "z", Why: "rewrite inline FQN for moved class"},
	})
	res, _ := Find(tmp, "Foo", "", probe)
	if res.Total != 1 {
		t.Errorf("total = %d, want 1 (import/file edits filtered): %+v", res.Total, res.Findings)
	}
}

func TestFind_LineAndColumn(t *testing.T) {
	tmp := t.TempDir() + "/x.go"
	src := "package p\n\nfunc Hello() {}\n"
	if err := writeFile(t, tmp, src); err != nil {
		t.Fatal(err)
	}
	probe := stubRename([]edit.Edit{
		{File: tmp, Start: 16, End: 21, NewText: "Greet", Why: "rename declaration Hello -> Greet"},
	})
	res, _ := Find(tmp, "Hello", "", probe)
	if res.Findings[0].Line != 3 {
		t.Errorf("line = %d, want 3", res.Findings[0].Line)
	}
	if res.Findings[0].Column != 6 {
		t.Errorf("column = %d, want 6 (1-based)", res.Findings[0].Column)
	}
}

func TestFormatText_NoFindings(t *testing.T) {
	r := &Result{Symbol: "Ghost"}
	var buf bytes.Buffer
	if err := FormatText(&buf, r); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), "no usages") {
		t.Errorf("output = %q", buf.String())
	}
}

func TestFormatText_Summary(t *testing.T) {
	r := &Result{
		Symbol: "Foo",
		Total:  2,
		Findings: []Finding{
			{File: "a.go", Line: 1, Column: 1, Category: CatDeclaration, Text: "type Foo struct {}"},
			{File: "b.go", Line: 5, Column: 10, Category: CatUsage, Text: "var x = Foo{}"},
		},
	}
	var buf bytes.Buffer
	if err := FormatText(&buf, r); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if !strings.Contains(out, "a.go:1:1") {
		t.Errorf("missing declaration loc: %q", out)
	}
	if !strings.Contains(out, "b.go:5:10") {
		t.Errorf("missing usage loc: %q", out)
	}
	if !strings.Contains(out, "(declaration)") {
		t.Errorf("missing declaration tag: %q", out)
	}
	if !strings.Contains(out, "2 usage(s)") {
		t.Errorf("missing summary: %q", out)
	}
}

func TestFormatJSON_RoundTrip(t *testing.T) {
	r := &Result{
		Symbol: "Foo",
		Total:  1,
		Findings: []Finding{
			{File: "a.go", Line: 1, Column: 1, Category: CatDeclaration, Text: "type Foo struct {}"},
		},
	}
	var buf bytes.Buffer
	if err := FormatJSON(&buf, r); err != nil {
		t.Fatal(err)
	}
	var decoded Result
	if err := json.Unmarshal(buf.Bytes(), &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.Symbol != "Foo" || decoded.Total != 1 || len(decoded.Findings) != 1 {
		t.Errorf("decoded = %+v", decoded)
	}
	if decoded.Findings[0].Category != CatDeclaration {
		t.Errorf("category = %q", decoded.Findings[0].Category)
	}
}

func writeFile(t *testing.T, path, content string) error {
	t.Helper()
	return os.WriteFile(path, []byte(content), 0o644)
}
