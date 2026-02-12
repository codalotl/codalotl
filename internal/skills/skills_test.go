package skills

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/codalotl/codalotl/internal/codeunit"
	"github.com/codalotl/codalotl/internal/tools/authdomain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadSkill_Success_DirOrFile(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "alpha")
	require.NoError(t, os.MkdirAll(dir, 0o755))

	skillMD := filepath.Join(dir, "SKILL.md")
	require.NoError(t, os.WriteFile(skillMD, []byte(strings.Join([]string{
		"---",
		"name: alpha",
		"description: Alpha skill",
		"license: MIT",
		"compatibility: Requires foo",
		"metadata:",
		"  author: me",
		"  version: \"1.0\"",
		"---",
		"# Alpha",
		"",
		"Use this skill for alpha tasks.",
		"",
	}, "\n")), 0o644))

	t.Run("load by dir", func(t *testing.T) {
		s, err := LoadSkill(dir)
		require.NoError(t, err)

		assert.True(t, filepath.IsAbs(s.AbsDir))
		assert.Equal(t, filepath.Base(s.AbsDir), s.Name)
		assert.Equal(t, "alpha", s.Name)
		assert.Equal(t, "Alpha skill", s.Description)
		assert.Equal(t, "MIT", s.License)
		assert.Equal(t, "Requires foo", s.Compatibility)
		assert.Equal(t, map[string]string{"author": "me", "version": "1.0"}, s.Metadata)
		assert.Contains(t, s.Body, "# Alpha")
		assert.Contains(t, s.Body, "Use this skill for alpha tasks.")
	})

	t.Run("load by file", func(t *testing.T) {
		s, err := LoadSkill(skillMD)
		require.NoError(t, err)
		assert.Equal(t, "alpha", s.Name)
	})
}

func TestLoadSkill_AcceptsLowercaseSkillMD(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "alpha")
	require.NoError(t, os.MkdirAll(dir, 0o755))

	skillMD := filepath.Join(dir, "skill.md")
	require.NoError(t, os.WriteFile(skillMD, []byte(strings.Join([]string{
		"---",
		"name: alpha",
		"description: Alpha skill",
		"---",
		"Body",
		"",
	}, "\n")), 0o644))

	s, err := LoadSkill(dir)
	require.NoError(t, err)
	assert.Equal(t, "alpha", s.Name)
	assert.Contains(t, s.Body, "Body")
}

func TestLoadSkill_Errors(t *testing.T) {
	t.Run("missing path", func(t *testing.T) {
		_, err := LoadSkill(filepath.Join(t.TempDir(), "nope"))
		require.Error(t, err)
	})

	t.Run("dir missing skill md", func(t *testing.T) {
		dir := filepath.Join(t.TempDir(), "alpha")
		require.NoError(t, os.MkdirAll(dir, 0o755))
		_, err := LoadSkill(dir)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "SKILL.md not found")
	})

	t.Run("no frontmatter", func(t *testing.T) {
		dir := filepath.Join(t.TempDir(), "alpha")
		require.NoError(t, os.MkdirAll(dir, 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte("nope\n"), 0o644))
		_, err := LoadSkill(dir)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "must start with YAML frontmatter")
	})

	t.Run("frontmatter not closed", func(t *testing.T) {
		dir := filepath.Join(t.TempDir(), "alpha")
		require.NoError(t, os.MkdirAll(dir, 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(strings.Join([]string{
			"---",
			"name: alpha",
			"description: Alpha skill",
			"Body",
		}, "\n")), 0o644))
		_, err := LoadSkill(dir)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "not properly closed")
	})

	t.Run("invalid YAML", func(t *testing.T) {
		dir := filepath.Join(t.TempDir(), "alpha")
		require.NoError(t, os.MkdirAll(dir, 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(strings.Join([]string{
			"---",
			"name: [",
			"description: Alpha skill",
			"---",
			"Body",
		}, "\n")), 0o644))
		_, err := LoadSkill(dir)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "invalid YAML")
	})
}

func TestSkillValidate_Table(t *testing.T) {
	tmp := t.TempDir()

	cases := []struct {
		name        string
		skill       Skill
		wantErr     bool
		contains    []string
		notContains []string
	}{
		{
			name: "valid minimal",
			skill: Skill{
				AbsDir:      filepath.Join(tmp, "pdf-processing"),
				Name:        "pdf-processing",
				Description: "Extract PDFs",
			},
			wantErr: false,
		},
		{
			name: "AbsDir contains quote",
			skill: Skill{
				AbsDir:      filepath.Join(tmp, `bad"dir`, "pdf-processing"),
				Name:        "pdf-processing",
				Description: "Extract PDFs",
			},
			wantErr:  true,
			contains: []string{"AbsDir must not contain"},
		},
		{
			name: "uppercase name",
			skill: Skill{
				AbsDir:      filepath.Join(tmp, "pdf-processing"),
				Name:        "PDF-Processing",
				Description: "Extract PDFs",
			},
			wantErr:  true,
			contains: []string{"must be lowercase", "directory name"},
		},
		{
			name: "leading hyphen",
			skill: Skill{
				AbsDir:      filepath.Join(tmp, "-pdf"),
				Name:        "-pdf",
				Description: "Extract PDFs",
			},
			wantErr:  true,
			contains: []string{"must not start or end"},
		},
		{
			name: "consecutive hyphens",
			skill: Skill{
				AbsDir:      filepath.Join(tmp, "pdf--processing"),
				Name:        "pdf--processing",
				Description: "Extract PDFs",
			},
			wantErr:  true,
			contains: []string{"consecutive hyphens"},
		},
		{
			name: "invalid character",
			skill: Skill{
				AbsDir:      filepath.Join(tmp, "pdf_processing"),
				Name:        "pdf_processing",
				Description: "Extract PDFs",
			},
			wantErr:  true,
			contains: []string{"invalid character"},
		},
		{
			name: "missing description",
			skill: Skill{
				AbsDir: filepath.Join(tmp, "pdf-processing"),
				Name:   "pdf-processing",
			},
			wantErr:  true,
			contains: []string{"invalid skill description"},
		},
		{
			name: "description too long",
			skill: Skill{
				AbsDir:      filepath.Join(tmp, "pdf-processing"),
				Name:        "pdf-processing",
				Description: strings.Repeat("a", 1025),
			},
			wantErr:  true,
			contains: []string{"exceeds 1024"},
		},
		{
			name: "compatibility whitespace only",
			skill: Skill{
				AbsDir:        filepath.Join(tmp, "pdf-processing"),
				Name:          "pdf-processing",
				Description:   "Extract PDFs",
				Compatibility: "   ",
			},
			wantErr:  true,
			contains: []string{"compatibility"},
		},
		{
			name: "compatibility too long",
			skill: Skill{
				AbsDir:        filepath.Join(tmp, "pdf-processing"),
				Name:          "pdf-processing",
				Description:   "Extract PDFs",
				Compatibility: strings.Repeat("a", 501),
			},
			wantErr:  true,
			contains: []string{"exceeds 500"},
		},
		{
			name: "missing AbsDir",
			skill: Skill{
				Name:        "pdf-processing",
				Description: "Extract PDFs",
			},
			wantErr:  true,
			contains: []string{"AbsDir is empty"},
		},
		{
			name: "AbsDir must be absolute",
			skill: Skill{
				AbsDir:      "relative/pdf-processing",
				Name:        "pdf-processing",
				Description: "Extract PDFs",
			},
			wantErr:  true,
			contains: []string{"absolute"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.skill.Validate()
			if tc.wantErr {
				require.Error(t, err)
				for _, sub := range tc.contains {
					assert.Contains(t, err.Error(), sub)
				}
				for _, sub := range tc.notContains {
					assert.NotContains(t, err.Error(), sub)
				}
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestLoadSkills(t *testing.T) {
	searchDir := t.TempDir()

	mkSkill := func(name string, content string) string {
		dir := filepath.Join(searchDir, name)
		require.NoError(t, os.MkdirAll(dir, 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(content), 0o644))
		return dir
	}

	mkSkill("alpha", strings.Join([]string{
		"---",
		"name: alpha",
		"description: Alpha skill",
		"---",
		"Body",
		"",
	}, "\n"))

	mkSkill("bad", strings.Join([]string{
		"---",
		"name: bad",
		"---",
		"Body",
		"",
	}, "\n"))

	mkSkill("broken", strings.Join([]string{
		"---",
		"name: [",
		"description: Broken",
		"---",
		"Body",
		"",
	}, "\n"))

	require.NoError(t, os.MkdirAll(filepath.Join(searchDir, "noskill"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(searchDir, "README.txt"), []byte("hi"), 0o644))

	valid, invalid, failed, fnErr := LoadSkills([]string{searchDir})
	require.NoError(t, fnErr)

	require.Len(t, valid, 1)
	assert.Equal(t, "alpha", valid[0].Name)

	require.Len(t, invalid, 1)
	assert.Equal(t, "bad", invalid[0].Name)

	require.Len(t, failed, 1)
	assert.Contains(t, failed[0].Error(), "broken")
}

func TestLoadSkills_NonExistentSearchDirIsNotFatal(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "missing")
	valid, invalid, failed, fnErr := LoadSkills([]string{missing})
	require.NoError(t, fnErr)
	assert.Empty(t, valid)
	assert.Empty(t, invalid)
	assert.Empty(t, failed)
}

func TestFormatSkillErrors(t *testing.T) {
	tmp := t.TempDir()
	invalid := []Skill{
		{
			AbsDir: filepath.Join(tmp, "bad"),
			Name:   "bad",
		},
	}
	failed := []error{
		errors.New("some load error"),
	}

	out := FormatSkillErrors(invalid, failed)
	assert.Contains(t, out, "Invalid skills:")
	assert.Contains(t, out, "invalid skill description")
	assert.Contains(t, out, "Failed to load skills:")
	assert.Contains(t, out, "some load error")

	assert.Equal(t, "", FormatSkillErrors(nil, nil))
}

func TestPrompt_Sanity(t *testing.T) {
	tmp := t.TempDir()
	alphaDir := filepath.Join(tmp, "alpha")
	betaDir := filepath.Join(tmp, "beta")
	shellToolName := "my_shell_tool"

	out := Prompt([]Skill{
		{
			AbsDir:        betaDir,
			Name:          "beta",
			Description:   "Second skill",
			License:       "MIT",
			Metadata:      map[string]string{"x": "y"},
			Body:          "beta body",
			Compatibility: "any",
		},
		{
			AbsDir:      alphaDir,
			Name:        "alpha",
			Description: "First skill",
		},
	}, shellToolName, true)

	assert.Contains(t, out, "# Skills")
	assert.Contains(t, out, "## Available skills")
	assert.Contains(t, out, "## How to use skills")

	// Smoke-check that the embedded content is present, without pinning tests to specific wording.
	assert.Contains(t, out, strings.TrimSuffix(promptOverviewMD, "\n"))
	assert.Contains(t, out, strings.ReplaceAll(strings.TrimSuffix(promptHowToMD, "\n"), "skill_shell", shellToolName))
	assert.NotContains(t, out, "`skill_shell`")

	// Skills should be listed in sorted order and include the SKILL.md location.
	iAlpha := strings.Index(out, "- alpha: First skill (file: "+filepath.Join(alphaDir, "SKILL.md")+")")
	iBeta := strings.Index(out, "- beta: Second skill (file: "+filepath.Join(betaDir, "SKILL.md")+")")
	require.Greater(t, iAlpha, -1)
	require.Greater(t, iBeta, -1)
	assert.Less(t, iAlpha, iBeta)
}

func TestPrompt_NoSkills_Minimal(t *testing.T) {
	for _, tc := range []struct {
		name          string
		isPackageMode bool
	}{
		{name: "package mode", isPackageMode: true},
		{name: "non-package mode", isPackageMode: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out := Prompt(nil, "anything", tc.isPackageMode)
			assert.Equal(t, "# Skills\n\nA skill is a set of local instructions stored in a SKILL.md file. No skills are available in this session.\n", out)
			assert.NotContains(t, out, "## Available skills")
			assert.NotContains(t, out, "## How to use skills")
		})
	}
}

func TestPrompt_NonPackageMode_OmitsShellRestrictionSentence(t *testing.T) {
	tmp := t.TempDir()
	shellToolName := "shell"

	out := Prompt([]Skill{{
		AbsDir:      filepath.Join(tmp, "alpha"),
		Name:        "alpha",
		Description: "First skill",
	}}, shellToolName, false)

	howTo := strings.TrimSuffix(promptHowToMD, "\n")
	howTo = strings.ReplaceAll(howTo, " Do NOT use `skill_shell` unless a skill explicitly directs you to use a shell command.", "")
	howTo = strings.ReplaceAll(howTo, "skill_shell", shellToolName)

	assert.Contains(t, out, howTo)
	assert.NotContains(t, out, "Do NOT use `shell` unless a skill")
}

func TestAuthorize_CodeUnitGrantsSkillDir(t *testing.T) {
	// Create a sandbox dir with a space so we exercise the quoting path in Authorize.
	sandboxDir := filepath.Join(t.TempDir(), "sand box")
	require.NoError(t, os.MkdirAll(sandboxDir, 0o755))

	unitDir := filepath.Join(sandboxDir, "unit")
	require.NoError(t, os.MkdirAll(unitDir, 0o755))

	skillDir := filepath.Join(sandboxDir, "skills", "alpha")
	require.NoError(t, os.MkdirAll(skillDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("---\nname: alpha\ndescription: x\n---\n"), 0o644))

	fallback, _, err := authdomain.NewSandboxAuthorizer(sandboxDir, authdomain.NewShellAllowedCommands())
	require.NoError(t, err)

	unit, err := codeunit.NewCodeUnit("unit", unitDir)
	require.NoError(t, err)

	authorizer := authdomain.NewCodeUnitAuthorizer(unit, fallback)

	skillFile := filepath.Join(skillDir, "SKILL.md")

	err = authorizer.IsAuthorizedForRead(false, "", "read_file", skillFile)
	require.Error(t, err)
	assert.ErrorIs(t, err, authdomain.ErrCodeUnitPathOutside)

	require.NoError(t, Authorize([]Skill{{
		AbsDir:        skillDir,
		Name:          "alpha",
		Description:   "Alpha skill",
		License:       "MIT",
		Compatibility: "any",
	}}, authorizer))

	assert.NoError(t, authorizer.IsAuthorizedForRead(false, "", "read_file", skillFile))
	assert.NoError(t, authorizer.IsAuthorizedForRead(false, "", "ls", skillDir))

	otherDir := filepath.Join(sandboxDir, "other")
	require.NoError(t, os.MkdirAll(otherDir, 0o755))
	otherFile := filepath.Join(otherDir, "x.txt")
	require.NoError(t, os.WriteFile(otherFile, []byte("x"), 0o644))

	err = authorizer.IsAuthorizedForRead(false, "", "read_file", otherFile)
	require.Error(t, err)
	assert.ErrorIs(t, err, authdomain.ErrCodeUnitPathOutside)
}

func TestAuthorize_Errors(t *testing.T) {
	tmp := t.TempDir()
	sandbox, _, err := authdomain.NewSandboxAuthorizer(tmp, authdomain.NewShellAllowedCommands())
	require.NoError(t, err)

	t.Run("nil authorizer", func(t *testing.T) {
		require.Error(t, Authorize([]Skill{{AbsDir: tmp}}, nil))
	})

	t.Run("empty skills slice", func(t *testing.T) {
		require.NoError(t, Authorize(nil, sandbox))
	})

	t.Run("invalid skill", func(t *testing.T) {
		require.Error(t, Authorize([]Skill{{AbsDir: "", Name: "alpha", Description: "x"}}, sandbox))
	})
}

func TestSearchPaths_Basic(t *testing.T) {
	tmp := t.TempDir()

	home := filepath.Join(tmp, "home")
	require.NoError(t, os.MkdirAll(home, 0o755))
	t.Setenv("HOME", home)

	proj := filepath.Join(tmp, "proj")
	startDir := filepath.Join(proj, "a", "b")
	require.NoError(t, os.MkdirAll(startDir, 0o755))

	// Create skills dirs at two points in the parent chain, plus in the home dir.
	projSkills := filepath.Join(proj, ".codalotl", "skills")
	aSkills := filepath.Join(proj, "a", ".codalotl", "skills")
	homeSkills := filepath.Join(home, ".codalotl", "skills")

	require.NoError(t, os.MkdirAll(projSkills, 0o755))
	require.NoError(t, os.MkdirAll(aSkills, 0o755))
	require.NoError(t, os.MkdirAll(homeSkills, 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(projSkills, "alpha"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(aSkills, "beta"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(homeSkills, "gamma"), 0o755))

	want := []string{aSkills, projSkills, homeSkills}
	assert.Equal(t, want, SearchPaths(startDir))
}

func TestSearchPaths_FileStartDirIsTreatedAsItsDirectory(t *testing.T) {
	tmp := t.TempDir()

	home := filepath.Join(tmp, "home")
	require.NoError(t, os.MkdirAll(home, 0o755))
	t.Setenv("HOME", home)

	proj := filepath.Join(tmp, "proj")
	startDir := filepath.Join(proj, "a", "b")
	require.NoError(t, os.MkdirAll(startDir, 0o755))

	projSkills := filepath.Join(proj, ".codalotl", "skills")
	aSkills := filepath.Join(proj, "a", ".codalotl", "skills")
	homeSkills := filepath.Join(home, ".codalotl", "skills")

	require.NoError(t, os.MkdirAll(projSkills, 0o755))
	require.NoError(t, os.MkdirAll(aSkills, 0o755))
	require.NoError(t, os.MkdirAll(homeSkills, 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(projSkills, "alpha"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(aSkills, "beta"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(homeSkills, "gamma"), 0o755))

	f := filepath.Join(startDir, "x.txt")
	require.NoError(t, os.WriteFile(f, []byte("x"), 0o644))

	want := []string{aSkills, projSkills, homeSkills}
	assert.Equal(t, want, SearchPaths(f))
}

func TestSearchPaths_ReturnsNilWhenNoCandidatesExist(t *testing.T) {
	tmp := t.TempDir()

	home := filepath.Join(tmp, "home")
	require.NoError(t, os.MkdirAll(home, 0o755))
	t.Setenv("HOME", home)

	empty := filepath.Join(tmp, "empty")
	require.NoError(t, os.MkdirAll(empty, 0o755))
	assert.Nil(t, SearchPaths(empty))
}

func TestSearchPaths_CandidateSkillsDirMustContainAtLeastOneSubdir(t *testing.T) {
	tmp := t.TempDir()

	home := filepath.Join(tmp, "home")
	require.NoError(t, os.MkdirAll(home, 0o755))
	t.Setenv("HOME", home)

	proj := filepath.Join(tmp, "proj")
	startDir := filepath.Join(proj, "a", "b")
	require.NoError(t, os.MkdirAll(startDir, 0o755))

	projSkills := filepath.Join(proj, ".codalotl", "skills")
	aSkills := filepath.Join(proj, "a", ".codalotl", "skills")
	homeSkills := filepath.Join(home, ".codalotl", "skills")

	require.NoError(t, os.MkdirAll(projSkills, 0o755))
	require.NoError(t, os.MkdirAll(aSkills, 0o755))
	require.NoError(t, os.MkdirAll(homeSkills, 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(projSkills, "alpha"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(aSkills, "beta"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(homeSkills, "gamma"), 0o755))

	want := []string{aSkills, projSkills, homeSkills}

	// An empty .codalotl/skills under the startDir should be ignored.
	emptySkills := filepath.Join(startDir, ".codalotl", "skills")
	require.NoError(t, os.MkdirAll(emptySkills, 0o755))
	assert.Equal(t, want, SearchPaths(startDir))

	// A .codalotl/skills containing only files should also be ignored.
	require.NoError(t, os.WriteFile(filepath.Join(emptySkills, "README.txt"), []byte("x"), 0o644))
	assert.Equal(t, want, SearchPaths(startDir))

	// Once it contains a directory, it becomes a candidate and should appear first.
	require.NoError(t, os.MkdirAll(filepath.Join(emptySkills, "delta"), 0o755))
	wantWithStart := append([]string{emptySkills}, want...)
	assert.Equal(t, wantWithStart, SearchPaths(startDir))
}

func TestSearchPaths_AlsoSearchesHomeSystemSkillsDir(t *testing.T) {
	tmp := t.TempDir()

	home := filepath.Join(tmp, "home")
	require.NoError(t, os.MkdirAll(home, 0o755))
	t.Setenv("HOME", home)

	proj := filepath.Join(tmp, "proj")
	startDir := filepath.Join(proj, "a", "b")
	require.NoError(t, os.MkdirAll(startDir, 0o755))

	projSkills := filepath.Join(proj, ".codalotl", "skills")
	aSkills := filepath.Join(proj, "a", ".codalotl", "skills")
	homeSkills := filepath.Join(home, ".codalotl", "skills")
	homeSystemSkills := filepath.Join(homeSkills, ".system")

	require.NoError(t, os.MkdirAll(projSkills, 0o755))
	require.NoError(t, os.MkdirAll(aSkills, 0o755))
	require.NoError(t, os.MkdirAll(homeSkills, 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(projSkills, "alpha"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(aSkills, "beta"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(homeSkills, "gamma"), 0o755))

	want := []string{aSkills, projSkills, homeSkills}

	// Empty ~/.codalotl/skills/.system should be ignored.
	require.NoError(t, os.MkdirAll(homeSystemSkills, 0o755))
	assert.Equal(t, want, SearchPaths(startDir))

	// Once it contains a directory, it becomes a candidate and should appear after ~/.codalotl/skills.
	require.NoError(t, os.MkdirAll(filepath.Join(homeSystemSkills, "sys"), 0o755))
	wantWithSystem := append(append([]string(nil), want...), homeSystemSkills)
	assert.Equal(t, wantWithSystem, SearchPaths(startDir))
}
