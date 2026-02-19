# skills

Package skills implements support for agent skills per https://agentskills.io/home and https://github.com/agentskills/agentskills.

This repo includes a saved copy of that spec at agentskills_spec.mdx.

## Public API

```go
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

// LoadSkill accepts a path to either the skill dir or the SKILL.md file, and returns a Skill struct.
//
// An error is returned only if this cannot be done (ex: IO error; path not found; no SKILL.md; SKILL.md is in an invalid format; no front-matter). However, it does
// NOT return an error if the Skill can be loaded, but is in some other way invalid (e.g., Validate() would return an error).
func LoadSkill(path string) (Skill, error)

// LoadSkills looks non-recursively in searchDirs (ex: `[]string{"/myproj/.skills"}`) for skills. It tries to load a skill in these dirs only if the subdir has a
// SKILL.md file in it (ex: `/myproj/.skills/myskill/SKILL.md`).
//
// It returns:
//   - validSkills: valid skills with no Validate() issues.
//   - invalidSkills: skills which partially loaded but fail Validate().
//   - failedSkillLoads: dirs in searchDirs (i.e., possible skills) but for which LoadSkill returned an error.
//   - fnErr: only non-nil if there was a fatal error (ex: IO error). The presence of, e.g., failedSkillLoads, does NOT by itself cause fnErr to be non-nil.
func LoadSkills(searchDirs []string) (validSkills []Skill, invalidSkills []Skill, failedSkillLoads []error, fnErr error)

// FormatSkillErrors accepts invalid skills and skills that failed to load (from LoadSkills) and formats them in a nice user message that is suitable to be printed
// to stdout. The returned string will likely be formatted with `\n`, but no ANSI formats, markdown formatting, etc.
func FormatSkillErrors(invalidSkills []Skill, failedSkillLoads []error) string

// Validate validates the skill and returns nil if no issues. Otherwise, it returns an error describing one or more issues.
//
// Example errors:
//   - Invalid skill name (from the spec, skill name must be: `Max 64 characters. Lowercase letters, numbers, and hyphens only. Must not start or end with a hyphen.`)
func (s Skill) Validate() error

// Prompt returns a markdown string suitable to be given to the LLM that explains skills and enumerates the provided available skills.
//
// If there are no skills, Prompt returns a minimal snippet indicating that.
//
// shellToolName indicates the tool name the LLM should use to invoke skill scripts and execute shell commands.
//
// isPackageMode indicates whether the LLM is running in package-mode (no general-purpose shell tool; shell is typically only available via the skills tool).
//
// It takes this form:
//
//	# Skills
//	A skill is a...
//	## Available skills
//	- skill-1: This is skill 1 description. (file: /path/to/skill-1/SKILL.md)
//	- skill-2: This is skill 2 description. (file: /path/to/skill-2/SKILL.md)
//	## How to use skills
//	- Discovery: The list above is...
//	- Missing/blocked: If a named skill...
//
// In other words, three headers, with an overview, list of actual skills and their location, and then a guide on how to use skills with various rules and tips.
func Prompt(skills []Skill, shellToolName string, isPackageMode bool) string

// Authorize adds read grants for the provided skills' directories to authorizer. Each skill must be valid (Validate() returns nil) and unique.
//
// This is intended to allow tools like read_file / ls to access skill files even outside a sandbox or code unit jail.
func Authorize(skills []Skill, authorizer authdomain.Authorizer) error

// SearchPaths returns absolute directories where skills may be located.
//
// Starting in startDir (can be "" for cwd), it looks for `$DIR/.codalotl/skills`, then repeats for each parent directory up to the filesystem root. Lastly, it checks
// `~/.codalotl/skills` and `~/.codalotl/skills/.system` (where `~` resolves to the current user's home directory). This search order mirrors that of cascade's config
// files.
//
// A candidate `.codalotl/skills` directory is only returned if it contains at least one subdirectory (i.e., at least one potential skill dir). Empty directories,
// or directories containing only files, are ignored.
//
// If no paths are found, it returns nil. Errors are ignored.
func SearchPaths(startDir string) []string

// InstallDefault installs built-in (system) skills to `~/.codalotl/skills/.system`.
//
// It creates the destination directory if needed. It overwrites any existing skill dirs of the same name, but must not delete or modify other skill dirs under `~/.codalotl/skills`.
func InstallDefault() error
```

