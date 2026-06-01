package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"github.com/codalotl/codalotl/internal/lints"
	"github.com/codalotl/codalotl/internal/llmmodel"
	"github.com/codalotl/codalotl/internal/noninteractive/integration"
)

// Main records a noninteractive integration test case from command-line flags.
func main() {
	var (
		repoPath          = flag.String("repo", "", "path to the repo to record")
		packagePath       = flag.String("package", "", "package path relative to --repo")
		modelID           = flag.String("model", "", "model id to run")
		prompt            = flag.String("prompt", "", "prompt to send to noninteractive")
		outputDir         = flag.String("output", "", "output dir for config.json/http.json/repo")
		reflowWidth       = flag.Int("reflowwidth", 0, "optional reflow width passed into lint resolution")
		lintsConfigPath   = flag.String("lints-config", "", "optional path to a JSON file containing the lints config object")
		includeTokenUsage = flag.Bool("include-token-usage", false, "include token usage in the expected done event")
	)
	flag.Parse()

	var lintCfg lints.Lints
	if *lintsConfigPath != "" {
		data, err := os.ReadFile(*lintsConfigPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "read --lints-config: %v\n", err)
			os.Exit(1)
		}
		if err := json.Unmarshal(data, &lintCfg); err != nil {
			fmt.Fprintf(os.Stderr, "parse --lints-config: %v\n", err)
			os.Exit(1)
		}
	}

	err := integration.CreateCase(integration.CreateOptions{
		RepoPath:          *repoPath,
		PackagePath:       *packagePath,
		ModelID:           llmmodel.ModelID(*modelID),
		Prompt:            *prompt,
		OutputDir:         *outputDir,
		ReflowWidth:       *reflowWidth,
		Lints:             lintCfg,
		IncludeTokenUsage: *includeTokenUsage,
		ProgressOut:       os.Stderr,
		JSONStreamOut:     os.Stdout,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "create integration case: %v\n", err)
		os.Exit(1)
	}
}
