package main

import (
	"fmt"
	"log"

	"github.com/codalotl/codalotl/internal/gittools"
)

func main() {
	commit, ref, err := gittools.HeuristicMergeBase(".")
	if err != nil {
		log.Fatal(err)
	}

	fmt.Println("Commit: ", commit)
	fmt.Println("Ref: ", ref)
}
