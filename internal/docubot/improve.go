package docubot

import (
	"encoding/json"
	"github.com/codalotl/codalotl/internal/gocode"
	"github.com/codalotl/codalotl/internal/gocodecontext"
	"github.com/codalotl/codalotl/internal/gopackagediff"
	"github.com/codalotl/codalotl/internal/llmcomplete"
	"github.com/codalotl/codalotl/internal/updatedocs"
	"strings"
)

// ImproveDocsOptions specifies options for improving Go documentation using a large language model.
type ImproveDocsOptions struct {
	// True means LLM doesn't get to see current docs in source, so it gets to think more from first principles. On the other hand, it may significantly re-write docs, which can result
	// in unnecessarily large diffs.
	HideCurrentDocs bool

	// Shared configuration and dependencies (ex: model, conversationalist, logging) for LLM-backed operations.
	BaseOptions
}

// betterDocs represents two documentation alternatives for a single identifier and records the selected best version.
type betterDocs struct {
	identifier string // The identifier names the symbol under consideration for documentation improvement.
	a, b       string // Code snippets.
	best       string // Will be set to either a's or b's value.
	reason     string // Why the LLM chose what it did. Useful for debugging or displaying to a user.
}

// ImproveDocs uses an LLM to propose and apply better documentation for the given identifiers in pkg (including identifiers in associated test packages). It generates alternative comments,
// compares them against the current comments, applies the winners to source files, and returns a documentation-only diff.
//
// If identifiers is empty, ImproveDocs considers all identifiers eligible under the default identifier filter, but requiring documentation; otherwise it processes only the provided
// set.
//
// If an identifier has no documentation, it will not be sent to the LLM for improvement, and no error will be reported.
//
// The operation modifies pkg's source files to apply improved comments. On success, it returns the set of documentation changes; if no improvements are chosen, the returned slice is
// empty. If any step fails (ex: cloning, context building, generation, selection, update, or diff), an error is returned and partial updates may already have been applied.
//
// When options.HideCurrentDocs is true, existing comments are hidden from the LLM during generation to encourage first-principles rewrites, which can increase diff sizes.
func ImproveDocs(pkg *gocode.Package, identifiers []string, options ImproveDocsOptions) ([]*gopackagediff.Change, error) {
	var changes []*gopackagediff.Change

	filterOptions := gocode.FilterIdentifiersOptions{OnlyAnyDocs: true}
	err := gocode.EachPackageWithIdentifiers(pkg, identifiers, filterOptions, gocode.FilterIdentifiersOptionsDocumentedNonAmbiguous, func(p *gocode.Package, ids []string, onlyTests bool) error {

		pChanges, err := improveDocsForIDs(p, ids, onlyTests, options)
		if err != nil {
			return err
		}

		changes = append(changes, pChanges...)

		return nil
	})

	return changes, err
}

// improveDocsForIDs generates alternative documentation for the given identifiers, compares the alternatives to the existing comments, applies any winners to pkg, and returns a documentation-only
// diff.
//
// Each identifier must already have some documentation; if any is missing, an error is returned and no improvements are attempted. The function operates per context group: it constructs
// minimal code contexts, generates new comments in a cloned working copy, forms A/B choices (new vs current) for each identifier, asks the LLM to pick the better snippet, and applies
// only the winners back to the real package on disk.
//
// pkg, identifiers, and onlyTests must align: identifiers must be in pkg (and not pkg.TestPackage). If onlyTests, all identifiers are tests. Otherwise, all identifiers are non-tests.
//
// On success, the returned changes describe documentation differences (function bodies are excluded) between the original package state and the updated package, limited to the given
// identifiers. If no improvements are chosen, the slice is empty. If a fatal error occurs (ex: cloning failures, context building, selection, apply, or diff), an error is returned;
// partial updates to pkg may already have been written. Generation-related non-fatal cases (ex: no snippets or partial application) are tolerated and reported via logging.
func improveDocsForIDs(pkg *gocode.Package, identifiers []string, onlyTests bool, options ImproveDocsOptions) ([]*gopackagediff.Change, error) {

	// Ensure identifiers all have docs of some kind (in order to improve docs, we need existing docs):
	// NOTE: speculative idea: we could, upon discovering missing docs, generate docs now. But this approach is easier for now.
	for _, id := range identifiers {
		snippet := pkg.GetSnippet(id)
		anyDocs, _ := gocode.IDIsDocumented(snippet, id, false)
		if !anyDocs {
			return nil, options.LogNewErr("identifier missing documentation", "id", id)
		}
	}

	// Clone the original package for diffing at the end:
	// NOTE: Can't use clonedPkg below because those are mutated (new docs are generated/applied to them).
	clonedForDiff, err := pkg.Clone()
	if err != nil {
		return nil, options.LogWrappedErr("improve_docs.clone.for_diff", err)
	}
	defer clonedForDiff.Module.DeleteClone()

	// Create a stripped clone of the package so the LLM can generate fresh docs:
	clonedPkg, err := pkg.Clone()
	if err != nil {
		return nil, options.LogWrappedErr("improve_docs.clone.for_gen", err)
	}
	defer clonedPkg.Module.DeleteClone()

	if options.HideCurrentDocs {
		clonedPkg, err = updatedocs.RemoveDocumentation(clonedPkg, identifiers)
		if err != nil {
			return nil, options.LogWrappedErr("improve_docs.remove_documentation", err)
		}
	}

	// Build contexts for the identifiers using the clone:
	groups, err := gocodecontext.Groups(pkg.Module, clonedPkg, gocodecontext.GroupOptions{IncludePackageDocs: true, IncludeTestFiles: onlyTests, IncludeExternalDeps: true, CountTokens: countTokens})
	if err != nil {
		return nil, options.LogWrappedErr("improve_docs.groups", err)
	}
	targetGroups := gocodecontext.FilterGroupsForIdentifiers(groups, identifiers)
	contexts := gocodecontext.NewContextsForIdentifiers(targetGroups, identifiers)

	// For each (context, ids):
	//   - generate new docs, apply to clone (generateAndApplyDocs)
	//   - generate choices: current docs, or new ones. let LLM choose (chooseBetterDocsForIdentifiers)
	//   - apply the best one (updatedocs.UpdateDocumentation)
	baseOpts := BaseOptions{Conversationalist: options.Conversationalist, Ctx: options.Ctx, ReflowMaxWidth: options.ReflowMaxWidth}
	for ctx, ids := range contexts {
		idsStr := strings.Join(ids, ", ") // for logging and messaging

		options.userMessagef("")
		options.userMessagef("Improving docs for %s...", idsStr)
		options.userMessagef("-------------------------")

		// Generate new docs, apply to clone:
		updatedClone, _, genErr := generateAndApplyDocs(clonedPkg, ctx, ids, true, baseOpts)
		if genErr != nil && genErr != errNoSnippets && genErr != errSomeSnippetsFailed {
			return nil, options.LogWrappedErr("improve_docs_for_identifiers.generate_and_apply_docs", genErr, "ids", idsStr)
		}
		if updatedClone != nil {
			clonedPkg = updatedClone
		}

		// Construct A/B choices for each identifier (A = new, B = current):
		var choices []betterDocs
		for _, id := range ids {
			newSnip := clonedPkg.GetSnippet(id)
			oldSnip := pkg.GetSnippet(id)
			if newSnip == nil || oldSnip == nil {
				return nil, options.LogNewErr("improve_docs_for_identifiers.get_snippet: nil", "newSnipNil", newSnip == nil, "oldSnipNil", oldSnip == nil)
			}

			newSnipStr := strings.TrimSpace(string(newSnip.Bytes()))
			oldSnipStr := strings.TrimSpace(string(oldSnip.Bytes()))

			// If there's no change for some reason, continue:
			if newSnipStr == oldSnipStr {
				options.Log("no change in snippet after generating new docs", "id", id, "multiline", newSnipStr)
				continue
			}
			choices = append(choices, betterDocs{
				identifier: id,
				a:          newSnipStr,
				b:          oldSnipStr,
			})
		}

		// nothing to compare:
		if len(choices) == 0 {
			options.userMessagef("No alternative docs generated for ids=%q", idsStr)
			continue
		}

		for _, c := range choices {
			options.userMessagef("Choices for %s:", c.identifier)
			options.userMessagef("A - NEW:")
			options.userMessagef(c.a)
			options.userMessagef("")
			options.userMessagef("B - ORIGINAL:")
			options.userMessagef(c.b)
			options.userMessagef("")
		}

		// Pick the better docs for each identifier:
		updatedChoices, err := chooseBetterDocsForIdentifiers(ctx, choices, options)
		if err != nil {
			return nil, options.LogWrappedErr("improve_docs_for_identifiers.choose_better_docs_for_identifiers", err, "ids", idsStr)
		}

		// Gather the winning snippets that differ from the original docs.
		var snippetsToApply []string
		for _, c := range updatedChoices {
			if c.best == "" {
				options.userMessagef("Both docs are ~equal for %s. Ignoring...", c.identifier)
				options.userMessagef("Reason: %s", c.reason)
				continue
			}
			oldSnip := pkg.GetSnippet(c.identifier)
			if oldSnip == nil {
				return nil, options.LogNewErr("improve_docs_for_identifiers.get_snippet after choose: nil", "id", c.identifier)
			}

			// If model votes for current docs, nothing to do:
			// NOTE: In future, if we randomize a vs b, need to update this
			// NOTE: strings.TrimSpace(c.best) == strings.TrimSpace(string(oldSnip.Bytes())) is wrong because we could apply multiple improvements to the same block.
			if c.best == c.b {
				options.userMessagef("Original docs for %s were better. Ignoring...", c.identifier)
				options.userMessagef("Reason: %s", c.reason)
				continue
			}

			options.userMessagef("New docs for %s are better. Using...", c.identifier)
			options.userMessagef("Reason: %s", c.reason)

			snippetsToApply = append(snippetsToApply, c.best)
		}

		if len(snippetsToApply) == 0 {
			continue // LLM decided existing docs are already the best.
		}

		// Apply the improved documentation snippets to the real package.
		updatedPkg, _, snippetErrs, err := updatedocs.UpdateDocumentation(pkg, snippetsToApply, options.updatedocsOptions(false))
		if err != nil {
			return nil, options.LogWrappedErr("improve_docs_for_identifiers.update_documentation", err)
		}

		if len(snippetErrs) > 0 {
			options.userMessagef("When updating codebase's documentation got errors:")
			for _, se := range snippetErrs {
				options.userMessagef("Snippet:\n%s", se.Snippet)
				options.userMessagef("Error:", se.UserErrorMessage)
			}
		}

		if updatedPkg != nil {
			pkg = updatedPkg
		}
	}

	// Compute and return documentation diff.
	docChanges, err := gopackagediff.Diff(clonedForDiff, pkg, identifiers, nil, true)
	if err != nil {
		return nil, options.LogWrappedErr("improve_docs_for_identifiers.diff", err)
	}
	return docChanges, nil
}

// chooseBetterDocsForIdentifiers makes a single request to the LLM, sending context.Code() and choices. For each identifier in choices, it will ask the LLM to choose either a or b.
// An updated choices slice will be returned, with each choice's `best` field set to the better snippet. If the snippets are ~equal, `best` may be empty. An error is returned for hard
// errors (ex: I/O, failure parsing JSON response).
//
// All identifiers in choices should be in context so that the LLM can see function bodies. Callers may choose to create a context where choices' identifiers have no docs to avoid tainting;
// this function will use context as-is.
func chooseBetterDocsForIdentifiers(ctx *gocodecontext.Context, choices []betterDocs, options ImproveDocsOptions) ([]betterDocs, error) {

	// Build the instructions section that enumerates the identifiers and their documentation options.
	instructions := chooseBetterDocsInstructions(choices)

	// Combine the code context with the instructions that list the choices.
	llmUserMessage := ctx.Code() + instructions

	// Ensure we have a conversationalist to talk to the LLM.
	conv := options.Conversationalist
	if conv == nil {
		conv = llmcomplete.NewConversationalist()
	}

	// Send to LLM:
	prompt := promptChooseBestDocs()
	conversation := conv.NewConversation(llmcomplete.ModelIDOrDefault(options.Model), prompt)
	conversation.SetLogger(options.Logger)
	conversation.AddUserMessage(llmUserMessage)
	response, err := conversation.Send()
	if err != nil {
		return nil, options.LogWrappedErr("choose_better_docs.llm_send", err)
	}

	type pick struct {
		Best   string `json:"best,omitempty"`
		Reason string `json:"reason,omitempty"`
	}

	var raw map[string]pick
	if err := json.Unmarshal([]byte(strings.TrimSpace(unwrapSingleSnippet(response.Text))), &raw); err != nil {
		return nil, options.LogWrappedErr("choose_better_docs.json_unmarshal", err)
	}

	// Populate the best and reason fields for each choice based on the LLM's selection.
	for i, c := range choices {
		p, ok := raw[c.identifier]
		if !ok {
			continue // leave best empty if not provided
		}
		pickVal := p.Best

		switch pickVal {
		case "A":
			choices[i].best = c.a
		case "B":
			choices[i].best = c.b
		case "":
			// equal quality
		default:
			// Unknown value; ignore.
		}

		choices[i].reason = p.Reason
	}

	return choices, nil
}

// chooseBetterDocsInstructions returns instructions for an LLM to review documentation alternatives.
//
// For each identifier in choices, the output includes both options (a and b), each wrapped as a Go code block, enabling the LLM to choose between them.
func chooseBetterDocsInstructions(choices []betterDocs) string {
	var b strings.Builder

	// Provide a header so the LLM knows what follows. The system prompt already describes the task, but
	// reiterating the structure here helps avoid mistakes.
	b.WriteString("\nIdentifiers and documentation choices:\n\n")

	for _, choice := range choices {
		b.WriteString(choice.identifier)
		b.WriteString(":\n")

		// Option A
		b.WriteString("A:\n")
		b.WriteString("```go\n")
		b.WriteString(strings.TrimSpace(choice.a))
		b.WriteString("\n```\n\n")

		// Option B
		b.WriteString("B:\n")
		b.WriteString("```go\n")
		b.WriteString(strings.TrimSpace(choice.b))
		b.WriteString("\n```\n\n")
	}

	return b.String()
}
