package main

import (
	"fmt"
	"os"

	"github.com/alexandre-daubois/ember"
)

var version = "1.0.0-dev"

func main() {
	ember.Version = version
	if err := ember.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}
