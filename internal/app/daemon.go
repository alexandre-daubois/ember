package app

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/alexandredaubois/ember/internal/exporter"
	"github.com/alexandredaubois/ember/internal/fetcher"
	"github.com/alexandredaubois/ember/internal/model"
)

func metricsURL(addr string) string {
	if len(addr) > 0 && addr[0] == ':' {
		addr = "localhost" + addr
	}
	return "http://" + addr + "/metrics"
}

func runDaemon(ctx context.Context, f *fetcher.HTTPFetcher, cfg *config) error {
	ctx, cancel := context.WithCancelCause(ctx)
	defer cancel(nil)

	holder := &exporter.StateHolder{}
	var state model.State

	mux := http.NewServeMux()
	mux.HandleFunc("/metrics", exporter.Handler(holder))
	srv := &http.Server{Addr: cfg.expose, Handler: mux}

	go func() {
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			cancel(fmt.Errorf("metrics server: %w", err))
		}
	}()

	fmt.Fprintf(os.Stderr, "ember daemon: exposing metrics on %s\n", metricsURL(cfg.expose))

	poll := func() {
		snap, err := f.Fetch(ctx)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			return
		}
		state.Update(snap)
		holder.Store(state.CopyForExport())
	}

	poll()

	ticker := time.NewTicker(cfg.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer shutdownCancel()
			srv.Shutdown(shutdownCtx)
			if cause := context.Cause(ctx); cause != nil && !errors.Is(cause, context.Canceled) {
				return cause
			}
			return nil
		case <-ticker.C:
			poll()
		}
	}
}
