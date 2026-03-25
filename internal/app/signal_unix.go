//go:build !windows

package app

import (
	"encoding/json"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/alexandre-daubois/ember/internal/model"
)

// dumpSignal returns a channel that receives a value each time SIGUSR1 is sent to the process.
func dumpSignal() <-chan os.Signal {
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, syscall.SIGUSR1)
	return ch
}

// reloadSignal returns a channel that receives a value each time SIGHUP is sent to the process.
func reloadSignal() <-chan os.Signal {
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, syscall.SIGHUP)
	return ch
}

func dumpState(state *model.State, log *slog.Logger) {
	if state.Current == nil {
		log.Info("dump requested but no data available yet")
		return
	}

	out := buildJSONOutput(state.Current, state)
	b, err := json.Marshal(out)
	if err != nil {
		log.Error("dump failed", "err", err)
		return
	}

	log.Info("state dump (SIGUSR1)", "snapshot", string(b))
}
