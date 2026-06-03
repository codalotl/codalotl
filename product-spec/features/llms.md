# LLMs

Codalotl supports several LLM providers.

## API Keys

LLM providers' API key is typically configured via an env variable. For instance, if `OPENAI_API_KEY` is set, we'll use that.

## Subscriptions

Some providers offer subscriptions which we can use: for instance, the ChatGPT subscription for OpenAI. As of now, this is the only supported one.

These are configured by: TODO.

- Logging out deletes the auth file.
- When a subscription file exists (e.g., `~/.codalotl/openai_auth.json`), we will NOT use the API key, even if the auth has expired or is now invalid for some reason. The user must log out explicitly. Reason: we
