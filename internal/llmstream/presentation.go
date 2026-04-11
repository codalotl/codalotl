package llmstream

// Presenter describes semantic presentation for a tool call and its eventual result. A nil Presenter is valid and means the tool has no custom presentation.
type Presenter interface {
	// Present returns semantic display data for a tool call. When result is nil, the tool call is still in progress. Phase 0 only wires this API through the type system;
	// llmstream does not consume presentations yet.
	Present(call ToolCall, result *ToolResult) Presentation
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
