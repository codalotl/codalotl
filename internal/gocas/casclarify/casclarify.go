// Package casclarify stores clarify_public_api answers in Go CAS metadata.
package casclarify

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/codalotl/codalotl/internal/gocas"
	"github.com/codalotl/codalotl/internal/gocode"
	qcas "github.com/codalotl/codalotl/internal/q/cas"
)

// NamespaceSpec stores clarify_public_api answers.
var NamespaceSpec = gocas.NamespaceSpec{
	Name:     "clarify-public-api",
	Version:  1,
	HashMode: gocas.HashModePackage,
}

// Entry is one clarification captured for a target package.
type Entry struct {
	OriginPackage string `json:"origin_package"`
	TargetPackage string `json:"target_package"`
	Identifier    string `json:"identifier"`
	Question      string `json:"question"`
	Answer        string `json:"answer"`
}

// Metadata is the stored JSON payload.
type Metadata struct {
	Entries []Entry `json:"entries"`
}

// InPlayRecord is a clarify CAS record selected for the current workstream.
type InPlayRecord struct {
	Path          string // Absolute path to the CAS record file.
	TargetPackage string // Target package import path, when known.
	Metadata      Metadata
}

// Append stores entry alongside any existing entries for pkg.
func Append(db *gocas.DB, pkg *gocode.Package, entry Entry) error {
	_, metadata, err := Retrieve(db, pkg)
	if err != nil {
		return err
	}

	metadata.Entries = append(metadata.Entries, entry)
	return db.Store(pkg, NamespaceSpec, metadata)
}

// Retrieve loads clarify metadata for pkg.
//
// found reports whether a record existed.
func Retrieve(db *gocas.DB, pkg *gocode.Package) (found bool, metadata Metadata, err error) {
	found, _, err = db.Retrieve(pkg, NamespaceSpec, &metadata)
	if err != nil {
		return false, Metadata{}, err
	}

	return found, metadata, nil
}

// FindInPlay finds clarify records relevant to the current git workstream.
func FindInPlay(db *gocas.DB, mod *gocode.Module) ([]InPlayRecord, error) {
	namespaceDir := filepath.Join(db.DB.AbsRoot, string(NamespaceSpec.Namespace()))
	gitState, ok := readGitState(mod.AbsolutePath, namespaceDir)
	if !ok {
		return nil, nil
	}

	files, err := listRecordFiles(namespaceDir)
	if err != nil {
		return nil, err
	}

	records := make([]InPlayRecord, 0, len(files))
	selected := make(map[string]struct{})
	hashes := make(map[string]currentPackageHash)
	for _, file := range files {
		rel, ok := gitState.relPath(file.Path)
		if !ok {
			continue
		}

		_, branchAdded := gitState.branchAdded[rel]
		_, worktreeChanged := gitState.worktreeChanged[rel]
		if !branchAdded && !worktreeChanged {
			continue
		}
		if _, seen := selected[file.Path]; seen {
			continue
		}

		metadata, found, err := readRecord(db, file.Hash)
		if err != nil {
			return nil, err
		}
		if !found {
			continue
		}

		targetPackage := metadataTargetPackage(metadata)
		if !branchAdded && !matchesCurrentPackageHash(db, mod, targetPackage, file.Hash, hashes) {
			continue
		}

		selected[file.Path] = struct{}{}
		records = append(records, InPlayRecord{
			Path:          file.Path,
			TargetPackage: targetPackage,
			Metadata:      metadata,
		})
	}

	sort.Slice(records, func(i, j int) bool {
		return records[i].Path < records[j].Path
	})
	return records, nil
}

// Delete removes this clarify record from disk.
func (record InPlayRecord) Delete() error {
	err := os.Remove(record.Path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}

	_ = os.Remove(filepath.Dir(record.Path))
	return nil
}

type recordFile struct {
	Path string
	Hash string
}

func listRecordFiles(namespaceDir string) ([]recordFile, error) {
	shards, err := os.ReadDir(namespaceDir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}

	var files []recordFile
	for _, shard := range shards {
		if !shard.IsDir() {
			continue
		}

		shardDir := filepath.Join(namespaceDir, shard.Name())
		entries, err := os.ReadDir(shardDir)
		if err != nil {
			return nil, err
		}
		for _, entry := range entries {
			if entry.IsDir() {
				continue
			}
			files = append(files, recordFile{
				Path: filepath.Join(shardDir, entry.Name()),
				Hash: shard.Name() + entry.Name(),
			})
		}
	}

	return files, nil
}

type gitState struct {
	repoRoot        string
	branchAdded     map[string]struct{}
	worktreeChanged map[string]struct{}
}

func readGitState(workDir, namespaceDir string) (gitState, bool) {
	repoRoot, err := gitOutput(workDir, "rev-parse", "--show-toplevel")
	if err != nil {
		return gitState{}, false
	}
	repoRoot = strings.TrimRight(repoRoot, "\r\n")

	namespaceRel, ok := relSlash(repoRoot, namespaceDir)
	if !ok {
		return gitState{}, true
	}

	statusOut, err := gitOutput(repoRoot, "status", "--porcelain=v1", "--untracked-files=all", "--", namespaceRel)
	if err != nil {
		return gitState{}, false
	}

	branchAdded := map[string]struct{}{}
	base, ok := mergeBase(repoRoot)
	if !ok {
		return gitState{}, false
	}
	if base != "" {
		addedOut, err := gitOutput(repoRoot, "diff", "--name-only", "--diff-filter=A", base+"..HEAD", "--", namespaceRel)
		if err != nil {
			return gitState{}, false
		}
		branchAdded = parsePathLines(addedOut)
	}

	return gitState{
		repoRoot:        repoRoot,
		branchAdded:     branchAdded,
		worktreeChanged: parseStatusPaths(statusOut),
	}, true
}

func (state gitState) relPath(path string) (string, bool) {
	return relSlash(state.repoRoot, path)
}

func mergeBase(repoRoot string) (string, bool) {
	currentBranch, _ := gitOutput(repoRoot, "branch", "--show-current")
	currentBranch = strings.TrimRight(currentBranch, "\r\n")

	candidates := []string{"origin/main", "origin/master", "origin/trunk", "main", "master", "trunk", "@{upstream}"}
	for _, candidate := range candidates {
		if candidate == currentBranch {
			continue
		}
		base, err := gitOutput(repoRoot, "merge-base", "HEAD", candidate)
		if err == nil {
			return strings.TrimRight(base, "\r\n"), true
		}
	}

	head, err := gitOutput(repoRoot, "rev-parse", "--verify", "HEAD")
	if err != nil {
		return "", false
	}
	return strings.TrimRight(head, "\r\n"), true
}

func gitOutput(workDir string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = workDir
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return string(out), nil
}

func parseStatusPaths(output string) map[string]struct{} {
	paths := make(map[string]struct{})
	for _, line := range strings.Split(strings.TrimSuffix(output, "\n"), "\n") {
		if len(line) < 4 {
			continue
		}
		path := line[3:]
		if renamedTo, ok := strings.CutPrefix(path, "-> "); ok {
			path = renamedTo
		} else if before, after, renamed := strings.Cut(path, " -> "); renamed && before != "" {
			path = after
		}
		paths[filepath.ToSlash(path)] = struct{}{}
	}
	return paths
}

func parsePathLines(output string) map[string]struct{} {
	paths := make(map[string]struct{})
	for _, path := range strings.Split(strings.TrimSuffix(output, "\n"), "\n") {
		if path == "" {
			continue
		}
		paths[filepath.ToSlash(path)] = struct{}{}
	}
	return paths
}

func relSlash(base, path string) (string, bool) {
	base = canonicalPath(base)
	path = canonicalPath(path)

	rel, err := filepath.Rel(base, path)
	if err != nil {
		return "", false
	}
	if rel == "." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || rel == ".." || filepath.IsAbs(rel) {
		return "", false
	}
	return filepath.ToSlash(rel), true
}

func canonicalPath(path string) string {
	abs, err := filepath.Abs(path)
	if err != nil {
		return path
	}
	resolved, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return abs
	}
	return resolved
}

type hashString string

func (h hashString) Hash() string {
	return string(h)
}

func readRecord(db *gocas.DB, hash string) (Metadata, bool, error) {
	var metadata Metadata
	found, _, err := db.DB.Retrieve(hashString(hash), string(NamespaceSpec.Namespace()), &metadata)
	if err != nil {
		return Metadata{}, false, err
	}
	return metadata, found, nil
}

func metadataTargetPackage(metadata Metadata) string {
	for _, entry := range metadata.Entries {
		if entry.TargetPackage != "" {
			return entry.TargetPackage
		}
	}
	return ""
}

type currentPackageHash struct {
	hash string
	ok   bool
}

func matchesCurrentPackageHash(db *gocas.DB, mod *gocode.Module, targetPackage, recordHash string, cache map[string]currentPackageHash) bool {
	if targetPackage == "" {
		return false
	}

	current, ok := cache[targetPackage]
	if !ok {
		current.hash, current.ok = packageCurrentHash(db, mod, targetPackage)
		cache[targetPackage] = current
	}
	return current.ok && current.hash == recordHash
}

func packageCurrentHash(db *gocas.DB, mod *gocode.Module, targetPackage string) (string, bool) {
	moduleAbsDir, _, packageRelDir, _, err := mod.ResolvePackageByImport(targetPackage)
	if err != nil {
		return "", false
	}

	moduleAbsDir, err = filepath.Abs(moduleAbsDir)
	if err != nil {
		return "", false
	}
	modAbsDir, err := filepath.Abs(mod.AbsolutePath)
	if err != nil {
		return "", false
	}
	if moduleAbsDir != modAbsDir {
		return "", false
	}

	pkg, err := mod.ReadPackage(packageRelDir, nil)
	if err != nil {
		return "", false
	}

	hasher, err := packageHasher(db, pkg)
	if err != nil {
		return "", false
	}
	return hasher.Hash(), true
}

func packageHasher(db *gocas.DB, pkg *gocode.Package) (qcas.Hasher, error) {
	paths := packagePaths(pkg)
	if pkg.TestPackage != nil {
		paths = append(paths, packagePaths(pkg.TestPackage)...)
	}

	specPath := filepath.Join(pkg.AbsolutePath(), "SPEC.md")
	_, err := os.Stat(specPath)
	if err == nil {
		paths = append(paths, specPath)
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}

	return qcas.NewDirRelativeFileSetHasher(db.BaseDir, paths)
}

func packagePaths(pkg *gocode.Package) []string {
	paths := make([]string, 0, len(pkg.Files))
	for _, file := range pkg.Files {
		paths = append(paths, file.AbsolutePath)
	}
	return paths
}
