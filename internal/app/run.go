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
	"github.com/spf13/cobra"
)

type config struct {
	addr          string
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
  Tab / 1 / 2      Switch between Caddy and FrankenPHP tabs
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
  ember --expose :9191 --daemon           # headless metrics exporter`,
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

			pid := int32(cfg.frankenphpPID)
			if pid == 0 {
				detected, err := fetcher.FindFrankenPHPProcess(ctx)
				if err != nil {
					detected, err = fetcher.FindCaddyProcess(ctx)
					if err != nil && (cfg.jsonMode || cfg.daemon) {
						fmt.Fprintf(os.Stderr, "warning: no frankenphp or caddy process found\n")
					}
				}
				if err == nil {
					pid = detected
				}
			}

			f := fetcher.NewHTTPFetcher(cfg.addr, pid)
			if err := configureTLS(f, &cfg); err != nil {
				return err
			}
			if cfg.expose != "" {
				cfg.recorder = instrumentation.New(version)
				f.SetRecorder(cfg.recorder)
			}
			hasFrankenPHP := f.DetectFrankenPHP(ctx)
			f.FetchServerNames(ctx)

			switch {
			case cfg.jsonMode:
				runJSON(ctx, f, cfg.interval, cfg.once, cfg.logger)
			case cfg.daemon:
				return runDaemon(ctx, f, &cfg)
			default:
				return runTUI(f, &cfg, hasFrankenPHP, version)
			}
			return nil
		},
	}

	pf := cmd.PersistentFlags()
	pf.StringVar(&cfg.addr, "addr", "http://localhost:2019", "Caddy admin API address (http://, https://, or unix//path)")
	pf.DurationVarP(&cfg.interval, "interval", "i", 1*time.Second, "Polling interval")
	pf.DurationVar(&cfg.timeout, "timeout", 0, "Global timeout (0 = no timeout)")
	pf.IntVar(&cfg.frankenphpPID, "frankenphp-pid", 0, "FrankenPHP PID (auto-detected if not set)")
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
	cmd.AddCommand(newDiffCmd())
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

func configureTLS(f *fetcher.HTTPFetcher, cfg *config) error {
	if f.IsUnixSocket() {
		return nil
	}
	tlsCfg, err := fetcher.BuildTLSConfig(fetcher.TLSOptions{
		CACert:     cfg.caCert,
		ClientCert: cfg.clientCert,
		ClientKey:  cfg.clientKey,
		Insecure:   cfg.insecure,
	})
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
		if val, ok := os.LookupEnv(env); ok {
			_ = f.Value.Set(val)
		}
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
	if cfg.timeout > 0 && cfg.timeout < cfg.interval {
		return fmt.Errorf("--timeout (%s) must be at least --interval (%s)", cfg.timeout, cfg.interval)
	}
	if !strings.HasPrefix(cfg.addr, "http://") && !strings.HasPrefix(cfg.addr, "https://") && !fetcher.IsUnixAddr(cfg.addr) {
		return fmt.Errorf("--addr must start with http://, https://, or unix//")
	}
	if fetcher.IsUnixAddr(cfg.addr) {
		if _, ok := fetcher.ParseUnixAddr(cfg.addr); !ok {
			return fmt.Errorf("--addr must include a non-empty Unix socket path")
		}
		if cfg.caCert != "" || cfg.clientCert != "" || cfg.clientKey != "" || cfg.insecure {
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
