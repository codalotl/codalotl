package cli

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"

	"github.com/codalotl/codalotl/internal/goclitools"
	"github.com/codalotl/codalotl/internal/gocodecontext"
	"github.com/codalotl/codalotl/internal/initialcontext"
	"github.com/codalotl/codalotl/internal/llmmodel"
	"github.com/codalotl/codalotl/internal/noninteractive"
	qcli "github.com/codalotl/codalotl/internal/q/cli"
	"github.com/codalotl/codalotl/internal/q/remotemonitor"
	"github.com/codalotl/codalotl/internal/tui"
)

var runTUIWithConfig = tui.RunWithConfig

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

func newRootCommand(loadConfigForRuns bool) (*qcli.Command, *cliRunState) {
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

	root := &qcli.Command{
		Name:  "codalotl",
		Short: "codalotl is an LLM-assisted Go coding agent.",
		Args: func(args []string) error {
			// Allow `codalotl .` as an alias for launching the TUI (muscle memory
			// with tools like `code .`). Any other path continues to be invalid.
			if len(args) == 1 && args[0] == "." {
				return nil
			}
			return qcli.NoArgs(args)
		},
		Run: runWithConfig("start_tui", func(c *qcli.Context, cfg Config, m *remotemonitor.Monitor) error {
			// If PreferredModel is empty, pass the zero value so TUI keeps its
			// default model behavior.
			modelID := llmmodel.ModelID(strings.TrimSpace(cfg.PreferredModel))
			return runTUIWithConfig(tui.Config{
				ModelID: modelID,
				Monitor: m,
				PersistModelID: func(newModelID llmmodel.ModelID) error {
					return persistPreferredModelID(cfg, newModelID)
				},
			})
		}),
	}

	execCmd := &qcli.Command{
		Name:  "exec",
		Short: "Run codalotl noninteractively with a prompt.",
		Args:  qcli.MinimumArgs(1),
	}
	execFlags := execCmd.Flags()
	execPackage := execFlags.String("package", 'p', "", "Run in Go package mode, rooted at this package path (must be within cwd).")
	execYes := execFlags.Bool("yes", 'y', false, "Auto-approve any permission checks (noninteractive).")
	execNoColor := execFlags.Bool("no-color", 0, false, "Disable ANSI colors and formatting.")
	execModel := execFlags.String("model", 0, "", "LLM model ID to use (overrides config preferredmodel; empty = default).")
	execCmd.Run = runWithConfig("exec", func(c *qcli.Context, cfg Config, _ *remotemonitor.Monitor) error {
		userPrompt := strings.TrimSpace(strings.Join(c.Args, " "))

		// Match the TUI behavior: if the user hasn't explicitly selected a model
		// on the command line, use the configured preferred model, and otherwise
		// let noninteractive keep its default model behavior.
		modelID := llmmodel.ModelID(strings.TrimSpace(*execModel))
		if modelID == "" {
			modelID = llmmodel.ModelID(strings.TrimSpace(cfg.PreferredModel))
		}
		err := noninteractive.Exec(userPrompt, noninteractive.Options{
			PackagePath:  *execPackage,
			ModelID:      modelID,
			AutoYes:      *execYes,
			NoFormatting: *execNoColor,
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

	contextCmd := &qcli.Command{
		Name:  "context",
		Short: "Print code contexts suitable for sending to an LLM.",
	}

	versionCmd := &qcli.Command{
		Name:  "version",
		Short: "Print codalotl version.",
		Args:  qcli.NoArgs,
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
		Name:  "config",
		Short: "Print codalotl configuration.",
		Args:  qcli.NoArgs,
		Run: runWithConfig("config", func(c *qcli.Context, cfg Config, m *remotemonitor.Monitor) error {
			if err := maybeWriteUpdateNotice(c.Out, m, Version, defaultNoticeWaitTimeout); err != nil {
				return err
			}
			return writeConfig(c.Out, cfg)
		}),
	}

	panicCmd := &qcli.Command{
		Name:   "panic",
		Hidden: true,
		Args:   qcli.NoArgs,
		Run: runWithConfig("panic", func(*qcli.Context, Config, *remotemonitor.Monitor) error {
			panic("intentional panic")
		}),
	}

	publicCmd := &qcli.Command{
		Name:  "public",
		Short: "Print the public API of a package.",
		Args:  qcli.ExactArgs(1),
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
		Args:  qcli.ExactArgs(1),
		Run: runWithConfig("context_initial", func(c *qcli.Context, _ Config, _ *remotemonitor.Monitor) error {
			pkg, _, err := loadPackageArg(c.Args[0])
			if err != nil {
				return err
			}
			out, err := initialcontext.Create(pkg, false)
			if err != nil {
				return err
			}
			return writeStringln(c.Out, out)
		}),
	}

	packagesCmd := &qcli.Command{
		Name:  "packages",
		Short: "Print an LLM-friendly list of packages available in the current module.",
		Args:  qcli.NoArgs,
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
	root.AddCommand(execCmd, contextCmd, versionCmd, configCmd, panicCmd)
	return root, runState
}

func writeStringln(w io.Writer, s string) error {
	if !strings.HasSuffix(s, "\n") {
		s += "\n"
	}
	_, err := fmt.Fprint(w, s)
	return err
}
