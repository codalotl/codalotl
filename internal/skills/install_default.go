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
)

// InstallDefault installs built-in (system) skills to `~/.codalotl/skills/.system`.
//
// It creates the destination directory if needed. It overwrites any existing skill dirs of the same name, but must not delete or modify other skill dirs under `~/.codalotl/skills`.
func InstallDefault() error {
	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("determine home directory: %w", err)
	}
	if home == "" {
		return errors.New("determine home directory: empty")
	}

	systemSkillsDir := filepath.Join(home, ".codalotl", "skills", ".system")
	if err := os.MkdirAll(systemSkillsDir, 0o755); err != nil {
		return fmt.Errorf("create system skills dir: %w", err)
	}
	if info, err := os.Stat(systemSkillsDir); err != nil {
		return fmt.Errorf("stat system skills dir: %w", err)
	} else if !info.IsDir() {
		return fmt.Errorf("system skills path is not a directory: %s", systemSkillsDir)
	}

	entries, err := fs.ReadDir(defaultSkillsFS, "default")
	if err != nil {
		return fmt.Errorf("read embedded default skills dir: %w", err)
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
		// embed paths use '/', and ReadDir names should never include separators, but sanity-check.
		if strings.ContainsAny(name, `/\`) {
			return fmt.Errorf("invalid embedded skill dir name: %q", name)
		}
		skillNames = append(skillNames, name)
	}
	sort.Strings(skillNames)
	if len(skillNames) == 0 {
		return errors.New("no embedded default skills are available")
	}

	for _, skillName := range skillNames {
		destSkillDir := filepath.Join(systemSkillsDir, skillName)

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
}

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

		mode := os.FileMode(0o644)
		if bytes.HasPrefix(contents, []byte("#!")) {
			mode = 0o755
		}
		if err := os.WriteFile(destFile, contents, mode); err != nil {
			return fmt.Errorf("write system skill file %q: %w", destFile, err)
		}

		return nil
	})
}
