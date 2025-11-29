package reorgbot

import (
	"github.com/codalotl/codalotl/internal/goclitools"
	"github.com/codalotl/codalotl/internal/gocode"
	"github.com/codalotl/codalotl/internal/llmcomplete"
	"github.com/codalotl/codalotl/internal/q/health"
	_ "embed"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
)

// BaseOptions carries shared configuration and dependencies for LLM-backed documentation operations.
type BaseOptions struct {
	// Model enables callers to choose an explicit model (this can, in theory, also be accomplished by Conversationalist, but is less ergonomic to callers).
	Model llmcomplete.ModelID

	// Conversationalist allows callers to inject their own LLM implementations, including mock implementations for testing.
	Conversationalist llmcomplete.Conversationalist

	// Logging and health context for operations.
	health.Ctx
}

// ReorgOptions configures package reorganization.
type ReorgOptions struct {
	BaseOptions // BaseOptions provides shared configuration for LLM operations.
}

//go:embed prompts/reorg_oneshot.md
var promptAskForOrgOneshot string

//go:embed prompts/reorg_group.md
var promptAskForOrgGroup string

//go:embed prompts/sort_file.md
var promptSortFile string

// Reorg reorganizes a Go package's files using LLM suggestions.
func Reorg(pkg *gocode.Package, oneShot bool, options ReorgOptions) error {
	filterOptions := gocode.FilterIdentifiersOptions{IncludeTestFuncs: true, IncludeGeneratedFile: false, IncludeAmbiguous: true}

	reorgPkg := func(p *gocode.Package, ids []string, onlyTests bool) error {
		testStr := "non-tests"
		if onlyTests {
			if p.IsTestPackage() {
				testStr = "_test package"
			} else {
				testStr = "tests"
			}
		}
		fmt.Printf("Reorganizing %s... (%s)\n", p.Name, testStr)

		ctx, snippetByCanonicalID := codeContextForPackage(p, ids, onlyTests)

		org, err := askLLMForOrganization(ctx, oneShot, options.BaseOptions)
		if err != nil {
			return err
		}

		if err := orgIsValid(org, snippetByCanonicalID, onlyTests); err != nil {
			return err
		}

		if err := applyReorganization(p, org, snippetByCanonicalID, onlyTests); err != nil {
			return err
		}

		return nil
	}

	// First do non-test code. Since this reorganizes files, pkg will need to be reloaded so that test file reorganization can use this result.
	// (EachPackageWithIdentifiers correctly doesn't relaod packages between iterations).
	err := gocode.EachPackageWithIdentifiers(pkg, nil, filterOptions, filterOptions, func(p *gocode.Package, ids []string, onlyTests bool) error {
		if onlyTests {
			return nil
		}
		return reorgPkg(p, ids, onlyTests)
	})
	if err != nil {
		return err
	}

	// Use ReadPackage instead of reload, since the files changed and reload doesn't rescan files.
	pkg, err = pkg.Module.ReadPackage(pkg.RelativeDir, nil)
	if err != nil {
		return options.LogWrappedErr("reorg.read_package", err)
	}

	err = gocode.EachPackageWithIdentifiers(pkg, nil, filterOptions, filterOptions, func(p *gocode.Package, ids []string, onlyTests bool) error {
		if !onlyTests {
			return nil
		}
		return reorgPkg(p, ids, onlyTests)
	})
	if err != nil {
		return err
	}

	if !oneShot {
		// Use ReadPackage instead of reload, since the files changed and reload doesn't rescan files.
		pkg, err = pkg.Module.ReadPackage(pkg.RelativeDir, nil)
		if err != nil {
			return options.LogWrappedErr("reorg.read_package", err)
		}

		// Resort all files (main and test packages) to refine within-file ordering.
		// Process with bounded concurrency to avoid overwhelming the LLM/provider.
		fileNames := make([]string, 0, len(pkg.Files))
		for name := range pkg.Files {
			fileNames = append(fileNames, name)
		}
		if pkg.TestPackage != nil {
			for name := range pkg.TestPackage.Files {
				fileNames = append(fileNames, name)
			}
		}

		// Default parallelism.
		parallelism := 5

		sem := make(chan struct{}, parallelism)
		errCh := make(chan error, len(fileNames))
		var wg sync.WaitGroup
		for _, fn := range fileNames {
			fn := fn
			wg.Add(1)
			sem <- struct{}{}
			go func() {
				defer wg.Done()
				defer func() { <-sem }()
				if err := ResortFile(pkg, fn, options); err != nil {
					errCh <- err
				}
			}()
		}
		wg.Wait()
		close(errCh)
		for e := range errCh {
			if e != nil {
				return e
			}
		}
	}

	return nil
}

// askLLMForOrganization queries the LLM to get a package reorganization map.
func askLLMForOrganization(ctx string, oneshot bool, opts BaseOptions) (map[string][]string, error) {

	prompt := ""
	if oneshot {
		prompt = promptAskForOrgOneshot
	} else {
		prompt = promptAskForOrgGroup
	}

	conv := opts.Conversationalist
	if conv == nil {
		conv = llmcomplete.NewConversationalist()
	}
	conversation := conv.NewConversation(llmcomplete.ModelIDOrDefault(opts.Model), prompt)
	conversation.SetLogger(opts.Logger)
	conversation.AddUserMessage(ctx)
	resp, err := conversation.Send()
	if err != nil {
		return nil, opts.LogWrappedErr("reorgbot.ask_llm_for_organization", err)
	}

	var org map[string][]string
	if err := json.Unmarshal([]byte(strings.TrimSpace(resp.Text)), &org); err != nil {
		return nil, opts.LogWrappedErr("reorgbot.unmarshal_llm_response", err)
	}
	return org, nil
}

// ResortFile sort's pkg's fileName file only. fileName must be a file in pkg or pkg.TestPackage (ex: "foo.go").
// It does the by asking the LLM to re-order the snippets of a file.
// The imports and everything above the package keyword remain identical.
func ResortFile(pkg *gocode.Package, fileName string, options ReorgOptions) error {
	// Determine which package owns the file
	p := pkg
	file := p.Files[fileName]
	if file == nil && pkg.TestPackage != nil {
		p = pkg.TestPackage
		file = p.Files[fileName]
	}
	if file == nil {
		return options.LogWrappedErr("reorgbot.resort_file.file_not_found", fmt.Errorf("file %q not found in package or test package", fileName))
	}

	fmt.Printf("Fine-tuning order in %s...\n", fileName)

	// Build LLM context for just this file, and collect canonical ids
	ctx, idToSnippet := codeContextForFile(p, file)
	if len(idToSnippet) == 0 {
		// Nothing to reorder
		return nil
	}
	// Derive ids in source order from the file's snippets, excluding package docs
	var ids []string
	for _, s := range p.SnippetsByFile(nil)[fileName] {
		if _, isPkgDoc := s.(*gocode.PackageDocSnippet); isPkgDoc {
			continue
		}
		ids = append(ids, canonicalSnippetID(s))
	}

	// Ask LLM for new order
	sortedIDs, err := askLLMForFileSort(ctx, options.BaseOptions)
	if err != nil {
		return err
	}

	// Validate: must be a permutation of the file's ids
	if err := fileSortIsValid(ids, sortedIDs); err != nil {
		return options.LogWrappedErr("reorgbot.resort_file.invalid_llm_response", err)
	}

	// Apply resort to the single file, preserving header and imports
	if err := applyResort(p, fileName, sortedIDs, idToSnippet); err != nil {
		return err
	}
	return nil
}

// askLLMForFileSort queries the LLM to get a sorted order of snippets for a single file.
func askLLMForFileSort(ctx string, opts BaseOptions) ([]string, error) {

	conv := opts.Conversationalist
	if conv == nil {
		conv = llmcomplete.NewConversationalist()
	}
	conversation := conv.NewConversation(llmcomplete.ModelIDOrDefault(opts.Model), promptSortFile)
	conversation.SetLogger(opts.Logger)
	conversation.AddUserMessage(ctx)
	resp, err := conversation.Send()
	if err != nil {
		return nil, opts.LogWrappedErr("reorgbot.ask_llm_for_file_sort", err)
	}

	var fileSort []string
	if err := json.Unmarshal([]byte(strings.TrimSpace(resp.Text)), &fileSort); err != nil {
		return nil, opts.LogWrappedErr("reorgbot.unmarshal_llm_response_file_sort", err)
	}
	return fileSort, nil
}

// applyResort rewrites a single file in p by preserving its header (package doc and clause)
// and import section verbatim, and re-emitting all other snippets in sortedIDs order.
// Package doc snippets in sortedIDs are ignored when emitting, since they are part of the preserved header.
func applyResort(p *gocode.Package, fileName string, sortedIDs []string, idToSnippet map[string]gocode.Snippet) error {
	// Locate file (main or test package)
	file := p.Files[fileName]
	if file == nil && p.TestPackage != nil {
		file = p.TestPackage.Files[fileName]
	}
	if file == nil {
		return fmt.Errorf("applyResort: file %q not found", fileName)
	}

	// Ensure AST is available to compute prefix end (imports region)
	if file.AST == nil {
		if _, err := file.Parse(nil); err != nil {
			return err
		}
	}

	// Compute end of preserved prefix to exactly match original header (including any blank
	// lines after imports): take the minimum start offset among all non-package snippets.
	// If that fails, fall back to preserving through end of imports or package clause.
	pkgEnd := file.FileSet.Position(file.AST.Name.End()).Offset
	prefixEnd := pkgEnd
	// Fallback using imports if present
	if len(file.AST.Imports) > 0 {
		maxEnd := 0
		for _, imp := range file.AST.Imports {
			end := file.FileSet.Position(imp.End()).Offset
			if end > maxEnd {
				maxEnd = end
			}
		}
		if maxEnd > prefixEnd {
			prefixEnd = maxEnd
		}
	}
	// Prefer exact start of the earliest snippet in this file, and remember its id
	var firstSnippetID string
	if perFile := p.SnippetsByFile(nil)[fileName]; len(perFile) > 0 {
		first := -1
		var firstSnippet gocode.Snippet
		for _, s := range perFile {
			if _, isPkgDoc := s.(*gocode.PackageDocSnippet); isPkgDoc {
				// package doc belongs to header; ignore
				continue
			}
			start := s.Position().Offset
			if start <= 0 {
				continue
			}
			if first == -1 || start < first {
				first = start
				firstSnippet = s
			}
		}
		if first > 0 {
			prefixEnd = first
			if firstSnippet != nil {
				firstSnippetID = canonicalSnippetID(firstSnippet)
			}
		}
	}

	// Build mapping of unattached comments that should be emitted immediately before their next snippet
	idToPreComments := make(map[string][]string)
	var orphanComments []string
	for _, uc := range p.UnattachedComments {
		if uc.FileName != fileName {
			continue
		}
		if uc.AbovePackage {
			// Above package comments are part of preserved header
			continue
		}
		if uc.Next != nil {
			id := canonicalSnippetID(uc.Next)
			// If this unattached comment precedes the earliest snippet in the file,
			// it already exists in the preserved prefix; do not emit it again.
			if firstSnippetID != "" && id == firstSnippetID {
				continue
			}
			idToPreComments[id] = append(idToPreComments[id], uc.Comment)
			continue
		}
		// Orphan comment (no following snippet): append at end of file
		orphanComments = append(orphanComments, uc.Comment)
	}

	// Start composing new file
	var b strings.Builder
	if prefixEnd > len(file.Contents) {
		prefixEnd = len(file.Contents)
	}
	b.Write(file.Contents[:prefixEnd])

	// Ensure file prefix ends with a single newline before appending snippets
	if !strings.HasSuffix(b.String(), "\n") {
		b.WriteString("\n")
	}

	// Emit sorted snippets, skipping package doc which is part of header
	for _, id := range sortedIDs {
		s := idToSnippet[id]
		if s == nil {
			continue
		}
		if _, isPkgDoc := s.(*gocode.PackageDocSnippet); isPkgDoc {
			continue
		}
		if pres := idToPreComments[id]; len(pres) > 0 {
			for _, c := range pres {
				b.Write(ensureSingleTrailingNewline([]byte(c)))
			}
			b.WriteString("\n")
		}
		snippetBytes := ensureSingleTrailingNewline(s.FullBytes())
		b.Write(snippetBytes)
		b.WriteString("\n")
	}

	// Append any orphan unattached comments at end
	if len(orphanComments) > 0 {
		// Ensure separation
		if !strings.HasSuffix(b.String(), "\n\n") {
			if strings.HasSuffix(b.String(), "\n") {
				b.WriteString("\n")
			} else {
				b.WriteString("\n\n")
			}
		}
		for _, c := range orphanComments {
			b.Write(ensureSingleTrailingNewline([]byte(c)))
		}
		b.WriteString("\n")
	}

	// Persist changes to disk and reparse this file in memory
	if err := file.PersistNewContents([]byte(b.String()), true); err != nil {
		return err
	}

	// Format and organize imports in the package directory.
	_, err := goclitools.Gofmt(file.AbsolutePath)
	if err != nil {
		return err
	}

	return nil
}
