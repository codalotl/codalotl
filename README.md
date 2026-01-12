# codalotl

Codalotl is a coding agent for Go (TUI + CLI). By focusing exclusively on Go, the same LLM can deliver better performance, faster, for a cheaper cost.

## Results

Top agents and LLMs have been benchmarked for Go-specific tasks. Codalotl with gpt-5.2-high has the same success rate as codex, but is **20% cheaper** and **25% faster** with the same underlying LLM. I think that's pretty cool.

| Agent | Model | Success | Avg Cost | Avg Time |
| --- | --- | --- | --- | --- |
| codalotl | gpt-5.2-high | 100% | $0.45 | 7m 50s |
| codex | gpt-5.2-high | 100% | $0.56 | 10m 31s |
| codex | gpt-5.1-codex-high | 86% | $1.39 | 10m 37s |
| cursor-agent | composer-1 | 57% | $0.27 | 1m 6s |
| claude | claude-opus-4.5-thinking | 43% | $1.47 | 4m 23s |
| crush | grok-code-fast-1 | 43% | $0.94 | 4m 57s |
| claude | claude-sonnet-4.5-thinking | 14% | $0.94 | 4m 45s |
| crush | grok-4-1-fast-reasoning | 0% | $0.07 | 2m 9s |

Results as of 2025-12-22. See [github.com/codalotl/goagentbench](https://github.com/codalotl/goagentbench).

Important Note:
It is important to look at the benchmark test scenarios to see if they align with how you use coding agents. These focus on making specific changes to a package, often with updates to other related packages. They are NOT high-level prompts to vibe code entire applications; nor are they prompts to make extensive changes to **many** packages at once; nor are they "fix this ambiguous issue reported on GitHub".

## Getting Started

### Install or Upgrade

```
go install github.com/codalotl/codalotl@latest
```

### Configure LLM Provider Keys

Codalotl requires OpenAI API keys. The best way to do that is to set the ENV variable `OPENAI_API_KEY`.

Currently, only OpenAI models are supported - I recommend `gpt-5.2` on `high` thinking:
- Other models, notably Opus, exhibit significantly poorer performance in my tests.
- Given that, I haven't prioritized other providers.

### Running it

Start the TUI:

```
codalotl
```

### Using Package Mode

Enter package mode (the main mechanism Codalotl uses to build dedicated Go support) by typing `/package path/to/pkg`. Note that `path/to/pkg` may just be `.`.

From there, type your prompt (ex: `implement xyz feature`). The agent will automatically be supplied with context from your target package.

## Status and Limitations

Codalotl is **usable** and **effective** as-is: I have replaced most of my other agent usage with it. There are some important limitations:
- Not extensively tested on various platforms/OSes. Works on OSX and Linux.
- Not extensively tested on various repos/codebases/versions of Go. Untested in multi-module/go.work projects.
- Has a number of UX issues (example: poor copy/paste support; onboarding; error messages).
- Some common agent features are unimplemented (session resumption; mcp; skills; custom commands).
- Only OpenAI models are supported at the moment; you must bring your own key.

All of these will be addressed over time.

## How Codalotl Works

To use Codalotl's Go-specific features, you'll need to enter **Package Mode**: `/package path/to/pkg`. This isolates the agent to primarily working on this package. The following mechanisms are used:

### Package Isolation

In Package Mode, the agent is isolated to work in a single package: directly reading and writing files and listing directories are **limited to this package**.

The agent does NOT have a `shell` tool (in my experience, LLMs cannot help but use `shell` and violate their instructions to explore outside their package, even with strong prompting).

In exchange for these limitations, the agent gets **confidence** to work in the current package without analysis-paralysis of working in a large codebase. As long as you, the human developer, set the correct package, this is a very large benefit.

That being said, the agent DOES have levers to work in a multi-package environment:
- It can read the public API (eg, its godoc) of any other package in the module. This is often **much more token efficient** than using `grep` and directly reading various files throughout a codebase.
- If the public API is poorly commented, it can spawn a subagent to answer questions about upstream packages (ex: "how does func Foo work when xyz").
- It can spawn subagents to implement changes either upstream or downstream. Examples:
    - Change a used package so that it has xyz feature, that we need.
    - Change packages that use the current package to use a new API that was just implemented (in the future, this will be parallelized).
    - In both of these examples, the subagent uses a separate context window, so the main agent's context is protected from getting watered down.
- It can run the overall project tests.

You can manually give the agent context outside its package by using `@` to mention specific files or directories - the agent will be able to directly read them, even if outside the package.

### Automatic context creation for a Go package

Every session in Package Mode starts with a **bundle of context** for the current package. This context includes:
- List of files in the package.
- A "package map": a list of functions and identifiers, and where they are found in the files. Comments/function bodies are stripped.
- A list of packages that **use** the current package.
- Build status, test status, and lint status.

So, before the agent even starts, it knows which files exist, which code is defined where, who uses the package, and the package's build/test status. Compare that to traditional agents: they'll usually start off using `ls` in various directories, then `grep` to find out where things are, then reading files to find relevant code. Traditional agents might only check for failing tests/build later, throwing a wrench in their assumptions. All of this is given a small, neat package from the get-go.

You can explore this initial context using Codalotl CLI: `codalotl context initial path/to/pkg` will print to stdout this initial context. Here's an [example](https://gist.github.com/cypriss/ec4153a142566267488958cec6f4b7cb).

### Automatic gofmt

Any patches the agent applies to the codebase will be automatically `gofmt`ed (in the same tool call as the patch). This can cut out a lot of back and forth of failing builds and multiple tool calls.

### Automatic lints on patch

All patches made will automatically check for build errors and lint issues (in the same tool call as the patch). Again, cuts out a lot of back and forth.

In the future, I intend to make the lint checkers extensible, so things like `golangci-lint` or `staticcheck` automatically run on every patch.

## Dependencies

This repository strives for minimal dependencies. Any dependency that can plausibly be re-implemented in this repo, will be. A new dependency will only be added with the greatest of regret. A few reasons:
- Dependencies have costs. They also often come with transitive dependencies.
- Dependencies often don't solve exactly your problem and come with bloat.
- AI can now quickly re-implement dependencies. I suspect it changes the equation, especially over time.
- It gives me more opportunity to dog-food this software.

## Metrics

Codalotl records pseudonymous usage metrics (tied to a device-specific hash), which maintainers rely on to inform development and support priorities. The metrics include solely usage metadata; prompts, responses, and code are NEVER collected.

To disable metrics collection, add to your config:

```
{
    "disabletelemetry": true,
    "disablecrashreporting": true
}
```

