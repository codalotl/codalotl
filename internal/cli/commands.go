package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/codalotl/codalotl/internal/agentbuilder"
	"github.com/codalotl/codalotl/internal/docubot"
	"github.com/codalotl/codalotl/internal/goclitools"
	"github.com/codalotl/codalotl/internal/gocode"
	"github.com/codalotl/codalotl/internal/gocodecontext"
	"github.com/codalotl/codalotl/internal/initialcontext"
	"github.com/codalotl/codalotl/internal/lints"
	"github.com/codalotl/codalotl/internal/llmmodel"
	"github.com/codalotl/codalotl/internal/llmstream"
	"github.com/codalotl/codalotl/internal/noninteractive"
	qcas "github.com/codalotl/codalotl/internal/q/cas"
	qcli "github.com/codalotl/codalotl/internal/q/cli"
	"github.com/codalotl/codalotl/internal/q/health"
	"github.com/codalotl/codalotl/internal/q/remotemonitor"
	"github.com/codalotl/codalotl/internal/specmd"
	toolcli "github.com/codalotl/codalotl/internal/tools/cli"
	toolrefactor "github.com/codalotl/codalotl/internal/tools/refactor"
	"github.com/codalotl/codalotl/internal/tools/toolsetinterface"
	"github.com/codalotl/codalotl/internal/tui"
	"github.com/codalotl/codalotl/internal/updatedocs"
)

var runTUIWithConfig = tui.RunWithConfig
var runNoninteractiveExec = noninteractive.Exec
var runDocubotAddDocs = docubot.AddDocs

const packagePathArgDescription = "Go-style package path (for example ., .., ./internal/cli), " +
	"import path, explicit relative/absolute package directory, or bare CWD-relative fallback directory. " +
	"Import paths take precedence. Package patterns with ... are not supported."

const specPathArgDescription = "Package import path, package directory, or SPEC.md file path. " +
	"Import paths take precedence over bare CWD-relative directories. Package patterns with ... are not supported."

type configState struct {
	once sync.Once
	cfg  Config
	err  error
}

func (s *configState) get() (Config, error) {
	s.once.Do(func() {
		s.cfg, s.err = loadConfig()
	})
	return s.cfg, s.err
}

type startupState struct {
	once sync.Once
	err  error
}

func (s *startupState) validate(cfg Config) error {
	s.once.Do(func() {
		s.err = validateStartup(cfg, goclitools.DefaultRequiredTools())
	})
	return s.err
}

func installAgentToolOverrides() {
	agentbuilder.OverrideTool(toolcli.ToolNameCodalotlCLI, func(toolsetinterface.Options) (llmstream.Tool, error) {
		return toolcli.NewCodalotlCLITool(newCodalotlCLICommandTree), nil
	})
	agentbuilder.OverrideTool(toolrefactor.ToolNameRefactor, func(opts toolsetinterface.Options) (llmstream.Tool, error) {
		return toolrefactor.NewRefactorTool(opts.Authorizer, toolrefactor.Options{
			AgentInvoker:   opts.AgentInvoker,
			Model:          opts.Model,
			LintSteps:      opts.LintSteps,
			NewCommandTree: newCodalotlCLICommandTree,
		}), nil
	})
}

func newCLIRunWithConfig(loadConfigForRuns bool) (runWithConfigFunc, *cliRunState) {
	cfgState := &configState{}
	startup := &startupState{}
	runState := &cliRunState{}

	ensureMonitor := func(cfg Config) *remotemonitor.Monitor {
		if !loadConfigForRuns {
			return nil
		}
		if m := runState.getMonitor(); m != nil {
			return m
		}

		m := newCLIMonitor(Version)
		configureMonitorReporting(m, cfg)
		if m != nil {
			// Version checking doesn't send data; launch early so commands can
			// display update notices without blocking too long.
			m.FetchLatestVersionFromHost()
		}

		runState.setMonitor(m)
		return m
	}

	runWithConfig := func(event string, next func(c *qcli.Context, cfg Config, m *remotemonitor.Monitor) error) qcli.RunFunc {
		if !loadConfigForRuns {
			return func(c *qcli.Context) error {
				runState.setEvent(event)
				return next(c, Config{}, nil)
			}
		}
		return func(c *qcli.Context) error {
			cfg, err := cfgState.get()
			if err != nil {
				return qcli.ExitError{Code: 1, Err: err}
			}

			m := ensureMonitor(cfg)
			runState.setEvent(event)

			return withPanicReporting(m, runState, event, func() error {
				if m != nil {
					m.ReportEventAsync(event, nil, true)
				}
				if err := startup.validate(cfg); err != nil {
					return qcli.ExitError{Code: 1, Err: err}
				}
				return next(c, cfg, m)
			})
		}
	}
	return runWithConfig, runState
}

func newCLIRunWithConfigNoStartup(loadConfigForRuns bool, runState *cliRunState) runWithConfigFunc {
	cfgState := &configState{}

	ensureMonitor := func(cfg Config) *remotemonitor.Monitor {
		if !loadConfigForRuns {
			return nil
		}
		if m := runState.getMonitor(); m != nil {
			return m
		}

		m := newCLIMonitor(Version)
		configureMonitorReporting(m, cfg)
		if m != nil {
			m.FetchLatestVersionFromHost()
		}

		runState.setMonitor(m)
		return m
	}

	return func(event string, next func(c *qcli.Context, cfg Config, m *remotemonitor.Monitor) error) qcli.RunFunc {
		if !loadConfigForRuns {
			return func(c *qcli.Context) error {
				runState.setEvent(event)
				return next(c, Config{}, nil)
			}
		}
		return func(c *qcli.Context) error {
			cfg, err := cfgState.get()
			if err != nil {
				return qcli.ExitError{Code: 1, Err: err}
			}

			m := ensureMonitor(cfg)
			runState.setEvent(event)

			return withPanicReporting(m, runState, event, func() error {
				if m != nil {
					m.ReportEventAsync(event, nil, true)
				}
				return next(c, cfg, m)
			})
		}
	}
}

func newRootCommand(loadConfigForRuns bool) (*qcli.Command, *cliRunState) {
	runWithConfig, runState := newCLIRunWithConfig(loadConfigForRuns)
	runWithConfigNoStartup := newCLIRunWithConfigNoStartup(loadConfigForRuns, runState)
	root := &qcli.Command{
		Name:  "codalotl",
		Short: "LLM-assisted Go coding agent.",
		Long:  "Launch the codalotl TUI, or run one of the codalotl subcommands. Use `codalotl .` as an alias for launching the TUI.",
		Usage: "[command]",
		Args: func(args []string) error {
			// Allow `codalotl .` as an alias for launching the TUI (muscle memory
			// with tools like `code .`). Any other path continues to be invalid.
			if len(args) == 1 && args[0] == "." {
				return nil
			}
			if len(args) == 0 {
				return nil
			}
			return qcli.UsageError{Message: fmt.Sprintf("unknown command: %s", args[0])}
		},
		Run: runWithConfig("start_tui", func(c *qcli.Context, cfg Config, m *remotemonitor.Monitor) error {
			// If PreferredModel is empty, pass the zero value so TUI keeps its
			// default model behavior.
			modelID := llmmodel.ModelID(strings.TrimSpace(cfg.PreferredModel))

			steps, err := lints.ResolveSteps(&cfg.Lints, cfg.ReflowWidth)
			if err != nil {
				return qcli.ExitError{Code: 1, Err: fmt.Errorf("invalid configuration: lints: %w", err)}
			}
			var casDB *qcas.DB
			if wd, err := os.Getwd(); err == nil {
				// Best-effort: CAS is optional for TUI, but when available we want
				// consistent root selection with `codalotl cas set`.
				if mod, err := gocode.NewModule(wd); err == nil {
					if db, err := casQDBForBaseDir(mod.AbsolutePath); err == nil {
						casDB = db
					}
				}
			}

			return runTUIWithConfig(tui.Config{
				Palette:   tui.PaletteName(cfg.Theme),
				ModelID:   modelID,
				LintSteps: steps,
				AutoYes:   cfg.AutoYes,
				CASDB:     casDB,
				Monitor:   m,
				PersistModelID: func(newModelID llmmodel.ModelID) error {
					return persistPreferredModelID(cfg, newModelID)
				},
			})
		}),
	}

	execCmd := &qcli.Command{
		Name:  "exec",
		Short: "Run the noninteractive agent with a prompt.",
		Long: "Runs codalotl's noninteractive agent once. The prompt is sent as the user message. " +
			"Use --package to enter package mode, --yes to auto-approve permission checks, and --slash-command=orchestrate to start the built-in orchestrator flow.",
		Usage: "[<prompt> ...]",
		ArgHelp: []qcli.ArgHelp{
			{
				Display:     "[<prompt> ...]",
				Description: "User message to send to the agent. Required unless --slash-command starts a session that can run without an initial message.",
			},
		},
		Example: strings.TrimSpace(`
codalotl exec "Summarize this repository"
codalotl exec --package internal/cli "Explain the CLI commands"
codalotl exec --yes --slash-command=orchestrate "Plan this refactor"
`),
	}
	execFlags := execCmd.Flags()
	execPackage := execFlags.String("package", 'p', "", "Run in Go package mode at this package path (import path or dir; must resolve inside cwd).")
	execYes := execFlags.Bool("yes", 'y', false, "Auto-approve any permission checks (noninteractive).")
	execNoColor := execFlags.Bool("no-color", 0, false, "Disable ANSI colors and formatting.")
	execJSON := execFlags.Bool("json", 0, false, "Output newline-delimited JSON.")
	execModel := execFlags.String("model", 0, "", "LLM model ID to use (overrides config preferredmodel; empty = default).")
	execSlashCommand := execFlags.String("slash-command", 0, "", "Apply a TUI-style slash command at session start (supported: orchestrate, /orchestrate).")
	execArgs := qcli.MinimumArgs(1)
	execCmd.Args = func(args []string) error {
		if len(args) == 0 {
			switch strings.TrimSpace(*execSlashCommand) {
			case "orchestrate", "/orchestrate":
				return nil
			}
		}
		return execArgs(args)
	}
	execCmd.Run = runWithConfig("exec", func(c *qcli.Context, cfg Config, _ *remotemonitor.Monitor) error {
		userPrompt := strings.TrimSpace(strings.Join(c.Args, " "))
		slashCommand := strings.TrimSpace(*execSlashCommand)

		// Match the TUI behavior: if the user hasn't explicitly selected a model
		// on the command line, use the configured preferred model, and otherwise
		// let noninteractive keep its default model behavior.
		modelID := llmmodel.ModelID(strings.TrimSpace(*execModel))
		if modelID == "" {
			modelID = llmmodel.ModelID(strings.TrimSpace(cfg.PreferredModel))
		}

		steps, err := lints.ResolveSteps(&cfg.Lints, cfg.ReflowWidth)
		if err != nil {
			return qcli.ExitError{Code: 1, Err: fmt.Errorf("invalid configuration: lints: %w", err)}
		}

		packagePath := strings.TrimSpace(*execPackage)
		if packagePath != "" && !slashCommandAllowsEmptyInitialPrompt(slashCommand) {
			packagePath, err = resolvePackagePathInsideCWD(packagePath)
			if err != nil {
				return err
			}
		}

		err = runNoninteractiveExec(userPrompt, noninteractive.Options{
			PackagePath:  packagePath,
			SlashCommand: slashCommand,
			ModelID:      modelID,
			LintSteps:    steps,
			AutoYes:      cfg.AutoYes || *execYes,
			NoFormatting: *execNoColor,
			OutputJSON:   *execJSON,
			Out:          c.Out,
		})
		if err == nil {
			return nil
		}
		if noninteractive.IsPrinted(err) {
			return qcli.ExitError{Code: 1, Err: errors.New("")}
		}
		return err
	})
	iterateCmd := newIterateCommand(runWithConfig)

	contextCmd := &qcli.Command{
		Name:  "context",
		Short: "Print code contexts suitable for sending to an LLM.",
		Long:  "Print LLM-ready context derived from Go packages. These commands are intended for humans and tools that want to copy codalotl's package context into an LLM prompt.",
	}

	versionCmd := &qcli.Command{
		Name:             "version",
		Short:            "Print the codalotl version.",
		Long:             "Prints update status when available, followed by the current codalotl version on the last line.",
		Args:             qcli.NoArgs,
		NoPositionalArgs: true,
		Example: strings.TrimSpace(`
codalotl version
`),
		Run: func(c *qcli.Context) error {
			m := newCLIMonitor(Version)
			if m != nil {
				// `version` does not send events/errors/panics (it doesn't load
				// config, so we don't know user telemetry settings).
				m.SetReportingEnabled(false, false, false)
				m.FetchLatestVersionFromHost()
			}

			latest, ok := latestVersionWithTimeout(m, defaultVersionTimeout)
			if ok {
				status, ok := versionStatusOutput(Version, latest)
				if ok {
					if _, err := io.WriteString(c.Out, status); err != nil {
						return err
					}
					return writeStringln(c.Out, Version)
				}
			}
			return writeStringln(c.Out, Version)
		},
	}

	configCmd := &qcli.Command{
		Name:             "config",
		Short:            "Print codalotl configuration.",
		Long:             "Prints the effective codalotl configuration, redacting provider keys and showing config locations, effective model, and provider API key environment variables.",
		Args:             qcli.NoArgs,
		NoPositionalArgs: true,
		Example: strings.TrimSpace(`
codalotl config
`),
		Run: runWithConfig("config", func(c *qcli.Context, cfg Config, m *remotemonitor.Monitor) error {
			if err := maybeWriteUpdateNotice(c.Out, m, Version, defaultNoticeWaitTimeout); err != nil {
				return err
			}
			return writeConfig(c.Out, cfg)
		}),
	}

	specCmd := &qcli.Command{
		Name:  "spec",
		Short: "SPEC.md tools.",
		Long:  "Commands for formatting, comparing, and reporting package SPEC.md files.",
	}
	fmtCmd := &qcli.Command{
		Name:  "fmt",
		Short: "Format Go code blocks in SPEC.md.",
		Long:  "Formats Go code blocks in a SPEC.md file. Prints the SPEC.md path when the file was modified.",
		Usage: "<path/to/pkg_or_SPEC.md>",
		ArgHelp: []qcli.ArgHelp{
			{
				Display:     "<path/to/pkg_or_SPEC.md>",
				Description: specPathArgDescription,
			},
		},
		Example: strings.TrimSpace(`
codalotl spec fmt internal/mypkg
codalotl spec fmt internal/mypkg/SPEC.md
`),
		Args: qcli.ExactArgs(1),
		Run: runWithConfig("spec_fmt", func(c *qcli.Context, cfg Config, _ *remotemonitor.Monitor) error {
			specPath, err := resolveSpecPathArg(c.Args[0])
			if err != nil {
				return err
			}
			spec, err := specmd.Read(specPath)
			if err != nil {
				return err
			}
			reflowWidth := cfg.ReflowWidth
			if !cfg.Lints.Reflows() {
				reflowWidth = 0
			}
			modified, err := spec.FormatGoCodeBlocks(reflowWidth)
			if err != nil {
				return err
			}
			if !modified {
				return nil
			}
			return writeStringln(c.Out, specPath)
		}),
	}
	diffCmd := &qcli.Command{
		Name:  "diff",
		Short: "Print diffs between SPEC.md and the package implementation.",
		Long:  "Compares the Public API declared in SPEC.md with the package's implemented public API and prints human/LLM-friendly diffs. Prints nothing when there are no differences.",
		Usage: "<path/to/pkg_or_SPEC.md>",
		ArgHelp: []qcli.ArgHelp{
			{
				Display:     "<path/to/pkg_or_SPEC.md>",
				Description: specPathArgDescription,
			},
		},
		Example: strings.TrimSpace(`
codalotl spec diff internal/mypkg
codalotl spec diff internal/mypkg/SPEC.md
`),
		Args: qcli.ExactArgs(1),
		Run: runWithConfig("spec_diff", func(c *qcli.Context, _ Config, _ *remotemonitor.Monitor) error {
			specPath, err := resolveSpecPathArg(c.Args[0])
			if err != nil {
				return err
			}
			spec, err := specmd.Read(specPath)
			if err != nil {
				return err
			}
			diffs, err := spec.ImplementationDiffs()
			if err != nil {
				return err
			}
			if len(diffs) == 0 {
				return nil
			}
			return specmd.FormatDiffs(diffs, c.Out)
		}),
	}
	lsMismatchCmd := &qcli.Command{
		Name:  "ls-mismatch",
		Short: "List packages where SPEC.md differs from the implementation.",
		Long:  "Lists packages in a Go package pattern where codalotl spec diff would print a diff. Packages without SPEC.md, invalid packages, or packages with errors but no diff are omitted.",
		Usage: "<pkg/pattern>",
		ArgHelp: []qcli.ArgHelp{
			{
				Display:     "<pkg/pattern>",
				Description: "Go package pattern to scan, such as ./... or ./internal/...",
			},
		},
		Example: strings.TrimSpace(`
codalotl spec ls-mismatch ./...
codalotl spec ls-mismatch ./internal/...
`),
		Args: qcli.ExactArgs(1),
		Run: runWithConfig("spec_ls_mismatch", func(c *qcli.Context, _ Config, _ *remotemonitor.Monitor) error {
			return runSpecLsMismatch(c.Context, c.Out, c.Args[0])
		}),
	}
	specCmd.AddCommand(fmtCmd, diffCmd, lsMismatchCmd, newSpecStatusCommand(runWithConfig))
	casCmd := &qcli.Command{
		Name:  "cas",
		Short: "Content-addressable metadata storage (CAS).",
		Long:  "Commands for inspecting content-addressed metadata keyed by package source contents.",
	}
	getCmd := &qcli.Command{
		Name:  "get",
		Short: "Get a JSON value for (package, namespace).",
		Long:  "Retrieves the stored CAS value and associated metadata for the current source contents of a package. Prints nothing and exits non-zero when no value is set.",
		Usage: "<namespace> <path/to/pkg>",
		ArgHelp: []qcli.ArgHelp{
			{
				Display:     "<namespace>",
				Description: "Registered non-versioned namespace name.",
			},
			{
				Display:     "<path/to/pkg>",
				Description: packagePathArgDescription,
			},
		},
		Example: strings.TrimSpace(`
codalotl cas get specconforms internal/mypkg
`),
		Args: qcli.ExactArgs(2),
		Run: runWithConfig("cas_get", func(c *qcli.Context, _ Config, _ *remotemonitor.Monitor) error {
			spec, err := resolveCASNamespaceSpec(c.Args[0])
			if err != nil {
				return qcli.UsageError{Message: err.Error()}
			}
			pkg, mod, err := loadPackageArg(c.Args[1])
			if err != nil {
				return err
			}
			db, err := casReadDBForBaseDir(mod.AbsolutePath)
			if err != nil {
				return err
			}
			var value any
			ok, info, err := db.Retrieve(pkg, spec, &value)
			if err != nil {
				return err
			}
			if !ok {
				return qcli.ExitError{Code: 1, Err: errors.New("")}
			}
			out := casRetrieveOutput{
				OK: ok,
			}
			out.Value = value
			out.AdditionalInfo = info
			enc := json.NewEncoder(c.Out)
			enc.SetIndent("", "  ")
			enc.SetEscapeHTML(false)
			return enc.Encode(out)
		}),
	}
	lsNamespacesCmd := &qcli.Command{
		Name:             "ls-namespaces",
		Short:            "List registered CAS namespaces.",
		Long:             "Lists registered CAS namespaces and active versions, sorted by namespace name.",
		Args:             qcli.NoArgs,
		NoPositionalArgs: true,
		Example: strings.TrimSpace(`
codalotl cas ls-namespaces
`),
		Run: runWithConfig("cas_ls_namespaces", func(c *qcli.Context, _ Config, _ *remotemonitor.Monitor) error {
			for _, spec := range sortedCASNamespaceSpecs() {
				if _, err := fmt.Fprintf(c.Out, "%s %d\n", spec.Name, spec.Version); err != nil {
					return err
				}
			}
			return nil
		}),
	}
	lsStaleCmd := &qcli.Command{
		Name:  "ls-stale",
		Short: "List packages with stale CAS coverage for a namespace.",
		Long: "Lists module packages, one per line, whose current contents lack a CAS record for the given namespace. " +
			"Packages with no prior CAS record are always listed; otherwise age and churn thresholds are ORed.",
		Usage: "<namespace>",
		ArgHelp: []qcli.ArgHelp{
			{
				Display:     "<namespace>",
				Description: "Registered non-versioned namespace name.",
			},
		},
		Example: strings.TrimSpace(`
codalotl cas ls-stale specconforms
codalotl cas ls-stale specconforms --stale-after-days=14 --min-churn-percent=10
`),
	}
	lsStaleAfterDays := lsStaleCmd.Flags().Int("stale-after-days", 0, defaultCASLsStaleAfterDays, "List packages whose prior CAS record is at least this many days old.")
	lsStaleMinChurnPercent := lsStaleCmd.Flags().Int("min-churn-percent", 0, defaultCASLsStaleMinChurnPercent, "List packages whose churn from prior CAS record is at least this percent.")
	lsStaleCmd.Args = func(args []string) error {
		if err := qcli.ExactArgs(1)(args); err != nil {
			return err
		}
		return validateCASLsStaleThresholds(*lsStaleAfterDays, *lsStaleMinChurnPercent)
	}
	lsStaleCmd.Run = runWithConfig("cas_ls_stale", func(c *qcli.Context, _ Config, _ *remotemonitor.Monitor) error {
		return runCASLsStale(c.Context, c.Out, c.Args[0], *lsStaleAfterDays, *lsStaleMinChurnPercent)
	})
	lsSummaryCmd := &qcli.Command{
		Name:  "ls-summary",
		Short: "Summarize CAS coverage for packages in the current repo.",
		Long:  "Displays per-package current and prior CAS coverage for a registered namespace across discovered modules in the nearest git repo.",
		Usage: "<namespace>",
		ArgHelp: []qcli.ArgHelp{
			{
				Display:     "<namespace>",
				Description: "Registered non-versioned namespace name.",
			},
		},
		Example: strings.TrimSpace(`
codalotl cas ls-summary specconforms
codalotl cas ls-summary specconforms --csv
`),
		Args: qcli.ExactArgs(1),
	}
	lsSummaryCSV := lsSummaryCmd.Flags().Bool("csv", 0, false, "Emit CSV instead of a terminal-oriented table.")
	lsSummaryCmd.Run = runWithConfig("cas_ls_summary", func(c *qcli.Context, _ Config, _ *remotemonitor.Monitor) error {
		return runCASLsSummary(c.Context, c.Out, c.Args[0], *lsSummaryCSV)
	})
	pruneCmd := &qcli.Command{
		Name:             "prune",
		Short:            "Delete obsolete CAS records.",
		Long:             "Deletes prior namespace versions and superseded package CAS records across discovered modules in the nearest git repo.",
		Args:             qcli.NoArgs,
		NoPositionalArgs: true,
		Example: strings.TrimSpace(`
codalotl cas prune
codalotl cas prune --days=14
`),
	}
	pruneDays := pruneCmd.Flags().Int("days", 0, defaultCASPruneDays, "Delete superseded package records older than this many days.")
	pruneCmd.Args = func(args []string) error {
		if err := qcli.NoArgs(args); err != nil {
			return err
		}
		return validateCASPruneDays(*pruneDays)
	}
	pruneCmd.Run = runWithConfig("cas_prune", func(c *qcli.Context, _ Config, _ *remotemonitor.Monitor) error {
		return runCASPrune(c.Context, c.Out, *pruneDays)
	})
	recertifyCmd := newCASRecertifyCommand(runWithConfig)
	casCmd.AddCommand(getCmd, lsNamespacesCmd, lsSummaryCmd, lsStaleCmd, pruneCmd, recertifyCmd)

	panicCmd := &qcli.Command{
		Name:             "panic",
		Hidden:           true,
		Args:             qcli.NoArgs,
		NoPositionalArgs: true,
		Run: runWithConfig("panic", func(*qcli.Context, Config, *remotemonitor.Monitor) error {
			panic("intentional panic")
		}),
	}

	publicCmd := &qcli.Command{
		Name:  "public",
		Short: "Print the public API of a package.",
		Long:  "Prints godoc-like public API context for a package, grouped by source file and formatted for use in an LLM prompt.",
		Usage: "<path/to/pkg>",
		ArgHelp: []qcli.ArgHelp{
			{
				Display:     "<path/to/pkg>",
				Description: packagePathArgDescription,
			},
		},
		Example: strings.TrimSpace(`
codalotl context public internal/cli
codalotl context public ./internal/cli
`),
		Args: qcli.ExactArgs(1),
		Run: runWithConfig("context_public", func(c *qcli.Context, _ Config, _ *remotemonitor.Monitor) error {
			pkg, _, err := loadPackageArg(c.Args[0])
			if err != nil {
				return err
			}
			doc, err := gocodecontext.PublicPackageDocumentation(pkg)
			if err != nil {
				return err
			}
			return writeStringln(c.Out, doc)
		}),
	}

	initialCmd := &qcli.Command{
		Name:  "initial",
		Short: "Print the initial context for an LLM starting to work on a package.",
		Long:  "Prints the package lay of the land used when an LLM starts working on a package: files, identifiers, used-by packages, diagnostics, tests, and lints.",
		Usage: "<path/to/pkg>",
		ArgHelp: []qcli.ArgHelp{
			{
				Display:     "<path/to/pkg>",
				Description: packagePathArgDescription,
			},
		},
		Example: strings.TrimSpace(`
codalotl context initial internal/cli
codalotl context initial ./internal/cli
`),
		Args: qcli.ExactArgs(1),
		Run: runWithConfig("context_initial", func(c *qcli.Context, cfg Config, _ *remotemonitor.Monitor) error {
			pkg, _, err := loadPackageArg(c.Args[0])
			if err != nil {
				return err
			}

			steps, err := lints.ResolveSteps(&cfg.Lints, cfg.ReflowWidth)
			if err != nil {
				return qcli.ExitError{Code: 1, Err: fmt.Errorf("invalid configuration: lints: %w", err)}
			}

			out, err := initialcontext.Create(pkg, steps, false)
			if err != nil {
				return err
			}
			return writeStringln(c.Out, out)
		}),
	}

	packagesCmd := &qcli.Command{
		Name:             "packages",
		Short:            "Print an LLM-friendly list of packages available in the current module.",
		Long:             "Prints packages available from the module containing the current working directory as LLM-ready context. The output is intentionally text-oriented and may change; do not parse it as a stable machine format.",
		Args:             qcli.NoArgs,
		NoPositionalArgs: true,
		Example: strings.TrimSpace(`
codalotl context packages
codalotl context packages --search 'internal/(cli|q)'
codalotl context packages --deps
`),
	}
	fs := packagesCmd.Flags()
	search := fs.String("search", 's', "", "Filter packages by Go regexp.")
	deps := fs.Bool("deps", 0, false, "Include packages from direct module dependencies.")
	packagesCmd.Run = runWithConfig("context_packages", func(c *qcli.Context, _ Config, _ *remotemonitor.Monitor) error {
		wd, err := os.Getwd()
		if err != nil {
			return err
		}
		_, llmContext, err := gocodecontext.PackageList(wd, *search, *deps)
		if err != nil {
			return err
		}
		return writeStringln(c.Out, llmContext)
	})

	contextCmd.AddCommand(publicCmd, initialCmd, packagesCmd)
	root.AddCommand(execCmd, iterateCmd, contextCmd, versionCmd, configCmd, newAuthCommand(runWithConfigNoStartup), newPRCommand(), newDocsCommand(runWithConfig, true), specCmd, casCmd, panicCmd)
	return root, runState
}

func newCodalotlCLICommandTree() *qcli.Command {
	runWithConfig, _ := newCLIRunWithConfig(true)
	root := &qcli.Command{
		Name:  "codalotl",
		Short: "Whitelisted codalotl CLI commands.",
		Long:  "Run whitelisted codalotl CLI commands in-process.",
		Usage: "[command]",
	}
	casCmd := &qcli.Command{
		Name:  "cas",
		Short: "Content-addressable metadata storage (CAS).",
		Long:  "Whitelisted CAS commands.",
	}
	casCmd.AddCommand(newCASRecertifyCommand(runWithConfig))
	specCmd := &qcli.Command{
		Name:  "spec",
		Short: "SPEC.md tools.",
		Long:  "Whitelisted SPEC.md commands.",
	}
	specCmd.AddCommand(newSpecStatusCommand(runWithConfig))
	root.AddCommand(newDocsCommand(runWithConfig, false), specCmd, casCmd)
	return root
}

func newSpecStatusCommand(runWithConfig runWithConfigFunc) *qcli.Command {
	statusCmd := &qcli.Command{
		Name:             "status",
		Short:            "Print per-package SPEC.md status across discovered repo modules.",
		Long:             "Prints a table for packages across Go modules discovered from the nearest git repo, showing package, whether SPEC.md exists, whether Public API matches implementation, and whether the CAS conformance result is set.",
		Args:             qcli.NoArgs,
		NoPositionalArgs: true,
		Example: strings.TrimSpace(`
codalotl spec status
`),
		Run: runWithConfig("spec_status", func(c *qcli.Context, _ Config, _ *remotemonitor.Monitor) error {
			return runSpecStatus(c.Context, c.Out)
		}),
	}
	return statusCmd
}

func newCASRecertifyCommand(runWithConfig runWithConfigFunc) *qcli.Command {
	recertifyCmd := &qcli.Command{
		Name:  "recertify",
		Short: "Copy prior CAS records forward to current package contents.",
		Long: "Recertifies current package contents for one or more registered CAS namespaces. " +
			"Existing current records are left unchanged; prior records are never deleted or mutated.",
		Usage: "<path/to/pkg> --namespaces=\"<namespace1>[,<namespace2>,...]\"",
		ArgHelp: []qcli.ArgHelp{
			{
				Display:     "<path/to/pkg>",
				Description: packagePathArgDescription,
			},
		},
		Example: strings.TrimSpace(`
codalotl cas recertify internal/mypkg --namespaces="docs-fix,specconforms"
`),
	}
	recertifyNamespaces := recertifyCmd.Flags().String("namespaces", 0, "", "Comma-separated registered non-versioned namespace names to recertify.")
	recertifyCmd.Args = func(args []string) error {
		if err := qcli.ExactArgs(1)(args); err != nil {
			return err
		}
		if _, err := parseCASNamespacesFlag(*recertifyNamespaces); err != nil {
			return qcli.UsageError{Message: err.Error()}
		}
		return nil
	}
	recertifyCmd.Run = runWithConfig("cas_recertify", func(c *qcli.Context, _ Config, _ *remotemonitor.Monitor) error {
		return runCASRecertify(c.Out, c.Args[0], *recertifyNamespaces)
	})
	return recertifyCmd
}

func newDocsCommand(runWithConfig runWithConfigFunc, includeReflow bool) *qcli.Command {
	docsCmd := &qcli.Command{
		Name:  "docs",
		Short: "Documentation tools.",
		Long:  "Commands for adding, fixing, or reflowing Go documentation comments.",
	}
	children := []*qcli.Command{
		newDocsAddCommand(runWithConfig),
		newDocsFixCommand(runWithConfig),
	}
	if includeReflow {
		children = append(children, newDocsReflowCommand(runWithConfig))
	}
	docsCmd.AddCommand(children...)
	return docsCmd
}

func newDocsAddCommand(runWithConfig runWithConfigFunc) *qcli.Command {
	addCmd := &qcli.Command{
		Name:  "add",
		Short: "Add missing documentation comments to a package.",
		Long: "Adds missing package documentation comments using an LLM. Existing comments are preserved. " +
			"By default, the command documents exported and unexported package-level identifiers in non-test files. " +
			"Use --public-only to document only exported identifiers, or --important to document exported identifiers plus other important identifiers.",
		Usage: "<path/to/pkg>",
		ArgHelp: []qcli.ArgHelp{
			{
				Display:     "<path/to/pkg>",
				Description: packagePathArgDescription,
			},
		},
		Example: strings.TrimSpace(`
codalotl docs add internal/mypkg
codalotl docs add --public-only internal/mypkg
codalotl docs add --important internal/mypkg
codalotl docs add --include-test ./internal/mypkg
`),
	}
	addFlags := addCmd.Flags()
	addPublicOnly := addFlags.Bool("public-only", 0, false, "Only document exported identifiers.")
	addImportant := addFlags.Bool("important", 0, false, "Only document exported identifiers and other important identifiers.")
	addIncludeTest := addFlags.Bool("include-test", 0, false, "Include test files, including black-box _test packages.")
	addCmd.Args = func(args []string) error {
		if *addPublicOnly && *addImportant {
			return qcli.UsageError{Message: "--public-only and --important are mutually exclusive"}
		}
		return qcli.ExactArgs(1)(args)
	}
	addCmd.Run = runWithConfig("docs_add", func(c *qcli.Context, cfg Config, _ *remotemonitor.Monitor) error {
		pkg, _, err := loadPackageArg(c.Args[0])
		if err != nil {
			return err
		}

		changes, err := runDocubotAddDocs(pkg, docubot.AddDocsOptions{
			DocumentTestFiles:                *addIncludeTest,
			OnlyDocumentExportedIdentifiers:  *addPublicOnly,
			OnlyDocumentImportantIdentifiers: *addImportant,
			BaseOptions: docubot.BaseOptions{
				ReflowMaxWidth: cfg.ReflowWidth,
				Context:        c.Context,
				Out:            c.Out,
				Model:          effectiveModel(cfg),
				Ctx:            health.NewCtx(slog.New(slog.NewTextHandler(io.Discard, nil))),
			},
		})
		if err != nil {
			return err
		}

		return writeStringln(c.Out, fmt.Sprintf("Applied %d documentation change(s).", len(changes)))
	})
	return addCmd
}

func newDocsReflowCommand(runWithConfig runWithConfigFunc) *qcli.Command {
	reflowCmd := &qcli.Command{
		Name:  "reflow",
		Short: "Reflow documentation in one or more paths.",
		Long: "Reflows Go documentation comments in the specified files or directories. " +
			"Prints changed .go files, one per line, using module-relative paths when possible.",
		Usage: "<path> ...",
		ArgHelp: []qcli.ArgHelp{
			{
				Display:     "<path> ...",
				Description: "One or more Go source files or directories to reflow.",
			},
		},
		Example: strings.TrimSpace(`
codalotl docs reflow internal/mypkg
codalotl docs reflow --width=100 --check internal/mypkg pkg/foo.go
`),
		Args: qcli.MinimumArgs(1),
	}
	reflowFlags := reflowCmd.Flags()
	reflowWidth := reflowFlags.Int("width", 'w', 0, "Override reflow width (default: config reflowwidth).")
	reflowCheck := reflowFlags.Bool("check", 0, false, "Don't write files; only print which files would change.")
	reflowCmd.Run = runWithConfig("docs_reflow", func(c *qcli.Context, cfg Config, _ *remotemonitor.Monitor) error {
		width := cfg.ReflowWidth
		if *reflowWidth != 0 {
			if *reflowWidth <= 0 {
				return qcli.UsageError{Message: fmt.Sprintf("invalid --width: must be > 0 (got %d)", *reflowWidth)}
			}
			width = *reflowWidth
		}

		findModuleRoot := func(dir string) string {
			for {
				modPath := filepath.Join(dir, "go.mod")
				if fi, err := os.Stat(modPath); err == nil && !fi.IsDir() {
					return dir
				}
				parent := filepath.Dir(dir)
				if parent == dir {
					return ""
				}
				dir = parent
			}
		}

		displayPathForModifiedFile := func(absPath string) string {
			modRoot := findModuleRoot(filepath.Dir(absPath))
			if modRoot != "" {
				if rel, err := filepath.Rel(modRoot, absPath); err == nil && rel != "." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && rel != ".." {
					return rel
				}
			}

			// Fallback: prefer cwd-relative when it doesn't escape.
			if wd, err := os.Getwd(); err == nil {
				if rel, err := filepath.Rel(wd, absPath); err == nil && rel != "." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && rel != ".." {
					return rel
				}
			}

			return absPath
		}

		modifiedFiles, skipped, err := updatedocs.ReflowDocumentationPaths(c.Args, *reflowCheck, updatedocs.Options{
			ReflowMaxWidth: width,
		})
		if err != nil {
			return err
		}

		uniqModified := map[string]struct{}{}
		for _, abs := range modifiedFiles {
			uniqModified[displayPathForModifiedFile(abs)] = struct{}{}
		}
		var displayModified []string
		for p := range uniqModified {
			displayModified = append(displayModified, p)
		}
		sort.Strings(displayModified)
		for _, p := range displayModified {
			if _, err := fmt.Fprintln(c.Out, p); err != nil {
				return err
			}
		}

		if len(skipped) == 0 {
			return nil
		}
		if _, err := fmt.Fprintln(c.Err, "Warning: some identifiers could not be reflowed:"); err != nil {
			return err
		}
		for _, id := range skipped {
			if _, err := fmt.Fprintf(c.Err, "- %s\n", strings.TrimSpace(id)); err != nil {
				return err
			}
		}
		return nil
	})
	return reflowCmd
}

func writeStringln(w io.Writer, s string) error {
	if !strings.HasSuffix(s, "\n") {
		s += "\n"
	}
	_, err := fmt.Fprint(w, s)
	return err
}

func resolveSpecPathArg(arg string) (string, error) {
	arg = strings.TrimSpace(arg)
	if arg == "" {
		return "", qcli.UsageError{Message: "missing <path/to/pkg_or_SPEC.md>"}
	}
	// Keep behavior consistent with other <path/to/pkg> arguments.
	if strings.Contains(arg, "...") {
		return "", qcli.UsageError{Message: `package patterns ("...") are not supported; provide a single package directory or SPEC.md`}
	}

	if isExplicitFilesystemPath(arg) {
		return resolveExistingSpecPath(arg)
	}

	pkg, _, err := loadPackageArg(arg)
	if err == nil {
		return filepath.Join(pkg.AbsolutePath(), "SPEC.md"), nil
	}
	pkgErr := err

	if specPath, ok, err := resolveExistingSpecPathIfPresent(arg); err != nil {
		return "", err
	} else if ok {
		return specPath, nil
	}

	return "", pkgErr
}

func resolveExistingSpecPath(arg string) (string, error) {
	specPath, ok, err := resolveExistingSpecPathIfPresent(arg)
	if err != nil {
		return "", err
	}
	if !ok {
		return "", qcli.UsageError{Message: fmt.Sprintf("expected SPEC.md file or package directory (got %q)", arg)}
	}
	return specPath, nil
}

func resolveExistingSpecPathIfPresent(arg string) (string, bool, error) {
	if info, err := os.Stat(arg); err == nil {
		if info.IsDir() {
			return filepath.Join(arg, "SPEC.md"), true, nil
		}
		if filepath.Base(arg) != "SPEC.md" {
			return "", false, qcli.UsageError{Message: fmt.Sprintf("expected SPEC.md file or package directory (got %q)", arg)}
		}
		return arg, true, nil
	} else if !os.IsNotExist(err) {
		return "", false, err
	}
	return "", false, nil
}
