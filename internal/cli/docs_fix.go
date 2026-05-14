package cli

import (
	"fmt"
	"io"
	"log/slog"
	"sort"
	"strings"

	"github.com/codalotl/codalotl/internal/docubot"
	"github.com/codalotl/codalotl/internal/gocas"
	"github.com/codalotl/codalotl/internal/gocode"
	qcli "github.com/codalotl/codalotl/internal/q/cli"
	"github.com/codalotl/codalotl/internal/q/health"
	"github.com/codalotl/codalotl/internal/q/remotemonitor"
)

const (
	docsFixCASNamespace     = gocas.Namespace("docs-fix-1")
	docsFixModeWholePackage = "whole-package"
	docsFixModeIdentifiers  = "identifiers"
)

var runDocubotFindAndFixDocErrors = docubot.FindAndFixDocErrors

type docsFixCASValue struct {
	Schema      string   `json:"schema"`
	Mode        string   `json:"mode"`
	Identifiers []string `json:"identifiers,omitempty"`
	FixCount    int      `json:"fix_count"`
}

func newDocsFixCommand(runWithConfig runWithConfigFunc) *qcli.Command {
	cmd := &qcli.Command{
		Name:  "fix",
		Short: "Fix materially false documentation comments in a package.",
		Long: "Finds materially false existing package documentation comments using an LLM and applies fixes. " +
			"Missing documentation and non-material wording issues are ignored. " +
			"By default, the command scans non-test files, test files, and black-box _test package files.",
		Usage: "<path/to/pkg>",
		ArgHelp: []qcli.ArgHelp{
			{
				Display:     "<path/to/pkg>",
				Description: packagePathArgDescription,
			},
		},
		Example: strings.TrimSpace(`
codalotl docs fix internal/mypkg
codalotl docs fix --identifiers Foo,Bar ./internal/mypkg
`),
		Args: qcli.ExactArgs(1),
	}
	flags := cmd.Flags()
	identifiersFlag := flags.String("identifiers", 0, "", "Comma-separated identifier allowlist to check.")
	cmd.Run = runWithConfig("docs_fix", func(c *qcli.Context, cfg Config, _ *remotemonitor.Monitor) error {
		identifiers, err := parseDocsFixIdentifiers(*identifiersFlag)
		if err != nil {
			return qcli.UsageError{Message: err.Error()}
		}

		pkg, mod, err := loadPackageArg(c.Args[0])
		if err != nil {
			return err
		}

		changes, err := runDocubotFindAndFixDocErrors(pkg, identifiers, docubot.FindFixDocErrorsOptions{
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

		if err := storeDocsFixCASRecord(pkg, mod, identifiers, len(changes)); err != nil {
			return err
		}
		return writeDocsFixSummary(c.Out, len(changes))
	})
	return cmd
}

func parseDocsFixIdentifiers(s string) ([]string, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, nil
	}

	seen := map[string]struct{}{}
	for _, part := range strings.Split(s, ",") {
		id := strings.TrimSpace(part)
		if id == "" {
			return nil, fmt.Errorf("invalid --identifiers: empty identifier")
		}
		seen[id] = struct{}{}
	}

	identifiers := make([]string, 0, len(seen))
	for id := range seen {
		identifiers = append(identifiers, id)
	}
	sort.Strings(identifiers)
	return identifiers, nil
}

func storeDocsFixCASRecord(pkg *gocode.Package, mod *gocode.Module, identifiers []string, fixCount int) error {
	db, err := casDBForBaseDir(mod.AbsolutePath)
	if err != nil {
		return err
	}

	mode := docsFixModeWholePackage
	if len(identifiers) > 0 {
		mode = docsFixModeIdentifiers
	}
	canonicalIdentifiers := append([]string(nil), identifiers...)
	sort.Strings(canonicalIdentifiers)

	value := docsFixCASValue{
		Schema:      string(docsFixCASNamespace),
		Mode:        mode,
		Identifiers: canonicalIdentifiers,
		FixCount:    fixCount,
	}
	return db.StoreOnPackage(pkg, docsFixCASNamespace, value)
}

func writeDocsFixSummary(w io.Writer, fixCount int) error {
	_, err := fmt.Fprintf(w, "Applied %d documentation fix(es).\n", fixCount)
	return err
}
