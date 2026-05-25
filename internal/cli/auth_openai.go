package cli

import (
	"errors"
	"fmt"
	"strings"
	"time"

	qcli "github.com/codalotl/codalotl/internal/q/cli"
	"github.com/codalotl/codalotl/internal/q/remotemonitor"
	"github.com/codalotl/codalotl/internal/subscriptions/openaisub"
)

var (
	runOpenAISubLogin       = openaisub.Login
	runOpenAISubLogout      = openaisub.LogoutWithOptions
	runOpenAISubCheckStatus = openaisub.CheckStatusWithOptions
)

func newAuthCommand(runWithConfig runWithConfigFunc) *qcli.Command {
	authCmd := &qcli.Command{
		Name:  "auth",
		Short: "Authentication commands.",
		Long:  "Commands for managing authentication with optional external services.",
	}

	openaiCmd := &qcli.Command{
		Name:  "openai",
		Short: "Manage OpenAI ChatGPT subscription auth.",
		Long:  "Login, logout, and inspect saved OpenAI ChatGPT subscription credentials.",
	}

	loginCmd := &qcli.Command{
		Name:             "login",
		Short:            "Login to an OpenAI ChatGPT subscription.",
		Long:             "Starts the OpenAI ChatGPT subscription device login flow and saves credentials for future use.",
		Usage:            "[--no-browser]",
		NoPositionalArgs: true,
		Example: strings.TrimSpace(`
codalotl auth openai login
codalotl auth openai login --no-browser
`),
	}
	noBrowser := loginCmd.Flags().Bool("no-browser", 0, false, "Print verification instructions without opening a browser.")
	loginCmd.Args = qcli.NoArgs
	loginCmd.Run = runWithConfig("auth_openai_login", func(c *qcli.Context, _ Config, _ *remotemonitor.Monitor) error {
		if err := runOpenAISubLogin(c.Context, openaisub.LoginOptions{
			Options: openaisub.Options{
				Out: c.Out,
			},
			NoBrowser: *noBrowser,
		}); err != nil {
			return qcli.ExitError{Code: 1, Err: err}
		}
		return writeStringln(c.Out, "OpenAI ChatGPT subscription login complete.")
	})

	logoutCmd := &qcli.Command{
		Name:             "logout",
		Short:            "Logout from an OpenAI ChatGPT subscription.",
		Long:             "Deletes saved OpenAI ChatGPT subscription credentials.",
		Args:             qcli.NoArgs,
		NoPositionalArgs: true,
		Example: strings.TrimSpace(`
codalotl auth openai logout
`),
		Run: runWithConfig("auth_openai_logout", func(c *qcli.Context, _ Config, _ *remotemonitor.Monitor) error {
			if err := runOpenAISubLogout(openaisub.Options{Out: c.Out}); err != nil {
				return qcli.ExitError{Code: 1, Err: err}
			}
			return writeStringln(c.Out, "OpenAI ChatGPT subscription credentials removed.")
		}),
	}

	statusCmd := &qcli.Command{
		Name:             "status",
		Short:            "Show OpenAI ChatGPT subscription auth status.",
		Long:             "Reports whether saved OpenAI ChatGPT subscription credentials are configured and usable.",
		Args:             qcli.NoArgs,
		NoPositionalArgs: true,
		Example: strings.TrimSpace(`
codalotl auth openai status
`),
		Run: runWithConfig("auth_openai_status", func(c *qcli.Context, _ Config, _ *remotemonitor.Monitor) error {
			status, err := runOpenAISubCheckStatus(c.Context, openaisub.Options{})
			if err != nil {
				return qcli.ExitError{Code: 1, Err: err}
			}
			if err := writeOpenAIAuthStatus(c, status); err != nil {
				return err
			}
			if !status.LoggedIn {
				return qcli.ExitError{Code: 1, Err: errors.New("")}
			}
			return nil
		}),
	}

	openaiCmd.AddCommand(loginCmd, logoutCmd, statusCmd)
	authCmd.AddCommand(openaiCmd)
	return authCmd
}

func writeOpenAIAuthStatus(c *qcli.Context, status openaisub.Status) error {
	if !status.LoggedIn {
		if err := writeStringln(c.Out, "OpenAI ChatGPT subscription: not logged in"); err != nil {
			return err
		}
		return writeOpenAIAuthPath(c, status.Path)
	}

	if err := writeStringln(c.Out, "OpenAI ChatGPT subscription: logged in"); err != nil {
		return err
	}
	if accountID := strings.TrimSpace(status.ChatGPTAccountID); accountID != "" {
		if _, err := fmt.Fprintf(c.Out, "Account ID: %s\n", accountID); err != nil {
			return err
		}
	}
	if !status.ExpiresAt.IsZero() {
		if _, err := fmt.Fprintf(c.Out, "Token expires: %s\n", status.ExpiresAt.UTC().Format(time.RFC3339)); err != nil {
			return err
		}
	}
	return writeOpenAIAuthPath(c, status.Path)
}

func writeOpenAIAuthPath(c *qcli.Context, path string) error {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil
	}
	_, err := fmt.Fprintf(c.Out, "Credential file: %s\n", path)
	return err
}
