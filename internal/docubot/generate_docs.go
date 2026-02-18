package docubot

import (
	"fmt"
	"github.com/codalotl/codalotl/internal/gocode"
	"github.com/codalotl/codalotl/internal/gocodecontext"
	"github.com/codalotl/codalotl/internal/llmcomplete"
	"github.com/codalotl/codalotl/internal/q/health"
	"github.com/codalotl/codalotl/internal/updatedocs"
	"strings"
)

// BaseOptions carries shared configuration and dependencies for LLM-backed documentation operations.
type BaseOptions struct {
	// Maximum number of concurrent LLM requests to perform (if parallelism is supported). If zero, the default (5) is used.
	MaxParallelism int

	// ReflowMaxWidth sets the desired wrap width when reflowing documentation comments. A value of zero uses a sensible default of 180 to preserve previous behavior.
	ReflowMaxWidth int

	// Model enables callers to choose an explicit model (this can, in theory, also be accomplished by Conversationalist, but is less ergonomic to callers).
	Model llmcomplete.ModelID

	// Conversationalist allows callers to inject their own LLM implementations, including mock implementations for testing.
	Conversationalist llmcomplete.Conversationalist

	// Logging and health context for operations.
	health.Ctx
}

// defaultReflowMaxWidth is the single source of truth for the fallback wrap width.
const defaultReflowMaxWidth = 180

// effectiveReflowMaxWidth returns the configured width, or a consistent default when unset.
func (o BaseOptions) effectiveReflowMaxWidth() int {
	if o.ReflowMaxWidth > 0 {
		return o.ReflowMaxWidth
	}
	return defaultReflowMaxWidth
}

// updatedocsOptions constructs updatedocs.Options using this BaseOptions and the provided rejectUpdates flag.
func (o BaseOptions) updatedocsOptions(rejectUpdates bool) updatedocs.Options {
	return updatedocs.Options{Reflow: true, ReflowMaxWidth: o.effectiveReflowMaxWidth(), RejectUpdates: rejectUpdates}
}

// userMessagef writes msg/args (in printf format) to stdout. If o.Logger is set, it also logs the message there.
//
// Design Note: This lets us, in the future, add BaseOptions#Verbose or a BaseOptions#Println func to control if and where the message is written to.
func (o *BaseOptions) userMessagef(msg string, args ...any) {
	str := fmt.Sprintf(strings.TrimRight(msg, "\n"), args...)
	fmt.Println(str)
	if o.Logger != nil {
		o.Logger.Info(str)
	}
}

// generateAndApplyDocs generates identifiers' docs via LLM with codeCtx as LLM context, and then applies those changes to pkg. redocument allows docs to be overwritten.
// It returns the package to continue using (updated if changes were applied; otherwise the original pkg), a list of updated files, and any error. The first return
// is not guaranteed to be nil when no changes occur; use the updated files list to detect whether modifications were made.
//
// Errors are returned in these situations:
//   - hard error (ex: I/O error; cannot talk to LLM).
//   - errNoSnippets if the LLM didn't generate any snippets.
//   - errSomeSnippetsFailed if we couldn't apply some (or all) snippets the LLM generated. In this case, the returned pkg may include any successful updates; if
//     none succeeded it will be the original pkg.
//
// generateAndApplyDocs makes a single LLM request for initial generation, so callers should ensure codeCtx is appropriately sized. If there are errors in application
// (ex: LLM got the format wrong), it may attempt to call the LLM again for a fix.
//
// Notes:
//   - identifiers only influence instructions to the LLM. In theory, any code can change, and we don't validate that only identifier docs change.
//   - generate MUST go with apply, because: application is how we validate (ex: ensure identifiers actually match source code); also: LLMs often return partial snippets:
//     `const Foo = 2 // Foo ...` when Foo is actually in a const block).
//
// If callers need a "dry run" mode, they can clone pkg first. If callers need more granular diffs of what was changed, they can diff pkg's changed files with the
// updated package.
func generateAndApplyDocs(pkg *gocode.Package, codeCtx *gocodecontext.Context, identifiers []string, redocument bool, options BaseOptions) (*gocode.Package, []string, error) {
	if len(identifiers) == 0 {
		return nil, nil, health.LogNewErr(options.Logger, "generateAndApplyDocs called with no identifiers")
	}

	// Get conversationalist to talk to LLM:
	conv := options.Conversationalist
	if conv == nil {
		conv = llmcomplete.NewConversationalist()
	}

	// Make prompt:
	prompt := promptAddDocumentation()
	codeContextStr := codeCtx.Code()
	targetIdentifiersInstructions := llmInstructionsForIdentifiers(pkg, identifiers, missingFieldDocs(pkg, identifiers))

	// Logging:
	promptToks := countTokens([]byte(prompt))
	codeContextToks := countTokens([]byte(codeContextStr))
	instructionsToks := countTokens([]byte(targetIdentifiersInstructions))
	options.Log("counting tokens", "prompt", promptToks, "code", codeContextToks, "instructions", instructionsToks)
	options.userMessagef("> Requesting docs for %d identifiers: %s (%s)", len(identifiers), strings.Join(identifiers, ", "), formatTokenCount(promptToks+codeContextToks+instructionsToks))

	// Create a conversation and get documentation snippets from LLM:
	conversation := conv.NewConversation(llmcomplete.ModelIDOrDefault(options.Model), prompt)
	conversation.SetLogger(options.Logger)
	conversation.AddUserMessage(codeContextStr + targetIdentifiersInstructions)
	response, err := conversation.Send()
	if err != nil {
		return nil, nil, health.LogWrappedErr(options.Logger, "failed to get documentation from LLM", err)
	}

	// Extract snippets from response:
	snippets := extractSnippets(response.Text)
	if len(snippets) == 0 {
		options.userMessagef("< Got 0 snippets")
		return nil, nil, errNoSnippets
	}

	// Apply snippets:
	updatedPkg, updatedFiles, snippetErrors, err := updatedocs.UpdateDocumentation(pkg, snippets, options.updatedocsOptions(!redocument))
	if err != nil {
		return nil, updatedFiles, health.LogWrappedErr(options.Logger, "failed to update documentation", err)
	}
	updatedFilesMap := sliceToSet(updatedFiles)
	if updatedPkg != nil {
		pkg = updatedPkg
	}

	// Logging:
	hardSnippetErrors := removePartiallyRejectedSnippetErrors(snippetErrors)
	successfulSnippets := successfulSnippets(snippets, snippetErrors)
	if len(hardSnippetErrors) == 0 {
		options.userMessagef("< Got %d snippets. %d/%d successful", len(snippets), len(successfulSnippets), len(snippets))
	} else {
		options.userMessagef("< Got %d snippets. %d/%d successful. %d failed", len(snippets), len(successfulSnippets), len(snippets), len(hardSnippetErrors))
	}
	logSnippetErrors(options.Logger, "original application of snippets", snippetErrors)

	// If there were non-partial-rejection snippet errors, ask the LLM to fix them:
	if len(hardSnippetErrors) > 0 {
		reUpdatedPkg, _, moreSnippetErrors, moreUpdatedFiles, err := fixDocumentation(pkg, hardSnippetErrors, codeContextStr, redocument, options)

		if reUpdatedPkg != nil {
			pkg = reUpdatedPkg
		}

		for k := range moreUpdatedFiles {
			updatedFilesMap[k] = struct{}{}
		}

		logSnippetErrors(options.Logger, "attempted fix of snippet errors (pre filter)", moreSnippetErrors)

		if err != nil && err != errNoSnippets {
			return nil, setToSlice(updatedFilesMap), health.LogWrappedErr(options.Logger, "fixDocumentation", err)
		}

		hardSnippetErrors = removePartiallyRejectedSnippetErrors(moreSnippetErrors)

		// Log any snippet errors and return errSomeSnippetsFailed, a non-fatal error. Be sure to return pkg and updatedFilesMap, since writes may have succeeded above.
		if len(hardSnippetErrors) > 0 {
			logSnippetErrors(options.Logger, "attempted fix of snippet errors (post filter)", hardSnippetErrors)
			return pkg, setToSlice(updatedFilesMap), errSomeSnippetsFailed
		}
	}

	return pkg, setToSlice(updatedFilesMap), nil
}

// missingFieldDocs returns a map from identifier to undocumented fields in that identifier. Identifiers without missing fields will not be present as keys.
func missingFieldDocs(pkg *gocode.Package, identifiers []string) map[string][]string {
	ret := make(map[string][]string)

	idLookup := make(map[string]bool)
	for _, id := range identifiers {
		idLookup[id] = true
	}

	// Track fields already recorded per identifier to avoid duplicates when multiple identifiers share a snippet (ex: type blocks).
	added := make(map[string]map[string]struct{})

	for _, id := range identifiers {
		snippet := pkg.GetSnippet(id)
		if snippet == nil {
			continue
		}

		for _, mf := range snippet.MissingDocs() {
			// if missing docs relates to identifiers we care about and a field, add it:
			// NOTE: snippet could have many identifiers if block
			if _, ok := idLookup[mf.Identifier]; ok {
				if mf.Field != "" {
					if added[mf.Identifier] == nil {
						added[mf.Identifier] = make(map[string]struct{})
					}
					if _, seen := added[mf.Identifier][mf.Field]; !seen {
						ret[mf.Identifier] = append(ret[mf.Identifier], mf.Field)
						added[mf.Identifier][mf.Field] = struct{}{}
					}
				}
			}
		}
	}

	return ret
}

// removePartiallyRejectedSnippetErrors filters out snippet errors that were only partially rejected and returns the remaining hard errors.
func removePartiallyRejectedSnippetErrors(snippetErrors []updatedocs.SnippetError) []updatedocs.SnippetError {
	var filtered []updatedocs.SnippetError
	for _, se := range snippetErrors {
		if !se.PartiallyRejected {
			filtered = append(filtered, se)
		}
	}
	return filtered
}
