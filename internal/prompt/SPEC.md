# prompt

The prompt package builds prompts for an LLM coding agent. The prompt returned can be a function of model and provider, among other factors.

## Variants

Different LLMs sometimes do better with significantly different "styles" of prompts. For instance, Anthropic LLMs tend to need more prompting and more examples. On the other hand, OpenAI LLMs do fine with precise and concise instructions. Based on the model, the prompt package may return a different variant of the prompt.

Variants:
- There's a default variant, which is optimized for all OpenAI models.
- Anthropic models have their own variant.

## Parameterization

The returned prompts can vary based on:
- Agent name
- Model (and the model provider)
- Agent type (generic, package mode, update usage subagent in package mode)

Requirements:
- The tools used to edit files must be able to be varied independently of the provider.
    - Option 1: `apply_patch`
    - Option 2: `edit`, `write`, and `delete`.

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
