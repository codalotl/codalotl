package docubot

import (
	"github.com/codalotl/codalotl/internal/gocode"
	"github.com/codalotl/codalotl/internal/gocodecontext"
	"github.com/codalotl/codalotl/internal/gopackagediff"
	"github.com/codalotl/codalotl/internal/llmcomplete"
	"github.com/codalotl/codalotl/internal/updatedocs"
	"slices"
	"strings"
)

// IncorporatedFeedback represents a single documentation change and all feedback entries that relate to that change. A change may be driven by multiple issues for the same identifier
// or multiple identifiers affected together (ex: within a var/type block).
type IncorporatedFeedback struct {
	Feedbacks []IdentifierFeedback // Feedbacks aggregated for this change.
	Change    gopackagediff.Change // The documentation change produced.
}

// incorporateFeedback applies documentation fixes for the provided identifierFeedback and returns incorporated feedback entries.
//
// The returned slice will contain one IncorporatedFeedback per documentation change, each including all IdentifierFeedback entries that relate to that change (possibly multiple per
// identifier or multiple identifiers if a block-level change was made).
//
// identifierFeedback's identifiers must apply to pkg (vs pkg.TestPackage). onlyTests indicates that all identifierFeedback is for test files; you cannot mix test and non-test identifiers.
func incorporateFeedback(pkg *gocode.Package, identifierFeedback []IdentifierFeedback, onlyTests bool, options FindFixDocErrorsOptions) ([]IncorporatedFeedback, error) {

	// TODO: proper handling of IncludeXxx for GroupOptions

	groups, err := gocodecontext.Groups(pkg.Module, pkg, gocodecontext.GroupOptions{IncludePackageDocs: false, IncludeTestFiles: onlyTests, IncludeExternalDeps: true})
	if err != nil {
		return nil, err
	}

	var identifiers []string
	idToFeedbacks := make(map[string][]IdentifierFeedback)
	for _, fb := range identifierFeedback {
		if !slices.Contains(identifiers, fb.Identifier) {
			identifiers = append(identifiers, fb.Identifier)
		}

		idToFeedbacks[fb.Identifier] = append(idToFeedbacks[fb.Identifier], fb)
	}

	// Clone original package for diffing at the end:
	clonedForDiff, err := pkg.Clone()
	if err != nil {
		return nil, options.LogWrappedErr("incorporate_feedback.clone.for_diff", err)
	}
	defer clonedForDiff.Module.DeleteClone()

	// Get conversationalist to talk to LLM:
	conv := options.Conversationalist
	if conv == nil {
		conv = llmcomplete.NewConversationalist()
	}

	prompt := promptIncorperateFeedback()

	//
	contexts := gocodecontext.NewContextsForIdentifiers(groups, identifiers)
	for ctx, ids := range contexts {

		var feedbacksForContext []IdentifierFeedback
		for _, id := range ids {
			feedbacksForContext = append(feedbacksForContext, idToFeedbacks[id]...)
		}

		instructions := instructionsForIncorporateFeedback(feedbacksForContext)
		llmUserMessage := ctx.Code() + instructions

		options.userMessagef("> Requesting docs for %d identifiers: %s (%s)", len(ids), strings.Join(ids, ", "), formatTokenCount(countTokens([]byte(llmUserMessage))))

		conversation := conv.NewConversation(llmcomplete.ModelIDOrDefault(options.Model), prompt)
		conversation.SetLogger(options.Logger)
		conversation.AddUserMessage(llmUserMessage)

		response, err := conversation.Send()
		if err != nil {
			options.userMessagef("< Got error: %v", err)
			return nil, options.LogWrappedErr("failed to incorporate feedback with LLM", err)
		}

		snippets := extractSnippets(response.Text)

		options.userMessagef("< Got %d snippets", len(snippets))

		if len(snippets) == 0 {
			return nil, options.LogNewErr("no snippets to apply", "ids", strings.Join(ids, ","))
		}

		updatedPkg, _, _, err := updatedocs.UpdateDocumentation(pkg, snippets, options.updatedocsOptions(false))
		if err != nil {
			return nil, options.LogWrappedErr("incorporate_feedback.update_documentation", err, "ids", strings.Join(ids, ","))
		}
		if updatedPkg != nil {
			pkg = updatedPkg
		}
	}

	// Compute documentation changes for the identifiers we targeted.
	docChanges, err := gopackagediff.Diff(clonedForDiff, pkg, identifiers, nil, true)
	if err != nil {
		return nil, options.LogWrappedErr("incorporate_feedback.diff", err)
	}

	// Correlate changes to identifier feedback via identifier and build []IncorporatedFeedback.
	var out []IncorporatedFeedback
	for _, ch := range docChanges {
		// Aggregate all related feedbacks for the identifiers changed by this change.
		var related []IdentifierFeedback
		for _, id := range ch.IDs() {
			if fbs, ok := idToFeedbacks[id]; ok {
				related = append(related, fbs...)
			}
		}

		// Output an IncorporatedFeedback (even if there's no related feedback -- not sure how this would happen, but better to expose any bugs this way):
		out = append(out, IncorporatedFeedback{Feedbacks: related, Change: *ch})
	}

	return out, nil
}

// instructionsForIncorporateFeedback formats a grouped instruction block for the LLM from the given feedback. The result starts with "Identifiers and feedback:" and lists each identifier
// followed by one or more bullet points with its feedback (ex: "- myID:\n - issue 1\n - issue 2"). Output order of identifiers is unspecified, and multi-line feedback is not re-indented.
func instructionsForIncorporateFeedback(identifierFeedback []IdentifierFeedback) string {
	var b strings.Builder

	idToFeedbacks := make(map[string][]IdentifierFeedback)
	for _, fb := range identifierFeedback {
		idToFeedbacks[fb.Identifier] = append(idToFeedbacks[fb.Identifier], fb)
	}

	b.WriteString("Identifiers and feedback:\n")
	for id, fbs := range idToFeedbacks {
		b.WriteString("- ")
		b.WriteString(id)
		b.WriteString(":\n")
		for _, fb := range fbs {
			b.WriteString("  - ")
			b.WriteString(fb.Feedback)
			b.WriteString("\n")
		}
		// NOTE: in the future this could be improved by making sure multiline errors are handled better. For instance: split by newline, insert some prefix like "     ", write each line with prefix
	}

	b.WriteString("\n")

	return b.String()
}
