package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/codalotl/codalotl/internal/llmmodel"
	"github.com/codalotl/codalotl/internal/noninteractive/integration"
)

func main() {
	var (
		repoPath          = flag.String("repo", "", "path to the repo to record")
		packagePath       = flag.String("package", "", "package path relative to --repo")
		modelID           = flag.String("model", "", "model id to run")
		prompt            = flag.String("prompt", "", "prompt to send to noninteractive")
		outputDir         = flag.String("output", "", "output dir for config.json/http.json/repo")
		includeTokenUsage = flag.Bool("include-token-usage", false, "include token usage in the expected done event")
	)
	flag.Parse()

	err := integration.CreateCase(integration.CreateOptions{
		RepoPath:          *repoPath,
		PackagePath:       *packagePath,
		ModelID:           llmmodel.ModelID(*modelID),
		Prompt:            *prompt,
		OutputDir:         *outputDir,
		IncludeTokenUsage: *includeTokenUsage,
		ProgressOut:       os.Stderr,
		JSONStreamOut:     os.Stdout,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "create integration case: %v\n", err)
		os.Exit(1)
	}
}
