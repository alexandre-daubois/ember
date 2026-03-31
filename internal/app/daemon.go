package app

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/alexandre-daubois/ember/internal/exporter"
	"github.com/alexandre-daubois/ember/internal/fetcher"
	"github.com/alexandre-daubois/ember/internal/model"
	"github.com/alexandre-daubois/ember/pkg/plugin"
)

const errorThrottleInterval = 30 * time.Second

type errorThrottle struct {
	lastLogged time.Time
	suppressed int
	failing    bool
}

func (e *errorThrottle) record(log *slog.Logger, err error) {
	e.failing = true
	if time.Since(e.lastLogged) >= errorThrottleInterval {
		if e.suppressed > 0 {
			log.Error("fetch failed", "err", err, "suppressed", e.suppressed)
		} else {
			log.Error("fetch failed", "err", err)
		}
		e.lastLogged = time.Now()
		e.suppressed = 0
	} else {
		e.suppressed++
	}
}

func (e *errorThrottle) recover(log *slog.Logger) {
	if e.failing {
		log.Info("fetch recovered")
		e.failing = false
		e.suppressed = 0
	}
}

func reloadTLS(f fetcher.Fetcher, cfg *config, log *slog.Logger) {
	hf, ok := f.(*fetcher.HTTPFetcher)
	if !ok {
		log.Warn("TLS reload not supported for this fetcher")
		return
	}
	if hf.IsUnixSocket() {
		log.Info("TLS reload skipped (Unix socket connection)")
		return
	}
	if err := configureTLS(hf, cfg); err != nil {
		log.Error("TLS reload failed", "err", err)
		return
	}
	log.Info("TLS certificates reloaded (SIGHUP)")
}

func metricsURL(addr string) string {
	if len(addr) > 0 && addr[0] == ':' {
		addr = "localhost" + addr
	}
	return "http://" + addr + "/metrics"
}

func newMetricsHandler(holder *exporter.StateHolder, cfg *config) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/metrics", exporter.Handler(holder, cfg.metricsPrefix, cfg.recorder))
	mux.HandleFunc("/healthz", exporter.HealthHandler(holder, cfg.interval))

	var handler http.Handler = mux
	if cfg.metricsAuth != "" {
		user, pass, _ := strings.Cut(cfg.metricsAuth, ":")
		handler = exporter.BasicAuth(mux, user, pass)
	}

	return handler
}

func runDaemon(ctx context.Context, f fetcher.Fetcher, cfg *config, plugins []plugin.Plugin) error {
	ctx, cancel := context.WithCancelCause(ctx)
	defer cancel(nil)

	holder := &exporter.StateHolder{}
	var state model.State
	dPlugins := newDaemonPlugins(plugins)

	srv := &http.Server{Addr: cfg.expose, Handler: newMetricsHandler(holder, cfg)}

	go func() {
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			cancel(err)
		}
	}()

	log := cfg.logger
	log.Info("daemon started", "metrics_url", metricsURL(cfg.expose))

	var errThrottle errorThrottle

	poll := func() {
		snap, err := f.Fetch(ctx)
		if err != nil {
			errThrottle.record(log, err)
			return
		}
		errThrottle.recover(log)
		state.Update(snap)
		fetchDaemonPlugins(ctx, dPlugins, log)
		holder.StoreAll(state.CopyForExport(), daemonPluginExports(dPlugins))
	}

	poll()

	ticker := time.NewTicker(cfg.interval)
	defer ticker.Stop()

	dumpCh := dumpSignal()
	reloadCh := reloadSignal()

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
		case <-reloadCh:
			reloadTLS(f, cfg, log)
		}
	}
}

type daemonPlugin struct {
	name     string
	fetcher  plugin.Fetcher
	exporter plugin.Exporter
	data     any
}

func newDaemonPlugins(plugins []plugin.Plugin) []daemonPlugin {
	var dps []daemonPlugin
	for _, p := range plugins {
		dp := daemonPlugin{name: p.Name()}
		if f, ok := p.(plugin.Fetcher); ok {
			dp.fetcher = f
		}
		if e, ok := p.(plugin.Exporter); ok {
			dp.exporter = e
		}
		if dp.fetcher != nil || dp.exporter != nil {
			dps = append(dps, dp)
		}
	}
	return dps
}

// fetchDaemonPlugins fetches data for all daemon plugins concurrently.
// Writes to dps[i].data happen inside goroutines, but wg.Wait() ensures
// all writes complete before this function returns. The caller (poll)
// only reads dps after this returns, so no additional synchronization is needed.
func fetchDaemonPlugins(ctx context.Context, dps []daemonPlugin, log *slog.Logger) {
	var wg sync.WaitGroup
	for i := range dps {
		if dps[i].fetcher == nil {
			continue
		}
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			data, err := plugin.SafeFetch(ctx, dps[i].fetcher)
			if err != nil {
				log.Warn("plugin fetch failed", "plugin", dps[i].name, "err", err)
			} else {
				dps[i].data = data
			}
		}(i)
	}
	wg.Wait()
}

func daemonPluginExports(dps []daemonPlugin) []plugin.PluginExport {
	var exports []plugin.PluginExport
	for _, dp := range dps {
		if dp.exporter != nil {
			exports = append(exports, plugin.PluginExport{
				Exporter: dp.exporter,
				Data:     dp.data,
			})
		}
	}
	return exports
}
