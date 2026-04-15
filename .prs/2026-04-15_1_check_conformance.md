# PR

## User Summary (do not modify)

Problem/Background:
- Many Go packages have SPEC.md files, which are control planes and specifications for their Go code.
- The $spec-md skill (see internal/skills/default/spec-md/SKILL.md) has the concept of "checking conformance" - seeing if the Go code complies with the SPEC.md.
- We ALSO have the concept of "cas" - content-addressable storage. If we spend the LLM tokens to verify that a Go package conforms, we can record that fact.
    - This lets us keep track of which Go packages are known to conform, and which we need to re-check.
    - It also perhaps lets us determine the last known good state of a package, and then inductively determine that a changeset still conforms based on diffs to SPEC.md and Go code.
        - Ex: "it conforms at merge base, and we only added a single comment, so its conformance must not have changed."
- Since SPEC.md files are integral to our development system, it's imperative we have good hygiene around keeping them up-to-date and accurate. Otherwise the system breaks down and cannot be trusted.
- The tools and workflow to keep the CAS up to date with spec conformance checks are very manual and unwieldy today:
    - Run a CLI command to determine packages that don't have CAS-verified conformance.
    - Use package mode on packages one by one.
        - Type "$spec-md check conformance" (literally just that; it works reasonably well. The prompt is in the skill.)
        - Interpret what the agent says:
            - conforms: run a CLI command to set CAS on the package for spec conformance
            - doesn't conform: read the analysis from the agent, filter it through my human judgment
            - agent is wrong or nitpicky about an irrelevant thing: set CAS conformance anyway
            - agent is right: address the issue (sometimes update spec, sometimes update code, again based on my human judgment)
- We are exploring the "orchestrator" mode in codalotl - it is making many iterative commits, bringing a PR across the finish line.
    - It doesn't currently know about SPEC conformance or CAS at all

Goal:
- Make a new tool, "check_spec_conformance"
- Add the tool to the orchestrator agent's list of tools
- Nongoal: tell the orchestrator about this tool (I intend to manually tell the orchestrator to use it for now, until we prove it works well through experimentation)

Details:
- check_spec_conformance lives in package internal/tools/spectools (new package).
- Tool shape: check_spec_conformance(only_changed: true)
    - only_changed: true only checks conformance of packages that have a diff. So if only_changed=true and we're on a git branch, and we modify package internal/foo but not internal/bar, then internal/bar is not checked for conformance.
        - NOTE on diff: we check current state of files on disk vs X, where X is:
            - if we're on the main or master branch, we diff vs HEAD (e.g., current commit)
            - if we're on another branch, X is the point that the branch branched off of its parent. So for typical simple feature branches, it's the point we branched off of main/master.
            - Note to orchestrator: Decide and document exact semantics here in `## Decisions`.
- Tool output (ToolResult.Result):
    - JSON (described below)
- Tool side effects: any package that conforms should have its CAS set (this writes a file, in my case, to .codalotl/cas/...)
- The tool has a presenter that formats the JSON into nice human-digestible text (see review tool).
- does NOT check conformance for a package if there is no SPEC.md file in the package
- does NOT check conformance for any package that CAS says already conforms.
    - (and again further restricts that if only_changed is true).

Tool JSON output shape:

```json
{
    "internal/foo": {
        "conforms": true
    },
    "internal/bar": {
        "conforms": false,
        "nonconformances": [
            {"severity": "trivial", "latent": true, "message": "The spec demands X, but instead we have Y"},
            {"severity": "major", "latent": false, "message": "explanation"}
        ]
    }
}
```

NOTES on output:
- Allowed severities: `{"trivial", "minor", "major"}`
- latent=true indicates the non-conformance existed at the merge base. latent=false indicates the diff introduced the nonconformance
    - If there's no diff (ex: branch new feature branch), then everything checked will be latent=true.
    - If there's no diff and only_changed=true, then nothing is checked

More Details:
- This tool spins up **concurrent** subagents. Each subagent checks one package.
    - Limit concurrency to a configurable number (start with 5)
    - For now, we will accept that the TUI/etc will show interleaved results. This is not great. I have a plan to make this nicer. But out of scope for the PR.
- The subagent can just use an existing subagent like "limited_package_mode" for now (it's not read only. that's okay for v1)
- It is given the diff for its package automatically. Its package definition is: all the files in the folder, as well as anything the code unit jail typically allows (e.g., data, testdata, etc.)
    - If there is no diff, you decide what to do. Tell it about no diff, hide from it the latent distinction, or ???
- The agent should have its skill, so tell it the equivalent of "$spec-md check conformance". Not much more than that! Probably explain severity and latent, and tell it the output format.
- The subagent should be ~read-only. It's NOT critical to "lock this down". We can do that later. But the overall intention is that this is a read-only operation.
- We do have some automatic conformance checks.
    - Run the equivalent of `codalotl spec diff path/to/pkg` (don't shell out! do what it does) and supply to subagent as context before it starts.
- Figure out how to reconcile the instructions in the $spec-md skill for checking conformance and slightly overriding those instructions. Document in `## Decisions`
