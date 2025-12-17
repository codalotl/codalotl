package cli

import (
	"fmt"
	"io"
	"strings"

	"github.com/codalotl/codalotl/internal/gocodecontext"
	"github.com/codalotl/codalotl/internal/initialcontext"
	qcli "github.com/codalotl/codalotl/internal/q/cli"
	"github.com/codalotl/codalotl/internal/tui"
)

func newRootCommand() *qcli.Command {
	root := &qcli.Command{
		Name:  "codalotl",
		Short: "codalotl is an LLM-assisted Go coding agent.",
		Run: func(c *qcli.Context) error {
			return tui.Run()
		},
	}

	contextCmd := &qcli.Command{
		Name:  "context",
		Short: "Print code contexts suitable for sending to an LLM.",
	}

	publicCmd := &qcli.Command{
		Name:  "public",
		Short: "Print the public API of a package.",
		Args:  qcli.ExactArgs(1),
		Run: func(c *qcli.Context) error {
			pkg, _, err := loadPackageArg(c.Args[0])
			if err != nil {
				return err
			}
			doc, err := gocodecontext.PublicPackageDocumentation(pkg)
			if err != nil {
				return err
			}
			return writeStringln(c.Out, doc)
		},
	}

	initialCmd := &qcli.Command{
		Name:  "initial",
		Short: "Print the initial context for an LLM starting to work on a package.",
		Args:  qcli.ExactArgs(1),
		Run: func(c *qcli.Context) error {
			pkg, mod, err := loadPackageArg(c.Args[0])
			if err != nil {
				return err
			}
			out, err := initialcontext.Create(mod.AbsolutePath, pkg)
			if err != nil {
				return err
			}
			return writeStringln(c.Out, out)
		},
	}

	contextCmd.AddCommand(publicCmd, initialCmd)
	root.AddCommand(contextCmd)
	return root
}

func writeStringln(w io.Writer, s string) error {
	if !strings.HasSuffix(s, "\n") {
		s += "\n"
	}
	_, err := fmt.Fprint(w, s)
	return err
}
