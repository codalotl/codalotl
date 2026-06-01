package skills

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

var installDefaultMu sync.Mutex

var errDefaultSkillMismatch = errors.New("installed default skill does not match embedded contents")

// InstallDefault ensures built-in (system) skills are installed to `~/.codalotl/skills/.system`.
//
// It creates the destination directory if needed. If an installed built-in skill differs from the embedded version, it overwrites that skill dir. It must not delete
// or modify other skill dirs under `~/.codalotl/skills`.
func InstallDefault() error {
	skillsRootDir, systemSkillsDir, err := defaultInstallPaths()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(skillsRootDir, 0o755); err != nil {
		return fmt.Errorf("create skills root dir: %w", err)
	}

	if err := withDefaultInstallLock(skillsRootDir, func() error {
		if err := os.MkdirAll(systemSkillsDir, 0o755); err != nil {
			return fmt.Errorf("create system skills dir: %w", err)
		}
		if info, err := os.Stat(systemSkillsDir); err != nil {
			return fmt.Errorf("stat system skills dir: %w", err)
		} else if !info.IsDir() {
			return fmt.Errorf("system skills path is not a directory: %s", systemSkillsDir)
		}

		skillNames, err := embeddedDefaultSkillNames()
		if err != nil {
			return err
		}

		for _, skillName := range skillNames {
			destSkillDir := filepath.Join(systemSkillsDir, skillName)

			current, err := installedDefaultSkillIsCurrent(destSkillDir, skillName)
			if err != nil {
				return err
			}
			if current {
				continue
			}

			// Overwrite by skill dir name only; do not touch unrelated skill dirs.
			if err := os.RemoveAll(destSkillDir); err != nil {
				return fmt.Errorf("remove existing system skill dir %q: %w", destSkillDir, err)
			}
			if err := os.MkdirAll(destSkillDir, 0o755); err != nil {
				return fmt.Errorf("create system skill dir %q: %w", destSkillDir, err)
			}

			srcRoot := path.Join("default", skillName)
			if err := installEmbeddedDefaultSkill(srcRoot, destSkillDir); err != nil {
				return err
			}
		}

		return nil
	}); err != nil {
		return err
	}

	return nil
}

func defaultInstallPaths() (string, string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", "", fmt.Errorf("determine home directory: %w", err)
	}
	if home == "" {
		return "", "", errors.New("determine home directory: empty")
	}

	skillsRootDir := filepath.Join(home, ".codalotl", "skills")
	systemSkillsDir := filepath.Join(skillsRootDir, ".system")
	return skillsRootDir, systemSkillsDir, nil
}

func withDefaultInstallLock(skillsRootDir string, fn func() error) error {
	installDefaultMu.Lock()
	defer installDefaultMu.Unlock()

	lock, err := lockDefaultInstallFile(filepath.Join(skillsRootDir, ".system.lock"))
	if err != nil {
		return fmt.Errorf("lock default system skills: %w", err)
	}
	defer func() {
		_ = lock.Close()
	}()

	return fn()
}

// embeddedDefaultSkillNames returns the sorted names of built-in skill directories embedded under default/. It ignores non-directory entries and returns an error
// if the embedded directory cannot be read, a directory name is invalid, or no skills are embedded.
func embeddedDefaultSkillNames() ([]string, error) {
	entries, err := fs.ReadDir(defaultSkillsFS, "default")
	if err != nil {
		return nil, fmt.Errorf("read embedded default skills dir: %w", err)
	}

	var skillNames []string
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		name := e.Name()
		if name == "" {
			continue
		}
		if strings.ContainsAny(name, `/\`) {
			return nil, fmt.Errorf("invalid embedded skill dir name: %q", name)
		}
		skillNames = append(skillNames, name)
	}
	sort.Strings(skillNames)
	if len(skillNames) == 0 {
		return nil, errors.New("no embedded default skills are available")
	}
	return skillNames, nil
}

// defaultSkillManifestEntry describes one relative path in an embedded default skill manifest.
type defaultSkillManifestEntry struct {
	IsDir    bool   // IsDir reports whether the manifest entry is a directory.
	Contents []byte // Contents contains the file bytes for non-directory entries and is nil for directories.
}

// embeddedDefaultSkillManifest builds a manifest for the embedded default skill named skillName. skillName must name a directory embedded under default/. The returned
// map is keyed by slash-separated paths relative to default/<skillName> and excludes the skill root itself. File entries include their contents; directory entries
// have nil Contents.
func embeddedDefaultSkillManifest(skillName string) (map[string]defaultSkillManifestEntry, error) {
	srcRoot := path.Join("default", skillName)
	manifest := make(map[string]defaultSkillManifestEntry)

	if err := fs.WalkDir(defaultSkillsFS, srcRoot, func(p string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if p == srcRoot {
			return nil
		}

		prefix := srcRoot + "/"
		if !strings.HasPrefix(p, prefix) {
			return fmt.Errorf("invalid embedded default skill path: %q", p)
		}
		rel := strings.TrimPrefix(p, prefix)
		if rel == "" {
			return fmt.Errorf("invalid embedded default skill path: %q", p)
		}

		entry := defaultSkillManifestEntry{IsDir: d.IsDir()}
		if !d.IsDir() {
			contents, err := defaultSkillsFS.ReadFile(p)
			if err != nil {
				return err
			}
			entry.Contents = contents
		}
		manifest[rel] = entry
		return nil
	}); err != nil {
		return nil, err
	}

	return manifest, nil
}

// installedDefaultSkillIsCurrent reports whether destSkillDir has the same entries and file contents as the embedded default skill named skillName. File modes are
// not compared. Missing paths, non-directory destinations, missing or extra entries, directory/file shape differences, and file-content differences are reported
// as not current with no error. A non-nil error means the comparison could not be completed.
func installedDefaultSkillIsCurrent(destSkillDir string, skillName string) (bool, error) {
	manifest, err := embeddedDefaultSkillManifest(skillName)
	if err != nil {
		return false, err
	}

	info, err := os.Stat(destSkillDir)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, fmt.Errorf("stat system skill dir %q: %w", destSkillDir, err)
	}
	if !info.IsDir() {
		return false, nil
	}

	seen := make(map[string]struct{}, len(manifest))
	err = filepath.WalkDir(destSkillDir, func(p string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if p == destSkillDir {
			return nil
		}

		rel, err := filepath.Rel(destSkillDir, p)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)

		expected, ok := manifest[rel]
		if !ok || expected.IsDir != d.IsDir() {
			return errDefaultSkillMismatch
		}
		if expected.IsDir {
			seen[rel] = struct{}{}
			return nil
		}

		contents, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		if !bytes.Equal(contents, expected.Contents) {
			return errDefaultSkillMismatch
		}

		seen[rel] = struct{}{}
		return nil
	})
	if err != nil {
		if errors.Is(err, errDefaultSkillMismatch) {
			return false, nil
		}
		return false, fmt.Errorf("walk system skill dir %q: %w", destSkillDir, err)
	}

	if len(seen) != len(manifest) {
		return false, nil
	}
	return true, nil
}

// installEmbeddedDefaultSkill copies all files below srcRoot in the embedded default skill filesystem into destSkillDir. srcRoot is a slash-separated embedded path,
// and destSkillDir is an operating-system path. It creates parent directories, overwrites copied files, uses mode 0755 for shebang files and 0644 for other files,
// and leaves unrelated destination files in place.
func installEmbeddedDefaultSkill(srcRoot string, destSkillDir string) error {
	// WalkDir guarantees lexicographic traversal, which helps determinism and debuggability.
	return fs.WalkDir(defaultSkillsFS, srcRoot, func(p string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			return nil
		}

		prefix := srcRoot + "/"
		if !strings.HasPrefix(p, prefix) {
			return fmt.Errorf("invalid embedded default skill file path: %q", p)
		}
		rel := strings.TrimPrefix(p, prefix)
		if rel == "" {
			return fmt.Errorf("invalid embedded default skill file path: %q", p)
		}

		contents, err := defaultSkillsFS.ReadFile(p)
		if err != nil {
			return err
		}

		destFile := filepath.Join(destSkillDir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(destFile), 0o755); err != nil {
			return fmt.Errorf("create system skill parent dir: %w", err)
		}

		mode := modeForDefaultSkillFile(contents)
		if err := os.WriteFile(destFile, contents, mode); err != nil {
			return fmt.Errorf("write system skill file %q: %w", destFile, err)
		}

		return nil
	})
}

func modeForDefaultSkillFile(contents []byte) os.FileMode {
	if bytes.HasPrefix(contents, []byte("#!")) {
		return 0o755
	}
	return 0o644
}
