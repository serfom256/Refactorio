package typescript

import (
	"testing"
)

func TestExtract_NamedImport(t *testing.T) {
	src := []byte(`import { Foo, Bar as B } from './foo';`)
	fs, _ := ExtractSymbols(src)
	if len(fs.Imports) != 1 {
		t.Fatalf("imports = %d", len(fs.Imports))
	}
	imp := fs.Imports[0]
	if imp.Path != "./foo" {
		t.Errorf("Path = %q", imp.Path)
	}
	if len(imp.Names) != 2 {
		t.Fatalf("names = %d", len(imp.Names))
	}
	if imp.Names[0].Name != "Foo" || imp.Names[0].Alias != "" {
		t.Errorf("[0] = %+v", imp.Names[0])
	}
	if imp.Names[1].Name != "Bar" || imp.Names[1].Alias != "B" {
		t.Errorf("[1] = %+v", imp.Names[1])
	}
}

func TestExtract_DefaultImport(t *testing.T) {
	fs, _ := ExtractSymbols([]byte(`import React from 'react';`))
	if len(fs.Imports) != 1 {
		t.Fatalf("imports = %d", len(fs.Imports))
	}
	if fs.Imports[0].Names[0].Name != "default" || fs.Imports[0].Names[0].Alias != "React" {
		t.Errorf("default import: %+v", fs.Imports[0].Names[0])
	}
}

func TestExtract_NamespaceImport(t *testing.T) {
	fs, _ := ExtractSymbols([]byte(`import * as path from 'path';`))
	if len(fs.Imports) != 1 {
		t.Fatalf("imports = %d", len(fs.Imports))
	}
	if fs.Imports[0].Names[0].Name != "*" || fs.Imports[0].Names[0].Alias != "path" {
		t.Errorf("namespace import: %+v", fs.Imports[0].Names[0])
	}
}

func TestExtract_SideEffectImport(t *testing.T) {
	fs, _ := ExtractSymbols([]byte(`import './polyfill';`))
	if len(fs.Imports) != 1 {
		t.Fatalf("imports = %d", len(fs.Imports))
	}
	if fs.Imports[0].Path != "./polyfill" || len(fs.Imports[0].Names) != 0 {
		t.Errorf("side-effect import: %+v", fs.Imports[0])
	}
}

func TestExtract_TypeOnlyImport(t *testing.T) {
	fs, _ := ExtractSymbols([]byte(`import type { Foo } from './types';`))
	if len(fs.Imports) != 1 || !fs.Imports[0].IsTypeOnly {
		t.Errorf("type-only import: %+v", fs.Imports)
	}
}

func TestExtract_DefaultPlusNamed(t *testing.T) {
	fs, _ := ExtractSymbols([]byte(`import React, { useState } from 'react';`))
	if len(fs.Imports) != 1 {
		t.Fatalf("imports = %d", len(fs.Imports))
	}
	if len(fs.Imports[0].Names) != 2 {
		t.Fatalf("names = %d: %+v", len(fs.Imports[0].Names), fs.Imports[0].Names)
	}
	if fs.Imports[0].Names[0].LocalName() != "React" {
		t.Errorf("default: %+v", fs.Imports[0].Names[0])
	}
	if fs.Imports[0].Names[1].LocalName() != "useState" {
		t.Errorf("named: %+v", fs.Imports[0].Names[1])
	}
}

func TestExtract_ClassDeclaration(t *testing.T) {
	fs, _ := ExtractSymbols([]byte(`export class Foo extends Bar { x = 1; }`))
	if len(fs.Declarations) != 1 {
		t.Fatalf("declarations = %d", len(fs.Declarations))
	}
	d := fs.Declarations[0]
	if d.Name != "Foo" || d.Kind != "class" || !d.IsExport {
		t.Errorf("class: %+v", d)
	}
}

func TestExtract_FunctionDeclaration(t *testing.T) {
	fs, _ := ExtractSymbols([]byte(`export function greet(name: string): string { return "hi"; }`))
	if len(fs.Declarations) != 1 {
		t.Fatalf("declarations = %d", len(fs.Declarations))
	}
	d := fs.Declarations[0]
	if d.Name != "greet" || d.Kind != "function" || !d.IsExport {
		t.Errorf("function: %+v", d)
	}
}

func TestExtract_InterfaceDeclaration(t *testing.T) {
	fs, _ := ExtractSymbols([]byte(`export interface Props { name: string; }`))
	if len(fs.Declarations) != 1 || fs.Declarations[0].Name != "Props" {
		t.Errorf("interface: %+v", fs.Declarations)
	}
}

func TestExtract_TypeAlias(t *testing.T) {
	fs, _ := ExtractSymbols([]byte(`export type ID = string | number;`))
	if len(fs.Declarations) != 1 || fs.Declarations[0].Name != "ID" || fs.Declarations[0].Kind != "type" {
		t.Errorf("type alias: %+v", fs.Declarations)
	}
}

func TestExtract_EnumDeclaration(t *testing.T) {
	fs, _ := ExtractSymbols([]byte(`export enum Color { Red, Green, Blue }`))
	if len(fs.Declarations) != 1 || fs.Declarations[0].Name != "Color" {
		t.Errorf("enum: %+v", fs.Declarations)
	}
}

func TestExtract_ConstDeclaration(t *testing.T) {
	fs, _ := ExtractSymbols([]byte(`export const API_URL = 'https://example.com';`))
	if len(fs.Declarations) != 1 || fs.Declarations[0].Name != "API_URL" || fs.Declarations[0].Kind != "const" {
		t.Errorf("const: %+v", fs.Declarations)
	}
}

func TestExtract_ExportDefault(t *testing.T) {
	fs, _ := ExtractSymbols([]byte(`export default class App {}`))
	if len(fs.Declarations) != 1 {
		t.Fatalf("declarations = %d", len(fs.Declarations))
	}
	d := fs.Declarations[0]
	if d.Name != "App" || !d.IsDefault || !d.IsExport {
		t.Errorf("export default class: %+v", d)
	}
}

func TestExtract_ReExport(t *testing.T) {
	fs, _ := ExtractSymbols([]byte(`export { Foo, Bar as Baz } from './foo';`))
	if len(fs.Exports) != 1 {
		t.Fatalf("exports = %d", len(fs.Exports))
	}
	exp := fs.Exports[0]
	if exp.From != "./foo" {
		t.Errorf("from = %q", exp.From)
	}
	if len(exp.Names) != 2 || exp.Names[0].Name != "Foo" || exp.Names[1].Alias != "Baz" {
		t.Errorf("export names: %+v", exp.Names)
	}
}

func TestExtract_ReExportStar(t *testing.T) {
	fs, _ := ExtractSymbols([]byte(`export * from './utils';`))
	if len(fs.Exports) != 1 || fs.Exports[0].From != "./utils" {
		t.Errorf("re-export star: %+v", fs.Exports)
	}
}

func TestExtract_AsyncFunction(t *testing.T) {
	fs, _ := ExtractSymbols([]byte(`export async function fetchData() { return []; }`))
	if len(fs.Declarations) != 1 || fs.Declarations[0].Name != "fetchData" {
		t.Errorf("async fn: %+v", fs.Declarations)
	}
}

func TestExtract_AbstractClass(t *testing.T) {
	fs, _ := ExtractSymbols([]byte(`export abstract class Base { abstract foo(): void; }`))
	if len(fs.Declarations) != 1 || fs.Declarations[0].Name != "Base" {
		t.Errorf("abstract class: %+v", fs.Declarations)
	}
}

func TestExtract_NameRange(t *testing.T) {
	src := []byte(`export class FooBar {}`)
	fs, _ := ExtractSymbols(src)
	r := fs.Declarations[0].NameRange
	got := string(src[r.Start:r.End])
	if got != "FooBar" {
		t.Errorf("NameRange points at %q, want FooBar", got)
	}
}

func TestExtract_ImportPathRange(t *testing.T) {
	src := []byte(`import { Foo } from './bar';`)
	fs, _ := ExtractSymbols(src)
	r := fs.Imports[0].PathRange
	got := string(src[r.Start:r.End])
	if got != "./bar" {
		t.Errorf("PathRange points at %q, want ./bar", got)
	}
}
