package main

import (
	"os"

	"github.com/codalotl/codalotl/internal/cli"
)

func main() {
	exitCode, _ := cli.Run(os.Args, nil)
	os.Exit(exitCode)
}
