package docubot

import (
	"github.com/codalotl/codalotl/internal/gocode"
	"github.com/codalotl/codalotl/internal/gopackagediff"
	"github.com/codalotl/codalotl/internal/llmcomplete"
	"github.com/codalotl/codalotl/internal/updatedocs"
	"strings"
)

// PolishOptions configures Polish().
type PolishOptions struct {
	BaseOptions // BaseOptions configures model selection, logging/health, and parallelism, and lets callers inject a Conversationalist.
}

// Maximum number of unique snippets included in a single LLM request.
const polishMaxGroupSize = 10

// snippetWithIdentifiers ties a raw snippet string to the identifiers whose documentation lives inside it.
type snippetWithIdentifiers struct {
	snippet     string   // The raw Go snippet exactly as in source, without ``` fences.
	identifiers []string // Identifier names whose documentation lives within the snippet; may be multiple for blocks.
}

// snippetBatch is a slice of snippets grouped together for a single LLM polishing request. The outer slice produced by getSnippetGroupsForPolish preserves deterministic
// ordering of batches.
type snippetBatch []*snippetWithIdentifiers

// Polish updates identifiers' existing documentation by rewording it, fixing superficial mistakes, and applying conventions. An identifier that is part of a block
// of identifiers causes the entire block to be polished (ex: `var ()` blocks). Both pkg and pkg.TestPackage are polished, and identifiers may be in either.
//
// If the identifiers slice is empty, Polish considers all identifiers eligible under the default identifier filter, but it requires documentation.
//
// If an identifier has no documentation, it will not be sent to the LLM for documentation, and no error will be reported.
//
// Polish returns the documentation changes as a slice of *gopackagediff.Change. An identifier passed in and not modified is not necessarily a problem: it may already
// have been good enough, or it may not have had any documentation. An error is returned only for hard errors (ex: I/O error).
func Polish(pkg *gocode.Package, identifiers []string, options PolishOptions) ([]*gopackagediff.Change, error) {
	var changes []*gopackagediff.Change

	filterOptions := gocode.FilterIdentifiersOptions{OnlyAnyDocs: true}
	err := gocode.EachPackageWithIdentifiers(pkg, identifiers, filterOptions, gocode.FilterIdentifiersOptionsDocumentedNonAmbiguous, func(p *gocode.Package, ids []string, onlyTests bool) error {
		pChanges, err := polishIDs(p, ids, onlyTests, options)
		if err != nil {
			return err
		}

		changes = append(changes, pChanges...)

		return nil
	})

	return changes, err
}

// polishIDs polishes identifiers in pkg. It assumes all identifiers are in pkg and needs no further filtering or validation. Callers should call separately for
// pkg and pkg.TestPackage.
func polishIDs(pkg *gocode.Package, identifiers []string, onlyTests bool, options PolishOptions) ([]*gopackagediff.Change, error) {

	// Determine desired parallelism – default to 5 if unset or negative.
	parallelism := options.MaxParallelism
	if parallelism <= 0 {
		parallelism = 5
	}

	// Build groups of snippets to send to the LLM.
	groups, err := getSnippetGroupsForPolish(pkg, identifiers, options)
	if err != nil {
		return nil, err
	}
	if len(groups) == 0 {
		return nil, nil
	}

	testsStr := "(non-tests)"
	if onlyTests {
		testsStr = "(tests)"
	}
	options.userMessagef("Polishing %s %s...", pkg.Name, testsStr)

	clonedPkg, err := pkg.Clone()
	if err != nil {
		return nil, options.LogWrappedErr("polish.clone", err)
	}

	// Channel to receive processing results.
	type groupResult struct {
		changedSnippetTexts []string
		err                 error
	}
	resultCh := make(chan groupResult, len(groups))

	// Semaphore channel to limit parallelism.
	sem := make(chan struct{}, parallelism)

	// Launch goroutines for each group.
	for gi, g := range groups {
		gi, g := gi, g    // capture
		sem <- struct{}{} // acquire
		go func() {
			defer func() { <-sem }() // release

			if len(groups) == 1 {
				options.userMessagef("> Requesting polishing for %d snippets", len(g))
			} else {
				options.userMessagef("> Requesting polishing for %d snippets [Group %d/%d]", len(g), gi+1, len(groups))
			}

			// Extract the raw snippet texts.
			var rawSnippets []string
			for _, info := range g {
				rawSnippets = append(rawSnippets, info.snippet)
			}

			newSnippets, err := polishSnippets(rawSnippets, options)
			if err != nil {
				resultCh <- groupResult{err: err}
				return
			}

			if len(groups) == 1 {
				options.userMessagef("< Got %d snippets", len(newSnippets))
			} else {
				options.userMessagef("< Got %d snippets [Group %d/%d]", len(newSnippets), gi+1, len(groups))
			}

			// Determine which snippets changed.
			var changedSnippetTexts []string
			for idx, info := range g {
				original := strings.TrimSpace(info.snippet)
				updated := strings.TrimSpace(newSnippets[idx])
				if original != updated {
					changedSnippetTexts = append(changedSnippetTexts, newSnippets[idx])
				}
			}

			resultCh <- groupResult{changedSnippetTexts: changedSnippetTexts}
		}()
	}

	// Collect results as they arrive and apply documentation updates serially.
	for range groups {
		res := <-resultCh
		if res.err != nil {
			return nil, res.err
		}
		if len(res.changedSnippetTexts) == 0 {
			continue
		}

		updatedPkg, _, _, err := updatedocs.UpdateDocumentation(pkg, res.changedSnippetTexts, options.updatedocsOptions(false))
		if err != nil {
			return nil, options.LogWrappedErr("failed to UpdateDocumentation", err)
		}
		if updatedPkg != nil {
			pkg = updatedPkg
		}
	}

	docChanges, err := gopackagediff.Diff(clonedPkg, pkg, identifiers, nil, true)
	if err != nil {
		return nil, options.LogWrappedErr("diff failed", err)
	}

	return docChanges, nil
}

// getSnippetGroupsForPolish deduplicates identifiers to their unique snippets, filters only those that have docs, groups them into batches of up to polishMaxGroupSize
// snippets each, and returns the grouped snippets ready for LLM consumption.
func getSnippetGroupsForPolish(pkg *gocode.Package, identifiers []string, options PolishOptions) ([]snippetBatch, error) {

	// Build mapping from snippet pointer to snippetWithIdentifiers.
	snippetMap := make(map[gocode.Snippet]*snippetWithIdentifiers)
	var snippetOrder []gocode.Snippet // preserve deterministic order of first encounter

	for _, id := range identifiers {
		sn := pkg.GetSnippet(id)
		if sn == nil {
			return nil, options.LogNewErr("polish.get_snippet - not found", "identifier", id)
		}

		// skip snippets that have no documentation:
		if len(sn.Docs()) == 0 {
			continue
		}

		if entry, ok := snippetMap[sn]; ok {
			entry.identifiers = append(entry.identifiers, id)
		} else {
			snippetMap[sn] = &snippetWithIdentifiers{
				snippet:     string(sn.Bytes()),
				identifiers: []string{id},
			}
			snippetOrder = append(snippetOrder, sn)
		}
	}

	if len(snippetOrder) == 0 {
		return nil, nil
	}

	// Build groups abiding by polishMaxGroupSize.
	var groups []snippetBatch

	for i := 0; i < len(snippetOrder); i += polishMaxGroupSize {
		upper := min(i+polishMaxGroupSize, len(snippetOrder))

		var groupSnippets []*snippetWithIdentifiers
		for _, snPtr := range snippetOrder[i:upper] {
			info := snippetMap[snPtr]
			groupSnippets = append(groupSnippets, info)
		}

		groups = append(groups, groupSnippets)
	}

	return groups, nil
}

// polishSnippets sends snippets to an LLM for polishing (see Polish for a summary of the polishing process). Each snippet is just Go code without any triple backtick
// fences.
//
// All snippets are sent to the LLM in a single batch; the caller is responsible for batching. Snippets are expected to be returned in the same order as input (assuming
// the LLM follows instructions).
//
// An error is returned for a hard error (ex: failure to communicate with the LLM) or if the number of returned snippets cannot be reconciled with the inputs.
func polishSnippets(snippets []string, options PolishOptions) ([]string, error) {
	// Quick exit if there's nothing to polish.
	if len(snippets) == 0 {
		return nil, nil
	}

	// Build the list of snippets to send to the LLM, appending hidden "rubber ducky" snippets:
	// one with obvious grammatical/spelling errors so the model always has something safe to fix,
	// and one that's already well-polished to establish a quality baseline.
	// The duckies are identified via sentinel identifiers and removed from results before returning.
	const duckySentinelBad = "4413129f-4444-c413-1b0a-deadb4935da0"
	duckySnippetBad := "// this is usd to access the primry service. it can be changd and altared in several places.\n" +
		"// One place is in the initization code. Another is is during SetProvider()\n" +
		"var provider = \"" + duckySentinelBad + "\"\n"

	const duckySentinelGood = "8e6b2c4a-9d3f-1e2a-7b8c-5f4e6d9a2b1c"
	duckySnippetGood := "// DefaultAuthToken is the token used for smoke tests when no token is provided.\n" +
		"// Keep this value in sync with internal CI configuration.\n" +
		"const DefaultAuthToken = \"" + duckySentinelGood + "\"\n"

	snippetsForLLM := make([]string, 0, len(snippets)+2)
	snippetsForLLM = append(snippetsForLLM, duckySnippetBad)
	snippetsForLLM = append(snippetsForLLM, snippets...)
	snippetsForLLM = append(snippetsForLLM, duckySnippetGood)

	// Build the context string by wrapping each snippet in ```go fences.
	var b strings.Builder
	for _, sn := range snippetsForLLM {
		b.WriteString("```go\n")
		b.WriteString(sn)
		if !strings.HasSuffix(sn, "\n") {
			b.WriteString("\n")
		}
		b.WriteString("```\n\n")
	}
	context := b.String()

	// Get conversationalist to talk to LLM:
	conv := options.Conversationalist
	if conv == nil {
		conv = llmcomplete.NewConversationalist()
	}

	prompt := promptPolish()

	// Send snippets to provider. Retry maxAttempts times if snippet count mismatches:
	const maxAttempts = 2
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		conversation := conv.NewConversation(llmcomplete.ModelIDOrDefault(options.Model), prompt)
		conversation.SetLogger(options.Logger)
		conversation.AddUserMessage(context)

		response, err := conversation.Send()
		if err != nil {
			return nil, options.LogWrappedErr("failed to polish documentation with LLM", err)
		}

		// Extract snippets and drop the rubber duckies, which are implementation details.
		extracted := extractSnippets(response.Text)
		filtered := make([]string, 0, len(extracted))
		sentinelFailureGood := false
		sentinelFailureBad := false
		for _, sn := range extracted {
			if strings.Contains(sn, duckySentinelBad) {
				if strings.TrimSpace(sn) == strings.TrimSpace(duckySnippetBad) {
					sentinelFailureBad = true
				}
				continue
			}
			if strings.Contains(sn, duckySentinelGood) {
				if strings.TrimSpace(sn) != strings.TrimSpace(duckySnippetGood) {
					sentinelFailureGood = true
				}
				continue
			}
			filtered = append(filtered, sn)
		}

		if sentinelFailureBad {
			options.userMessagef("Warning: The model failed to polish an injected sentinel with obvious mistakes.")
		}
		if sentinelFailureGood {
			options.userMessagef("NOTE: the model tried to polish an injected sentinel which didn't need polishing. Even strong models occasionally do this (they can't help themselves) but if you're seeing this a lot, use a stronger model to avoid churn.")
		}

		if len(filtered) != len(snippets) {
			options.Log("polishSnippets: snippet count mismatch", "expected", len(snippets), "got", len(filtered), "attempt", attempt, "maxAttempts", maxAttempts)
			continue
		}

		return filtered, nil
	}

	return nil, options.LogNewErr("polish.snippet_count_mismatch", "expected", len(snippets), "got", "different length after retries")
}
