package main

import (
	"log"

	"github.com/codalotl/codalotl/internal/tui"
)

func main() {
	if err := tui.Run(); err != nil {
		log.Fatal(err)
	}
}
