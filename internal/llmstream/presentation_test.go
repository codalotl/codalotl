package llmstream

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDefaultToolPresenter_PresentInProgress(t *testing.T) {
	presenter := NewDefaultToolPresenter()

	presentation := presenter.Present(ToolCall{
		CallID: "call_123",
		Name:   "read_file",
		Type:   "function_call",
		Input:  "{\"path\":\"README.md\"}",
	}, nil)

	assert.Equal(t, CompletionBehaviorReplace, presentation.Behavior)
	assert.Equal(t, []Segment{
		{Text: "Calling ", Role: RoleAction},
		{Text: "read_file", Role: RoleCode},
	}, presentation.Summary.Segments)

	require.Len(t, presentation.Body, 3)

	meta, ok := presentation.Body[0].(Paragraph)
	require.True(t, ok)
	assert.Equal(t, []Line{
		{Segments: []Segment{{Text: "Call ID: ", Role: RoleAccent}, {Text: "call_123", Role: RoleCode}}},
		{Segments: []Segment{{Text: "Type: ", Role: RoleAccent}, {Text: "function_call", Role: RoleCode}}},
	}, meta.Lines)

	inputLabel, ok := presentation.Body[1].(Paragraph)
	require.True(t, ok)
	assert.Equal(t, []Line{{Segments: []Segment{{Text: "Input", Role: RoleAccent}}}}, inputLabel.Lines)

	input, ok := presentation.Body[2].(Output)
	require.True(t, ok)
	assert.Equal(t, []OutputLine{{
		Line: Line{Segments: []Segment{{Text: "{\"path\":\"README.md\"}", Role: RoleNormal}}},
		Role: OutputRoleNormal,
	}}, input.Lines)
}

func TestAppendToolPresenter_PresentCompletedError(t *testing.T) {
	presenter := NewAppendToolPresenter()

	presentation := presenter.Present(ToolCall{
		Name:  "run_command",
		Input: "echo hi",
	}, &ToolResult{
		Result:  "boom\nstill broken",
		IsError: true,
	})

	assert.Equal(t, CompletionBehaviorAppend, presentation.Behavior)
	assert.Equal(t, []Segment{
		{Text: "Failed ", Role: RoleError},
		{Text: "run_command", Role: RoleCode},
	}, presentation.Summary.Segments)

	require.Len(t, presentation.Body, 4)

	resultLabel, ok := presentation.Body[2].(Paragraph)
	require.True(t, ok)
	assert.Equal(t, []Line{{Segments: []Segment{{Text: "Error", Role: RoleError}}}}, resultLabel.Lines)

	result, ok := presentation.Body[3].(Output)
	require.True(t, ok)
	assert.Equal(t, []OutputLine{
		{
			Line: Line{Segments: []Segment{{Text: "boom", Role: RoleNormal}}},
			Role: OutputRoleError,
		},
		{
			Line: Line{Segments: []Segment{{Text: "still broken", Role: RoleNormal}}},
			Role: OutputRoleError,
		},
	}, result.Lines)
}

func TestPresenterFunc(t *testing.T) {
	presenter := PresenterFunc(func(call ToolCall, result *ToolResult) Presentation {
		return Presentation{
			Behavior: CompletionBehaviorReplace,
			Summary: Line{Segments: []Segment{
				{Text: call.Name, Role: RoleCode},
			}},
		}
	})

	presentation := presenter.Present(ToolCall{Name: "custom"}, nil)

	assert.Equal(t, CompletionBehaviorReplace, presentation.Behavior)
	assert.Equal(t, []Segment{{Text: "custom", Role: RoleCode}}, presentation.Summary.Segments)
}
