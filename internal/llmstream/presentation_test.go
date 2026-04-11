package llmstream

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPresentationModel_CanRepresentUpdatePlanChecklist(t *testing.T) {
	presentation := Presentation{
		Behavior: CompletionBehaviorReplace,
		Summary: Line{
			Segments: []Segment{
				{Text: "Update Plan", Role: RoleAction},
			},
		},
		Body: []Block{
			Paragraph{
				Lines: []Line{
					{
						Segments: []Segment{
							{Text: "Need to align tool rendering with presenter output.", Role: RoleAccent},
						},
					},
				},
			},
			Checklist{
				Items: []ChecklistItem{
					{
						Status: ChecklistStatusCompleted,
						Line: Line{Segments: []Segment{
							{Text: "Review the existing formatting shapes", Role: RoleAccent},
						}},
					},
					{
						Status: ChecklistStatusInProgress,
						Line: Line{Segments: []Segment{
							{Text: "Add semantic block types for diffs and raw output", Role: RoleAction},
						}},
					},
					{
						Status: ChecklistStatusPending,
						Line: Line{Segments: []Segment{
							{Text: "Update consumers in a later phase", Role: RoleAccent},
						}},
					},
				},
			},
		},
	}

	require.Len(t, presentation.Body, 2)

	message, ok := presentation.Body[0].(Paragraph)
	require.True(t, ok)
	require.Len(t, message.Lines, 1)
	assert.Equal(t, "Need to align tool rendering with presenter output.", message.Lines[0].Segments[0].Text)

	checklist, ok := presentation.Body[1].(Checklist)
	require.True(t, ok)
	require.Len(t, checklist.Items, 3)
	assert.Equal(t, ChecklistStatusCompleted, checklist.Items[0].Status)
	assert.Equal(t, ChecklistStatusInProgress, checklist.Items[1].Status)
	assert.Equal(t, ChecklistStatusPending, checklist.Items[2].Status)
}

func TestPresentationModel_CanRepresentOutputBlocks(t *testing.T) {
	presentation := Presentation{
		Behavior: CompletionBehaviorReplace,
		Summary: Line{
			Segments: []Segment{
				{Text: "Ran Tests", Role: RoleAction},
				{Text: " ./internal/llmstream", Role: RoleNormal},
			},
		},
		Body: []Block{
			Output{
				Kind: OutputKindCommand,
				Lines: []string{
					"$ go test ./internal/llmstream",
					"ok  \tgithub.com/codalotl/codalotl/internal/llmstream\t0.123s",
				},
				OmittedLineCount: 4,
			},
			Output{
				Kind: OutputKindJSON,
				Lines: []string{
					"{",
					`  "field": "value"`,
					"}",
				},
			},
		},
	}

	require.Len(t, presentation.Body, 2)

	commandOutput, ok := presentation.Body[0].(Output)
	require.True(t, ok)
	assert.Equal(t, OutputKindCommand, commandOutput.Kind)
	assert.Equal(t, []string{
		"$ go test ./internal/llmstream",
		"ok  \tgithub.com/codalotl/codalotl/internal/llmstream\t0.123s",
	}, commandOutput.Lines)
	assert.Equal(t, 4, commandOutput.OmittedLineCount)

	jsonOutput, ok := presentation.Body[1].(Output)
	require.True(t, ok)
	assert.Equal(t, OutputKindJSON, jsonOutput.Kind)
	assert.Equal(t, []string{
		"{",
		`  "field": "value"`,
		"}",
	}, jsonOutput.Lines)
	assert.Zero(t, jsonOutput.OmittedLineCount)
}

func TestPresentationModel_CanRepresentDiffEdits(t *testing.T) {
	presentation := Presentation{
		Behavior: CompletionBehaviorReplace,
		Summary: Line{
			Segments: []Segment{
				{Text: "Edit", Role: RoleAction},
				{Text: " some/file.go", Role: RoleNormal},
			},
		},
		Body: []Block{
			Diff{
				Edits: []DiffEdit{
					{
						Kind:    DiffEditKindRename,
						OldPath: "some/file.go",
						NewPath: "some/other.go",
						Lines: []DiffLine{
							{Kind: DiffLineKindDelete, Text: "old line"},
							{Kind: DiffLineKindAdd, Text: "new line"},
							{Kind: DiffLineKindOmitted},
							{Kind: DiffLineKindContext, Text: "shared context"},
						},
					},
					{
						Kind:    DiffEditKindAdd,
						NewPath: "some/new_file.go",
						Lines: []DiffLine{
							{Kind: DiffLineKindAdd, Text: "package some"},
						},
					},
				},
			},
		},
	}

	require.Len(t, presentation.Body, 1)

	diff, ok := presentation.Body[0].(Diff)
	require.True(t, ok)
	require.Len(t, diff.Edits, 2)

	assert.Equal(t, DiffEditKindRename, diff.Edits[0].Kind)
	assert.Equal(t, "some/file.go", diff.Edits[0].OldPath)
	assert.Equal(t, "some/other.go", diff.Edits[0].NewPath)
	assert.Equal(t, []DiffLine{
		{Kind: DiffLineKindDelete, Text: "old line"},
		{Kind: DiffLineKindAdd, Text: "new line"},
		{Kind: DiffLineKindOmitted, Text: ""},
		{Kind: DiffLineKindContext, Text: "shared context"},
	}, diff.Edits[0].Lines)

	assert.Equal(t, DiffEditKindAdd, diff.Edits[1].Kind)
	assert.Empty(t, diff.Edits[1].OldPath)
	assert.Equal(t, "some/new_file.go", diff.Edits[1].NewPath)
	assert.Equal(t, []DiffLine{
		{Kind: DiffLineKindAdd, Text: "package some"},
	}, diff.Edits[1].Lines)
}
