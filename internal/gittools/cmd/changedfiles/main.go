package main

import (
	"fmt"
	"os"

	"github.com/codalotl/codalotl/internal/gittools"
)

func main() {
	commit, ref, err := gittools.HeuristicMergeBase(".")
	if err != nil {
		fmt.Fprintf(os.Stderr, "get merge base: %v\n", err)
		os.Exit(1)
	}

	paths, err := gittools.ChangedPathsSince(".", commit, true)
	if err != nil {
		fmt.Fprintf(os.Stderr, "get changed files: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("commit: %s\n", commit)
	fmt.Printf("ref: %s\n", ref)
	for _, path := range paths {
		fmt.Println(path)
	}
}
