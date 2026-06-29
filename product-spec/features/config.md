# Config

Codalotl is configured with a mix of JSON config files, environment variables, auth commands, and per-command flags.

Configuration lets users keep stable preferences outside each prompt: which model to use, how permission checks behave, what theme the TUI uses, which lint checks run, and whether telemetry/reporting is enabled.

## Config Files

Codalotl loads JSON config from:
- `~/.codalotl/config.json`: user-wide defaults.
- `.codalotl/config.json`: project-specific settings, found from the current directory upward.

Config files cascade by setting, not wholesale by file: project config overrides user-wide config for settings it specifies, while unspecified settings can still come from user-wide config or defaults. Environment variables and command flags may also affect effective behavior, especially for credentials and one-off model/permission choices.

Config files are optional. A user can start with only provider credentials and add config files later when they want persistent preferences.

## Inspecting Config

The user can run:

```bash
codalotl config
```

This prints effective configuration, where it came from, effective model, and relevant provider API-key environment variable names.

Secrets shown by `codalotl config` are redacted.

## Credentials

LLM credentials can come from:
- Provider API-key environment variables.
- Provider keys in config.
- Supported provider subscription auth.

Environment variables are the recommended way to configure API keys because they keep secrets out of project files.

Provider auth behavior is described in `features/llms.md`.

## User Preferences

Configuration can set:
- Preferred model or provider.
- TUI theme.
- Auto-approval behavior.
- Documentation reflow width.
- Automatic lint checks.
- Telemetry and crash-reporting opt-outs.
- Custom models for users who need a specific provider endpoint or model alias.

Command flags can override durable preferences for a single run, like selecting a model for `codalotl exec` or enabling auto-approval for one noninteractive command.

## Model Persistence

When the user selects a model in the TUI, Codalotl should persist that selection when configuration persistence is available.

The persisted choice should apply to later sessions without the user needing to select the same model again.

## Invalid Config

If configuration is invalid, Codalotl should fail early with a message the user can act on.

Agent startup should not proceed with invalid config, missing required credentials, or an unavailable selected model.
