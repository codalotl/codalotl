package coretools

import "github.com/codalotl/codalotl/internal/llmstream"

func (applyPatchPresenter) SubagentEventPolicy(call llmstream.ToolCall) llmstream.SubagentEventPolicy {
	_ = call
	return llmstream.SubagentEventPolicyDefault
}

func (deletePresenter) SubagentEventPolicy(call llmstream.ToolCall) llmstream.SubagentEventPolicy {
	_ = call
	return llmstream.SubagentEventPolicyDefault
}

func (editPresenter) SubagentEventPolicy(call llmstream.ToolCall) llmstream.SubagentEventPolicy {
	_ = call
	return llmstream.SubagentEventPolicyDefault
}

func (writePresenter) SubagentEventPolicy(call llmstream.ToolCall) llmstream.SubagentEventPolicy {
	_ = call
	return llmstream.SubagentEventPolicyDefault
}

func (lsPresenter) SubagentEventPolicy(call llmstream.ToolCall) llmstream.SubagentEventPolicy {
	_ = call
	return llmstream.SubagentEventPolicyDefault
}

func (readFilePresenter) SubagentEventPolicy(call llmstream.ToolCall) llmstream.SubagentEventPolicy {
	_ = call
	return llmstream.SubagentEventPolicyDefault
}

func (shellPresenter) SubagentEventPolicy(call llmstream.ToolCall) llmstream.SubagentEventPolicy {
	_ = call
	return llmstream.SubagentEventPolicyDefault
}

func (updatePlanPresenter) SubagentEventPolicy(call llmstream.ToolCall) llmstream.SubagentEventPolicy {
	_ = call
	return llmstream.SubagentEventPolicyDefault
}
