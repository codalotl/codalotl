# skills

The skills package implements support for agent skills, as per https://agentskills.io/home, with github repo https://github.com/agentskills/agentskills. From that repo, I have saved their spec at agentskills_spec.mdx.

## Public API

```go

type Skill struct {
    AbsDir string // AbsDir is the dir that contains the SKILL.md file. Should not in "/". It's last segment must match Name in valid Skills.
    Name string
    Description string
    License string
    Compatibility string
    Metadata map[string]string
    Body string
}

// LoadSkill accepts a path to either the skill dir or the SKILL.md file, and returns a Skill struct.
//
// An error is returned only if this cannot be done (ex: IO error; path not found; no SKILL.md; SKILL.md is in an invalid format; no front-matter). However, it does NOT
// return an error if the Skill can be loaded, but is in some other way invalid (e.g., Validate() would return an error).
func LoadSkill(path string) (Skill, error)

// LoadSkills looks non-recursively in searchDirs (ex: `[]string{"/myproj/.skills"}`) for skills. It tries to load a skill in these dirs only if
// the subdir has a SKILL.md file in it (ex: `/myproj/.skills/myskill/SKILL.md`).
//
//  It returns:
//   - validSkills: valid skills with no Validate() issues.
//   - invalidSkills: skills which partially loaded but fail Validate().
//   - failedSkillLoads: dirs in searchDirs (i.e., possible skills) but for which LoadSkill returned an error.
//   - fnErr: only non-nil if there was a fatal error (ex: IO error). The presence of, e.g., failedSkillLoads, does NOT by itself cause fnErr to be non-nil.
func LoadSkills(searchDirs []string) (validSkills []Skill, invalidSkills []Skill, failedSkillLoads []error, fnErr error)

// FormatSkillErrors accepts invalid skills and skills that failed to load (from LoadSkills) and formats them in a nice user message that is suitable to be
// printed to stdout. The returned string will likely be formatted with `\n`, but no ANSI formats, markdown formatting, etc.
func FormatSkillErrors(invalidSkills []Skill, failedSkillLoads []error) string

// Validate validates the skill and returns nil if no issues. Otherwise, it returns an error describing one or more issues.
//
// Example errors:
//   - Invalid skill name (from the spec, skill name must be: `Max 64 characters. Lowercase letters, numbers, and hyphens only. Must not start or end with a hyphen.`)
func (s Skill) Validate() error
```

