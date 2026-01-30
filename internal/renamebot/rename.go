package renamebot

import (
	"fmt"
	"github.com/codalotl/codalotl/internal/gocode"
	"github.com/codalotl/codalotl/internal/gorenamer"
	"github.com/codalotl/codalotl/internal/llmcomplete"
	"github.com/codalotl/codalotl/internal/q/health"
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

func RenameForConsistency(pkg *gocode.Package, options BaseOptions) error {

	filterOptions := gocode.FilterIdentifiersOptionsAll
	err := gocode.EachPackageWithIdentifiers(pkg, nil, filterOptions, filterOptions, func(p *gocode.Package, ids []string, onlyTests bool) error {
		testStr := "non-tests"
		if onlyTests {
			if p.IsTestPackage() {
				testStr = "_test package"
			} else {
				testStr = "tests"
			}
		}
		fmt.Printf("Renaming identifiers in %s... (%s)\n", p.Name, testStr)

		// DESIGN NOTE:
		// In theory, we may want to iteratively get renames for a single file, then recalculate the type summary.
		// This let's borderline cases become dominant, letting future renames be more obvious.
		// However, this comes at the cost of doing each file serially, which is very slow.

		// Get renames in parallel:
		renames, err := getRenamesForConsistency(p, onlyTests, options)
		if err != nil {
			return err
		}

		// fmt.Println("GOT RENAMES:")
		// for _, r := range renames {
		// 	fmt.Println(r)
		// }

		fmt.Printf("  Applying renames:\n")
		for _, r := range renames {
			fmt.Printf("    %s -> %s in %s in %s\n", r.From, r.To, r.FuncID, r.File)
		}

		// apply to codebase
		err = applyRenames(p, renames)
		if err != nil {
			return err
		}

		return nil
	})
	if err != nil {
		return err
	}

	return nil
}

// getRenamesForConsistency in parallel (limit K concurrency) will get renames on a per-file basis, then aggregate them all.
func getRenamesForConsistency(pkg *gocode.Package, onlyTests bool, options BaseOptions) ([]ProposedRename, error) {
	// Build package-wide summary (restricted to tests or non-tests) and drop already-unified types.
	ps, err := newPackageSummary(pkg, onlyTests)
	if err != nil {
		return nil, err
	}
	ps.rejectUnified()

	// Collect files to process for this pass (tests vs non-tests).
	var files []*gocode.File
	for _, f := range pkg.Files {
		if f == nil {
			continue
		}
		if f.IsTest != onlyTests {
			continue
		}
		files = append(files, f)
	}
	if len(files) == 0 {
		return nil, nil
	}

	// Limit concurrency when querying the LLM per file.
	parallelism := 5
	sem := make(chan struct{}, parallelism)
	var wg sync.WaitGroup

	renameCh := make(chan []ProposedRename, len(files))
	errCh := make(chan error, len(files))

	for _, file := range files {
		file := file
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-sem }()

			// Build context for this file including relevant package-wide naming summary.
			ctx := contextForFile(pkg, file, ps)

			fmt.Printf("  Getting renames for %s...\n", file.FileName)

			renames, e := askLLMForRenames(ctx, options)
			if e != nil {
				errCh <- e
				return
			}
			// Annotate file name if missing.
			for i := range renames {
				if renames[i].File == "" {
					renames[i].File = file.FileName
				}
			}
			renameCh <- renames
		}()
	}

	wg.Wait()
	close(renameCh)
	close(errCh)

	for e := range errCh {
		if e != nil {
			return nil, e
		}
	}

	var all []ProposedRename
	for rs := range renameCh {
		if len(rs) > 0 {
			all = append(all, rs...)
		}
	}

	return all, nil
}

// apply renames does the renames.
func applyRenames(pkg *gocode.Package, renames []ProposedRename) error {
	if len(renames) == 0 {
		return nil
	}

	reqs := make([]gorenamer.IdentifierRename, 0, len(renames))
	for _, r := range renames {
		reqs = append(reqs, gorenamer.IdentifierRename{
			From:     r.From,
			To:       r.To,
			DeclID:   r.FuncID,
			Context:  r.Context,
			FileName: r.File,
		})
	}

	succeeded, failed, err := gorenamer.Rename(pkg, reqs)
	if err != nil {
		return err
	}

	fmt.Printf("  %d succeeded; %d failed\n", len(succeeded), len(failed))
	if len(failed) > 0 {
		fmt.Println("  Failed renames:")
		for _, r := range failed {
			reason := ""
			if r.Err != nil {
				reason = r.Err.Error()
			}
			fmt.Printf("    %s -> %s in %s in %s: %s\n", r.From, r.To, r.DeclID, r.FileName, reason)
		}
	}
	return nil
}
