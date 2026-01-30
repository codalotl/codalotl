package agentsmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRead_NoFiles(t *testing.T) {
	sandbox := t.TempDir()
	cwd := filepath.Join(sandbox, "subdir")
	if err := os.MkdirAll(cwd, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	got, err := Read(sandbox, cwd)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if got != "" {
		t.Fatalf("expected empty string, got: %q", got)
	}
}

func TestRead_EmptyFilesAreIgnored(t *testing.T) {
	sandbox := t.TempDir()
	cwd := filepath.Join(sandbox, "subdir")
	if err := os.MkdirAll(cwd, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(sandbox, agentsFilename), []byte(" \n\t\n"), 0o644); err != nil {
		t.Fatalf("write root AGENTS.md: %v", err)
	}
	if err := os.WriteFile(filepath.Join(cwd, agentsFilename), []byte("\n\n"), 0o644); err != nil {
		t.Fatalf("write subdir AGENTS.md: %v", err)
	}

	got, err := Read(sandbox, cwd)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if got != "" {
		t.Fatalf("expected empty string, got: %q", got)
	}
}

func TestRead_MultipleFiles_OrderAndFormatting(t *testing.T) {
	sandbox := t.TempDir()
	cwd := filepath.Join(sandbox, "a", "b")
	if err := os.MkdirAll(cwd, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	rootText := "root instructions"
	midText := "mid instructions"
	cwdText := "cwd instructions"

	if err := os.WriteFile(filepath.Join(sandbox, agentsFilename), []byte(rootText), 0o644); err != nil {
		t.Fatalf("write root AGENTS.md: %v", err)
	}
	if err := os.WriteFile(filepath.Join(sandbox, "a", agentsFilename), []byte(midText), 0o644); err != nil {
		t.Fatalf("write mid AGENTS.md: %v", err)
	}
	if err := os.WriteFile(filepath.Join(cwd, agentsFilename), []byte(cwdText), 0o644); err != nil {
		t.Fatalf("write cwd AGENTS.md: %v", err)
	}

	got, err := Read(sandbox, cwd)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}

	if !strings.Contains(got, "The following AGENTS.md files were found") {
		t.Fatalf("missing header; got:\n%s", got)
	}

	// Ensure output is farthest-to-nearest so nearer instructions come later.
	iRoot := strings.Index(got, rootText)
	iMid := strings.Index(got, midText)
	iCwd := strings.Index(got, cwdText)
	if iRoot == -1 || iMid == -1 || iCwd == -1 {
		t.Fatalf("expected all instruction texts to be present; got:\n%s", got)
	}
	if !(iRoot < iMid && iMid < iCwd) {
		t.Fatalf("expected root < mid < cwd ordering; got indexes root=%d mid=%d cwd=%d\n%s", iRoot, iMid, iCwd, got)
	}

	// Ensure we include file paths.
	wantRootPath := filepath.Join(sandbox, agentsFilename)
	if !strings.Contains(got, "AGENTS.md found at "+wantRootPath+":") {
		t.Fatalf("missing root file path header; got:\n%s", got)
	}
}

func TestRead_CwdMustBeWithinSandbox(t *testing.T) {
	sandbox := t.TempDir()
	outside := t.TempDir()

	_, err := Read(sandbox, outside)
	if err == nil {
		t.Fatalf("expected error, got nil")
	}
}

func TestRead_SandboxEqualsCwd(t *testing.T) {
	sandbox := t.TempDir()
	if err := os.WriteFile(filepath.Join(sandbox, agentsFilename), []byte("hi"), 0o644); err != nil {
		t.Fatalf("write AGENTS.md: %v", err)
	}

	got, err := Read(sandbox, sandbox)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if !strings.Contains(got, "hi") {
		t.Fatalf("expected file contents, got:\n%s", got)
	}
}
