# tui

The tui package is the primary package that implements the coding agent TUI, bringing together `internal/agent` and `internal/tools/...` into a UI. This UI is intended to expose capabilities like `internal/docubot` (auto documentation), `internal/reorgbot` (file organization), and so on.

## Dependencies

- `internal/agent` is the main agent loop.
- `internal/agentformatter` formats events from the agent.
- `internal/llmstream` is the library to communicate with LLMs.
- `internal/q/termformat` should be used where possible for terminal formatting.
- `internal/q/tui` is the TUI runtime (raw mode, alt screen, input, resize, render loop).
- `internal/q/tui/tuicontrols` provides common controls like a scrollable view and text area.

## Basic Agent

There's a Messages Area on top to see chat history and agent activity. On the bottom is a Text Area to enter commands. Entering a message and hitting the Enter key will send a message to the agent. The agent loop will work on it for a while, outputting thoughts, issuing tool calls, and sending messages. The user will see this activity in the Messages Area.

At any point in time, the agent is either Running or Idle.
- If the agent is Idle, sending a message causes it to be Running.
- If the agent is Running, the user may still type a message and send it (press ENTER). This enqueues the message to be sent at the next opportunity (for instance, a tool call result can be sent WITH this message).
    - This enqueued message will be reflected in the Messages Area immediately, and the Text Area will be cleared.
    - It will be re-reflected in the Messages Area when it is actually sent.
- If the user types a message and wants the agent to stop what it's doing and process the new message right away, they'd need to press ESC to stop the agent and then ENTER to send the message.
    - Pressing ESC to stop the agent while a message is enqueued will cause all enqueued-but-unsent messages to appear in the Text Area. Pressing ENTER will then send this new message as per normal.

Basic controls:
- Pressing ENTER sends a message. The message is reflected in the Messages Area, and the Text Area is cleared. ENTER does nothing if the Text Area is empty.
- Ctrl-J enters a newline.
- ESC clears the Text Area if it has any text.
  Otherwise, ESC stops the agent if it's Running.
    - ESC is overloaded. It may apply to other scenarios before stopping the agent. Ex: exiting Cycle Mode; exiting edit-previous-message-mode; closing a "dialog", if we had a dialog up.
    - Spamming ESC should be safe and should eventually stop the agent. Extra ESC when the agent is stopped and the Text Area is empty does nothing.
- Ctrl-C stops the agent if it's Running. If the agent is Idle, Ctrl-C terminates the process.
  Typing "/quit", "/exit", or "/logout" also terminates the process.
- Basic text navigation should work. For instance, on OSX, option-left/right jumps the cursor left/right to word boundaries.

## Messages Area

- Each discrete message is separated by a blank line.
- Tools have a Call and a Result.
    - When a Call comes in, we print it.
    - When a paired Result in, we replace the Call message with the Result.
    - Exception: SubAgent calls (change_api, update_usage, clarify_public_api) should NOT replace the Call with Result (it just prints both).
- User messages are displayed as a block of text with the same background color as the Text Area's background, with same prompt caret (ex: `›`). There is no need to write "You:" or similar.
- When the agent finishes its turn, don't print anything like "Agent finished the turn". This can be indicated in other ways.
- The mouse scroll wheel should scroll the message area (without scrolling the "entire TUI").
- Page Up/Page Down/Home/End should also scroll the Messages Area (and not the text area).

## Text Area

- The text area consists of both user-visible lines (rows of characters) as well as logical lines (separated by \n). If the user enters a long line, they will perceive multiple lines, but there is just one logical line.
- The Text Area adjusts in size from 3 user-visible lines by default, up to 10. It shows the most user-visible lines it can, within the limit.

## Working Indicator

- If the agent is Running, it has a Working Indicator visible with the amount of time it's been working. Otherwise it doesn't.
- If present, the Working Indicator is always the last item of the Messages Area.
- By default it will say (for instance): "• Working (1m 34s • ESC to interrupt)"
- The runtime is updated periodically while the agent is running; otherwise.

## Cycling Mode

Up/Down cycle through previous/next messages that user previously sent. Messages include non-trivial slash commands (For the sake of argument, imagine there's a "/refactor <detailed message>" command. That can be cycled through. But "/new" or "/model gemini-2.5" can't be).
- The user is either in Cycling Mode or not. Defaults to not.
- If not in Cycling Mode, pressing Up when the Text Area is blank enters Cycling Mode if there's previous messages.
    - (If there were no previous messages, nothing happens - Cycling Mode not entered.)
    - The Text Area shows the previous message the user entered, with the typing cursor remaining at the start.
- If in Cycling Mode, pressing Down shows the next message. If the Text Area was showing the most recent message, pressing Down exits Cycling Mode and the Text Area goes to its default state.
- If in Cycling Mode, pressing Up shows the previous message. Pressing Up when Cycling Mode is showing the first message does nothing.
- If in Cycling Mode, typing or moving the cursor exits Cycling Mode. The Text Area is now filled with the previous message and the user can edit it and send it.
- If in Cycling Mode, pressing ESC exits Cycling Mode, as if the user pressed Down on the last message. (ESC does not stop the agent here).
- If not in Cycling Mode, but editing a previous message, pressing ESC jumps the cursor to the start and re-enters Cycling Mode (ESC does not stop the agent here). If the message was edited before re-entering
    Cycling Mode, the edited version will be remembered if the user goes back/forth.
- Upon sending a message, all edited-but-unsent messages will be forgotten. Cycling mode will again cycle through actually-sent messages.

## Granting Permission

If the agent needs permission to use some tool, a Permission Area will be shown above the Text Area and below the Messages Area. It should show a message with a Yes or No option.
- pressing Y or N resolves the permission check and hides the Permission Area.
- ESC stops the agent. If the request is to do X, X must not happen after ESC is pressed (ESC is semantically deny-and-stop-agent).
- the Y or N should not be echoed to the Text Area (the Permission Area receives all key input).

## Slash Commands

- /quit, /exit, /logout - terminates process.
- /new - Makes a new session.
- /models, /model (with no args) - prints the available models and usage help. 
- /model <id> - Switches the active model (validated via `llmmodel`) and starts a new session. If `tui.Config.PersistModelID` is set, the model selection is persisted.
- /package path/to/pkg - enter Package Mode for a given package.
- /package - exit Package Mode. Prints a message indicating how Package Mode works.
- /generic - exits Package Mode. Enters generic mode.

## New Sessions

When a new session is initiated (application startup; /new; /package; etc), the Message Area is cleared, and replaced with "new session text".
- There are two types of new session text: generic, and Package Mode.
- Both new session texts have ASCII art (ex: codalotl icon + codalotl word art).
- The new session text will describe the currently active mode. For example: "Package mode is a Go-specific mode that..."
- Non-package mode describes how to enter package mode. Ex: "To enter package mode, use the /package path/to/pkg command."
- Other than the mode, the new session text does not mention any configuration (ex: model, session ID, current package).

## Package Mode

The TUI is either in Package Mode or Generic Mode. It starts in Generic Mode. Being in Package Mode requires a "package" (a path relative to the sandbox root) be selected. To enter Package Mode, enter the slash command "/package path/to/package". This command also makes a new session. To exit Package Mode, use the /package command with no argument. Alternatively, use /generic. Exiting Package Mode also make a new session.

Currently, Package Mode is only implemented for Go. (In the future, I can image something like "code unit mode" [needs better naming] that restricts operations in a similar way.)

While in Package Mode with a given package:
- The agent is mostly restricted to read/write in a given package.
- A custom prompt is used.
- Custom tools are used.

Package Mode file access boundaries are implemented via a "code unit" rooted at the selected package directory. The code unit is computed when the session starts (snapshot semantics):
- The base package directory is included.
- Subdirectories are recursively included iff they do not contain any `*.go` files (this allows access to supporting data dirs like fixtures and snapshots without allowing access to nested Go packages).
- Exception: any `testdata` directory that is directly under an included directory is included entirely (even if it contains `*.go` fixture files).

Other notes:
- /new while in Package Mode retains the active package.
- Package Mode requires the selected path to exist and be a directory ("." is allowed), but does not need to be a buildable Go package.
- Package mode uses initialcontext for the agent's initial context. This can take time to run (it runs tests). Switching to package mode starts getting this context immediately, and does not block the UI. If the user sends a message before this completes, the intialcontext is allowed to complete and then used to as context for the User's message.
    - Uses the message `• Gathering context for path/to/package`, where the bullet indicates status (Accent=in progress -> Red vs Green).

## Color Palette

There exists a non-colorized mode. In this mode, no colors will be applied. Text may still be styled to be bold, etc.

In a colorized mode, there exists a palette. **All colors used must be defined in the palette** (see caveat on text animation). Callers of this package may specify a palette by name (they cannot indicate individual colors). By default, the palette will be based on the default terminal palette, as determined by `internal/q/termformat`.

Supported palette names:
- `auto` - derive colors from the terminal (default)
- `dark` - force the built-in dark palette
- `light` - force the built-in light palette
- `plain` - disable colors entirely

Palette Colors:
- Primary Background Color (most of the screen will be this color)
- Accent Background Color (secondary color for various areas. Example: Text Area background)
- Primary Foreground Color (normal text)
- Accent Foreground Color (less important text. Example: help hints)
- Red Foreground Color (text for error statuses, error messages, diff removals)
- Green Foreground Color (text for success statuses, diff additions)
- Colorful Foreground Color (used to highlight important words, tool calls)

All foreground colors in a palette must be readable on all background colors.

Caveat for text animation: some text will be animated (example: Working Indicator). In such cases, colors may be computed based on the configured palette.

## Mouse

Capture mouse events. Handle as follows:
- Scroll wheel always scrolls Messages Area.

## Option Mode

Typing Ctrl-O, or double-clicking the terminal, enters "Option Mode". Doing it again exits the mode. In Option Mode, various "options" appear in various places in the UI that can be clicked on with the mouse (in the future, but not now, they can be cycled through with the keyboard).

### Copying Text

Because we capture mouse events to handle scrolling the Messages Area, normal selection of text for copying doesn't work (even if it did, it's also confounded by the dual-column view of the Messages Area and Info Panel). Therefore, in Option Mode, a `copy` button will appear below each message in the Messages Area:
- `copy` appears below the message in the lower right (in the blank line between messages).
- It is rendered in Colorful text with an Accent background.
- Clicking it with the mouse will copy the message to the clipboard. A transient `copied!` is displayed in its place momentarily.
- This applies to any type of message: a user message, agent response, tool use, etc. Welcome message is can be included or excluded based on whatever is easier to implement.
- Copy what you see (text as displayed, plain text, no formatting, wrapped).
- Clipboard: use `q/tui`'s `SetClipboard`.

## Info Panel

There's an info panel to the right of the Messages Area, shown if there's sufficient width.

### Version Upgrade Notice

If a `*remotemonitor.Monitor` is passed to TUI, it is used to determine the latest version. If that version is higher than the current version, the top of the info panel will display (for instance):

```
An update is available: 1.2.4 (current 1.2.3)
Run go install github.com/codalotl/codalotl@latest
```

### Session / Model / Tokens / Cost

The top of the panel shows the current session ID, model, tokens, and cost:
- Session
- Model
- Context window (ex: "100% context left", "82% context left", etc)
- Cost
- Total tokens (input + cached + output). This token count is rounded as necessary (ex: 313, 1.4k, 520k, 1.2M, etc).
- Input tokens (non-cached)
- Cached input tokens
- Output tokens (includes reasoning tokens)

Display format:

```
Session: 123-abc
Model: gpt-5.2-high
Context: 32% left   |   Cost: $3.24
Tokens: 123k (input: 42k, cached: 60k, output: 21k)
```

This information is reset when the /new command is run.

### Package Mode

While in Package Mode, the UI will say so:

```
Package: path/to/package
```

When not in package mode, the UI will say:

```
Package: <none>
Use `/package path/to/pkg` to select a package.
```
