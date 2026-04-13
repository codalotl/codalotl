package llmstream

// A Presenter can present a tool call (and optional result) in a "semantic" way - a tree representation that can be further styled for different modalities. As
// an analogy, it's the HTML (but not the CSS) of underlying data.
type Presenter interface {
	// Present presents call and result in a semantic way (no width decisions, no assumptions about ANSI terminals, colors). To present a tool call (no result yet),
	// call Present(call, nil). To present a call with result, call Present(call, result). For instance, for a read file tool, the call might return the equivalent of
	// "Reading file.go". The result might return the equivalent of "Read file.go (123 bytes)".
	Present(call ToolCall, result *ToolResult) Presentation

	// SubagentEventPolicy defines how descendant subagent events are displayed by consumers. Tools that do not launch subagents can return SubagentEventPolicyDefault.
	SubagentEventPolicy(call ToolCall) SubagentEventPolicy
}

type SubagentEventPolicy string

const (
	SubagentEventPolicyDefault          SubagentEventPolicy = ""
	SubagentEventPolicyHideFinalMessage SubagentEventPolicy = "hide_final_message"
)

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
//   - Do not worry about line width - a semantic `Line` will be split into multiple lines by the final formatter if necessary.
//   - Summary is usually the visible 1-line tool header. When Body is a Diff, Summary must be left empty; consumers should derive the header from Diff.Edits instead.
//
// By default, a ToolResult with IsError dose NOT need to present the error in Body - final formatters will automatically display an error based on IsError and SourceErr.
// To override this, set ErrorBehavior to ErrorBehaviorPresenterOwned.
type Presentation struct {
	Behavior       CompletionBehavior
	ErrorBehavior  ErrorBehavior
	NarrowBehavior PresentationNarrowBehavior
	Status         PresentationStatus

	// Summary is usually a 1-line tool header (ex: "Read path/to/file.go"; "Update Plan"; "Running go test ./..."). When Body is a Diff, leave Summary empty and let
	// consumers derive the header from the diff body.
	Summary Line

	// Tool details (ex: diff body; command output; checklist items). Diff bodies include enough metadata for consumers to synthesize their own header.
	Body Block
}

// PresentationStatus indicates whether a presenter explicitly owns the visible success/failure state for completion rendering.
type PresentationStatus string

const (
	// PresentationStatusDefault means consumers should infer success/failure from the underlying ToolResult.
	PresentationStatusDefault PresentationStatus = ""

	// PresentationStatusSuccess means consumers should treat the presentation as successful.
	PresentationStatusSuccess PresentationStatus = "success"

	// PresentationStatusFailure means consumers should treat the presentation as failed.
	PresentationStatusFailure PresentationStatus = "failure"
)

// PresentationNarrowBehavior indicates whether a presenter wants the formatter's narrow-width fallback behavior adjusted.
type PresentationNarrowBehavior string

const (
	// PresentationNarrowBehaviorDefault keeps the formatter's default minimum-width TUI behavior for presenters.
	PresentationNarrowBehaviorDefault PresentationNarrowBehavior = ""

	// PresentationNarrowBehaviorPreferCLI asks consumers to keep using the formatter's CLI fallback at the minimum width boundary.
	PresentationNarrowBehaviorPreferCLI PresentationNarrowBehavior = "prefer_cli"
)

// ErrorBehavior indicates whether shared formatter-owned error rendering should still override presenter body content.
type ErrorBehavior string

const (
	// ErrorBehaviorDefault means the formatter should keep using shared default tool error rendering when the tool result is an error.
	ErrorBehaviorDefault ErrorBehavior = ""

	// ErrorBehaviorPresenterOwned means the presenter body already models the desired error presentation.
	ErrorBehaviorPresenterOwned ErrorBehavior = "presenter_owned"
)

// Line is a single rendered line made of styled segments. If JoinWithSpace is true, consumers should join adjacent segments with a single space. Otherwise, Segment.Text
// owns any needed leading or trailing whitespace explicitly.
type Line struct {
	// JoinWithSpace indicates whether consumers should insert a single space between segments. When false, Segment.Text owns any needed leading or trailing whitespace.
	JoinWithSpace bool

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

// Block is an interface with a private method, to lock down possible Block implementors to the following:
//   - Paragraph
//   - Checklist
//   - Output
//   - Diff
type Block interface {
	isBlock()
}

type Paragraph struct {
	Lines []Line
}

func (Paragraph) isBlock() {}

type Checklist struct {
	Overview Line
	Items    []ChecklistItem
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
	Lines            []string // Lines are the visible output lines in display order.
	OmittedLineCount int      // OmittedLineCount records how many additional lines were intentionally omitted from the presentation.
}

func (Output) isBlock() {}

// Diff is a diff-like edit block, potentially spanning multiple file edits. For a Presentation whose Body is a Diff, presenters must leave Presentation.Summary
// empty. Consumers that need a 1-line visible header should derive it from the first edit in Edits.
type Diff struct {
	Edits []DiffEdit // Edits are in display order. The first edit is the lead edit for consumers that synthesize a diff header.
}

func (Diff) isBlock() {}

type DiffEdit struct {
	Kind       DiffEditKind
	OldPath    string     // OldPath is the source path for edits, deletes, and renames. It may be empty for newly added files.
	NewPath    string     // NewPath is the destination path for adds and renames. It may be empty for deleted files.
	ReplaceAll bool       // ReplaceAll indicates the edit semantically applies to all matches in the file, for UIs that surface that distinction in the header.
	Lines      []DiffLine // Lines are the visible diff lines. Presentations that suppress hunk anchors can still model the changed lines semantically here.
	Error      *Line      // If this edit resulted in an error, Error should be set and describe the error.
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
