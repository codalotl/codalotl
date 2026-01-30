package main

import (
	"fmt"
	"os"

	"github.com/codalotl/codalotl/internal/q/termformat"
)

func main() {
	profile, err := termformat.GetColorProfile()
	if err != nil {
		fmt.Fprintf(os.Stderr, "detect color profile: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("Color profile: %s\n", profile)

	fg, bg := termformat.DefaultFBBGColor()
	if _, isNoColor := fg.(termformat.NoColor); isNoColor {
		fmt.Println("Default terminal colors: unavailable (non-interactive?)")
		return
	}

	fmt.Printf("Default FG: %s\n", fg)
	fmt.Printf("Default BG: %s\n", bg)
}
