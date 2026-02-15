package applypatch

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

type patchCase struct {
	name    string
	before  map[string]string
	patch   string
	want    map[string]string
	wantErr string
}

// runPatchCases executes ApplyPatch in an isolated temporary directory for each test case. Adding a new test is as simple as filling in a patchCase with the initial
// files, patch text, and the expected resulting filesystem snapshot.
func runPatchCases(t *testing.T, cases []patchCase) {
	t.Helper()
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			td := t.TempDir()

			if len(tc.before) > 0 {
				require.NoError(t, writeFiles(td, tc.before))
			}

			changes, err := ApplyPatch(td, trimLeadingNewline(tc.patch))
			if tc.wantErr != "" {
				require.Error(t, err)
				require.Contains(t, err.Error(), tc.wantErr)
				return
			}
			require.NoError(t, err)
			_ = changes

			got, err := snapshotDir(td)
			require.NoError(t, err)

			want := tc.want
			if want == nil {
				want = map[string]string{}
			}
			require.Equal(t, want, got)
		})
	}
}

func TestApplyPatch_TargetedScenarios(t *testing.T) {
	cases := []patchCase{
		{
			name: "add file",
			patch: `
*** Begin Patch
*** Add File: greetings.txt
+hello
+world
*** End Patch
`,
			want: map[string]string{"greetings.txt": "hello\nworld\n"},
		},
		{
			name: "delete ignores missing file",
			patch: `
*** Begin Patch
*** Delete File: missing.txt
*** End Patch
`,
			want: map[string]string{},
		},
		{
			name:   "delete existing file",
			before: map[string]string{"tmp/data.txt": "value\n"},
			patch: `
*** Begin Patch
*** Delete File: tmp/data.txt
*** End Patch
`,
			want: map[string]string{},
		},
		{
			name: "update with context",
			before: map[string]string{
				"app/main.go": "package main\n\nfunc hi() {\n    println(\"old\")\n}\n",
			},
			patch: `
*** Begin Patch
*** Update File: app/main.go
@@
 func hi() {
-    println("old")
+    println("new")
 }
*** End Patch
`,
			want: map[string]string{
				"app/main.go": "package main\n\nfunc hi() {\n    println(\"new\")\n}\n",
			},
		},
		{
			name:   "rename file",
			before: map[string]string{"docs/readme.txt": "hi\n"},
			patch: `
*** Begin Patch
*** Update File: docs/readme.txt
*** Move to: docs/README.md
@@
-hi
+hi there
*** End Patch
`,
			want: map[string]string{"docs/README.md": "hi there\n"},
		},
		{
			name:   "move creates directories",
			before: map[string]string{"old/name.txt": "content\n"},
			patch: `
*** Begin Patch
*** Update File: old/name.txt
*** Move to: new/path/name.txt
@@
 content
*** End Patch
`,
			want: map[string]string{"new/path/name.txt": "content\n"},
		},
		{
			name:   "no final newline directive",
			before: map[string]string{"notes.txt": "line1\nline2\n"},
			patch: `
*** Begin Patch
*** Update File: notes.txt
@@
-line2
+line-two
*** End of File
*** End Patch
`,
			want: map[string]string{"notes.txt": "line1\nline-two"},
		},
		{
			name: "context prefers exact match over indent-only",
			before: map[string]string{
				"app/main.go": "package main\n\nfunc tabs() {\n\tif cond {\n\t\trun()\n\t}\n}\n\nfunc spaces() {\n    if cond {\n        run()\n    }\n}\n",
			},
			patch: `
*** Begin Patch
*** Update File: app/main.go
@@
-    if cond {
-        run()
+    if cond {
+        runLater()
     }
*** End Patch
`,
			want: map[string]string{
				"app/main.go": "package main\n\nfunc tabs() {\n\tif cond {\n\t\trun()\n\t}\n}\n\nfunc spaces() {\n    if cond {\n        runLater()\n    }\n}\n",
			},
		},
		{
			name:   "add file fails when exists",
			before: map[string]string{"greetings.txt": "hello\n"},
			patch: `
*** Begin Patch
*** Add File: greetings.txt
+hello
*** End Patch
`,
			wantErr: "file already exists",
		},
	}

	runPatchCases(t, cases)
}

func TestApplyPatch_ReportsFileChanges(t *testing.T) {
	td := t.TempDir()

	before := map[string]string{
		"existing.txt": "keep\nold\n",
		"move.txt":     "same\n",
		"delete.txt":   "bye\n",
	}
	require.NoError(t, writeFiles(td, before))

	patch := trimLeadingNewline(`
*** Begin Patch
*** Add File: new.txt
+new
*** Update File: existing.txt
@@
-old
+new
*** Update File: move.txt
*** Move to: moved.txt
@@
 same
*** Delete File: delete.txt
*** End Patch
`)

	changes, err := ApplyPatch(td, patch)
	require.NoError(t, err)

	expected := []FileChange{
		{Path: "new.txt", Kind: FileChangeAdded},
		{Path: "existing.txt", Kind: FileChangeModified},
		{Path: "move.txt", Kind: FileChangeDeleted},
		{Path: "moved.txt", Kind: FileChangeAdded},
		{Path: "delete.txt", Kind: FileChangeDeleted},
	}
	require.Equal(t, expected, changes)

	got, err := snapshotDir(td)
	require.NoError(t, err)
	require.Equal(t, map[string]string{
		"existing.txt": "keep\nnew\n",
		"moved.txt":    "same\n",
		"new.txt":      "new\n",
	}, got)
}

func TestApplyPatch_AcceptsAbsolutePaths(t *testing.T) {
	td := t.TempDir()

	absPath := filepath.Join(td, "nested", "hello.txt")
	patch := fmt.Sprintf(`*** Begin Patch
*** Add File: %s
+hi
*** End Patch
`, filepath.ToSlash(absPath))

	changes, err := ApplyPatch(td, patch)
	require.NoError(t, err)
	require.Equal(t, []FileChange{
		{Path: "nested/hello.txt", Kind: FileChangeAdded},
	}, changes)

	data, readErr := os.ReadFile(absPath)
	require.NoError(t, readErr)
	require.Equal(t, "hi\n", string(data))
}

func TestApplyPatch_PathEscapesRoot(t *testing.T) {
	td := t.TempDir()

	outside := filepath.Join(td, "..", "escape.txt")
	patch := fmt.Sprintf(`*** Begin Patch
*** Add File: %s
+nope
*** End Patch
`, filepath.ToSlash(outside))

	_, err := ApplyPatch(td, patch)
	require.Error(t, err)
	require.Contains(t, err.Error(), "escapes working directory")
}

func TestApplyPatch_RootMustBeAbsolute(t *testing.T) {
	patch := trimLeadingNewline(`
*** Begin Patch
*** Add File: file.txt
+hi
*** End Patch
`)

	_, err := ApplyPatch("relative/root", patch)
	require.Error(t, err)
	require.Contains(t, err.Error(), "must be absolute")
}

func TestApplyPatch_Anchors(t *testing.T) {
	anchorsBefore := "package anchors\n\nfunc first() string {\n    return \"first\"\n}\n\nfunc target() string {\n    return \"old\"\n}\n\nfunc second() string {\n    return \"second\"\n}\n"
	anchorsAfterTarget := "package anchors\n\nfunc first() string {\n    return \"first\"\n}\n\nfunc target() string {\n    return \"new\"\n}\n\nfunc second() string {\n    return \"second\"\n}\n"
	anchorsAfterTrailing := "package anchors\n\nfunc first() string {\n    return \"first\"\n}\n\nfunc target() string {\n    return \"old\"\n}\n\nfunc second() string {\n    return \"second!\"\n}\n"
	indentBefore := "package anchors\n\nfunc adjust() int {\n\tvalue := compute()\n\treturn value\n}\n"
	indentAfter := "package anchors\n\nfunc adjust() int {\n\tvalue := compute() + 1\n\treturn value\n}\n"
	commentsBefore := "package anchors\n\nfunc log() {\n    // step - done\n    call()\n}\n"
	commentsAfter := "package anchors\n\nfunc log() {\n    // step - done\n    callMore()\n}\n"
	scriptBefore := "class MyClass:\n    def first(self):\n        return \"first\"\n\n    def second(self):\n        return \"old\"\n\nclass Other:\n    def second(self):\n        return \"other\"\n"
	scriptAfter := "class MyClass:\n    def first(self):\n        return \"first\"\n\n    def second(self):\n        return \"new\"\n\nclass Other:\n    def second(self):\n        return \"other\"\n"

	cases := []patchCase{
		{
			name:   "anchor zooms to function signature",
			before: map[string]string{"pkg/anchors.go": anchorsBefore},
			patch: `
*** Begin Patch
*** Update File: pkg/anchors.go
@@ func target() string {
 func target() string {
-    return "old"
+    return "new"
 }
*** End Patch
`,
			want: map[string]string{"pkg/anchors.go": anchorsAfterTarget},
		},
		{
			name:   "anchor trims trailing whitespace",
			before: map[string]string{"pkg/anchors.go": anchorsBefore},
			patch: `
*** Begin Patch
*** Update File: pkg/anchors.go
@@ func second() string {   
 func second() string {
-    return "second"
+    return "second!"
 }
*** End Patch
`,
			want: map[string]string{"pkg/anchors.go": anchorsAfterTrailing},
		},
		{
			name:   "anchor trims leading whitespace",
			before: map[string]string{"pkg/indent.go": indentBefore},
			patch:  trimLeadingNewline("\n*** Begin Patch\n*** Update File: pkg/indent.go\n@@        value := compute()\n-\tvalue := compute()\n+\tvalue := compute() + 1\n*** End Patch\n"),
			want:   map[string]string{"pkg/indent.go": indentAfter},
		},
		{
			name:   "anchor converts unicode punctuation",
			before: map[string]string{"pkg/comments.go": commentsBefore},
			patch: `
*** Begin Patch
*** Update File: pkg/comments.go
@@ // step — done
-    call()
+    callMore()
*** End Patch
`,
			want: map[string]string{"pkg/comments.go": commentsAfter},
		},
		{
			name:   "multiple anchors narrow match",
			before: map[string]string{"pkg/script.py": scriptBefore},
			patch: `
*** Begin Patch
*** Update File: pkg/script.py
@@ class MyClass:
@@     def second(self):
     def second(self):
-        return "old"
+        return "new"
*** End Patch
`,
			want: map[string]string{"pkg/script.py": scriptAfter},
		},
	}

	runPatchCases(t, cases)
}
func TestApplyPatch_ErrorScenarios(t *testing.T) {
	cases := []patchCase{
		{
			name:    "bad start",
			patch:   "*** Update File: nope\n",
			wantErr: "patch must start",
		},
		{
			name: "context mismatch",
			before: map[string]string{
				"file.txt": "alpha\nbeta\n",
			},
			patch: `
*** Begin Patch
*** Update File: file.txt
@@
-gamma
+delta
*** End Patch
`,
			wantErr: "context not found",
		},
	}
	runPatchCases(t, cases)
}

func TestApplyPatch_IntegrationCases(t *testing.T) {
	casesDir := filepath.Join("testdata", "cases")
	entries, err := os.ReadDir(casesDir)
	require.NoError(t, err)
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		name := entry.Name()
		t.Run(name, func(t *testing.T) {
			base := filepath.Join(casesDir, name)
			patchBytes, err := os.ReadFile(filepath.Join(base, "patch.txt"))
			require.NoError(t, err)

			expectErrBytes, err := os.ReadFile(filepath.Join(base, "error.txt"))
			hasErrFile := err == nil
			require.Truef(t, hasErrFile || os.IsNotExist(err), "unexpected error.txt read error: %v", err)

			beforeDir := filepath.Join(base, "before")
			afterDir := filepath.Join(base, "after")
			beforeDirAbs, err := filepath.Abs(beforeDir)
			require.NoError(t, err)
			afterDirAbs, err := filepath.Abs(afterDir)
			require.NoError(t, err)

			td := t.TempDir()
			require.NoError(t, copyTree(td, beforeDirAbs))

			_, applyErr := ApplyPatch(td, string(patchBytes))
			if hasErrFile {
				require.Error(t, applyErr)
				require.Contains(t, applyErr.Error(), strings.TrimSpace(string(expectErrBytes)))
				return
			}
			require.NoError(t, applyErr)

			got, err := snapshotDir(td)
			require.NoError(t, err)
			want, err := snapshotDir(afterDirAbs)
			require.NoError(t, err)
			require.Equal(t, want, got)
		})
	}
}
func TestCRLFPreservation(t *testing.T) {
	td := t.TempDir()

	path := filepath.FromSlash("a/b/crlf.txt")
	target := filepath.Join(td, path)
	require.NoError(t, os.MkdirAll(filepath.Dir(target), 0o777))
	require.NoError(t, os.WriteFile(target, []byte("alpha\r\nbeta\r\n"), 0o644))

	patch := trimLeadingNewline(`
*** Begin Patch
*** Update File: a/b/crlf.txt
@@
-alpha
+alpha!
 beta
*** End Patch
`)

	_, err := ApplyPatch(td, patch)
	require.NoError(t, err)

	data, err := os.ReadFile(target)
	require.NoError(t, err)
	require.Contains(t, string(data), "\r\n")
	require.Equal(t, "alpha!\r\nbeta\r\n", string(data))
}

func trimLeadingNewline(s string) string {
	return strings.TrimLeft(s, "\n")
}

func writeFiles(root string, files map[string]string) error {
	for rel, content := range files {
		abs := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(abs), 0o777); err != nil {
			return err
		}
		if err := os.WriteFile(abs, []byte(content), 0o644); err != nil {
			return err
		}
	}
	return nil
}

func snapshotDir(root string) (map[string]string, error) {
	snapshot := make(map[string]string)
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		snapshot[filepath.ToSlash(rel)] = string(data)
		return nil
	})
	return snapshot, err
}

// copyTree mirrors the contents of src into dst. If src does not exist the copy is a no-op.
func copyTree(dst, src string) error {
	info, err := os.Stat(src)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if !info.IsDir() {
		return fmt.Errorf("source %s is not a directory", src)
	}
	return filepath.WalkDir(src, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o777)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o777); err != nil {
			return err
		}
		return os.WriteFile(target, data, 0o644)
	})
}
