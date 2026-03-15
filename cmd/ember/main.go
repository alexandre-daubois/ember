package main

import (
	"fmt"
	"os"

	"github.com/alexandredaubois/ember/internal/app"
)

var version = "1.0.0-dev"

func main() {
	if err := app.Run(os.Args[1:], version); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}
