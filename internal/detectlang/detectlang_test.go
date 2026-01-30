package detectlang

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestDetectFileKnown(t *testing.T) {
	tcs := []struct {
		name     string
		filename string
		lang     Lang
	}{
		{name: "Go", filename: "main.go", lang: LangGo},
		{name: "Ruby", filename: "script.rb", lang: LangRuby},
		{name: "Python", filename: "handler.py", lang: LangPython},
		{name: "Rust", filename: "lib.rs", lang: LangRust},
		{name: "JavaScript", filename: "app.js", lang: LangJavaScript},
		{name: "TypeScript", filename: "component.tsx", lang: LangTypeScript},
		{name: "Java", filename: "App.java", lang: LangJava},
		{name: "C", filename: "main.c", lang: LangC},
		{name: "Cpp", filename: "main.cpp", lang: LangCpp},
		{name: "CSharp", filename: "Program.cs", lang: LangCSharp},
		{name: "PHP", filename: "index.php", lang: LangPHP},
		{name: "Swift", filename: "App.swift", lang: LangSwift},
		{name: "Kotlin", filename: "Main.kt", lang: LangKotlin},
		{name: "Scala", filename: "Main.scala", lang: LangScala},
		{name: "ObjectiveC", filename: "main.mm", lang: LangObjectiveC},
	}

	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			path := filepath.Join(root, tc.filename)
			mustWriteFile(t, path, "placeholder\n")

			lang, err := Detect(root, path)
			if err != nil {
				t.Fatalf("Detect returned error: %v", err)
			}
			if lang != tc.lang {
				t.Fatalf("expected %q, got %q", tc.lang, lang)
			}
		})
	}
}

func TestDetectFileUnknown(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "README.txt")
	mustWriteFile(t, path, "hello\n")

	lang, err := Detect(root, path)
	if err != nil {
		t.Fatalf("Detect returned error: %v", err)
	}
	if lang != LangUnknown {
		t.Fatalf("expected %q, got %q", LangUnknown, lang)
	}
}

func TestDetectDirectoryPlurality(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "proj")
	mustMkdir(t, target)

	mustWriteFile(t, filepath.Join(target, "main.go"), "package main\n")
	mustWriteFile(t, filepath.Join(target, "util.go"), "package main\n")
	mustWriteFile(t, filepath.Join(target, "script.rb"), "puts 'hi'\n")

	lang, err := Detect(root, target)
	if err != nil {
		t.Fatalf("Detect returned error: %v", err)
	}
	if lang != LangGo {
		t.Fatalf("expected %q, got %q", LangGo, lang)
	}
}

func TestDetectDirectoryTie(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "proj")
	mustMkdir(t, target)

	mustWriteFile(t, filepath.Join(target, "main.go"), "package main\n")
	mustWriteFile(t, filepath.Join(target, "script.rb"), "puts 'hi'\n")

	lang, err := Detect(root, target)
	if err != nil {
		t.Fatalf("Detect returned error: %v", err)
	}
	if lang != LangMultiple {
		t.Fatalf("expected %q, got %q", LangMultiple, lang)
	}
}

func TestDetectDirectoryBFSParent(t *testing.T) {
	root := t.TempDir()
	parent := filepath.Join(root, "parent")
	target := filepath.Join(parent, "target")
	mustMkdir(t, target)

	mustWriteFile(t, filepath.Join(parent, "main.go"), "package main\n")

	lang, err := Detect(root, target)
	if err != nil {
		t.Fatalf("Detect returned error: %v", err)
	}
	if lang != LangGo {
		t.Fatalf("expected %q, got %q", LangGo, lang)
	}
}

func TestDetectDirectoryBFSChild(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "target")
	child := filepath.Join(target, "child")
	mustMkdir(t, child)

	mustWriteFile(t, filepath.Join(child, "script.py"), "print('hi')\n")

	lang, err := Detect(root, target)
	if err != nil {
		t.Fatalf("Detect returned error: %v", err)
	}
	if lang != LangPython {
		t.Fatalf("expected %q, got %q", LangPython, lang)
	}
}

func TestDetectDirectoryBFSExhaust(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "target")
	other := filepath.Join(root, "other")
	mustMkdir(t, target)
	mustMkdir(t, other)

	mustWriteFile(t, filepath.Join(target, "README"), "no extension\n")
	mustWriteFile(t, filepath.Join(other, "LICENSE"), "no extension\n")

	lang, err := Detect(root, target)
	if err != nil {
		t.Fatalf("Detect returned error: %v", err)
	}
	if lang != LangUnknown {
		t.Fatalf("expected %q, got %q", LangUnknown, lang)
	}
}

func TestDetectErrors(t *testing.T) {
	root := t.TempDir()
	fileOutside := filepath.Join(t.TempDir(), "outside.go")
	mustWriteFile(t, fileOutside, "package main\n")

	if _, err := Detect("not/abs", "also/not/abs"); !errors.Is(err, errPathNotAbsolute) {
		t.Fatalf("expected errPathNotAbsolute, got %v", err)
	}

	target := filepath.Join(root, "file.go")
	mustWriteFile(t, target, "package main\n")
	if _, err := Detect(root, fileOutside); !errors.Is(err, errPathOutsideRoot) {
		t.Fatalf("expected errPathOutsideRoot, got %v", err)
	}

	rootFile := filepath.Join(root, "rootfile")
	mustWriteFile(t, rootFile, "data\n")
	if _, err := Detect(rootFile, rootFile); err == nil {
		t.Fatalf("expected error when absRootDir is not directory")
	}

	missing := filepath.Join(root, "missing.go")
	if _, err := Detect(root, missing); err == nil {
		t.Fatalf("expected error when absPath does not exist")
	}
}

func mustWriteFile(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
}

func mustMkdir(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
}
