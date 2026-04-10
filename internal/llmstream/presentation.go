package llmstream

import "strings"

type Presenter interface {
	Present(call ToolCall, result *ToolResult) Presentation
}

type PresenterFunc func(call ToolCall, result *ToolResult) Presentation

func (f PresenterFunc) Present(call ToolCall, result *ToolResult) Presentation {
	return f(call, result)
}

type CompletionBehavior string

const (
	CompletionBehaviorReplace CompletionBehavior = "replace"
	CompletionBehaviorAppend  CompletionBehavior = "append"
)

type Presentation struct {
	Behavior CompletionBehavior
	Summary  Line
	Body     []Block
}

type Line struct {
	Segments []Segment
}

type Segment struct {
	Text string
	Role SegmentRole
}

type SegmentRole string

const (
	RoleNormal   SegmentRole = "normal"
	RoleAccent   SegmentRole = "accent"
	RoleAction   SegmentRole = "action"
	RoleSuccess  SegmentRole = "success"
	RoleError    SegmentRole = "error"
	RoleCode     SegmentRole = "code"
	RoleEmphasis SegmentRole = "emphasis"
)

type Block interface{ isBlock() }

type Paragraph struct {
	Lines []Line
}

func (Paragraph) isBlock() {}

type Checklist struct {
	Items []ChecklistItem
}

func (Checklist) isBlock() {}

type ChecklistItem struct {
	Status ChecklistStatus
	Line   Line
}

type ChecklistStatus string

const (
	ChecklistDone       ChecklistStatus = "done"
	ChecklistInProgress ChecklistStatus = "in_progress"
	ChecklistPending    ChecklistStatus = "pending"
)

type Output struct {
	Lines []OutputLine
}

func (Output) isBlock() {}

type OutputLine struct {
	Line Line
	Role OutputRole
}

type OutputRole string

const (
	OutputRoleNormal  OutputRole = "normal"
	OutputRoleSuccess OutputRole = "success"
	OutputRoleError   OutputRole = "error"
	OutputRoleAccent  OutputRole = "accent"
)

type Diff struct {
	Files []DiffFile
}

func (Diff) isBlock() {}

type DiffFile struct {
	Kind       DiffFileKind
	Path       string
	ToPath     string
	ReplaceAll bool
	Lines      []DiffLine
}

type DiffFileKind string

const (
	DiffFileAdd        DiffFileKind = "add"
	DiffFileDelete     DiffFileKind = "delete"
	DiffFileEdit       DiffFileKind = "edit"
	DiffFileRenameOnly DiffFileKind = "rename_only"
)

type DiffLine struct {
	Kind DiffLineKind
	Text string
}

type DiffLineKind string

const (
	DiffLineContext DiffLineKind = "context"
	DiffLineAdd     DiffLineKind = "add"
	DiffLineRemove  DiffLineKind = "remove"
	DiffLineGap     DiffLineKind = "gap"
)

func NewDefaultToolPresenter() Presenter {
	return genericToolPresenter{behavior: CompletionBehaviorReplace}
}

func NewAppendToolPresenter() Presenter {
	return genericToolPresenter{behavior: CompletionBehaviorAppend}
}

type genericToolPresenter struct {
	behavior CompletionBehavior
}

func (p genericToolPresenter) Present(call ToolCall, result *ToolResult) Presentation {
	behavior := p.behavior
	if behavior == "" {
		behavior = CompletionBehaviorReplace
	}

	return Presentation{
		Behavior: behavior,
		Summary:  genericToolSummary(call, result),
		Body:     genericToolBody(call, result),
	}
}

func genericToolSummary(call ToolCall, result *ToolResult) Line {
	name := genericToolName(call)

	switch {
	case result == nil:
		return Line{Segments: []Segment{
			{Text: "Calling ", Role: RoleAction},
			{Text: name, Role: RoleCode},
		}}
	case result.IsError:
		return Line{Segments: []Segment{
			{Text: "Failed ", Role: RoleError},
			{Text: name, Role: RoleCode},
		}}
	default:
		return Line{Segments: []Segment{
			{Text: "Completed ", Role: RoleSuccess},
			{Text: name, Role: RoleCode},
		}}
	}
}

func genericToolBody(call ToolCall, result *ToolResult) []Block {
	body := make([]Block, 0, 5)

	metaLines := make([]Line, 0, 2)
	if call.CallID != "" {
		metaLines = append(metaLines, Line{Segments: []Segment{
			{Text: "Call ID: ", Role: RoleAccent},
			{Text: call.CallID, Role: RoleCode},
		}})
	}
	if call.Type != "" {
		metaLines = append(metaLines, Line{Segments: []Segment{
			{Text: "Type: ", Role: RoleAccent},
			{Text: call.Type, Role: RoleCode},
		}})
	}
	if len(metaLines) > 0 {
		body = append(body, Paragraph{Lines: metaLines})
	}

	if call.Input != "" {
		body = append(body,
			Paragraph{Lines: []Line{singleSegmentLine("Input", RoleAccent)}},
			Output{Lines: outputLines(call.Input, OutputRoleNormal)},
		)
	}

	if result == nil || result.Result == "" {
		return body
	}

	label := "Result"
	labelRole := RoleSuccess
	outputRole := OutputRoleSuccess
	if result.IsError {
		label = "Error"
		labelRole = RoleError
		outputRole = OutputRoleError
	}

	body = append(body,
		Paragraph{Lines: []Line{singleSegmentLine(label, labelRole)}},
		Output{Lines: outputLines(result.Result, outputRole)},
	)

	return body
}

func singleSegmentLine(text string, role SegmentRole) Line {
	return Line{Segments: []Segment{{Text: text, Role: role}}}
}

func outputLines(text string, role OutputRole) []OutputLine {
	rawLines := strings.Split(text, "\n")
	lines := make([]OutputLine, 0, len(rawLines))
	for _, rawLine := range rawLines {
		lines = append(lines, OutputLine{
			Line: singleSegmentLine(rawLine, RoleNormal),
			Role: role,
		})
	}
	return lines
}

func genericToolName(call ToolCall) string {
	switch {
	case call.Name != "":
		return call.Name
	case call.CallID != "":
		return call.CallID
	default:
		return "tool"
	}
}
