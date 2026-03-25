//go:build windows

package app

import (
	"log/slog"
	"os"

	"github.com/alexandre-daubois/ember/internal/model"
)

// dumpSignal returns a channel that never receives on Windows (no SIGUSR1 support).
func dumpSignal() <-chan os.Signal {
	return make(chan os.Signal)
}

func dumpState(_ *model.State, log *slog.Logger) {
	log.Warn("state dump not supported on Windows")
}

// reloadSignal returns a channel that never receives on Windows (no SIGHUP support).
func reloadSignal() <-chan os.Signal {
	return make(chan os.Signal)
}
