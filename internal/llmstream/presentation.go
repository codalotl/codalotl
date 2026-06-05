package llmstream

// A Presenter can present a tool call (and optional result) in a "semantic" way - a tree representation that can be further styled for different modalities. As
// an analogy, it's the HTML (but not the CSS) of underlying data.
//
// NOTE: llmstream package does NOT have any additional information about how to use Presenter or Presentation -- consuming packages should NOT interrogate this
// package with clarify_public_api concerning Presenter or its types. These types are provided as-is for packages to build upon.
type Presenter interface {
	// Present presents call and result in a semantic way (no width decisions, no assumptions about ANSI terminals, colors). To present a tool call (no result yet),
	// call Present(call, nil). To present a call with result, call Present(call, result). For instance, for a read file tool, the call might return the equivalent of
	// "Reading file.go". The result might return the equivalent of "Read file.go (123 bytes)".
	Present(call ToolCall, result *ToolResult) Presentation
}

// SubagentFinalMessagePresenter optionally customizes the final message of a descendant subagent launched directly by call. The interface is defined in terms of
// that direct tool-call/subagent relationship. Consumers that collapse deeper descendant activity into the direct subagent's visible slot may reuse the same presentation
// for that slot's terminal visible message.
//
// Consumers should type-assert a tool presenter to this interface. When the presenter does not implement it, the descendant subagent final message should be shown
// as plain text. Returning nil suppresses the descendant final message. Returning a non-nil Block replaces the plain-text rendering with a semantic block.
type SubagentFinalMessagePresenter interface {
	// SubagentFinalMessage returns a semantic replacement for finalMessage for the direct subagent launched by call. A non-nil Block replaces plain-text rendering;
	// nil suppresses the final message.
	SubagentFinalMessage(call ToolCall, subagentLabel string, finalMessage string) Block
}

// CompletionBehavior indicates what happens when the tool completes. For instance, imagine a TUI:
//   - With Replace, the tool call presentation is replaced by the result presentation (ideal for quick and/or atomic operations like reading a file).
//   - With Append, the tool call is displayed. When the result comes in, it should also be displayed (ideal for subagents, which are long-lived and themselves emit
//     tool calls).
type CompletionBehavior string

// Completion behavior values describe how consumers should display completed tool presentations.
const (
	// CompletionBehaviorReplace replaces the tool-call presentation with the result presentation.
	CompletionBehaviorReplace CompletionBehavior = "replace"

	// CompletionBehaviorAppend keeps the tool-call presentation visible and appends the result presentation.
	CompletionBehaviorAppend CompletionBehavior = "append"
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
	Behavior      CompletionBehavior // Behavior controls how a completed tool call presentation is reconciled with its result presentation.
	ErrorBehavior ErrorBehavior      // ErrorBehavior controls whether shared formatter-owned error rendering should override presenter body content.
	Status        PresentationStatus // Status indicates whether the presenter explicitly owns the visible success or failure state.

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

	// Segments are the ordered styled spans that make up the line.
	Segments []Segment
}

// Segment is a styled span of text within a Line.
type Segment struct {
	Text string      // Text is the literal text to render for this segment.
	Role SegmentRole // Role is the semantic style to apply to Text.
}

// SegmentRole describes the semantic presentation style for a text segment.
type SegmentRole string

// Segment roles describe the intended visual treatment of presentation text.
const (
	RoleNormal   SegmentRole = "normal"   // RoleNormal uses the default presentation style.
	RoleAccent   SegmentRole = "accent"   // RoleAccent highlights text.
	RoleAction   SegmentRole = "action"   // RoleAction marks an action or operation.
	RoleSuccess  SegmentRole = "success"  // RoleSuccess marks a successful result.
	RoleError    SegmentRole = "error"    // RoleError marks an error or failure.
	RoleCode     SegmentRole = "code"     // RoleCode marks code, commands, paths, or other literals.
	RoleEmphasis SegmentRole = "emphasis" // RoleEmphasis marks text that should be emphasized.
)

// Block is an interface with a private method, to lock down possible Block implementors to the following:
//   - Paragraph
//   - Checklist
//   - Output
//   - Diff
type Block interface {
	// isBlock marks package-defined values that can be used as presentation blocks.
	isBlock()
}

// Paragraph is a presentation block containing ordered lines of prose.
type Paragraph struct {
	Lines []Line // Lines are the paragraph lines in display order.
}

// isBlock marks Paragraph as a Block implementation.
func (Paragraph) isBlock() {}

// Checklist is a presentation block containing checklist items.
type Checklist struct {
	Overview Line            // Overview is an optional line that introduces the checklist.
	Items    []ChecklistItem // Items are the checklist entries in display order.
}

// isBlock marks Checklist as a Block implementation.
func (Checklist) isBlock() {}

// ChecklistItem is one item in a checklist presentation.
type ChecklistItem struct {
	Status ChecklistStatus // Status is the progress state of the item.
	Line   Line            // Line is the visible text for the item.
}

// ChecklistStatus identifies the progress state of a checklist item.
type ChecklistStatus string

// These consts identify checklist item progress states.
const (
	ChecklistStatusPending    ChecklistStatus = "pending"     // ChecklistStatusPending indicates the item has not started.
	ChecklistStatusInProgress ChecklistStatus = "in_progress" // ChecklistStatusInProgress indicates the item is currently active.
	ChecklistStatusCompleted  ChecklistStatus = "completed"   // ChecklistStatusCompleted indicates the item is finished.
)

// Output is verbatim, line-oriented tool output such as shell command output or a pretty-printed raw payload.
type Output struct {
	Lines            []string // Lines are the visible output lines in display order.
	OmittedLineCount int      // OmittedLineCount records how many additional lines were intentionally omitted from the presentation.
}

// isBlock marks Output as a package-defined Block implementation.
func (Output) isBlock() {}

// Diff is a diff-like edit block, potentially spanning multiple file edits. For a Presentation whose Body is a Diff, presenters must leave Presentation.Summary
// empty. Consumers that need a 1-line visible header should derive it from the first edit in Edits.
type Diff struct {
	Edits []DiffEdit // Edits are in display order. The first edit is the lead edit for consumers that synthesize a diff header.
}

// isBlock marks Diff as a valid presentation body block.
func (Diff) isBlock() {}

// DiffEdit describes one file-level edit in a Diff presentation.
type DiffEdit struct {
	Kind       DiffEditKind // Kind identifies the semantic file operation represented by the edit.
	OldPath    string       // OldPath is the source path for edits, deletes, and renames. It may be empty for newly added files.
	NewPath    string       // NewPath is the destination path for adds and renames. It may be empty for deleted files.
	ReplaceAll bool         // ReplaceAll indicates the edit semantically applies to all matches in the file, for UIs that surface that distinction in the header.
	Lines      []DiffLine   // Lines are the visible diff lines. Presentations that suppress hunk anchors can still model the changed lines semantically here.
	Error      *Line        // If this edit resulted in an error, Error should be set and describe the error.
}

// DiffEditKind identifies the semantic file operation represented by a DiffEdit.
type DiffEditKind string

// Diff edit kinds describe file-level operations in a DiffEdit.
const (
	DiffEditKindEdit   DiffEditKind = "edit"   // DiffEditKindEdit modifies an existing file.
	DiffEditKindAdd    DiffEditKind = "add"    // DiffEditKindAdd adds a new file.
	DiffEditKindDelete DiffEditKind = "delete" // DiffEditKindDelete deletes an existing file.
	DiffEditKindRename DiffEditKind = "rename" // DiffEditKindRename renames or moves a file.
)

// DiffLine represents one semantic line in a diff presentation.
type DiffLine struct {
	Kind DiffLineKind // Kind identifies whether the line is context, added, deleted, or omitted.
	Text string       // Text is the line content without the leading diff marker. For omitted lines, Text may be empty.
}

// DiffLineKind identifies how a DiffLine should be interpreted in a semantic diff.
type DiffLineKind string

// Diff line kinds describe line-level semantics in a DiffLine.
const (
	DiffLineKindContext DiffLineKind = "context" // DiffLineKindContext marks an unchanged context line.
	DiffLineKindAdd     DiffLineKind = "add"     // DiffLineKindAdd marks an added line.
	DiffLineKindDelete  DiffLineKind = "delete"  // DiffLineKindDelete marks a deleted line.
	DiffLineKindOmitted DiffLineKind = "omitted" // DiffLineKindOmitted marks one or more intentionally omitted lines.
)
