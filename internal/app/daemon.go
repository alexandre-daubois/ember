package app

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/alexandredaubois/ember/internal/exporter"
	"github.com/alexandredaubois/ember/internal/fetcher"
	"github.com/alexandredaubois/ember/internal/model"
)

func runDaemon(ctx context.Context, f *fetcher.HTTPFetcher, cfg *config) {
	holder := &exporter.StateHolder{}
	var state model.State

	mux := http.NewServeMux()
	mux.HandleFunc("/metrics", exporter.Handler(holder))
	srv := &http.Server{Addr: cfg.expose, Handler: mux}

	go func() {
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			fmt.Fprintf(os.Stderr, "metrics server error: %v\n", err)
			os.Exit(1)
		}
	}()

	fmt.Fprintf(os.Stderr, "ember daemon: exposing metrics on %s/metrics\n", cfg.expose)

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
			return
		case <-ticker.C:
			poll()
		}
	}
}
