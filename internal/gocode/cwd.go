package gocode

import "path/filepath"

// MustCwd returns the absolute path to the current working directory. It panics if the path cannot be determined.
func MustCwd() string {
	dir, err := filepath.Abs(".")
	if err != nil {
		panic(err)
	}
	return dir
}
