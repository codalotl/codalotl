package cli

import (
	"fmt"
	"strings"

	"github.com/codalotl/codalotl/internal/openaisubscription"
	qcli "github.com/codalotl/codalotl/internal/q/cli"
)

func newAuthCommand() *qcli.Command {
	authCmd := &qcli.Command{
		Name:  "auth",
		Short: "Manage OpenAI ChatGPT subscription authentication.",
		Long:  "Commands for signing in to OpenAI with a ChatGPT subscription, checking auth status, and signing out.",
	}

	loginCmd := &qcli.Command{
		Name:             "login",
		Short:            "Sign in with a ChatGPT subscription.",
		Long:             "Starts a browser-based OpenAI sign-in flow and stores refresh credentials under ~/.codalotl.",
		Args:             qcli.NoArgs,
		NoPositionalArgs: true,
		Example: strings.TrimSpace(`
codalotl auth login
codalotl auth login --no-browser
`),
	}
	loginNoBrowser := loginCmd.Flags().Bool("no-browser", 0, false, "Print the sign-in URL without opening a browser.")
	loginCmd.Run = func(c *qcli.Context) error {
		creds, err := openaisubscription.Login(c.Context, c.Out, !*loginNoBrowser)
		if err != nil {
			return err
		}
		if creds.ChatGPTAccountID != "" {
			return writeStringln(c.Out, fmt.Sprintf("Signed in with ChatGPT account %s.", creds.ChatGPTAccountID))
		}
		return writeStringln(c.Out, "Signed in with ChatGPT.")
	}

	statusCmd := &qcli.Command{
		Name:             "status",
		Short:            "Show OpenAI subscription auth status.",
		Args:             qcli.NoArgs,
		NoPositionalArgs: true,
		Run: func(c *qcli.Context) error {
			status := openaisubscription.CurrentStatus()
			if !status.SignedIn {
				if err := writeStringln(c.Out, "OpenAI subscription auth: signed out"); err != nil {
					return err
				}
				return writeStringln(c.Out, fmt.Sprintf("Credentials path: %s", status.Path))
			}
			if err := writeStringln(c.Out, "OpenAI subscription auth: signed in"); err != nil {
				return err
			}
			if status.ChatGPTAccountID != "" {
				if err := writeStringln(c.Out, fmt.Sprintf("ChatGPT account: %s", status.ChatGPTAccountID)); err != nil {
					return err
				}
			}
			if !status.ExpiresAt.IsZero() {
				if err := writeStringln(c.Out, fmt.Sprintf("Access token expires: %s", status.ExpiresAt.Format("2006-01-02 15:04:05 MST"))); err != nil {
					return err
				}
			}
			return writeStringln(c.Out, fmt.Sprintf("Credentials path: %s", status.Path))
		},
	}

	logoutCmd := &qcli.Command{
		Name:             "logout",
		Short:            "Remove stored OpenAI subscription credentials.",
		Args:             qcli.NoArgs,
		NoPositionalArgs: true,
		Run: func(c *qcli.Context) error {
			if err := openaisubscription.DeleteCredentials(); err != nil {
				return err
			}
			return writeStringln(c.Out, "Signed out of OpenAI subscription auth.")
		},
	}

	authCmd.AddCommand(loginCmd, statusCmd, logoutCmd)
	return authCmd
}
