package main

import (
	"fmt"
	"os"

	"github.com/alexandre-daubois/ember"
)

var version = "dev"

func main() {
	if err := ember.RunWithVersion(version); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}
