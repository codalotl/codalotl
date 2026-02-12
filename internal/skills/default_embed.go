package skills

import "embed"

// defaultSkillsFS contains built-in/system skills shipped with codalotl.
//
// Source tree: internal/skills/default/* Embedded path root: "default/"
//
//go:embed default
var defaultSkillsFS embed.FS
