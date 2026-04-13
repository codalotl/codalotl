package agentbuilder

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"testing"

	"github.com/codalotl/codalotl/internal/llmstream"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNormalizeYAMLPresenterSpec_ReviewRequiresBaseParameter(t *testing.T) {
	_, err := normalizeYAMLPresenterSpec(&yamlPresenterSpec{
		Preset: &yamlPresenterPresetSpec{Name: yamlPresenterPresetReview},
	}, map[string]yamlNormalizedParameter{
		"path": {Type: "string"},
	})

	require.ErrorContains(t, err, `presenter.preset.name "review" requires parameter "base"`)
}

func TestNormalizeYAMLPresenterSpec_ReviewRejectsTunableFields(t *testing.T) {
	_, err := normalizeYAMLPresenterSpec(&yamlPresenterSpec{
		Preset: &yamlPresenterPresetSpec{
			Name:       yamlPresenterPresetReview,
			CallAction: "Investigating",
		},
	}, map[string]yamlNormalizedParameter{
		"base": {Type: "string"},
	})

	require.ErrorContains(t, err, `presenter.preset.name "review" does not support call_action`)
}

func TestYAMLReviewPresenterPresent_FormatsFindingsAndTruncates(t *testing.T) {
	presenter := requireYAMLReviewPresenter(t)

	call := llmstream.ToolCall{
		Name:  "review",
		Input: `{"base":"origin/main"}`,
	}
	result := &llmstream.ToolResult{
		Name:   "review",
		Result: mustMarshalReviewResult(t, 12),
	}

	callPresentation := presenter.Present(call, nil)
	resultPresentation := presenter.Present(call, result)

	assert.Equal(t, llmstream.Presentation{
		Behavior:      llmstream.CompletionBehaviorAppend,
		ErrorBehavior: llmstream.ErrorBehaviorDefault,
		Summary: llmstream.Line{
			JoinWithSpace: true,
			Segments: []llmstream.Segment{
				{Text: "Reviewing", Role: llmstream.RoleAction},
				{Text: "origin/main", Role: llmstream.RoleNormal},
			},
		},
	}, callPresentation)

	require.IsType(t, llmstream.Output{}, resultPresentation.Body)
	assert.Equal(t, llmstream.Presentation{
		Behavior:      llmstream.CompletionBehaviorAppend,
		ErrorBehavior: llmstream.ErrorBehaviorDefault,
		Summary: llmstream.Line{
			JoinWithSpace: true,
			Segments: []llmstream.Segment{
				{Text: "Reviewed", Role: llmstream.RoleAction},
				{Text: "origin/main", Role: llmstream.RoleNormal},
			},
		},
		Body: llmstream.Output{
			Lines: []string{
				"[P2] Finding 01",
				"[P2] Finding 02",
				"[P2] Finding 03",
				"[P2] Finding 04",
				"[P2] Finding 05",
				"[P2] Finding 06",
				"[P2] Finding 07",
				"[P2] Finding 08",
				"[P2] Finding 09",
				"[P2] Finding 10",
				"... +2 findings",
			},
		},
	}, resultPresentation)
}

func TestYAMLReviewPresenterPresent_NoFindingsUsesSuccessLine(t *testing.T) {
	presenter := requireYAMLReviewPresenter(t)

	presentation := presenter.Present(llmstream.ToolCall{
		Name:  "review",
		Input: `{"base":"main"}`,
	}, &llmstream.ToolResult{
		Name: "review",
		Result: `{
			"findings": [],
			"overall_correctness": "patch is correct",
			"overall_explanation": "No actionable issues found.",
			"overall_confidence_score": 0.93
		}`,
	})

	assert.Equal(t, llmstream.Output{
		Lines: []string{yamlReviewBodyNoFindings},
	}, presentation.Body)
}

func TestYAMLReviewPresenterPresent_InvalidReviewJSONFallsBackToRawOutput(t *testing.T) {
	presenter := requireYAMLReviewPresenter(t)

	presentation := presenter.Present(llmstream.ToolCall{
		Name:  "review",
		Input: `{"base":"main"}`,
	}, &llmstream.ToolResult{
		Name:   "review",
		Result: "{\n  \"unexpected\": true\n}",
	})

	assert.Equal(t, llmstream.Output{
		Lines: []string{"{", `  "unexpected": true`, "}"},
	}, presentation.Body)
}

func requireYAMLReviewPresenter(t *testing.T) llmstream.Presenter {
	t.Helper()

	normalized, err := normalizeYAMLPresenterSpec(&yamlPresenterSpec{
		Preset: &yamlPresenterPresetSpec{Name: yamlPresenterPresetReview},
	}, map[string]yamlNormalizedParameter{
		"base": {Type: "string"},
	})
	require.NoError(t, err)

	presenter := buildYAMLPresenter(normalized, map[string]yamlNormalizedParameter{
		"base": {Type: "string"},
	})
	require.NotNil(t, presenter)
	return presenter
}

func mustMarshalReviewResult(t *testing.T, findingCount int) string {
	t.Helper()

	findings := make([]map[string]any, 0, findingCount)
	for i := 1; i <= findingCount; i++ {
		findings = append(findings, map[string]any{
			"title":            fmt.Sprintf("[P2] Finding %02d", i),
			"body":             "Explain why this is actionable.",
			"confidence_score": 0.81,
			"priority":         2,
			"code_location": map[string]any{
				"absolute_file_path": filepath.Join("/tmp", fmt.Sprintf("file-%02d.go", i)),
				"line_range": map[string]any{
					"start": i,
					"end":   i,
				},
			},
		})
	}

	payload, err := json.Marshal(map[string]any{
		"findings":                 findings,
		"overall_correctness":      "patch is incorrect",
		"overall_explanation":      "There are actionable findings.",
		"overall_confidence_score": 0.81,
	})
	require.NoError(t, err)
	return string(payload)
}
