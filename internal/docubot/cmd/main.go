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
	commonConfig
	documentTestFiles  bool
	onlyPublicAPI      bool
	excludeIdentifiers []string
	tokenBudget        int
}

type commonFlagValues struct {
	model       string
	reflowWidth int
	logFile     string
}

type commonConfig struct {
	model       llmmodel.ModelID
	reflowWidth int
	logFile     string
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
		Usage: "<pkg>",
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

	fixCmd := &cli.Command{
		Name:  "fix",
		Short: "Find and fix bad comments in a Go package.",
		Usage: "<pkg> [identifier ...]",
		Args:  minimumArgs(1),
	}
	fixCmd.Run = func(c *cli.Context) error {
		cfg, err := resolveCommonConfig(commonFlagValues{
			model:       *model,
			reflowWidth: *reflowWidth,
			logFile:     *logFile,
		})
		if err != nil {
			return err
		}

		return runFixDocs(c, c.Args[0], c.Args[1:], cfg)
	}

	root.AddCommand(docCmd, fixCmd)
	return root
}

func resolveCommonConfig(flags commonFlagValues) (commonConfig, error) {
	modelValue := flags.model
	model := llmmodel.ModelID(modelValue)
	if model == "" {
		model = llmmodel.DefaultModel
	}
	if !model.Valid() {
		return commonConfig{}, cli.UsageError{Message: fmt.Sprintf("invalid --model: %q", modelValue)}
	}

	if flags.reflowWidth < 0 {
		return commonConfig{}, cli.UsageError{Message: "--reflow-width must be >= 0"}
	}

	return commonConfig{
		model:       model,
		reflowWidth: flags.reflowWidth,
		logFile:     flags.logFile,
	}, nil
}

func resolveAddDocsConfig(flags addDocsFlagValues) (addDocsConfig, error) {
	common, err := resolveCommonConfig(commonFlagValues{
		model:       flags.model,
		reflowWidth: flags.reflowWidth,
		logFile:     flags.logFile,
	})
	if err != nil {
		return addDocsConfig{}, err
	}
	if flags.tokenBudget < 0 {
		return addDocsConfig{}, cli.UsageError{Message: "--token-budget must be >= 0"}
	}

	return addDocsConfig{
		commonConfig:       common,
		documentTestFiles:  flags.documentTestFiles,
		onlyPublicAPI:      flags.onlyPublicAPI,
		excludeIdentifiers: parseIdentifierList(flags.excludeIdentifiers),
		tokenBudget:        flags.tokenBudget,
	}, nil
}

func minimumArgs(n int) cli.ArgsFunc {
	return func(args []string) error {
		if len(args) < n {
			return cli.UsageError{Message: fmt.Sprintf("requires at least %d arg(s), got %d", n, len(args))}
		}
		return nil
	}
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

func runFixDocs(c *cli.Context, target string, identifiers []string, cfg commonConfig) error {
	logger, closeLogger, err := loggerFromFile(cfg.logFile)
	if err != nil {
		return err
	}
	defer closeLogger()

	pkg, err := loadPackage(target)
	if err != nil {
		return fmt.Errorf("load package: %w", err)
	}

	changes, err := docubot.FindAndFixDocErrors(pkg, identifiers, docubot.FindFixDocErrorsOptions{
		BaseOptions: docubot.BaseOptions{
			ReflowMaxWidth: cfg.reflowWidth,
			Out:            c.Out,
			Model:          cfg.model,
			Ctx:            health.NewCtx(logger),
		},
	})
	if err != nil && len(changes) == 0 {
		return err
	}

	fmt.Fprintf(c.Out, "Applied %d documentation fix(es).\n", len(changes))
	for _, change := range changes {
		fmt.Fprintf(c.Out, "- %s\n", strings.Join(change.Change.IDs(), ", "))
	}
	return err
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
