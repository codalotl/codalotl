# LLMs

Codalotl supports several LLM providers and lets the user choose which model powers agent sessions.

OpenAI is the primary provider. Anthropic and Gemini are also supported.

## API Keys

LLM providers' API keys are typically configured via env variables:

- `OPENAI_API_KEY`
- `ANTHROPIC_API_KEY`
- `GEMINI_API_KEY`

API keys can also be configured in `.codalotl/config.json` or `~/.codalotl/config.json` under `providerkeys`.

Env variables are the recommended setup because they keep secrets out of project files. When `codalotl config` displays configuration, configured provider keys are redacted.

## Subscriptions

Some providers offer subscriptions which we can use: for instance, the ChatGPT subscription for OpenAI. As of now, this is the only supported one.

OpenAI ChatGPT subscription auth is configured with:

- `codalotl auth openai login`
- `codalotl auth openai login --no-browser`
- `codalotl auth openai status`
- `codalotl auth openai logout`

Login starts a device login flow, has the user approve access in a browser or by following printed instructions, and stores credentials in `~/.codalotl/openai_auth.json`.

- Logging out deletes the auth file.
- Status reports whether saved credentials are present and usable.
- Startup and status checks may refresh saved credentials when possible.
- When the subscription auth file exists, Codalotl uses subscription auth instead of the API key, even if saved auth is expired or invalid. The user must log out explicitly to return to API-key auth for that provider. Reason: if the user logged in with subscription auth, silently falling back to API-key billing is surprising.
    - FUTURE: we may want to fall back to key API key when the subscription usage has been exceeded.
- The TUI indicates a subscription is active (for an appropriate model).

## Model Selection

The user can select a model:

- In the TUI with `/models` and `/model <id>`.
- In config with `preferredmodel`.
- For noninteractive runs with `codalotl exec --model <id>` or `codalotl iterate --model <id>`.

If `preferredprovider` is set (and `preferredmodel` is not set), we use that as a hint in choosing a model (only if that provider also has credentials).

Only models with usable credentials should be offered in model pickers or accepted by startup validation.

## Custom Models

Config may define `custommodels` for model aliases, custom provider model IDs, custom API key env vars, custom endpoints, and model-specific parameters.

Custom models are meant for users who know they need a specific provider endpoint or model variant. They should not be required for ordinary use of OpenAI, Anthropic, or Gemini.

## Startup Validation

Agent commands require usable LLM credentials, either an API key or supported subscription auth. If no usable model can be authenticated, Codalotl exits with instructions for setting provider API keys or logging in to supported subscription auth.

Commands used to configure auth, display help, create PR files, or perform other non-agent setup may run without existing LLM credentials.
