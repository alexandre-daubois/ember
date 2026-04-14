package app

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/alexandre-daubois/ember/internal/exporter"
	"github.com/alexandre-daubois/ember/internal/fetcher"
	"github.com/alexandre-daubois/ember/internal/model"
	"github.com/alexandre-daubois/ember/internal/ui"
	tea "github.com/charmbracelet/bubbletea"
)

func runTUI(f fetcher.Fetcher, cfg *config, hasFrankenPHP bool, version string) error {
	uiCfg := ui.Config{
		Interval:      cfg.interval,
		SlowThreshold: time.Duration(cfg.slowThreshold) * time.Millisecond,
		NoColor:       cfg.noColor,
		Version:       version,
		HasFrankenPHP: hasFrankenPHP,
	}

	// Bubble Tea intercepts SIGINT, but not SIGTERM. Without this trap a
	// `systemctl stop` or `kill <pid>` would skip our defer chain (and leave
	// a stale "__ember__" sink in Caddy). The relay is installed *before*
	// setupLogSource so a signal received during admin-API registration
	// (which may block up to 3s) still drains through the defer chain
	// instead of Go's default terminate-on-signal handler.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGTERM)
	defer close(sigCh)
	defer signal.Stop(sigCh)

	logCleanup := setupLogSource(cfg, f, &uiCfg)
	defer logCleanup()

	var srv *http.Server
	if cfg.expose != "" {
		holder := &exporter.StateHolder{}
		uiCfg.OnStateUpdate = func(s model.State) {
			holder.Store(s.CopyForExport())
		}

		srv = &http.Server{Addr: cfg.expose, Handler: newMetricsHandler(holder, cfg)}

		listenErr := make(chan error, 1)
		go func() {
			if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
				listenErr <- fmt.Errorf("metrics server on %s: %w", cfg.expose, err)
			}
		}()

		select {
		case err := <-listenErr:
			return err
		case <-time.After(50 * time.Millisecond):
		}

		uiCfg.MetricsServerErr = listenErr
	}

	app := ui.NewApp(f, uiCfg)
	p := tea.NewProgram(app, tea.WithAltScreen())

	// Goroutine started after p is initialized. A SIGTERM received during
	// setup is buffered in sigCh (capacity 1) and picked up here.
	go func() {
		if _, ok := <-sigCh; ok {
			p.Quit()
		}
	}()

	if _, err := p.Run(); err != nil {
		return err
	}

	if srv != nil {
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer shutdownCancel()
		_ = srv.Shutdown(shutdownCtx)
	}

	return nil
}

// setupLogSource starts streaming Caddy access logs into the UI buffer.
// Strategy:
//  1. --log-listen <addr>: bind on the given address (e.g. ":9210" for remote
//     Caddy).
//  2. Auto: when Caddy looks reachable from the same host, bind a free
//     loopback port and ask Caddy to push logs to it.
//
// In both modes Ember hot-registers an "__ember__" sink in Caddy and enables
// access logging on every server that did not already have a logs block. The
// returned cleanup function reverses both changes.
func setupLogSource(cfg *config, f fetcher.Fetcher, uiCfg *ui.Config) func() {
	addr := cfg.logListen
	if addr == "" {
		if !isLocalAdminAddr(cfg.addr) {
			return func() {}
		}
		addr = "127.0.0.1:0"
	}
	if cleanup, ok := startNetListener(addr, f, uiCfg); ok {
		return cleanup
	}
	return func() {}
}

var sinkWatchdogInterval = 30 * time.Second

// startNetListener opens a TCP listener and asks Caddy to push access logs
// into it via a hot-registered sink. A background watchdog re-registers the
// sink if Caddy is reloaded (which wipes runtime config) or was not reachable
// at startup. Returns ok=false only when the local TCP bind fails or the
// fetcher is not an HTTPFetcher.
func startNetListener(addr string, f fetcher.Fetcher, uiCfg *ui.Config) (func(), bool) {
	noop := func() {}
	hf, ok := f.(*fetcher.HTTPFetcher)
	if !ok {
		return noop, false
	}

	// Try to bind directly on the requested address. When the host part
	// cannot be resolved locally (e.g. "host.docker.internal:9210"), fall
	// back to binding on just the port and advertise the original address
	// to Caddy so a containerised Caddy can reach the host.
	var advertiseAddr string
	ln, err := fetcher.NewLogNetListener(addr)
	if err != nil {
		_, port, splitErr := net.SplitHostPort(addr)
		if splitErr != nil {
			return noop, false
		}
		ln, err = fetcher.NewLogNetListener(":" + port)
		if err != nil {
			return noop, false
		}
		advertiseAddr = addr
	}
	if advertiseAddr == "" {
		advertiseAddr = ln.Addr()
	}
	warnIfPublicListener(ln.Addr())

	// PUT on the sink endpoint is idempotent: Caddy replaces an
	// existing sink with the new definition. A stale entry left by a prior
	// crash (pointing at a dead port) is naturally overwritten here, so no
	// defensive DELETE is needed. If Caddy is not yet reachable (e.g. Ember
	// started first), the watchdog retries periodically until it succeeds.
	regCtx, regCancel := context.WithTimeout(context.Background(), 3*time.Second)
	_ = hf.RegisterEmberLogSink(regCtx, advertiseAddr)
	regCancel()

	// Caddy only emits access logs when a server has a `logs` block. Without
	// this step the sink would receive nothing, defeating the zero-config
	// promise. enableAccessLogs records which servers we touched so we can
	// undo only those at cleanup.
	enabled := enableAccessLogs(hf)

	buf := model.NewLogBuffer(0)
	uiCfg.LogBuffer = buf
	uiCfg.LogSource = "net " + advertiseAddr

	ctx, cancel := context.WithCancel(context.Background())
	go ln.Start(ctx, func(batch []fetcher.LogEntry) {
		for _, e := range batch {
			buf.Append(e)
		}
	})

	// Watchdog: re-registers the sink and access-logs blocks periodically.
	// Covers both Caddy reloads (which wipe runtime config) and late Caddy
	// starts (where the initial registration could not reach the admin API).
	// `enabled` is only read/written by this single goroutine; the cleanup
	// closure reads it after watchdogDone.Wait(), which provides the
	// happens-before guarantee — no mutex needed.
	var watchdogDone sync.WaitGroup
	watchdogDone.Go(func() {
		ticker := time.NewTicker(sinkWatchdogInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				checkCtx, checkCancel := context.WithTimeout(ctx, 3*time.Second)
				exists := hf.CheckEmberLogSink(checkCtx)
				checkCancel()
				if !exists {
					reregCtx, reregCancel := context.WithTimeout(ctx, 3*time.Second)
					_ = hf.RegisterEmberLogSink(reregCtx, advertiseAddr)
					reregCancel()
				}
				// Always retry enableAccessLogs when the enabled list
				// is empty: the sink may have been registered on a
				// prior tick but enableAccessLogs may have failed
				// (e.g. Caddy's server list was not ready yet).
				if !exists || len(enabled) == 0 {
					enabled = enableAccessLogs(hf)
				}
			}
		}
	})

	return func() {
		cancel()
		ln.Close()
		watchdogDone.Wait()
		unregCtx, unregCancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer unregCancel()
		_ = hf.UnregisterEmberLogSink(unregCtx)
		restoreAccessLogs(hf, enabled)
	}, true
}

// enableAccessLogs walks every HTTP server known to Caddy and turns on access
// logging on those that did not already have a `logs` block. Returns the list
// of server names we modified, so restoreAccessLogs can undo only that subset.
func enableAccessLogs(hf *fetcher.HTTPFetcher) []string {
	servers := hf.ServerNames()
	if len(servers) == 0 {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		servers = hf.FetchServerNames(ctx)
	}
	var enabled []string
	for _, name := range servers {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		ok, err := hf.EnableServerAccessLogs(ctx, name)
		cancel()
		if err == nil && ok {
			enabled = append(enabled, name)
		}
	}
	return enabled
}

func restoreAccessLogs(hf *fetcher.HTTPFetcher, names []string) {
	for _, name := range names {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		_ = hf.RestoreServerAccessLogs(ctx, name)
		cancel()
	}
}

// warnIfPublicListener prints a stderr warning when the log listener ends up
// bound on a non-loopback address. Access logs contain hostnames, URIs and
// remote IPs: a 0.0.0.0 bind exposes that content to anyone on the network.
// This runs before the TUI starts so the message is visible.
func warnIfPublicListener(addr string) {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return
	}
	if ip.IsLoopback() {
		return
	}
	fmt.Fprintf(os.Stderr,
		"warning: log listener bound on %s -- access log contents will be readable by any host that can reach this port\n",
		addr)
}

// isLocalAdminAddr reports whether the configured Caddy admin address points
// at the same host. Used to decide whether we can hand Caddy a loopback
// listener address.
func isLocalAdminAddr(addr string) bool {
	if fetcher.IsUnixAddr(addr) {
		return true
	}
	host := addr
	// Only strip the path after the host when a real scheme was present: a
	// raw input like "/10.0.0.5" (no scheme) has no URL path to strip, and
	// cutting at the first slash would reduce it to "" which matches the
	// loopback case. That would falsely classify a garbage address as local
	// and expose logs on a loopback listener Caddy cannot reach.
	schemeTrimmed := false
	for _, prefix := range []string{"http://", "https://"} {
		if rest, ok := strings.CutPrefix(host, prefix); ok {
			host = rest
			schemeTrimmed = true
			break
		}
	}
	if schemeTrimmed {
		if i := strings.Index(host, "/"); i >= 0 {
			host = host[:i]
		}
		// "http://" or "http:///foo": a scheme without an authority is
		// malformed. Refuse to auto-bind a loopback listener rather than
		// falling through to the empty-host "local" branch.
		if host == "" {
			return false
		}
	}
	// SplitHostPort understands bracketed IPv6 like [::1]:2019. When the
	// address has no port, it fails: in that case the whole string is the
	// host (after stripping brackets).
	//
	// SplitHostPort is lenient about the port value (it only splits on the
	// last colon). ":10.0.0.5" parses as host="" port="10.0.0.5", which would
	// otherwise fall into the empty-host "local" branch. A non-numeric port
	// is a signal the whole string is malformed, not a loopback address.
	if h, p, err := net.SplitHostPort(host); err == nil {
		if _, perr := strconv.ParseUint(p, 10, 16); perr != nil {
			return false
		}
		host = h
	} else {
		host = strings.TrimPrefix(strings.TrimSuffix(host, "]"), "[")
	}
	switch host {
	case "", "localhost", "127.0.0.1", "::1":
		return true
	}
	return false
}
