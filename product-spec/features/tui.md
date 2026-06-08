# TUI

The naked `codalotl` command launches Codalotl's terminal UI.

The TUI is the primary interactive agent experience: a persistent chat-like session where the user gives coding goals, watches agent progress, answers permission checks, changes modes, and continues steering the same session.

## CLI

### codalotl

Launches the TUI in the sandbox dir.

### codalotl .

Alias for `codalotl`, supported for editor-like muscle memory. Other path-like root arguments are not TUI launch aliases.

Startup loads configuration, validates required Go/git tools, and validates that at least one usable LLM model is available. Configuration can select the preferred model, auto-approval behavior, and TUI color theme.

## Layout

The TUI has:
- Messages area: chat history, assistant output, tool calls, tool output, warnings, errors, and system messages.
- Input area: where the user types messages and slash commands.
- Permission area: shown when the agent needs interactive approval.
- Info panel: shown when terminal width permits, with session/model/token/cost/status details.

The UI works in ordinary terminals on supported OSes. It responds to resize events and should remain usable on narrower terminals by hiding optional panels and wrapping content.

## Agent Sessions

The user can send a message by typing in the input area and pressing Enter. The agent then works on that message and streams visible progress into the messages area.

The agent is either running or idle:
- When idle, Enter sends the input as a new user message.
- When running, Enter queues the input for the next opportunity, while still displaying it immediately.
- The user can interrupt a running agent and then send revised instructions.

New sessions clear the messages area and start fresh agent context. They preserve durable configuration like selected model, but do not preserve previous chat history.

The TUI starts in generic mode unless a slash command changes mode. Generic mode is appropriate for repo-wide questions, planning, and broad coding tasks.

## Package Mode

Package mode is the main Go-optimized coding mode.

The user enters package mode with:

```text
/package <path/to/pkg>
```

`<path/to/pkg>` follows `features/cli.md` package argument semantics and must resolve inside the sandbox dir.

While in package mode:
- The selected package is shown in the UI.
- New user messages are sent to a package-mode agent.
- The agent receives Go-specific initial context for that package.
- The agent is guided to work primarily inside that package and use Go-specific tools for cross-package understanding.

Entering package mode starts a new session. `/new` while in package mode keeps the same package. `/package` with no argument or `/generic` exits package mode and starts a generic session.

Package initial context may take time to gather. The TUI shows context-gathering progress and may let the user type before it completes; the gathered context should still be included before the agent acts on the user's message.

## Orchestrator Mode

The user starts the built-in PR orchestrator with:

```text
/orchestrate
/orchestrate <message>
```

This starts a fresh generic-mode orchestrator session matching the PR Orchestrator feature. If `<message>` is present, it is sent as the initial user message. Otherwise, the orchestrator starts without an initial prompt and can be steered by later user messages.

Package mode does not apply to orchestrator startup.

## Slash Commands

Slash commands are typed in the normal input area.

Supported commands:
- `/new`: start a new session.
- `/package <path/to/pkg>`: enter package mode.
- `/package`: exit package mode.
- `/generic`: exit package mode.
- `/orchestrate [message]`: start an orchestrator session.
- `/models`: show available models and model-selection usage.
- `/model <id>`: switch active model and start a new session.
- `/skills`: list installed skills and skill discovery issues.
- `/quit`, `/exit`, `/logout`: terminate the TUI process.

The TUI may show additional diagnostic or development-only commands, but ordinary user workflows should not depend on them.

## Model Selection

The TUI shows and selects only models that are usable with current credentials.

`/models` lists available models and indicates when a model uses provider subscription auth. `/model <id>` switches the active model, starts a new session, and persists the selection when configuration persistence is available.

If no usable model is available, TUI startup fails with instructions for setting provider API keys or logging in to supported subscription auth.

## Permissions

When the agent requests permission for an action, the TUI displays an approval prompt and waits for the user to approve or deny it.

Permission controls are keyboard-oriented:
- Approve grants the requested action.
- Deny rejects the requested action.
- Interrupt denies the pending request and stops the current agent turn.

If `autoyes: true` is configured, permission checks are approved automatically for the TUI session.

## Output

The TUI streams assistant text, reasoning summaries when available, tool calls, tool output, tool results, warnings, retries, and errors in human-readable form.

Tool calls should be understandable at a glance, with access to details when the terminal UI supports it. Subagent activity should be grouped or indented so the user can understand nested work without needing to read raw event payloads.

The UI tracks token usage, context-window usage, and estimated cost for the active session when the provider reports enough data.

The TUI is not a stable machine-readable output interface. Scripts and integrations should use `codalotl exec --json`.

## Input and Navigation

The input area supports multi-line messages, ordinary text editing, and terminal-native navigation as much as practical.

Common controls:
- Enter sends the current message.
- Ctrl-J inserts a newline.
- Esc clears input, exits transient UI modes, or interrupts a running agent, depending on current state.
- Ctrl-C interrupts a running agent; when idle, it exits the TUI.
- Up/Down can recall previous non-trivial user messages.
- Page Up/Page Down/Home/End scroll the messages area.
- Mouse wheel scrolls the messages area when mouse support is available.

## Copying and Details

The TUI should provide a practical way to copy visible messages even when mouse capture interferes with normal terminal selection.

When details are available for tool calls, tool results, generated package context, or similar structured events, the user can inspect the underlying data without leaving the TUI.

## Color and Theme

The TUI supports colorized and non-colorized display.

Theme selection comes from configuration:
- unset or `auto`: choose a readable built-in theme.
- `dark`: force dark theme.
- `light`: force light theme.
- `plain`: disable colors.

Color should improve scanability but must not be required for understanding status, errors, permission prompts, or agent output.
