package docubot

import (
	"fmt"
	"strings"

	"github.com/codalotl/codalotl/internal/gocode"
	"github.com/codalotl/codalotl/internal/gocodecontext"
	"github.com/codalotl/codalotl/internal/gopackagediff"
	"github.com/codalotl/codalotl/internal/updatedocs"
)

// Clarification is one clarify_public_api answer to consider for documentation.
type Clarification struct {
	Identifier string
	Question   string
	Answer     string
}

// ImproveFromClarificationsOptions configures clarification-driven documentation improvements.
type ImproveFromClarificationsOptions struct {
	BaseOptions
}

// ImproveFromClarifications improves docs when clarification answers add useful public-doc context.
func ImproveFromClarifications(pkg *gocode.Package, clarifications []Clarification, options ImproveFromClarificationsOptions) ([]IncorporatedFeedback, error) {
	clarificationsByID, identifiers := publicClarificationsByIdentifier(pkg, clarifications)
	if len(identifiers) == 0 {
		return nil, nil
	}

	return incorporateClarifications(pkg, clarificationsByID, identifiers, options)
}

func publicClarificationsByIdentifier(pkg *gocode.Package, clarifications []Clarification) (map[string][]Clarification, []string) {
	if len(clarifications) == 0 {
		return nil, nil
	}

	candidateSet := make(map[string]struct{})
	var candidates []string
	for _, clarification := range clarifications {
		if strings.TrimSpace(clarification.Identifier) == "" || strings.TrimSpace(clarification.Answer) == "" {
			continue
		}
		if _, ok := candidateSet[clarification.Identifier]; ok {
			continue
		}
		candidateSet[clarification.Identifier] = struct{}{}
		candidates = append(candidates, clarification.Identifier)
	}
	if len(candidates) == 0 {
		return nil, nil
	}

	filtered := pkg.FilterIdentifiers(candidates, gocode.FilterIdentifiersOptions{
		NoTests:                   true,
		IncludeSnippetFuncs:       true,
		IncludeSnippetType:        true,
		IncludeSnippetValue:       true,
		IncludeSnippetVar:         true,
		IncludeSnippetConst:       true,
		IncludeSnippetPackageDocs: true,
	})
	filteredSet := sliceToSet(filtered)

	ids := NewIdentifiersFromPackage(pkg)
	clarificationsByID := make(map[string][]Clarification)
	var identifiers []string
	for _, clarification := range clarifications {
		if strings.TrimSpace(clarification.Identifier) == "" || strings.TrimSpace(clarification.Answer) == "" {
			continue
		}

		identifier := clarification.Identifier
		if _, ok := filteredSet[identifier]; !ok {
			continue
		}
		if !publicClarificationIdentifier(ids, identifier) {
			continue
		}

		if _, ok := clarificationsByID[identifier]; !ok {
			identifiers = append(identifiers, identifier)
		}
		clarificationsByID[identifier] = append(clarificationsByID[identifier], clarification)
	}

	return clarificationsByID, identifiers
}

func publicClarificationIdentifier(ids *Identifiers, identifier string) bool {
	if identifier == gocode.PackageIdentifier {
		return !ids.isTestPkg
	}
	_, exported := ids.isExported[identifier]
	return exported
}

func incorporateClarifications(pkg *gocode.Package, clarificationsByID map[string][]Clarification, identifiers []string, options ImproveFromClarificationsOptions) ([]IncorporatedFeedback, error) {
	specContext, err := specContextForPackage(pkg, nil)
	if err != nil {
		return nil, options.LogWrappedErr("improve_from_clarifications.spec_context", err)
	}

	groups, err := gocodecontext.Groups(pkg.Module, pkg, gocodecontext.GroupOptions{IncludePackageDocs: true, IncludeExternalDeps: true})
	if err != nil {
		return nil, options.LogWrappedErr("improve_from_clarifications.groups", err)
	}

	clonedForDiff, err := pkg.Clone()
	if err != nil {
		return nil, options.LogWrappedErr("improve_from_clarifications.clone.for_diff", err)
	}
	defer clonedForDiff.Module.DeleteClone()

	contexts := gocodecontext.NewContextsForIdentifiers(groups, identifiers)
	for ctx, ids := range contexts {
		var clarificationsForContext []Clarification
		for _, id := range ids {
			clarificationsForContext = append(clarificationsForContext, clarificationsByID[id]...)
		}
		if len(clarificationsForContext) == 0 {
			continue
		}

		instructions := instructionsForClarifications(clarificationsForContext)
		llmUserMessage := specContext + ctx.Code() + instructions

		options.userMessagef("> Considering %d clarifications for %d identifiers: %s (%s)", len(clarificationsForContext), len(ids), strings.Join(ids, ", "), formatTokenCount(countTokens([]byte(llmUserMessage))))

		responseText, err := completeText(promptImproveFromClarifications(), llmUserMessage, options.BaseOptions)
		if err != nil {
			options.userMessagef("< Got error: %v", err)
			return nil, options.LogWrappedErr("improve_from_clarifications.llm", err)
		}

		snippets := extractSnippets(responseText)
		options.userMessagef("< Got %d snippets", len(snippets))
		if len(snippets) == 0 {
			continue
		}

		updatedPkg, _, snippetErrs, err := updatedocs.UpdateDocumentation(pkg, snippets, options.updatedocsOptions(false))
		if err != nil {
			return nil, options.LogWrappedErr("improve_from_clarifications.update_documentation", err, "ids", strings.Join(ids, ","))
		}
		if len(snippetErrs) > 0 {
			logSnippetErrors(options.Logger, "improve from clarifications", snippetErrs)
		}
		if updatedPkg != nil {
			pkg = updatedPkg
		}
	}

	docChanges, err := gopackagediff.Diff(clonedForDiff, pkg, identifiers, nil, true)
	if err != nil {
		return nil, options.LogWrappedErr("improve_from_clarifications.diff", err)
	}

	return incorporatedClarificationChanges(docChanges, clarificationsByID), nil
}

func incorporatedClarificationChanges(docChanges []*gopackagediff.Change, clarificationsByID map[string][]Clarification) []IncorporatedFeedback {
	var out []IncorporatedFeedback
	for _, ch := range docChanges {
		var related []IdentifierFeedback
		for _, id := range ch.IDs() {
			for _, clarification := range clarificationsByID[id] {
				related = append(related, clarificationFeedback(clarification))
			}
		}
		out = append(out, IncorporatedFeedback{Feedbacks: related, Change: *ch})
	}
	return out
}

func instructionsForClarifications(clarifications []Clarification) string {
	var b strings.Builder

	b.WriteString("Clarifications to consider:\n")
	for _, clarification := range clarifications {
		b.WriteString("- ")
		b.WriteString(clarification.Identifier)
		b.WriteString(":\n")
		if clarification.Question != "" {
			b.WriteString("  Question: ")
			b.WriteString(clarification.Question)
			b.WriteString("\n")
		}
		b.WriteString("  Answer: ")
		b.WriteString(clarification.Answer)
		b.WriteString("\n")
	}
	b.WriteString("\n")

	return b.String()
}

func clarificationFeedback(clarification Clarification) IdentifierFeedback {
	feedback := fmt.Sprintf("Clarification answer: %s", clarification.Answer)
	if clarification.Question != "" {
		feedback = fmt.Sprintf("Clarification question: %s\nClarification answer: %s", clarification.Question, clarification.Answer)
	}

	return IdentifierFeedback{Identifier: clarification.Identifier, Feedback: feedback}
}

func promptImproveFromClarifications() string {
	var b strings.Builder

	b.WriteString("You are an expert Go programmer. Your task is to improve public Go doc comments using clarification Q/A.\n\n")

	b.WriteString("## What you receive\n")
	b.WriteString("- Go code for context.\n")
	b.WriteString("- A list of public identifiers with clarification questions and answers.\n")
	b.WriteString("\n")

	b.WriteString("## What you return\n")
	b.WriteString("- Return Go snippets only for identifiers whose public documentation should change.\n")
	b.WriteString("- If no clarification should change documentation, return no code fences.\n")
	b.WriteString("- For each changed identifier, output the declaration verbatim except for minimal doc-comment text changes that incorporate useful clarification context.\n")
	b.WriteString("- You may add a missing doc comment when the clarification gives broadly useful public-doc context.\n")
	b.WriteString("- Preserve code, structure, and non-comment spacing exactly.\n")
	b.WriteString("- Leave documentation unrelated to the clarification EXACTLY as-is.\n")
	b.WriteString("- Do not move comments: keep doc comments above declarations and end-of-line comments inline.\n")
	b.WriteString("- For functions, only return the function header (doc comments, name, params) but not the body.\n")
	b.WriteString("- Wrap each changed declaration+comments in its OWN ```go``` fence.\n")
	b.WriteString("\n")

	b.WriteString("## When to no-op\n")
	b.WriteString("- No-op if the answer is irrelevant to the identifier's public contract.\n")
	b.WriteString("- No-op if existing documentation already covers the useful part of the answer.\n")
	b.WriteString("- No-op if the answer is too narrow, user-specific, implementation-only, speculative, or not useful to future users of the public API.\n")
	b.WriteString("- No-op if the answer does not map cleanly to the identifier's Go doc comments.\n")
	b.WriteString("\n")

	b.WriteString("## Guidelines\n")
	b.WriteString("- Make the smallest useful documentation edit.\n")
	b.WriteString("- Document public API behavior, constraints, side effects, errors, and semantic details that users need.\n")
	b.WriteString("- Do not include the question, answer, or meta-language like \"clarification\" in the docs.\n")
	b.WriteString("- Do not explain that old docs were incomplete or wrong.\n")
	b.WriteString("- Do not reflow comments for line length. Line length is irrelevant.\n")
	b.WriteString("\n")

	b.WriteString(promptFragmentCommentStyle())

	return b.String()
}
