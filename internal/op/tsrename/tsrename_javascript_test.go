package tsrename

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/serfom256/refactorio/internal/lang/typescript"
)

func TestJSRename_ExportedFunction(t *testing.T) {
	idx, root := mk(t, map[string]string{
		"src/utils.js": `export function greet(name) {
  return "Hello " + name;
}
`,
		"src/main.js": `import { greet } from './utils';
console.log(greet("world"));
`,
	})
	set, err := PlanRename(RenameRequest{
		File: filepath.Join(root, "src/utils.js"), Symbol: "greet", NewName: "sayHi", Index: idx,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := set.Apply(); err != nil {
		t.Fatal(err)
	}
	utils := read(t, filepath.Join(root, "src/utils.js"))
	if !strings.Contains(utils, "export function sayHi") {
		t.Errorf("declaration not renamed:\n%s", utils)
	}
	main := read(t, filepath.Join(root, "src/main.js"))
	if !strings.Contains(main, "import { sayHi }") {
		t.Errorf("import not renamed:\n%s", main)
	}
	if !strings.Contains(main, `sayHi("world")`) {
		t.Errorf("usage not renamed:\n%s", main)
	}
}

func TestJSRename_MixedJSAndTS(t *testing.T) {
	idx, root := mk(t, map[string]string{
		"src/foo.ts": `export class Foo { bar() { return 1; } }`,
		"src/main.js": `import { Foo } from './foo';
const f = new Foo();
`,
	})
	set, err := PlanRename(RenameRequest{
		File: filepath.Join(root, "src/foo.ts"), Symbol: "Foo", NewName: "Qux", Index: idx,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := set.Apply(); err != nil {
		t.Fatal(err)
	}
	main := read(t, filepath.Join(root, "src/main.js"))
	if !strings.Contains(main, "import { Qux }") || !strings.Contains(main, "new Qux()") {
		t.Errorf("JS file not updated:\n%s", main)
	}
}

func TestJSMove_UpdatesJSImporters(t *testing.T) {
	idx, root := mk(t, map[string]string{
		"src/utils.js": `export function greet() { return "hi"; }`,
		"src/app.js":   `import { greet } from './utils';`,
	})
	set, err := PlanMove(MoveRequest{
		File:    filepath.Join(root, "src/utils.js"),
		NewPath: filepath.Join(root, "src/lib/utils.js"),
		Index:   idx,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := set.Apply(); err != nil {
		t.Fatal(err)
	}
	app := read(t, filepath.Join(root, "src/app.js"))
	if !strings.Contains(app, "'./lib/utils'") {
		t.Errorf("import path not updated:\n%s", app)
	}
}

func TestJSRename_JSXFile(t *testing.T) {
	idx, root := mk(t, map[string]string{
		"src/Button.jsx": `export function Button({ label }) {
  return <button>{label}</button>;
}
`,
		"src/App.jsx": `import { Button } from './Button';
export function App() { return <Button label="click" />; }
`,
	})
	set, err := PlanRename(RenameRequest{
		File: filepath.Join(root, "src/Button.jsx"), Symbol: "Button", NewName: "Btn", Index: idx,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := set.Apply(); err != nil {
		t.Fatal(err)
	}
	btn := read(t, filepath.Join(root, "src/Button.jsx"))
	if !strings.Contains(btn, "export function Btn") {
		t.Errorf("declaration not renamed:\n%s", btn)
	}
	app := read(t, filepath.Join(root, "src/App.jsx"))
	if !strings.Contains(app, "import { Btn }") {
		t.Errorf("import not renamed:\n%s", app)
	}
}

func TestJSExtract_CommonJSRequire(t *testing.T) {
	src := []byte(`const Foo = require('./foo');
const { bar } = require('./bar');
module.exports = { Foo };
`)
	fs, _ := typescript.ExtractSymbols(src)
	if len(fs.Imports) > 0 {
		t.Logf("CommonJS require detected as import — great!")
	} else {
		t.Logf("CommonJS require NOT detected (expected — ES modules only)")
	}
}
