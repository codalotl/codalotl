# PR

## User Summary (do not modify)

Checking spec conformance is a key piece of the codalotl workflow. codalotl uses package-level SPEC.md files as the contract for what the code is supposed to do, then asks an AI reviewer to compare the implementation against that contract. The result is a list of possible nonconformances, categorized by severity and by whether the issue appears to be new or latent.

That check is useful because it gives the orchestrator a concrete bridge between "the spec says X" and "the code appears to do Y." In the broader workflow, those findings drive the next step: either assign implementation work, update the spec to match intended behavior, or choose a compromise when the spec and code are both partly right.

Problem: when the orchestrator gets a nonconformance report, it's mostly just a one-liner like "[new][minor] When X happens, Z doesn't happen because Y." (for some X, Z, and Y). The orchestrator takes all this at face value and spins up an implementor to fix it. However, in my experience, "just fixing the code" is the solution roughly one-third of the time. Another third is to change the SPEC.md (loosening some requirement, or simply bringing the spec up-to-date). The last third might be a pragmatic compromise between spec fix and code change.

Solution: I want to start by having the spec conformance check return additional information. I have asked the AI to answer these questions:

Answer the following questions (they might not all apply, depending on the nonconformance and your analysis).
- First, give a 1-2 paragraph summary of the issue, with an example (if relevant).
- Is it a real nonconformance?
- Imagine fixing the code to conform. Is the solution small/medium/large? What is the risk? blast radius? Is it isolated to the package? Does it change the public API in any way?
- Does fixing this nonconformance bring actual value to the end-user? Or is this just academic?
  - What is the UX if this nonconformance is "triggered" by the user?
  - How likely is the user to actually experience this?
  - What should the UX be here?
- Does fixing this introduce even worse UX, or some other bad tradeoff?
- How likely is the bug (if this is a bug) to occur?
- An AI generated this nonconformance report. Is the AI just being nitpicky? Would a senior engineer with good judgment care about this?
- What is the ROI of fixing the code?
- Overall, what is your recommendation? Should we fix the code, or update the SPEC to allow current behavior?

These questions guide the AI to think about the right types of things rather than blindly fixing something.

Instead of prompting the AI to think about these upfront when determining conformance vs nonconformance, I want to ask these during a follow-up turn. Basically (this is the gist, not the actual prompts):

```
Tool: Find spec conformance issues. List in plain text. Categorize by minor/etc and new vs latent.
* (AI thinking and reading files)
AI: I found these issues:
1. [new][minor] When X happens, Z doesn't happen because Y.
2. [new][minor] When X' happens, Z' doesn't happen because Y'.
Tool: Analyze these issues. For each nonconformance, answer the following questions. <questions above>. Then, return JSON in ___ form.
AI: ... working ...
AI: {
    "nonconformances": [
        {
            "summary": "When X happens, Z doesn't happen because Y",
            "severity": "minor",
            "type": "new",
            "analysis": "<answer to my questions>"
        }, ...
    ]
}
```

The 2-turn approach is important so that the AI doesn't change what it considers nonconformance by factors like "how hard is it to fix", and to simplify what it needs to think about at one time.

From a user perspective:
- The analysis is hidden. The overall tool presenter does not write the analysis.

In order to do this, I believe we'll need to add `Create` to the interface `AgentInvoker`.

## Plan

### Phase 0: Design two-turn conformance analysis [DONE]

#### Package internal/tools/spectools [DONE]
- Update `check_spec_conformance` contract so nonconformance results include hidden `analysis` details for orchestrator decision-making.
- Preserve first-turn conformance detection as the source of truth; only after nonconformances are identified should the checker ask a follow-up turn for analysis.
- Keep presenter output compact and omit analysis from user-facing tool presentation.
- Spec changes live in `internal/tools/spectools/SPEC.md`.

#### Package internal/tools/toolsetinterface [DONE]
- Add `Create(ctx, agentName, req)` to `AgentInvoker` so tools can create an idle agent and conduct multi-turn subagent workflows.
- Keep `Invoke` as the convenience one-shot API.
- No `SPEC.md` exists for this package; rely on `agentregistry` public API/spec plus implementation tests.

#### Package internal/agentregistry [DONE]
- Registry already exposes `Create`; make it satisfy the expanded `AgentInvoker` interface.
- Update tests if needed after the interface change.

### Phase 1: Implement two-turn analysis [DONE]

#### Package internal/tools/spectools [DONE]
- Use `AgentInvoker.Create` in `check_spec_conformance` to run package checks as:
  1. Send initial check-conformance instructions and parse strict verdict JSON.
  2. If `conforms=false`, send follow-up instructions that include the first verdict and request analysis for each issue.
  3. Return final raw JSON with `analysis` on each nonconformance.
- Validate and parse the expanded result shape.
- Keep CAS writes keyed only to final conformance verdict.
- Add/update focused tests around result parsing, presenter hiding, and two-turn invocation.

#### Package internal/tools/toolsetinterface and downstream fakes [DONE]
- Expand `AgentInvoker` and update fake/test implementations across packages.
- If compile breakages appear in downstream packages, update callsites in the same implementation step.

## Review

### SPEC conformance: 2026-04-25 [DONE]

`check_spec_conformance({"only_changed":true})` passed.

- `internal/agentbuilder`: conforms
- `internal/agentregistry`: conforms
- `internal/tools/pkgtools`: conforms
- `internal/tools/spectools`: conforms

## Summary

TBD

## State

- Active PR file: `.prs/2026-04-25_2_csp-analysis.md`
- Branch: `jn/csc-analysis`
- Workspace was clean at start of orchestration.
- Relevant packages:
  - `internal/tools/spectools`: `check_spec_conformance` implementation and SPEC.
  - `internal/tools/toolsetinterface`: `AgentInvoker` interface and `InvokeRequest`.
  - `internal/agentregistry`: already has `Registry.Create` and `Registry.Invoke`; `Registry` is assigned into `ToolOptions.AgentInvoker`.
- Implementation commit: `8d19136 spectools: analyze conformance findings`
- Implementation details:
  - `AgentInvoker` now includes `Create`.
  - `check_spec_conformance` creates an idle package-check subagent, sends first-turn conformance instructions, then sends follow-up analysis instructions only for nonconforming packages.
  - Follow-up result must preserve first-turn issue count/order/severity/latent/message; only `analysis` is merged.
  - Raw JSON includes nonconformance `analysis`; compact presentation hides it.
- Validation run after implementation:
  - `go test ./internal/tools/spectools`
  - `go test ./internal/tools/toolsetinterface ./internal/tools/pkgtools ./internal/agentbuilder ./internal/agentregistry`
  - `go test ./...`
- SPEC conformance run:
  - `check_spec_conformance({"only_changed":true})`
  - Passed for `internal/agentbuilder`, `internal/agentregistry`, `internal/tools/pkgtools`, `internal/tools/spectools`.
