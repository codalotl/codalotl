package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRun_Help(t *testing.T) {
	var out bytes.Buffer
	var errOut bytes.Buffer
	code, err := Run([]string{"codalotl", "-h"}, &RunOptions{Out: &out, Err: &errOut})
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if code != 0 {
		t.Fatalf("expected exit code 0, got %d", code)
	}
	if out.Len() == 0 {
		t.Fatalf("expected help output on stdout")
	}
	if errOut.Len() != 0 {
		t.Fatalf("expected empty stderr, got: %q", errOut.String())
	}
}

func TestRun_ContextPublic_MissingArg_IsUsageError(t *testing.T) {
	var out bytes.Buffer
	var errOut bytes.Buffer
	code, err := Run([]string{"codalotl", "context", "public"}, &RunOptions{Out: &out, Err: &errOut})
	if err == nil {
		t.Fatalf("expected non-nil error")
	}
	if code != 2 {
		t.Fatalf("expected exit code 2, got %d (err=%v)", code, err)
	}
	if errOut.Len() == 0 {
		t.Fatalf("expected stderr output for usage error")
	}
}

func TestRun_ContextPublic_WritesDocs(t *testing.T) {
	tmp := t.TempDir()

	// Create a tiny module with one package.
	if err := os.WriteFile(filepath.Join(tmp, "go.mod"), []byte("module example.com/tmpmod\n\ngo 1.22\n"), 0644); err != nil {
		t.Fatalf("write go.mod: %v", err)
	}
	pkgDir := filepath.Join(tmp, "p")
	if err := os.MkdirAll(pkgDir, 0755); err != nil {
		t.Fatalf("mkdir pkg: %v", err)
	}
	src := `package p

// Foo does a thing.
func Foo() {}
`
	if err := os.WriteFile(filepath.Join(pkgDir, "p.go"), []byte(src), 0644); err != nil {
		t.Fatalf("write p.go: %v", err)
	}

	var out bytes.Buffer
	var errOut bytes.Buffer
	code, err := Run([]string{"codalotl", "context", "public", pkgDir}, &RunOptions{Out: &out, Err: &errOut})
	if err != nil {
		t.Fatalf("expected nil error, got %v (stderr=%q)", err, errOut.String())
	}
	if code != 0 {
		t.Fatalf("expected exit code 0, got %d (stderr=%q)", code, errOut.String())
	}
	if errOut.Len() != 0 {
		t.Fatalf("expected empty stderr, got %q", errOut.String())
	}
	if !strings.Contains(out.String(), "Foo") {
		t.Fatalf("expected docs to mention Foo, got:\n%s", out.String())
	}
}

func TestRun_ContextPackages_WritesList(t *testing.T) {
	tmp := t.TempDir()

	// Create a tiny module with two packages.
	if err := os.WriteFile(filepath.Join(tmp, "go.mod"), []byte("module example.com/tmpmod\n\ngo 1.22\n"), 0644); err != nil {
		t.Fatalf("write go.mod: %v", err)
	}
	pkgPDir := filepath.Join(tmp, "p")
	if err := os.MkdirAll(pkgPDir, 0755); err != nil {
		t.Fatalf("mkdir p: %v", err)
	}
	if err := os.WriteFile(filepath.Join(pkgPDir, "p.go"), []byte("package p\n\nfunc P() {}\n"), 0644); err != nil {
		t.Fatalf("write p.go: %v", err)
	}
	pkgQDir := filepath.Join(tmp, "q")
	if err := os.MkdirAll(pkgQDir, 0755); err != nil {
		t.Fatalf("mkdir q: %v", err)
	}
	if err := os.WriteFile(filepath.Join(pkgQDir, "q.go"), []byte("package q\n\nfunc Q() {}\n"), 0644); err != nil {
		t.Fatalf("write q.go: %v", err)
	}

	origWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(tmp); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(origWD) })

	var out bytes.Buffer
	var errOut bytes.Buffer
	code, err := Run([]string{"codalotl", "context", "packages"}, &RunOptions{Out: &out, Err: &errOut})
	if err != nil {
		t.Fatalf("expected nil error, got %v (stderr=%q)", err, errOut.String())
	}
	if code != 0 {
		t.Fatalf("expected exit code 0, got %d (stderr=%q)", code, errOut.String())
	}
	if errOut.Len() != 0 {
		t.Fatalf("expected empty stderr, got %q", errOut.String())
	}
	got := out.String()
	if !strings.Contains(got, "example.com/tmpmod/p") {
		t.Fatalf("expected output to include package p, got:\n%s", got)
	}
	if !strings.Contains(got, "example.com/tmpmod/q") {
		t.Fatalf("expected output to include package q, got:\n%s", got)
	}
}

func TestRun_ContextPackages_SearchFilters(t *testing.T) {
	tmp := t.TempDir()

	// Create a tiny module with two packages.
	if err := os.WriteFile(filepath.Join(tmp, "go.mod"), []byte("module example.com/tmpmod\n\ngo 1.22\n"), 0644); err != nil {
		t.Fatalf("write go.mod: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(tmp, "p"), 0755); err != nil {
		t.Fatalf("mkdir p: %v", err)
	}
	if err := os.WriteFile(filepath.Join(tmp, "p", "p.go"), []byte("package p\n\nfunc P() {}\n"), 0644); err != nil {
		t.Fatalf("write p.go: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(tmp, "q"), 0755); err != nil {
		t.Fatalf("mkdir q: %v", err)
	}
	if err := os.WriteFile(filepath.Join(tmp, "q", "q.go"), []byte("package q\n\nfunc Q() {}\n"), 0644); err != nil {
		t.Fatalf("write q.go: %v", err)
	}

	origWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(tmp); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(origWD) })

	var out bytes.Buffer
	var errOut bytes.Buffer
	code, err := Run([]string{"codalotl", "context", "packages", "--search", "q$"}, &RunOptions{Out: &out, Err: &errOut})
	if err != nil {
		t.Fatalf("expected nil error, got %v (stderr=%q)", err, errOut.String())
	}
	if code != 0 {
		t.Fatalf("expected exit code 0, got %d (stderr=%q)", code, errOut.String())
	}
	got := out.String()
	if strings.Contains(got, "example.com/tmpmod/p") {
		t.Fatalf("expected output to omit package p, got:\n%s", got)
	}
	if !strings.Contains(got, "example.com/tmpmod/q") {
		t.Fatalf("expected output to include package q, got:\n%s", got)
	}
}

func TestRun_ContextPackages_ExtraArg_IsUsageError(t *testing.T) {
	var out bytes.Buffer
	var errOut bytes.Buffer
	code, err := Run([]string{"codalotl", "context", "packages", "nope"}, &RunOptions{Out: &out, Err: &errOut})
	if err == nil {
		t.Fatalf("expected non-nil error")
	}
	if code != 2 {
		t.Fatalf("expected exit code 2, got %d (err=%v)", code, err)
	}
	if errOut.Len() == 0 {
		t.Fatalf("expected stderr output for usage error")
	}
}
