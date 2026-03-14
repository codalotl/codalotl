# codalotl

Codalotl is a coding agent for Go (TUI + CLI). By focusing exclusively on Go, the same LLM can deliver better performance, faster, for a cheaper cost.

![Screenshot](/../images/images/screenshot1.png)

## Results

Top agents and LLMs have been benchmarked for Go-specific tasks. Codalotl with gpt-5.4-high has a similar success rate to codex, but is **40% cheaper** and **185.0% faster** with the same underlying LLM. I think that's pretty cool.

| Agent | Model | Success | Avg Cost | Avg Time |
| --- | --- | --- | --- | --- |
| codalotl | gpt-5.4-high | 79% | $0.40 | 4m 31s |
| codex | gpt-5.4-high | 79% | $0.66 | 12m 52s |
| codalotl | claude-opus-4-6 | 78% | $1.71 | 7m 46s |
| codalotl | gemini-3.1 | 71% | $0.35 | 3m 21s |

Results as of 2026-03-13. See [result_summaries/summary_2026-03-13_16-00-24](result_summaries/summary_2026-03-13_16-00-24).

Important Note:
It is important to look at the benchmark test scenarios to see if they align with how you use coding agents. These focus on making specific changes to a package, often with updates to other related packages. They are NOT high-level prompts to vibe code entire applications; nor are they prompts to make extensive changes to **many** packages at once; nor are they "fix this ambiguous issue reported on GitHub".

## Getting Started

### Install or Upgrade

```
go install github.com/codalotl/codalotl@latest
```

### Configure LLM Provider Keys

Codalotl requires an LLM provider API key. The best way to do that is to set one of:
- `OPENAI_API_KEY`
- `ANTHROPIC_API_KEY`
- `GEMINI_API_KEY`

OpenAI, Anthropic, and Gemini models are supported. I recommend starting with `gpt-5.4-high`. As of 2026/03/13:
- OpenAI reasoning models have performed best for me on the benchmark above.
- But Gemini 3.1 has reasonable intelligence, and is faster and cheaper.
- Anthropic has good intelligence but is slow and expensive.

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
- Some common agent features are unimplemented (session resumption; mcp).
- Only OpenAI, Anthropic, and Gemini models are supported; you must bring your own key.

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

All patches made will automatically check for build errors and lint issues (in the same tool call as the patch). Lints are configurable and extensible. Again, cuts out a lot of back and forth.

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
