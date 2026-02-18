package docubot

import (
	"fmt"
	"github.com/codalotl/codalotl/internal/gocode"
	"github.com/codalotl/codalotl/internal/llmcomplete"
	"github.com/codalotl/codalotl/internal/q/health"
	"github.com/codalotl/codalotl/internal/updatedocs"
)

// errNoSnippets is returned when the LLM produces no documentation snippets to apply.
var errNoSnippets = fmt.Errorf("no snippets")

// fixDocumentation fixes snippet errors from a call to EnsureDocs. It takes the following parameters:
//   - pkg: can be the original code or partially updated code.
//   - snippetErrors: errors from a previous call.
//   - codeContext: context for the code.
//   - redocument: flag for redocumenting.
//   - options: base options.
//
// It returns the following:
//   - updated package.
//   - successfully applied snippets.
//   - snippet errors.
//   - modified files.
//   - any error.
//
// The error returned is errNoSnippets if there are no snippets. A nil error is returned for non-fatal cases where we tried to get a fix but the LLM provided junk
// again. A real error is returned in fatal cases (ex: failure to communicate with the LLM or an I/O error).
func fixDocumentation(pkg *gocode.Package, snippetErrors []updatedocs.SnippetError, codeContext string, redocument bool, options BaseOptions) (*gocode.Package, []string, []updatedocs.SnippetError, map[string]struct{}, error) {
	if len(snippetErrors) == 0 {
		panic("no snippet errors")
	}

	// Create a conversationalist if one wasn't provided:
	conv := options.Conversationalist
	if conv == nil {
		conv = llmcomplete.NewConversationalist()
	}

	options.userMessagef("> Attempting to fix %d snippets", len(snippetErrors))

	// Create a conversation with the fix snippet errors prompt:
	conversation := conv.NewConversation(llmcomplete.ModelIDOrDefault(options.Model), promptFixSnippetErrors(snippetErrors))
	conversation.SetLogger(options.Logger)
	conversation.AddUserMessage(codeContext)
	response, err := conversation.Send()
	if err != nil {
		return nil, nil, nil, nil, health.LogWrappedErr(options.Logger, "failed to get fixed documentation from LLM", err)
	}

	// Extract snippets from response:
	snippets := extractSnippets(response.Text)

	if len(snippets) == 0 {
		options.userMessagef("< Got %d snippets from fix attempt", len(snippets))
		return nil, nil, nil, nil, errNoSnippets
	}

	// Apply the fixed documentation updates:
	updatedPkg, updatedFiles, newSnippetErrors, err := updatedocs.UpdateDocumentation(pkg, snippets, options.updatedocsOptions(!redocument))
	successfulSnippets := successfulSnippets(snippets, newSnippetErrors)
	updatedFilesMap := sliceToSet(updatedFiles)
	if err != nil {
		err = health.LogWrappedErr(options.Logger, "failed to update documentation during fix", err)
	}

	numFailed := len(snippets) - len(successfulSnippets)
	options.userMessagef("< Got %d snippets from fix attempt. %d/%d successful. %d failed", len(snippets), len(successfulSnippets), len(snippets), numFailed)

	return updatedPkg, successfulSnippets, newSnippetErrors, updatedFilesMap, err
}
