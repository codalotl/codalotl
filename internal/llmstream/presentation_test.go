package llmstream

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPresentationModel_CanRepresentUpdatePlanChecklist(t *testing.T) {
	presentation := Presentation{
		Behavior:      CompletionBehaviorReplace,
		ErrorBehavior: ErrorBehaviorDefault,
		Summary: Line{
			Segments: []Segment{
				{Text: "Update Plan", Role: RoleAction},
			},
		},
		Body: Checklist{
			Overview: Line{
				Segments: []Segment{
					{Text: "Need to align tool rendering with presenter output.", Role: RoleAccent},
				},
			},
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
	}

	checklist, ok := presentation.Body.(Checklist)
	require.True(t, ok)
	require.Len(t, checklist.Overview.Segments, 1)
	assert.Equal(t, "Need to align tool rendering with presenter output.", checklist.Overview.Segments[0].Text)
	require.Len(t, checklist.Items, 3)
	assert.Equal(t, ChecklistStatusCompleted, checklist.Items[0].Status)
	assert.Equal(t, ChecklistStatusInProgress, checklist.Items[1].Status)
	assert.Equal(t, ChecklistStatusPending, checklist.Items[2].Status)
}

func TestPresentationModel_CanRepresentOutputBlock(t *testing.T) {
	presentation := Presentation{
		Behavior:      CompletionBehaviorReplace,
		ErrorBehavior: ErrorBehaviorDefault,
		Summary: Line{
			Segments: []Segment{
				{Text: "Ran Tests", Role: RoleAction},
				{Text: " ./internal/llmstream", Role: RoleNormal},
			},
		},
		Body: Output{
			Lines: []string{
				"$ go test ./internal/llmstream",
				"ok  \tgithub.com/codalotl/codalotl/internal/llmstream\t0.123s",
			},
			OmittedLineCount: 4,
		},
	}

	commandOutput, ok := presentation.Body.(Output)
	require.True(t, ok)
	assert.Equal(t, []string{
		"$ go test ./internal/llmstream",
		"ok  \tgithub.com/codalotl/codalotl/internal/llmstream\t0.123s",
	}, commandOutput.Lines)
	assert.Equal(t, 4, commandOutput.OmittedLineCount)
}

func TestPresentationModel_CanRepresentDiffEditsWithoutSummary(t *testing.T) {
	errorLine := Line{
		Segments: []Segment{
			{Text: "Error: patch failed", Role: RoleError},
		},
	}
	presentation := Presentation{
		Behavior:      CompletionBehaviorReplace,
		ErrorBehavior: ErrorBehaviorDefault,
		Body: Diff{
			Edits: []DiffEdit{
				{
					Kind:       DiffEditKindRename,
					OldPath:    "some/file.go",
					NewPath:    "some/other.go",
					ReplaceAll: true,
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
					Error: &errorLine,
				},
			},
		},
	}

	assert.Empty(t, presentation.Summary.Segments)

	diff, ok := presentation.Body.(Diff)
	require.True(t, ok)
	require.Len(t, diff.Edits, 2)

	assert.Equal(t, DiffEditKindRename, diff.Edits[0].Kind)
	assert.Equal(t, "some/file.go", diff.Edits[0].OldPath)
	assert.Equal(t, "some/other.go", diff.Edits[0].NewPath)
	assert.True(t, diff.Edits[0].ReplaceAll)
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
	require.NotNil(t, diff.Edits[1].Error)
	assert.Equal(t, errorLine, *diff.Edits[1].Error)
}

func TestPresentationModel_LineJoinModes(t *testing.T) {
	joined := Line{
		JoinWithSpace: true,
		Segments: []Segment{
			{Text: "Read", Role: RoleAction},
			{Text: "file.go", Role: RoleCode},
		},
	}

	assert.True(t, joined.JoinWithSpace)
	assert.Equal(t, []Segment{
		{Text: "Read", Role: RoleAction},
		{Text: "file.go", Role: RoleCode},
	}, joined.Segments)

	manual := Line{
		Segments: []Segment{
			{Text: "foo", Role: RoleAccent},
			{Text: "(bar)", Role: RoleCode},
		},
	}

	assert.False(t, manual.JoinWithSpace)
	assert.Equal(t, []Segment{
		{Text: "foo", Role: RoleAccent},
		{Text: "(bar)", Role: RoleCode},
	}, manual.Segments)
}

func TestPresentationModel_ErrorBehavior(t *testing.T) {
	presentation := Presentation{
		Behavior:       CompletionBehaviorReplace,
		ErrorBehavior:  ErrorBehaviorPresenterOwned,
		NarrowBehavior: PresentationNarrowBehaviorPreferCLI,
		Status:         PresentationStatusFailure,
		Summary: Line{
			Segments: []Segment{{Text: "Apply Patch", Role: RoleAction}},
		},
	}

	assert.Equal(t, CompletionBehaviorReplace, presentation.Behavior)
	assert.Equal(t, ErrorBehaviorPresenterOwned, presentation.ErrorBehavior)
	assert.Equal(t, PresentationNarrowBehaviorPreferCLI, presentation.NarrowBehavior)
	assert.Equal(t, PresentationStatusFailure, presentation.Status)
}

func TestPresentationModel_Status(t *testing.T) {
	assert.Equal(t, PresentationStatusDefault, Presentation{}.Status)
	assert.Equal(t, PresentationStatusSuccess, Presentation{Status: PresentationStatusSuccess}.Status)
	assert.Equal(t, PresentationStatusFailure, Presentation{Status: PresentationStatusFailure}.Status)
}

func TestPresentationModel_Behavior(t *testing.T) {
	assert.Equal(t, CompletionBehaviorReplace, Presentation{Behavior: CompletionBehaviorReplace}.Behavior)
	assert.Equal(t, CompletionBehaviorAppend, Presentation{Behavior: CompletionBehaviorAppend}.Behavior)
}

func TestPresentationModel_NarrowBehavior(t *testing.T) {
	assert.Equal(t, PresentationNarrowBehaviorDefault, Presentation{}.NarrowBehavior)
	assert.Equal(t, PresentationNarrowBehaviorPreferCLI, Presentation{NarrowBehavior: PresentationNarrowBehaviorPreferCLI}.NarrowBehavior)
}
