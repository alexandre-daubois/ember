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

func reloadTLS(f fetcher.Fetcher, opts fetcher.TLSOptions, log *slog.Logger) {
	hf, ok := f.(*fetcher.HTTPFetcher)
	if !ok {
		log.Warn("TLS reload not supported for this fetcher")
		return
	}
	if hf.IsUnixSocket() {
		log.Info("TLS reload skipped (Unix socket connection)")
		return
	}
	if err := configureTLS(hf, opts); err != nil {
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

func newMetricsHandler(holder *exporter.StateHolder, cfg *config, perInstance map[string]time.Duration) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/metrics", exporter.Handler(holder, cfg.metricsPrefix, cfg.recorder))
	mux.HandleFunc("/healthz", exporter.HealthHandler(holder, cfg.interval, perInstance))
	mux.HandleFunc("/healthz/", exporter.InstanceHealthHandler(holder, cfg.interval, perInstance))

	var handler http.Handler = mux
	if cfg.metricsAuth != "" {
		user, pass, _ := strings.Cut(cfg.metricsAuth, ":")
		handler = exporter.BasicAuth(mux, user, pass)
	}

	return handler
}

func runDaemon(ctx context.Context, instances []*instance, cfg *config, plugins []plugin.Plugin) error {
	ctx, cancel := context.WithCancelCause(ctx)
	defer cancel(nil)

	holder := &exporter.StateHolder{}
	holder.SetMulti(isMulti(instances))

	dPlugins := newDaemonPlugins(plugins)

	srv := &http.Server{Addr: cfg.expose, Handler: newMetricsHandler(holder, cfg, perInstanceIntervals(instances))}

	go func() {
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			cancel(err)
		}
	}()

	log := cfg.logger
	log.Info("daemon started", "metrics_url", metricsURL(cfg.expose), "instances", len(instances))

	pollAll(ctx, instances, holder, dPlugins, log)

	multi := isMulti(instances)
	dumpChans := make([]chan struct{}, len(instances))
	var wg sync.WaitGroup
	for i, inst := range instances {
		dumpChans[i] = make(chan struct{}, 1)
		wg.Add(1)
		go func(inst *instance, dump <-chan struct{}) {
			defer wg.Done()
			pollLoop(ctx, inst, holder, multi, dPlugins, log, dump)
		}(inst, dumpChans[i])
	}

	dumpCh := dumpSignal()
	reloadCh := reloadSignal()

	for {
		select {
		case <-ctx.Done():
			shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
			_ = srv.Shutdown(shutdownCtx)
			shutdownCancel()
			wg.Wait()
			for _, inst := range instances {
				inst.fetcher.CloseIdleConnections()
			}
			if cause := context.Cause(ctx); cause != nil && !errors.Is(cause, context.Canceled) {
				return cause
			}
			return nil
		case <-dumpCh:
			for _, ch := range dumpChans {
				select {
				case ch <- struct{}{}:
				default:
				}
			}
		case <-reloadCh:
			for _, inst := range instances {
				rlog := log
				if multi {
					rlog = log.With("instance", inst.name)
				}
				reloadTLS(inst.fetcher, inst.tls, rlog)
			}
		}
	}
}

// pollLoop polls a single instance at its own interval. State reads (dump)
// happen on this same goroutine so they don't race with concurrent writes
// from the polling path.
func pollLoop(ctx context.Context, inst *instance, holder *exporter.StateHolder, multi bool, dps []daemonPlugin, log *slog.Logger, dumpCh <-chan struct{}) {
	ticker := time.NewTicker(inst.interval)
	defer ticker.Stop()
	ilog := log
	if multi {
		ilog = log.With("instance", inst.name)
	}
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			pollInstance(ctx, inst, holder, multi, dps, log)
		case <-dumpCh:
			dumpState(&inst.state, ilog)
		}
	}
}

func isMulti(instances []*instance) bool { return len(instances) >= 2 }

// perInstanceIntervals maps each instance name to its effective polling
// interval, used by /healthz to compute the staleness threshold per
// instance. In single-instance mode the holder keys the slot under ""
// (StoreAll), so the lookup must also be reachable via "" or the threshold
// falls back to the global --interval and reports stale between polls.
func perInstanceIntervals(instances []*instance) map[string]time.Duration {
	m := make(map[string]time.Duration, len(instances))
	for _, inst := range instances {
		m[inst.name] = inst.interval
	}
	if len(instances) == 1 {
		m[""] = instances[0].interval
	}
	return m
}

func pollAll(ctx context.Context, instances []*instance, holder *exporter.StateHolder, dps []daemonPlugin, log *slog.Logger) {
	multi := isMulti(instances)
	var wg sync.WaitGroup
	for _, inst := range instances {
		wg.Add(1)
		go func(inst *instance) {
			defer wg.Done()
			pollInstance(ctx, inst, holder, multi, dps, log)
		}(inst)
	}
	wg.Wait()
}

func pollInstance(ctx context.Context, inst *instance, holder *exporter.StateHolder, multi bool, dps []daemonPlugin, log *slog.Logger) {
	ilog := log
	if multi {
		ilog = log.With("instance", inst.name)
	}
	snap, err := inst.fetcher.Fetch(ctx)
	if err != nil {
		inst.throttle.record(ilog, err)
		return
	}
	inst.throttle.recover(ilog)
	inst.state.Update(snap)

	if multi {
		exports := fetchInstancePluginExports(ctx, dps, inst, snap, ilog)
		holder.StoreInstance(inst.name, inst.addr, inst.state.CopyForExport(), exports, inst.recorder)
		return
	}

	var exports []plugin.PluginExport
	if len(dps) > 0 {
		notifyDaemonSubscribers(dps, snap)
		fetchDaemonPlugins(ctx, dps, ilog)
		exports = daemonPluginExports(dps)
	}
	holder.StoreAll(inst.state.CopyForExport(), exports)
}

// fetchInstancePluginExports runs Fetch on every multi-aware plugin with the
// given instance attached to the context, then returns one PluginExport per
// plugin so the exporter can emit per-instance metrics. Plugins are filtered
// at Provision time, so dps here only ever contains MultiInstancePlugin
// implementations in multi mode. Each call computes its own data slice; we
// must not write back to dp.data because instance pollers run concurrently.
func fetchInstancePluginExports(ctx context.Context, dps []daemonPlugin, inst *instance, snap *fetcher.Snapshot, log *slog.Logger) []plugin.PluginExport {
	if len(dps) == 0 {
		return nil
	}
	pi := plugin.PluginInstance{Name: inst.name, Addr: inst.addr}
	pctx := plugin.WithInstance(ctx, pi)

	notifyDaemonSubscribers(dps, snap)

	var exports []plugin.PluginExport
	for _, dp := range dps {
		if dp.exporter == nil {
			continue
		}
		var data any
		if dp.fetcher != nil {
			d, err := plugin.SafeFetch(pctx, dp.fetcher)
			if err != nil {
				log.Warn("plugin fetch failed", "plugin", dp.name, "err", err)
				continue
			}
			data = d
		}
		exports = append(exports, plugin.PluginExport{
			Exporter: dp.exporter,
			Data:     data,
		})
	}
	return exports
}

type daemonPlugin struct {
	p        plugin.Plugin
	name     string
	fetcher  plugin.Fetcher
	exporter plugin.Exporter
	data     any
}

func newDaemonPlugins(plugins []plugin.Plugin) []daemonPlugin {
	var dps []daemonPlugin
	for _, p := range plugins {
		dp := daemonPlugin{p: p, name: p.Name()}
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

func notifyDaemonSubscribers(dps []daemonPlugin, snap *fetcher.Snapshot) {
	for _, dp := range dps {
		if sub, ok := dp.p.(plugin.MetricsSubscriber); ok {
			plugin.SafeOnMetrics(sub, snap)
		}
	}
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
