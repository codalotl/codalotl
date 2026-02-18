package docubot

import (
	"encoding/json"
	"fmt"
	"github.com/codalotl/codalotl/internal/gocode"
	"github.com/codalotl/codalotl/internal/gocodecontext"
	"github.com/codalotl/codalotl/internal/llmcomplete"
	"strings"
	"sync"
)

// FindFixDocErrorsOptions configures FindAndFixDocErrors.
type FindFixDocErrorsOptions struct {
	BaseOptions // BaseOptions configures model selection, logging/health, and parallelism, and lets callers inject a Conversationalist.
}

// FindAndFixDocErrors scans pkg for documentation problems on documented functions, methods, and types and applies automatic fixes. It processes the package's non-test
// files, test files, and any black-box test package.
//
// If identifiers is empty, all documented functions and types are considered (generated files and testing functions like TestXxx/etc are excluded). If identifiers
// is non-empty, the scan is restricted to those names and further filtered to documented and unambiguous identifiers (testing functions, generated files, and all
// snippet types are allowed).
//
// The returned slice contains one entry per change applied, each aggregating the feedback that led to that change. The function may return both a non-empty slice
// and a non-nil error if only part of the package could be scanned or updated; check both return values.
func FindAndFixDocErrors(pkg *gocode.Package, identifiers []string, options FindFixDocErrorsOptions) ([]IncorporatedFeedback, error) {
	var incorporatedFeedbacks []IncorporatedFeedback

	filterOptionsEmpty := gocode.FilterIdentifiersOptions{OnlyAnyDocs: true, IncludeSnippetType: true, IncludeSnippetFuncs: true}
	filterOptionsNonmpty := gocode.FilterIdentifiersOptionsDocumentedNonAmbiguous
	err := gocode.EachPackageWithIdentifiers(pkg, identifiers, filterOptionsEmpty, filterOptionsNonmpty, func(p *gocode.Package, ids []string, onlyTests bool) error {
		testsStr := "(non-tests)"
		if onlyTests {
			testsStr = "(tests)"
		}
		options.userMessagef("Finding documentation errors in %s %s...", p.Name, testsStr)

		// NOTE: findDocErrorsForIdentifiers may return valid found feedback AND an error. Don't lose progress if there's an error.
		feedbacks, findErrorsErr := findDocErrorsForIDs(p, ids, onlyTests, options)
		if findErrorsErr != nil {
			if len(feedbacks) == 0 {
				return options.LogWrappedErr("find_and_fix_doc_errors.for_ids", findErrorsErr)
			}
			options.userMessagef("Warning! Got error when finding errors: %v -- likely that part of package was unscanned", findErrorsErr)
		}

		if len(feedbacks) == 0 {
			options.userMessagef("Found no documentation issues")
			return nil
		}

		changes, err := incorporateFeedback(p, feedbacks, onlyTests, options)
		if err != nil {
			return options.LogWrappedErr("find_and_fix_doc_errors.incorporate_feedback", err)
		}

		incorporatedFeedbacks = append(incorporatedFeedbacks, changes...)

		return findErrorsErr
	})

	return incorporatedFeedbacks, err
}

// findDocErrorsForIDs scans identifiers in pkg for documentation issues and returns feedback for those that need fixes. Callers must ensure pkg, identifiers, and
// onlyTests align; identifiers should be pre-validated.
//
// Identifiers are grouped into minimal analysis contexts and processed in parallel, bounded by options.MaxParallelism (default 5).
//
// The function may return partial results alongside a non-nil error; if multiple errors occur, only the first is returned.
func findDocErrorsForIDs(pkg *gocode.Package, identifiers []string, onlyTests bool, options FindFixDocErrorsOptions) ([]IdentifierFeedback, error) {
	options.userMessagef("> Finding issues in %s", strings.Join(identifiers, ", "))

	groups, err := gocodecontext.Groups(pkg.Module, pkg, gocodecontext.GroupOptions{IncludePackageDocs: false, IncludeTestFiles: onlyTests, IncludeExternalDeps: true})
	if err != nil {
		return nil, err
	}
	// Build contexts that cover our identifiers:
	contextsMap := gocodecontext.NewContextsForIdentifiers(groups, identifiers)

	var allDocErrors []IdentifierFeedback

	// Kick off concurrent processing of contexts, bounded by MaxParallelism:
	maxParallel := options.MaxParallelism
	if maxParallel <= 0 {
		maxParallel = 5
	}

	var mu sync.Mutex
	var firstErr error
	sem := make(chan struct{}, maxParallel)
	var wg sync.WaitGroup

	for ctx, ids := range contextsMap {
		wg.Add(1)
		sem <- struct{}{} // acquire a slot
		go func(ctx *gocodecontext.Context, ids []string) {
			defer wg.Done()
			defer func() { <-sem }() // release slot

			foundErrors, err := findDocErrorsBatch(pkg, ctx, ids, options)
			if err != nil {
				mu.Lock()
				if firstErr == nil {
					firstErr = err
				}
				mu.Unlock()
				return
			}
			if len(foundErrors) > 0 {
				mu.Lock()
				allDocErrors = append(allDocErrors, foundErrors...)
				mu.Unlock()
			}
		}(ctx, ids)
	}

	wg.Wait()

	return allDocErrors, firstErr
}

// IdentifierFeedback associates an identifier with feedback about its documentation.
type IdentifierFeedback struct {
	Identifier string // The identifier the feedback applies to.
	Feedback   string // Human-readable issue or guidance to address in the docs.
}

// findDocErrorsBatch calls a single LLM to detect documentation errors for the given identifiers in ctx and returns feedback only for identifiers with non-empty
// issues.
//
// ctx must describe the identifiers and their relevant dependencies; this function does not validate identifiers.
//
// An error is returned if the LLM call fails or the response cannot be parsed as the expected JSON.
func findDocErrorsBatch(pkg *gocode.Package, ctx *gocodecontext.Context, identifiers []string, options FindFixDocErrorsOptions) ([]IdentifierFeedback, error) {
	codeContext := ctx.Code()
	prompt := promptFindErrors()

	instructionsFindIdentifiers := "Find documentation errors in these identifiers:\n"
	for _, id := range identifiers {
		instructionsFindIdentifiers += fmt.Sprintf("- %s\n", id)
	}

	// Talk to LLM:
	conv := options.Conversationalist
	if conv == nil {
		conv = llmcomplete.NewConversationalist()
	}
	conversation := conv.NewConversation(llmcomplete.ModelIDOrDefault(options.Model), prompt)
	conversation.SetLogger(options.Logger)
	conversation.AddUserMessage(codeContext + instructionsFindIdentifiers)
	response, err := conversation.Send()
	if err != nil {
		return nil, options.LogWrappedErr("failed to polish documentation with LLM", err)
	}

	// Parse JSON returned by the LLM. It should be a mapping of identifier -> error string.
	// NOTE: Opus-4 likes to wrap response in ```json fences even if instructed not to.
	var raw map[string]string
	if err := json.Unmarshal([]byte(strings.TrimSpace(unwrapSingleSnippet(response.Text))), &raw); err != nil {
		return nil, options.LogWrappedErr("failed to unmarshal LLM response", err)
	}

	// Build []DocError, including only identifiers with non-empty error messages, preserving the original identifier order.
	var docErrs []IdentifierFeedback
	for _, id := range identifiers {
		if msg, ok := raw[id]; ok {
			if strings.TrimSpace(msg) != "" {
				docErrs = append(docErrs, IdentifierFeedback{Identifier: id, Feedback: msg})
			}
		}
	}

	// Log:
	if len(docErrs) == 0 {
		options.userMessagef("< found no issue in %s.", strings.Join(identifiers, ", "))
	} else if len(docErrs) == 1 {
		options.userMessagef("< found issue in %s:", strings.Join(identifiers, ", "))
		options.userMessagef("  Issue: %s", docErrs[0].Feedback)
	} else {
		options.userMessagef("< found %d issues in %s:", len(docErrs), strings.Join(identifiers, ", "))
		for _, de := range docErrs {
			options.userMessagef("  Issue in %s: %s", de.Identifier, de.Feedback)
		}
	}

	return docErrs, nil
}
