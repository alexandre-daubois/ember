// Package ember provides the public entry point for running Ember,
// a real-time monitoring tool for Caddy and FrankenPHP.
//
// EXPERIMENTAL: the plugin API (pkg/plugin, pkg/metrics) is not yet
// stable and may change in any future release. Feedback is very welcome;
// please open an issue on the Ember repository if something does not fit
// your use case so the plugin system can evolve with real needs.
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
//
// To stamp a custom version string (e.g. from -ldflags), use
// [RunWithVersion] instead of [Run].
package ember

import (
	"os"

	"github.com/alexandre-daubois/ember/internal/app"
)

// defaultVersion is reported when a binary calls [Run] without an
// explicit version. Custom binaries should prefer [RunWithVersion].
const defaultVersion = "dev"

// Run starts Ember with command-line arguments from os.Args.
// The reported version is "dev"; for a stamped build, use
// [RunWithVersion].
func Run() error {
	return app.Run(os.Args[1:], defaultVersion)
}

// RunWithVersion starts Ember with command-line arguments from os.Args
// and the given version string. Intended for custom binaries that set
// their own version via -ldflags.
func RunWithVersion(version string) error {
	return app.Run(os.Args[1:], version)
}

// RunWithArgs starts Ember with the given arguments and version string.
// This is useful for testing or embedding Ember with custom arguments.
func RunWithArgs(args []string, version string) error {
	return app.Run(args, version)
}
