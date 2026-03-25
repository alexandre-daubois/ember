package app

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/alexandre-daubois/ember/internal/exporter"
	"github.com/alexandre-daubois/ember/internal/fetcher"
	"github.com/alexandre-daubois/ember/internal/model"
)

func metricsURL(addr string) string {
	if len(addr) > 0 && addr[0] == ':' {
		addr = "localhost" + addr
	}
	return "http://" + addr + "/metrics"
}

func runDaemon(ctx context.Context, f fetcher.Fetcher, cfg *config) error {
	ctx, cancel := context.WithCancelCause(ctx)
	defer cancel(nil)

	holder := &exporter.StateHolder{}
	var state model.State

	mux := http.NewServeMux()
	mux.HandleFunc("/metrics", exporter.Handler(holder, cfg.metricsPrefix))
	mux.HandleFunc("/healthz", exporter.HealthHandler(holder, cfg.interval))

	var handler http.Handler = mux
	if cfg.metricsAuth != "" {
		user, pass, _ := strings.Cut(cfg.metricsAuth, ":")
		handler = exporter.BasicAuth(mux, user, pass)
	}
	srv := &http.Server{Addr: cfg.expose, Handler: handler}

	go func() {
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			cancel(err)
		}
	}()

	log := cfg.logger
	log.Info("daemon started", "metrics_url", metricsURL(cfg.expose))

	poll := func() {
		snap, err := f.Fetch(ctx)
		if err != nil {
			log.Error("fetch failed", "err", err)
			return
		}
		state.Update(snap)
		holder.Store(state.CopyForExport())
	}

	poll()

	ticker := time.NewTicker(cfg.interval)
	defer ticker.Stop()

	dumpCh := dumpSignal()

	for {
		select {
		case <-ctx.Done():
			shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer shutdownCancel()
			_ = srv.Shutdown(shutdownCtx)
			if cause := context.Cause(ctx); cause != nil && !errors.Is(cause, context.Canceled) {
				return cause
			}
			return nil
		case <-ticker.C:
			poll()
		case <-dumpCh:
			dumpState(&state, log)
		}
	}
}
