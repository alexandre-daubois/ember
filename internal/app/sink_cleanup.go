package app

import (
	"context"
	"log/slog"
	"time"
)

// unregisterWithRetry attempts to call the unregister function up to `attempts`
// times, waiting 3 seconds between failures. Used to make ember's quit
// handshake more robust against a busy Caddy admin endpoint (reload-in-progress,
// bouncer-sync, etc.) — without retry, a single transient timeout leaves a
// stale log sink that loops broken-pipe writes until the next reload.
//
// See Issue #4 in formin/ember-crowdsec for the reference incident
// (272478 broken-pipe entries in 16h on CT 122 caddy).
func unregisterWithRetry(ctx context.Context, name string, fn func(context.Context) error, attempts int) {
	if attempts < 1 {
		attempts = 1
	}
	for i := 0; i < attempts; i++ {
		err := fn(ctx)
		if err == nil {
			return
		}
		if i < attempts-1 {
			slog.Warn("retrying unregister", "sink", name, "attempt", i+1, "err", err)
			select {
			case <-time.After(3 * time.Second):
			case <-ctx.Done():
				slog.Warn("giving up unregister — context done", "sink", name, "err", err)
				return
			}
			continue
		}
		slog.Warn("giving up unregister — Caddy may keep stale sink", "sink", name, "err", err)
	}
}
