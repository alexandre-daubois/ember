// Package ember provides the public entry point for running Ember,
// a real-time monitoring tool for Caddy and FrankenPHP.
//
// EXPERIMENTAL: the plugin API (pkg/plugin, pkg/metrics) is not yet
// stable and may change in any future release.
//
// Plugin authors use this package to build custom Ember binaries:
//
//	import (
//	    "github.com/alexandre-daubois/ember"
//	    _ "github.com/myorg/ember-myplugin"
//	)
//
//	func main() {
//	    ember.Run()
//	}
package ember

import (
	"os"

	"github.com/alexandre-daubois/ember/internal/app"
)

// Version is set at build time via -ldflags.
// When empty, it defaults to "dev".
var Version = "dev"

// Run starts Ember with command-line arguments from os.Args.
func Run() error {
	return app.Run(os.Args[1:], Version)
}

// RunWithArgs starts Ember with the given arguments and version string.
// This is useful for testing or embedding Ember with custom arguments.
func RunWithArgs(args []string, version string) error {
	return app.Run(args, version)
}
