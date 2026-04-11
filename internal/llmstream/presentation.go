package llmstream

// A Presenter can present a tool call (and optional result) in a "semantic" way - a tree representation that can be further styled for different modalities. As
// an analogy, it's the HTML (but not the CSS) of underlying data.
type Presenter interface {
	// Present presents call and result in a semantic way (no width decisions, no assumptions about ANSI terminals, colors). To present a tool call (no result yet),
	// call Present(call, nil). To present a call with result, call Present(call, result). For instance, for a read file tool, the call might return the equivalent of
	// "Reading file.go". The result might return the equivalent of "Read file.go (123 bytes)".
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

const (
	ChecklistStatusPending    ChecklistStatus = "pending"
	ChecklistStatusInProgress ChecklistStatus = "in_progress"
	ChecklistStatusCompleted  ChecklistStatus = "completed"
)

// Output is verbatim, line-oriented tool output such as shell command output or a pretty-printed raw payload.
type Output struct {
	Kind             OutputKind
	Lines            []string // Lines are the visible output lines in display order.
	OmittedLineCount int      // OmittedLineCount records how many additional lines were intentionally omitted from the presentation.
}

func (Output) isBlock() {}

type OutputKind string

const (
	OutputKindText    OutputKind = "text"
	OutputKindCommand OutputKind = "command"
	OutputKindJSON    OutputKind = "json"
)

// Diff is a diff-like edit block, potentially spanning multiple file edits.
type Diff struct {
	Edits []DiffEdit
}

func (Diff) isBlock() {}

type DiffEdit struct {
	Kind    DiffEditKind
	OldPath string     // OldPath is the source path for edits, deletes, and renames. It may be empty for newly added files.
	NewPath string     // NewPath is the destination path for adds and renames. It may be empty for deleted files.
	Lines   []DiffLine // Lines are the visible diff lines. Presentations that suppress hunk anchors can still model the changed lines semantically here.
}

type DiffEditKind string

const (
	DiffEditKindEdit   DiffEditKind = "edit"
	DiffEditKindAdd    DiffEditKind = "add"
	DiffEditKindDelete DiffEditKind = "delete"
	DiffEditKindRename DiffEditKind = "rename"
)

type DiffLine struct {
	Kind DiffLineKind
	Text string // Text is the line content without the leading diff marker. For omitted lines, Text may be empty.
}

type DiffLineKind string

const (
	DiffLineKindContext DiffLineKind = "context"
	DiffLineKindAdd     DiffLineKind = "add"
	DiffLineKindDelete  DiffLineKind = "delete"
	DiffLineKindOmitted DiffLineKind = "omitted"
)
