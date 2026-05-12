# Philosophy

This document contains philosophies and principles that can guide product decisions and affect prioritization. These are not hard-and-fast invariants; rather, principles that are usually true.

## Direction: user defines goals

The direction we're heading is that the user can be nontechnical, and simply inputs their goals. "Build me a system to X, that has properties Y and Z." The user is welcome to supply their opinions at any step or layer in the process. From the goals, we build layers specs and actual software. We test and validate the software works.

## Go-optimal; Opinionated

We prioritize optimal tooling, specifically tuned for Go projects, and opinionated workflow. Provided that's satisfied, we can offer customizability.
- Examples of Go-specific optimization:
    - package mode
    - optimized Go LLM context
    - special tools that only work in go projects, like clarify_public_api
    - documentation generation that rely on parsing the AST
- Examples of opinionated workfow:
    - Each package should have a SPEC.md file, which code is based on.
    - An /orchestrate command runs an opinionated workflow.

Other projects, like Claude Code and Codex, naturally optimize for generic, good tooling, for 80-90% of situations. Adaptable and customizable. For instance, they have primitives that let you define subagents and custom prompts and skills, but mostly leave that to each developer to figure out.

## Cohesive > Customizable

The first priority is a cohesive, default setup that works really well together. Once that is satisfied, we are happy to let users customize.
- Strive to allow any prompt/markdown file sent to the LLM to be customized.
- Strive to allow tool descriptions to be customized.
- Config files allow various settings to be tweaked. Ex: lints, models, column widths, etc.

Since the first priority is cohesiveness, the customizability might not always be "optimal". For instance, multiple prompts/markdown files might refer to SPEC.md. So to cleanly delete that concept, a user might need to override several markdown files.

## Layered & Modular

Cohesive beats layered & modular. That being said, all else being equal, we strive to build a layered and modular system.

Software construction is layered and modular. We strive to allow users to operate at any layer, and to swap out modules for others that fit their needs.

Main layers:
0: This is the base layer, the bread and butter of actually editing code. Has generic agent mode; package mode. You type in a box, and the agent codes something.
1: SPEC.md in packages. Users don't have to use this. But we integrate it nicely (NOTE: this is a "~layer" -- bit different than other layers -- that's okay).
2: PR orchestration.
3: ??? product definition, prioritization, PR-definer, etc

Modules:
- QA: 
- Product Spec: 

These layers are respected abstraction points. For instance, the orchestrator knows about SPEC.md, but not vise versa.

Likewise, the Product Spec module can be swapped out for a different product system system in some way (for instance, by editing markdown files, or by implementing it to an interface - either Go or MCP, as examples).

## Security & Safety

We prioritize UX over security. We lean on models' improvements over time to offer better safety.
- Codalotl does not use an OS-level jail. For instance, if a user's source file has code to read/write outside the sandbox, Codalotl does not block it.
- Users who want top security can run codalotl in a VM/container/OS-protected sandbox.
- Many of the "security" features of Codalotl are actually UX and product features. For instance, "package mode" confines an agent to a single package. But really, that's just to teach the LLM it shouldn't directly read/write other packages, but instead rely on the built-in tools, like get_public_api.
- All that being said, don't make stupid security decisions for no reason; this is only when they are in tension.

## Models

Prioritize OpenAI models. Offer built-in access to top model providers. Deprioritize the long tail and locally running models.
- This could change over time. But for now, prompts are primarily developed and tested against OpenAI's latest models.
- Until local OSS models cross some intelligence threshold, we prefer optimizing for the strongest models, even at the expense of cost.
