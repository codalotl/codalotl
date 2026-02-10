package skills

import (
	_ "embed"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/codalotl/codalotl/internal/tools/authdomain"
	"gopkg.in/yaml.v3"
)

//go:embed prompt_overview.md
var promptOverviewMD string

//go:embed prompt_howto.md
var promptHowToMD string

// Skill is an Agent Skill, loaded from a skill directory containing a SKILL.md file.
type Skill struct {
	AbsDir        string // AbsDir is the dir that contains the SKILL.md file. Should not end in "/". Its last segment must match Name in valid Skills.
	Name          string
	Description   string
	License       string
	Compatibility string
	Metadata      map[string]string
	Body          string
}

// SearchPaths returns absolute directories where skills may be located.
//
// Starting in startDir (can be "" for cwd), it looks for `$DIR/.codalotl/skills`, then repeats for each parent directory up to the filesystem root. Lastly, it checks
// `~/.codalotl/skills` (where `~` resolves to the current user's home directory). This search order mirrors that of cascade's config files.
//
// A candidate `.codalotl/skills` directory is only returned if it contains at least one subdirectory (i.e., at least one potential skill dir). Empty directories,
// or directories containing only files, are ignored.
//
// If no paths are found, it returns nil. Errors are ignored.
func SearchPaths(startDir string) []string {
	// Best-effort: errors are ignored by contract.
	if startDir == "" {
		if wd, err := os.Getwd(); err == nil {
			startDir = wd
		}
	}

	// If startDir points to a file, walk from its directory.
	if startDir != "" {
		if info, err := os.Stat(startDir); err == nil && !info.IsDir() {
			startDir = filepath.Dir(startDir)
		}
	}

	if startDir == "" {
		// Couldn't determine a starting point.
		return nil
	}

	startAbs, err := filepath.Abs(startDir)
	if err != nil {
		// Fall back to the provided path if Abs fails.
		startAbs = startDir
	}

	paths := make([]string, 0, 4)
	seen := make(map[string]struct{}, 4)

	containsAnyDir := func(dir string) bool {
		entries, err := os.ReadDir(dir)
		if err != nil {
			return false
		}
		for _, e := range entries {
			if e.IsDir() {
				return true
			}
			// Treat symlinks to directories as directories. Best-effort: ignore errors.
			if e.Type()&os.ModeSymlink != 0 {
				if info, err := os.Stat(filepath.Join(dir, e.Name())); err == nil && info.IsDir() {
					return true
				}
			}
		}
		return false
	}

	addIfSkillsDir := func(dir string) {
		if dir == "" {
			return
		}
		if _, ok := seen[dir]; ok {
			return
		}
		info, err := os.Stat(dir)
		if err != nil || !info.IsDir() {
			return
		}
		if !containsAnyDir(dir) {
			return
		}
		seen[dir] = struct{}{}
		paths = append(paths, dir)
	}

	for dir := startAbs; dir != ""; {
		addIfSkillsDir(filepath.Join(dir, ".codalotl", "skills"))

		parent := filepath.Dir(dir)
		if parent == dir {
			break // reached filesystem root
		}
		dir = parent
	}

	if home, err := os.UserHomeDir(); err == nil && home != "" {
		homeAbs, err := filepath.Abs(home)
		if err != nil {
			homeAbs = home
		}
		addIfSkillsDir(filepath.Join(homeAbs, ".codalotl", "skills"))
	}

	if len(paths) == 0 {
		return nil
	}
	return paths
}

// Prompt returns a markdown string suitable to be given to the LLM that explains skills and enumerates the provided available skills.
//
// If there are no skills, Prompt returns a minimal snippet indicating that.
//
// shellToolName indicates the tool name the LLM should use to invoke skill scripts and execute shell commands.
//
// isPackageMode indicates whether the LLM is running in package-mode (no general-purpose shell tool; shell is typically only available via the skills tool).
func Prompt(skills []Skill, shellToolName string, isPackageMode bool) string {
	// Keep output deterministic regardless of input order.
	sorted := append([]Skill(nil), skills...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Name < sorted[j].Name })

	if len(sorted) == 0 {
		return "# Skills\n\nA skill is a set of local instructions stored in a SKILL.md file. No skills are available in this session.\n"
	}

	if shellToolName == "" {
		// Maintain backward-compatible wording if the caller doesn't supply a tool name.
		shellToolName = "skill_shell"
	}

	var b strings.Builder
	b.WriteString("# Skills\n\n")
	b.WriteString(strings.TrimSuffix(promptOverviewMD, "\n"))

	b.WriteString("\n\n## Available skills\n")
	for _, s := range sorted {
		b.WriteString(fmt.Sprintf("- %s: %s (file: %s)\n", s.Name, s.Description, filepath.Join(s.AbsDir, "SKILL.md")))
	}

	b.WriteString("\n## How to use skills\n\n")
	howTo := strings.TrimSuffix(promptHowToMD, "\n")
	if !isPackageMode {
		// In non-package mode, the agent typically has access to a general-purpose shell tool,
		// so avoid instructions that would imply shell use is restricted to skill-directed commands.
		howTo = strings.ReplaceAll(howTo, " Do NOT use `skill_shell` unless a skill explicitly directs you to use a shell command.", "")
	}
	howTo = strings.ReplaceAll(howTo, "skill_shell", shellToolName)
	b.WriteString(howTo)
	b.WriteByte('\n')
	return b.String()
}

// Authorize adds read grants for the provided skills' directories to authorizer. Each skill must be valid (Validate() returns nil) and unique.
//
// This is intended to allow tools like read_file / ls to access skill files even outside a sandbox or code unit jail.
func Authorize(skills []Skill, authorizer authdomain.Authorizer) error {
	if authorizer == nil {
		return errors.New("authorizer is nil")
	}
	if len(skills) == 0 {
		return nil
	}

	uniqueDirs := make(map[string]struct{}, len(skills))
	for _, s := range skills {
		if err := s.Validate(); err != nil {
			return err
		}
		uniqueDirs[s.AbsDir] = struct{}{}
	}

	dirs := make([]string, 0, len(uniqueDirs))
	for dir := range uniqueDirs {
		dirs = append(dirs, dir)
	}
	sort.Strings(dirs)

	// The grant parser supports @"..."; we always quote for deterministic behavior across platforms.
	tokens := make([]string, 0, len(dirs))
	for _, dir := range dirs {
		tokens = append(tokens, `@"`+dir+`"`)
	}

	if err := authdomain.AddGrantsFromUserMessage(authorizer, strings.Join(tokens, " ")); err != nil {
		return err
	}
	return nil
}

// LoadSkill accepts a path to either the skill dir or the SKILL.md file, and returns a Skill struct.
//
// An error is returned only if this cannot be done (ex: IO error; path not found; no SKILL.md; SKILL.md is in an invalid format; no front-matter). However, it does
// NOT return an error if the Skill can be loaded, but is in some other way invalid (e.g., Validate() would return an error).
func LoadSkill(path string) (Skill, error) {
	if path == "" {
		return Skill{}, errors.New("path is empty")
	}

	info, err := os.Stat(path)
	if err != nil {
		return Skill{}, err
	}

	var skillDir string
	var skillMDPath string

	if info.IsDir() {
		skillDir = path
		skillMDPath, err = findSkillMD(skillDir)
		if err != nil {
			return Skill{}, err
		}
	} else {
		skillMDPath = path
		skillDir = filepath.Dir(path)
	}

	absDir, err := filepath.Abs(skillDir)
	if err != nil {
		return Skill{}, err
	}

	contentBytes, err := os.ReadFile(skillMDPath)
	if err != nil {
		return Skill{}, err
	}
	content := string(contentBytes)

	frontMatter, body, err := parseFrontMatter(content)
	if err != nil {
		return Skill{}, err
	}

	var fm map[string]any
	if err := yaml.Unmarshal([]byte(frontMatter), &fm); err != nil {
		return Skill{}, fmt.Errorf("SKILL.md frontmatter is invalid YAML: %w", err)
	}
	if fm == nil {
		return Skill{}, errors.New("SKILL.md frontmatter must be a YAML mapping")
	}

	name, err := getStringField(fm, "name")
	if err != nil {
		return Skill{}, err
	}
	description, err := getStringField(fm, "description")
	if err != nil {
		return Skill{}, err
	}
	license, err := getStringField(fm, "license")
	if err != nil {
		return Skill{}, err
	}
	compatibility, err := getStringField(fm, "compatibility")
	if err != nil {
		return Skill{}, err
	}
	metadata, err := getMetadataField(fm, "metadata")
	if err != nil {
		return Skill{}, err
	}

	return Skill{
		AbsDir:        absDir,
		Name:          name,
		Description:   description,
		License:       license,
		Compatibility: compatibility,
		Metadata:      metadata,
		Body:          body,
	}, nil
}

// LoadSkills looks non-recursively in searchDirs (ex: `[]string{"/myproj/.skills"}`) for skills. It tries to load a skill in these dirs only if the subdir has a
// SKILL.md file in it (ex: `/myproj/.skills/myskill/SKILL.md`).
//
// It returns:
//   - validSkills: valid skills with no Validate() issues.
//   - invalidSkills: skills which partially loaded but fail Validate().
//   - failedSkillLoads: dirs in searchDirs (i.e., possible skills) but for which LoadSkill returned an error.
//   - fnErr: only non-nil if there was a fatal error (ex: IO error). The presence of, e.g., failedSkillLoads, does NOT by itself cause fnErr to be non-nil.
func LoadSkills(searchDirs []string) (validSkills []Skill, invalidSkills []Skill, failedSkillLoads []error, fnErr error) {
	for _, searchDir := range searchDirs {
		entries, err := os.ReadDir(searchDir)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			return nil, nil, nil, err
		}

		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}

			skillDir := filepath.Join(searchDir, entry.Name())
			if _, err := os.Stat(filepath.Join(skillDir, "SKILL.md")); err != nil {
				if errors.Is(err, os.ErrNotExist) {
					continue
				}
				return nil, nil, nil, err
			}

			s, err := LoadSkill(skillDir)
			if err != nil {
				failedSkillLoads = append(failedSkillLoads, fmt.Errorf("%s: %w", skillDir, err))
				continue
			}

			if err := s.Validate(); err != nil {
				invalidSkills = append(invalidSkills, s)
				continue
			}
			validSkills = append(validSkills, s)
		}
	}

	// Make output deterministic even if the caller passed multiple searchDirs.
	sort.Slice(validSkills, func(i, j int) bool { return validSkills[i].Name < validSkills[j].Name })
	sort.Slice(invalidSkills, func(i, j int) bool { return invalidSkills[i].Name < invalidSkills[j].Name })
	sort.Slice(failedSkillLoads, func(i, j int) bool { return failedSkillLoads[i].Error() < failedSkillLoads[j].Error() })

	return validSkills, invalidSkills, failedSkillLoads, nil
}

// FormatSkillErrors accepts invalid skills and skills that failed to load (from LoadSkills) and formats them in a nice user message that is suitable to be printed
// to stdout. The returned string will likely be formatted with `\n`, but no ANSI formats, markdown formatting, etc.
func FormatSkillErrors(invalidSkills []Skill, failedSkillLoads []error) string {
	var b strings.Builder

	writeLine := func(s string) {
		if b.Len() > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(s)
	}

	if len(invalidSkills) > 0 {
		writeLine("Invalid skills:")
		for _, s := range invalidSkills {
			err := s.Validate()
			if err == nil {
				// Shouldn't happen, but avoid printing "<nil>".
				writeLine(fmt.Sprintf("- %s (%s): invalid", s.Name, s.AbsDir))
				continue
			}
			writeLine(fmt.Sprintf("- %s (%s): %s", s.Name, s.AbsDir, err.Error()))
		}
	}

	if len(failedSkillLoads) > 0 {
		writeLine("Failed to load skills:")
		for _, err := range failedSkillLoads {
			writeLine(fmt.Sprintf("- %s", err.Error()))
		}
	}

	return b.String()
}

// Validate validates the skill and returns nil if no issues. Otherwise, it returns an error describing one or more issues.
//
// Example errors:
//   - Invalid skill name (from the spec, skill name must be: `Max 64 characters. Lowercase letters, numbers, and hyphens only. Must not start or end with a hyphen.`)
func (s Skill) Validate() error {
	var issues []string

	if s.AbsDir == "" {
		issues = append(issues, "invalid skill directory: AbsDir is empty")
	} else {
		if !filepath.IsAbs(s.AbsDir) {
			issues = append(issues, "invalid skill directory: AbsDir must be an absolute path")
		}
		// AbsDir is embedded in a quoted token for auth grants (@"..."), so disallow quotes.
		if strings.ContainsRune(s.AbsDir, '"') {
			issues = append(issues, `invalid skill directory: AbsDir must not contain '"'`)
		}
		if strings.HasSuffix(s.AbsDir, string(os.PathSeparator)) {
			issues = append(issues, "invalid skill directory: AbsDir must not end with a path separator")
		}
	}

	name := strings.TrimSpace(s.Name)
	if name == "" {
		issues = append(issues, "invalid skill name: missing or empty")
	} else {
		if utf8.RuneCountInString(name) > 64 {
			issues = append(issues, fmt.Sprintf("invalid skill name: exceeds 64 character limit (%d chars)", utf8.RuneCountInString(name)))
		}
		if name != strings.ToLower(name) {
			issues = append(issues, "invalid skill name: must be lowercase")
		}
		if strings.HasPrefix(name, "-") || strings.HasSuffix(name, "-") {
			issues = append(issues, "invalid skill name: must not start or end with a hyphen")
		}
		if strings.Contains(name, "--") {
			issues = append(issues, "invalid skill name: must not contain consecutive hyphens")
		}
		for _, r := range name {
			if r == '-' || unicode.IsLetter(r) || unicode.IsDigit(r) {
				continue
			}
			issues = append(issues, fmt.Sprintf("invalid skill name: contains invalid character %q", r))
			break
		}
	}

	desc := strings.TrimSpace(s.Description)
	if desc == "" {
		issues = append(issues, "invalid skill description: missing or empty")
	} else if utf8.RuneCountInString(desc) > 1024 {
		issues = append(issues, fmt.Sprintf("invalid skill description: exceeds 1024 character limit (%d chars)", utf8.RuneCountInString(desc)))
	}

	if s.Compatibility != "" {
		comp := strings.TrimSpace(s.Compatibility)
		if comp == "" {
			issues = append(issues, "invalid skill compatibility: must not be only whitespace")
		} else if utf8.RuneCountInString(comp) > 500 {
			issues = append(issues, fmt.Sprintf("invalid skill compatibility: exceeds 500 character limit (%d chars)", utf8.RuneCountInString(comp)))
		}
	}

	if name != "" && s.AbsDir != "" && filepath.IsAbs(s.AbsDir) && !strings.HasSuffix(s.AbsDir, string(os.PathSeparator)) {
		if filepath.Base(s.AbsDir) != name {
			issues = append(issues, fmt.Sprintf("invalid skill directory: directory name %q must match skill name %q", filepath.Base(s.AbsDir), name))
		}
	}

	if len(issues) == 0 {
		return nil
	}
	return validationError{issues: issues}
}

type validationError struct {
	issues []string
}

func (e validationError) Error() string {
	return strings.Join(e.issues, "; ")
}

func findSkillMD(skillDir string) (string, error) {
	for _, name := range []string{"SKILL.md", "skill.md"} {
		p := filepath.Join(skillDir, name)
		if _, err := os.Stat(p); err == nil {
			return p, nil
		} else if !errors.Is(err, os.ErrNotExist) {
			return "", err
		}
	}
	return "", fmt.Errorf("SKILL.md not found in %s", skillDir)
}

func parseFrontMatter(content string) (frontMatter string, body string, err error) {
	// YAML frontmatter must start on the first line.
	lines := strings.Split(content, "\n")
	if len(lines) == 0 {
		return "", "", errors.New("SKILL.md is empty")
	}

	if strings.TrimRight(lines[0], "\r") != "---" {
		return "", "", errors.New("SKILL.md must start with YAML frontmatter (---)")
	}

	end := -1
	for i := 1; i < len(lines); i++ {
		if strings.TrimRight(lines[i], "\r") == "---" {
			end = i
			break
		}
	}
	if end == -1 {
		return "", "", errors.New("SKILL.md frontmatter not properly closed with ---")
	}

	frontMatter = strings.Join(lines[1:end], "\n")
	if end+1 < len(lines) {
		body = strings.Join(lines[end+1:], "\n")
	}
	return frontMatter, body, nil
}

func getStringField(m map[string]any, key string) (string, error) {
	v, ok := m[key]
	if !ok || v == nil {
		return "", nil
	}
	s, ok := v.(string)
	if !ok {
		return "", fmt.Errorf("SKILL.md frontmatter field %q must be a string", key)
	}
	return strings.TrimSpace(s), nil
}

func getMetadataField(m map[string]any, key string) (map[string]string, error) {
	v, ok := m[key]
	if !ok || v == nil {
		return nil, nil
	}

	switch mm := v.(type) {
	case map[string]any:
		return convertMetadataMapStringAny(mm)
	case map[any]any:
		return convertMetadataMapAnyAny(mm)
	default:
		return nil, fmt.Errorf("SKILL.md frontmatter field %q must be a mapping", key)
	}
}

func convertMetadataMapStringAny(m map[string]any) (map[string]string, error) {
	out := make(map[string]string, len(m))
	for k, v := range m {
		s, err := toMetadataScalar(k, v)
		if err != nil {
			return nil, err
		}
		out[k] = s
	}
	return out, nil
}

func convertMetadataMapAnyAny(m map[any]any) (map[string]string, error) {
	out := make(map[string]string, len(m))
	for k, v := range m {
		ks, ok := k.(string)
		if !ok {
			return nil, errors.New("SKILL.md frontmatter metadata keys must be strings")
		}
		s, err := toMetadataScalar(ks, v)
		if err != nil {
			return nil, err
		}
		out[ks] = s
	}
	return out, nil
}

func toMetadataScalar(key string, v any) (string, error) {
	if v == nil {
		return "", nil
	}
	switch vv := v.(type) {
	case string:
		return vv, nil
	case int, int64, float64, bool:
		return fmt.Sprint(vv), nil
	default:
		return "", fmt.Errorf("SKILL.md frontmatter metadata value for %q must be a scalar", key)
	}
}
