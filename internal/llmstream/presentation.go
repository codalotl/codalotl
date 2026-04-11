package llmstream

// A Presenter can present a tool call (and optional result) in a "semantic" way - a tree representation that can be further styled for different modalities. As
// an analogy, it's the HTML (but not the CSS) of underlying data.
type Presenter interface {
	// Present presents call and result in a semantic way (no width decisions, no assumptions about ANSI terminals, colors). Result == nil means the tool call is in
	// progress; otherwise its complete.
	Present(call ToolCall, result *ToolResult) Presentation
}

// CompletionBehavior indicates what happens when the tool completes. For instance, imagine a TUI:
//   - With Replace, the tool call presentation is replaced by the result presentation (ideal for quick and/or atomic operations like reading a file).
//   - With Append, the tool call is displayed. When the result comes in, it should also be displayed (ideal for subagents, which are long-lived and themselves emit
//     tool calls).
type CompletionBehavior string

const (
	CompletionBehaviorReplace CompletionBehavior = "replace"
	CompletionBehaviorAppend  CompletionBehavior = "append"
)

// A Presentation is a semantic representation of a tool call (with optional tool result).
//   - Strings should not contain ANSI escape sequences or colors.
//   - Do not include "•" (leading bullets typical in TUI event streams).
//   - Do not include "└" (common in Body blocks).
//   - Do not assume/include indentation.
//   - Do not worry about line width.
type Presentation struct {
	Behavior CompletionBehavior
	Summary  Line    // Summary is a 1-liner indicating what the tool even is (ex: "Read path/to/file.go"; "Update Plan"; "Running go test ./...")
	Body     []Block // Tool details (ex: diff body; command output; checklist items)
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

type Block interface {
	isBlock()
}

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
