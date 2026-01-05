package cli

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"

	"github.com/codalotl/codalotl/internal/gocodecontext"
	"github.com/codalotl/codalotl/internal/initialcontext"
	"github.com/codalotl/codalotl/internal/llmmodel"
	"github.com/codalotl/codalotl/internal/noninteractive"
	qcli "github.com/codalotl/codalotl/internal/q/cli"
	"github.com/codalotl/codalotl/internal/tui"
)

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

func newRootCommand(loadConfigForRuns bool) *qcli.Command {
	cfgState := &configState{}
	runWithConfig := func(next func(c *qcli.Context, cfg Config) error) qcli.RunFunc {
		if !loadConfigForRuns {
			return func(c *qcli.Context) error {
				return next(c, Config{})
			}
		}
		return func(c *qcli.Context) error {
			cfg, err := cfgState.get()
			if err != nil {
				return qcli.ExitError{Code: 1, Err: err}
			}
			return next(c, cfg)
		}
	}

	root := &qcli.Command{
		Name:  "codalotl",
		Short: "codalotl is an LLM-assisted Go coding agent.",
		Args:  qcli.NoArgs,
		Run: runWithConfig(func(c *qcli.Context, cfg Config) error {
			// If PreferredModel is empty, pass the zero value so TUI keeps its
			// default model behavior.
			modelID := llmmodel.ModelID(strings.TrimSpace(cfg.PreferredModel))
			return tui.RunWithConfig(tui.Config{
				ModelID: modelID,
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
	execFlags.String("model", 0, "", "Model to use (placeholder; currently ignored).")
	execCmd.Run = runWithConfig(func(c *qcli.Context, _ Config) error {
		userPrompt := strings.TrimSpace(strings.Join(c.Args, " "))
		err := noninteractive.Exec(userPrompt, noninteractive.Options{
			PackagePath:  *execPackage,
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
			return writeStringln(c.Out, Version)
		},
	}

	configCmd := &qcli.Command{
		Name:  "config",
		Short: "Print codalotl configuration.",
		Args:  qcli.NoArgs,
		Run: runWithConfig(func(c *qcli.Context, cfg Config) error {
			return writeConfig(c.Out, cfg)
		}),
	}

	publicCmd := &qcli.Command{
		Name:  "public",
		Short: "Print the public API of a package.",
		Args:  qcli.ExactArgs(1),
		Run: runWithConfig(func(c *qcli.Context, _ Config) error {
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
		Run: runWithConfig(func(c *qcli.Context, _ Config) error {
			pkg, mod, err := loadPackageArg(c.Args[0])
			if err != nil {
				return err
			}
			out, err := initialcontext.Create(mod.AbsolutePath, pkg)
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
	packagesCmd.Run = runWithConfig(func(c *qcli.Context, _ Config) error {
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
	root.AddCommand(execCmd, contextCmd, versionCmd, configCmd)
	return root
}

func writeStringln(w io.Writer, s string) error {
	if !strings.HasSuffix(s, "\n") {
		s += "\n"
	}
	_, err := fmt.Fprint(w, s)
	return err
}
