# prompt

The prompt package builds prompts for an LLM coding agent. The prompt returned can be a function of model and provider, among other factors.

## Parameterization

The returned prompts can vary based on:
- Agent name
- Model (and the model provider)
- Agent type (generic, package mode, update usage subagent in package mode)

Requirements:
- OpenAI models use the `apply_patch` tool to edit files (edit, create, delete, rename).
- Non-OpenAI models use `edit`, `write`, and `delete` (no rename tool).

## Prompt Types

### Basic Prompt

The basic prompt is just a normal set of instructions that any agent could use. Nothing Go-specific in here. Suitable for "generic mode".

### Package Mode Prompt

The package mode prompt extends the basic prompt with additional instructions on how to operate in package mode.
- Explains isolation to single package.
- Explains special package tools.

### Package Mode Update Usage

The package mode update usage prompt is a variant of the package mode prompt used by the update_usage subagent.
- This agent is less powerful and has fewer tools (ex: can't modify downstream packages).
