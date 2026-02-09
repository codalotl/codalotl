package skills

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

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
