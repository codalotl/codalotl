package main

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/codalotl/codalotl/internal/docubot"
	"github.com/codalotl/codalotl/internal/gocode"
	"github.com/codalotl/codalotl/internal/llmmodel"
	"github.com/codalotl/codalotl/internal/q/cli"
	"github.com/codalotl/codalotl/internal/q/health"
)

type addDocsFlagValues struct {
	model              string
	reflowWidth        int
	logFile            string
	documentTestFiles  bool
	onlyPublicAPI      bool
	excludeIdentifiers string
	tokenBudget        int
}

type addDocsConfig struct {
	model              llmmodel.ModelID
	reflowWidth        int
	logFile            string
	documentTestFiles  bool
	onlyPublicAPI      bool
	excludeIdentifiers []string
	tokenBudget        int
}

func main() {
	os.Exit(run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}

func run(args []string, in io.Reader, out io.Writer, errW io.Writer) int {
	root := newRootCommand()
	return cli.Run(context.Background(), root, cli.Options{
		Args: args,
		In:   in,
		Out:  out,
		Err:  errW,
	})
}

func newRootCommand() *cli.Command {
	root := &cli.Command{
		Name:  "docubot",
		Short: "Development runner for docubot.",
	}

	common := root.PersistentFlags()
	model := common.String("model", 0, "", "Model ID to use.")
	reflowWidth := common.Int("reflow-width", 0, 0, "Documentation comment reflow width.")
	logFile := common.String("log-file", 0, "", "Path to a log file.")

	docCmd := &cli.Command{
		Name:  "doc",
		Short: "Add documentation to a Go package.",
		Args:  cli.ExactArgs(1),
	}
	docFlags := docCmd.Flags()
	documentTestFiles := docFlags.Bool("test-files", 0, false, "Document test files as well.")
	onlyPublicAPI := docFlags.Bool("only-public-api", 0, false, "Only apply documentation for public/exported identifiers.")
	excludeIdentifiers := docFlags.String("exclude-identifiers", 0, "", "Comma-separated identifiers to avoid documenting.")
	tokenBudget := docFlags.Int("token-budget", 0, 0, "Token limit to use.")

	docCmd.Run = func(c *cli.Context) error {
		cfg, err := resolveAddDocsConfig(addDocsFlagValues{
			model:              *model,
			reflowWidth:        *reflowWidth,
			logFile:            *logFile,
			documentTestFiles:  *documentTestFiles,
			onlyPublicAPI:      *onlyPublicAPI,
			excludeIdentifiers: *excludeIdentifiers,
			tokenBudget:        *tokenBudget,
		})
		if err != nil {
			return err
		}

		return runAddDocs(c, c.Args[0], cfg)
	}

	root.AddCommand(docCmd)
	return root
}

func resolveAddDocsConfig(flags addDocsFlagValues) (addDocsConfig, error) {
	modelValue := flags.model
	model := llmmodel.ModelID(modelValue)
	if model == "" {
		model = llmmodel.DefaultModel
	}
	if !model.Valid() {
		return addDocsConfig{}, cli.UsageError{Message: fmt.Sprintf("invalid --model: %q", modelValue)}
	}

	if flags.reflowWidth < 0 {
		return addDocsConfig{}, cli.UsageError{Message: "--reflow-width must be >= 0"}
	}
	if flags.tokenBudget < 0 {
		return addDocsConfig{}, cli.UsageError{Message: "--token-budget must be >= 0"}
	}

	return addDocsConfig{
		model:              model,
		reflowWidth:        flags.reflowWidth,
		logFile:            flags.logFile,
		documentTestFiles:  flags.documentTestFiles,
		onlyPublicAPI:      flags.onlyPublicAPI,
		excludeIdentifiers: parseIdentifierList(flags.excludeIdentifiers),
		tokenBudget:        flags.tokenBudget,
	}, nil
}

func parseIdentifierList(value string) []string {
	if value == "" {
		return nil
	}

	parts := strings.Split(value, ",")
	identifiers := make([]string, 0, len(parts))
	for _, part := range parts {
		identifier := strings.TrimSpace(part)
		if identifier != "" {
			identifiers = append(identifiers, identifier)
		}
	}
	return identifiers
}

func runAddDocs(c *cli.Context, target string, cfg addDocsConfig) error {
	logger, closeLogger, err := loggerFromFile(cfg.logFile)
	if err != nil {
		return err
	}
	defer closeLogger()

	pkg, err := loadPackage(target)
	if err != nil {
		return fmt.Errorf("load package: %w", err)
	}

	changes, err := docubot.AddDocs(pkg, docubot.AddDocsOptions{
		DocumentTestFiles:               cfg.documentTestFiles,
		OnlyDocumentExportedIdentifiers: cfg.onlyPublicAPI,
		TokenBudget:                     cfg.tokenBudget,
		ExcludeIdentifiers:              cfg.excludeIdentifiers,
		BaseOptions: docubot.BaseOptions{
			ReflowMaxWidth: cfg.reflowWidth,
			Out:            c.Out,
			Model:          cfg.model,
			Ctx:            health.NewCtx(logger),
		},
	})
	if err != nil {
		return err
	}

	fmt.Fprintf(c.Out, "Applied %d documentation change(s).\n", len(changes))
	for _, change := range changes {
		fmt.Fprintf(c.Out, "- %s\n", strings.Join(change.IDs(), ", "))
	}
	return nil
}

func loggerFromFile(path string) (*slog.Logger, func(), error) {
	if path == "" {
		return slog.New(slog.NewTextHandler(io.Discard, nil)), func() {}, nil
	}

	file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return nil, nil, fmt.Errorf("open log file: %w", err)
	}

	return slog.New(slog.NewTextHandler(file, nil)), func() { _ = file.Close() }, nil
}

func loadPackage(target string) (*gocode.Package, error) {
	absTarget, err := filepath.Abs(target)
	if err != nil {
		return nil, err
	}

	module, err := gocode.NewModule(absTarget)
	if err != nil {
		return nil, err
	}

	relDir, err := filepath.Rel(module.AbsolutePath, absTarget)
	if err != nil {
		return nil, err
	}
	if relDir == "." {
		relDir = ""
	}

	return module.LoadPackageByRelativeDir(filepath.ToSlash(relDir))
}
