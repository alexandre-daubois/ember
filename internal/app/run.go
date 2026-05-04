package app

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/alexandre-daubois/ember/internal/fetcher"
	"github.com/alexandre-daubois/ember/internal/instrumentation"
	"github.com/alexandre-daubois/ember/pkg/plugin"
	"github.com/spf13/cobra"
)

type config struct {
	addrsRaw      []string
	addrs         []addrSpec
	interval      time.Duration
	timeout       time.Duration
	slowThreshold int
	noColor       bool
	jsonMode      bool
	once          bool
	frankenphpPID int
	expose        string
	daemon        bool
	metricsPrefix string
	logFormat     string
	logger        *slog.Logger
	caCert        string
	clientCert    string
	clientKey     string
	insecure      bool
	metricsAuth   string
	recorder      *instrumentation.Recorder
	logListen     string
}

func Run(args []string, version string) error {
	cmd := newRootCmd(version)
	cmd.SetArgs(args)
	return cmd.Execute()
}

func newRootCmd(version string) *cobra.Command {
	var cfg config

	cmd := &cobra.Command{
		Use:     "ember [flags]",
		Short:   "Real-time monitoring for Caddy & FrankenPHP",
		Version: version,
		Long: `Ember - Real-time monitoring for Caddy & FrankenPHP

Monitor your Caddy server in real time: per-host traffic, latency
percentiles, status codes, and more. When FrankenPHP is detected,
unlock per-thread introspection, worker management, and memory tracking.

Keybindings:
  Tab / 1-9         Switch tab
  Up / Down / j / k Navigate list
  Home / End        Jump to first / last item
  PgUp / PgDn       Page navigation
  Enter              Open detail panel
  s / S              Cycle sort field
  p                  Pause / resume
  r                  Restart workers (FrankenPHP)
  /                  Filter
  g                  Full-screen graphs
  ?                  Help overlay
  q                  Quit`,
		Example: `  ember                                  # default: localhost:2019
  ember --addr http://prod:2019           # custom address
  ember --addr unix//run/caddy/admin.sock # Unix socket
  ember --json                            # pipe-friendly JSON output
  ember --json --once                     # single JSON snapshot and exit
  ember --expose :9191                    # TUI + Prometheus endpoint
  ember --expose :9191 --daemon           # headless metrics exporter
  ember --daemon --expose :9191 \
        --addr web1=https://web1.fr \
        --addr web2=https://web2.fr     # multi-instance daemon
  ember --daemon --expose :9191 \
        --addr web1=https://a,ca=/etc/ca1.pem \
        --addr web2=https://b,ca=/etc/ca2.pem # per-instance TLS`,
		SilenceUsage:  true,
		SilenceErrors: true,
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			bindEnv(cmd)
			if _, ok := os.LookupEnv("NO_COLOR"); ok {
				cfg.noColor = true
			}
			initLogger(&cfg)
			return validate(&cfg)
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
			defer cancel()
			ctx, tCancel := contextWithTimeout(ctx, cfg.timeout)
			defer tCancel()

			multi := len(cfg.addrs) >= 2
			warnMultiLimitations(&cfg, multi)

			if multi && !cfg.jsonMode && !cfg.daemon {
				return fmt.Errorf("the interactive TUI is single-instance by design; use --daemon (Prometheus aggregation) or --json (JSONL stream) to monitor multiple Caddy instances at once. See docs/cli-reference.md#multi-instance-monitoring")
			}

			instances, err := newInstances(ctx, &cfg, cmd.Version)
			if err != nil {
				return err
			}

			plugins := provisionPlugins(ctx, &cfg, multi)
			defer closePlugins(plugins)

			switch {
			case cfg.jsonMode:
				return runJSON(ctx, instances, &cfg)
			case cfg.daemon:
				return runDaemon(ctx, instances, &cfg, plugins)
			default:
				inst := instances[0]
				hasFrankenPHP := inst.fetcher.DetectFrankenPHP(ctx)
				inst.fetcher.FetchServerNames(ctx)
				return runTUI(inst.fetcher, &cfg, hasFrankenPHP, cmd.Version, plugins)
			}
		},
	}

	pf := cmd.PersistentFlags()
	pf.StringArrayVar(&cfg.addrsRaw, "addr", []string{"http://localhost:2019"}, "Caddy admin API address (http://, https://, or unix//path). Repeatable in --daemon, --json, status and wait modes; supports name=url aliases and per-instance suffixes (,ca=PATH ,cert=PATH ,key=PATH ,insecure ,interval=DUR).")
	pf.DurationVarP(&cfg.interval, "interval", "i", 1*time.Second, "Polling interval")
	pf.DurationVar(&cfg.timeout, "timeout", 0, "Global timeout (0 = no timeout)")
	pf.IntVar(&cfg.frankenphpPID, "frankenphp-pid", 0, "FrankenPHP PID (auto-detected if not set; ignored when --addr is repeated)")
	pf.StringVar(&cfg.caCert, "ca-cert", "", "Path to CA certificate for TLS verification")
	pf.StringVar(&cfg.clientCert, "client-cert", "", "Path to client certificate for mTLS")
	pf.StringVar(&cfg.clientKey, "client-key", "", "Path to client private key for mTLS")
	pf.BoolVar(&cfg.insecure, "insecure", false, "Skip TLS certificate verification")

	f := cmd.Flags()
	f.IntVar(&cfg.slowThreshold, "slow-threshold", 500, "Slow request threshold in ms")
	f.BoolVar(&cfg.noColor, "no-color", false, "Disable colors")
	f.BoolVar(&cfg.jsonMode, "json", false, "JSON output mode (streaming JSONL)")
	f.BoolVar(&cfg.once, "once", false, "Output a single snapshot and exit (requires --json)")
	f.StringVar(&cfg.expose, "expose", "", "Expose Prometheus metrics (e.g. :9191)")
	f.BoolVar(&cfg.daemon, "daemon", false, "Headless mode (requires --expose)")
	f.StringVar(&cfg.metricsPrefix, "metrics-prefix", "", "Prefix for exported Prometheus metric names")
	f.StringVar(&cfg.logFormat, "log-format", "text", "Log format for daemon/json modes (text or json)")
	f.StringVar(&cfg.metricsAuth, "metrics-auth", "", "Basic auth for metrics endpoint (user:password)")
	f.StringVar(&cfg.logListen, "log-listen", "", "Receive logs from Caddy via TCP, e.g. ':9210' or '127.0.0.1:9210'. Required when Caddy is on a remote host; auto-bound on a local loopback port otherwise.")

	cmd.AddCommand(newStatusCmd(&cfg))
	cmd.AddCommand(newWaitCmd(&cfg))
	cmd.AddCommand(newVersionCmd(version))
	cmd.AddCommand(newDiffCmd(&cfg))
	cmd.AddCommand(newInitCmd(&cfg))
	cmd.SetVersionTemplate("ember {{.Version}}\n")

	return cmd
}

func contextWithTimeout(parent context.Context, timeout time.Duration) (context.Context, context.CancelFunc) {
	if timeout > 0 {
		return context.WithTimeout(parent, timeout)
	}
	return parent, func() {}
}

func configureTLS(f *fetcher.HTTPFetcher, opts fetcher.TLSOptions) error {
	if f.IsUnixSocket() {
		return nil
	}
	tlsCfg, err := fetcher.BuildTLSConfig(opts)
	if err != nil {
		return err
	}
	if tlsCfg != nil {
		f.SetTLSConfig(tlsCfg)
	}
	return nil
}

func initLogger(cfg *config) {
	switch cfg.logFormat {
	case "json":
		cfg.logger = slog.New(slog.NewJSONHandler(os.Stderr, nil))
	default:
		cfg.logger = slog.New(slog.NewTextHandler(os.Stderr, nil))
	}
}

var envBindings = map[string]string{
	"addr":           "EMBER_ADDR",
	"interval":       "EMBER_INTERVAL",
	"expose":         "EMBER_EXPOSE",
	"metrics-prefix": "EMBER_METRICS_PREFIX",
	"metrics-auth":   "EMBER_METRICS_AUTH",
	"log-listen":     "EMBER_LOG_LISTEN",
}

func bindEnv(cmd *cobra.Command) {
	for name, env := range envBindings {
		f := cmd.Flag(name)
		if f == nil || f.Changed {
			continue
		}
		val, ok := os.LookupEnv(env)
		if !ok {
			continue
		}
		if name == "addr" {
			for v := range strings.SplitSeq(val, ";") {
				v = strings.TrimSpace(v)
				if v == "" {
					continue
				}
				_ = f.Value.Set(v)
			}
			continue
		}
		_ = f.Value.Set(val)
	}
}

const minInterval = 100 * time.Millisecond

func validate(cfg *config) error {
	if cfg.daemon && cfg.expose == "" {
		return fmt.Errorf("--daemon requires --expose")
	}
	if cfg.once && !cfg.jsonMode {
		return fmt.Errorf("--once requires --json")
	}
	if cfg.once && cfg.daemon {
		return fmt.Errorf("--once is incompatible with --daemon")
	}
	if cfg.interval < minInterval {
		return fmt.Errorf("--interval must be at least %s", minInterval)
	}

	addrs, err := parseAddrs(cfg.addrsRaw)
	if err != nil {
		return err
	}
	cfg.addrs = addrs

	if cfg.timeout > 0 {
		maxInterval := cfg.interval
		for _, spec := range cfg.addrs {
			if spec.interval > maxInterval {
				maxInterval = spec.interval
			}
		}
		if cfg.timeout < maxInterval {
			return fmt.Errorf("--timeout (%s) must be at least the largest polling interval (%s)", cfg.timeout, maxInterval)
		}
	}

	for _, spec := range cfg.addrs {
		if fetcher.IsUnixAddr(spec.url) && (cfg.caCert != "" || cfg.clientCert != "" || cfg.clientKey != "" || cfg.insecure) {
			return fmt.Errorf("TLS options cannot be used with Unix socket addresses")
		}
	}

	if cfg.metricsAuth != "" {
		user, pass, ok := strings.Cut(cfg.metricsAuth, ":")
		if !ok || user == "" || pass == "" {
			return fmt.Errorf("--metrics-auth must be in user:password format (both parts required)")
		}
		if cfg.expose == "" {
			return fmt.Errorf("--metrics-auth requires --expose")
		}
	}
	if cfg.metricsPrefix != "" && !isValidMetricPrefix(cfg.metricsPrefix) {
		return fmt.Errorf("--metrics-prefix %q is not a valid Prometheus metric name prefix (allowed: letters, digits, underscores; must not start with a digit; e.g. \"my_app\")", cfg.metricsPrefix)
	}
	return nil
}

// isValidMetricPrefix reports whether s is a legal leading segment of a
// Prometheus metric name. The Prometheus spec allows [a-zA-Z_:][a-zA-Z0-9_:]*,
// but ':' is conventionally reserved for recording rule outputs, so we keep
// the prefix to the safer underscore-only subset.
func isValidMetricPrefix(s string) bool {
	for i, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r == '_':
			// always allowed
		case r >= '0' && r <= '9':
			if i == 0 {
				return false
			}
		default:
			return false
		}
	}
	return true
}

func warnMultiLimitations(cfg *config, multi bool) {
	if !multi {
		return
	}
	if cfg.frankenphpPID != 0 {
		cfg.logger.Warn("--frankenphp-pid is ignored in multi-instance mode")
	}
}

// provisionPlugins runs Provision on every registered plugin and returns the
// ones that succeeded. A plugin whose Provision returns an error is logged as
// a warning and dropped; Ember continues without it instead of aborting.
// Plugins are skipped entirely in multi-instance mode.
func provisionPlugins(ctx context.Context, cfg *config, multi bool) []plugin.Plugin {
	all := plugin.All()
	if len(all) == 0 {
		return nil
	}
	if multi {
		for _, p := range all {
			cfg.logger.Warn("plugin disabled in multi-instance mode (issue #36)", "plugin", p.Name())
		}
		return nil
	}

	var ready []plugin.Plugin
	for _, p := range all {
		pcfg := plugin.PluginConfig{
			CaddyAddr: cfg.addrs[0].url,
			Options:   pluginEnvOptions(p.Name()),
		}
		if err := p.Provision(ctx, pcfg); err != nil {
			cfg.logger.Warn("plugin disabled: Provision failed",
				"plugin", p.Name(), "error", err)
			continue
		}
		ready = append(ready, p)
	}
	return ready
}

func closePlugins(plugins []plugin.Plugin) {
	for i := len(plugins) - 1; i >= 0; i-- {
		if c, ok := plugins[i].(plugin.Closer); ok {
			_ = c.Close()
		}
	}
}

func pluginEnvOptions(name string) map[string]string {
	prefix := plugin.EnvPrefix(name)
	opts := make(map[string]string)
	for _, env := range os.Environ() {
		if !strings.HasPrefix(env, prefix) {
			continue
		}
		kv := strings.SplitN(env[len(prefix):], "=", 2)
		if len(kv) == 2 {
			opts[strings.ToLower(kv[0])] = kv[1]
		}
	}
	return opts
}
