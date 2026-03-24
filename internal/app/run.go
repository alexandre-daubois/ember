package app

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/alexandre-daubois/ember/internal/fetcher"
	"github.com/spf13/cobra"
)

type config struct {
	addr          string
	interval      time.Duration
	slowThreshold int
	noColor       bool
	jsonMode      bool
	once          bool
	frankenphpPID int
	expose        string
	daemon        bool
	metricsPrefix string
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
  ember --json                            # pipe-friendly JSON output
  ember --json --once                     # single JSON snapshot and exit
  ember --expose :9191                    # TUI + Prometheus endpoint
  ember --expose :9191 --daemon           # headless metrics exporter`,
		SilenceUsage:  true,
		SilenceErrors: true,
		PreRunE: func(cmd *cobra.Command, args []string) error {
			return validate(&cfg)
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
			defer cancel()

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
			hasFrankenPHP := f.DetectFrankenPHP(ctx)
			f.FetchServerNames(ctx)

			switch {
			case cfg.jsonMode:
				runJSON(ctx, f, cfg.interval, cfg.once)
			case cfg.daemon:
				return runDaemon(ctx, f, &cfg)
			default:
				return runTUI(f, &cfg, hasFrankenPHP, version)
			}
			return nil
		},
	}

	pf := cmd.PersistentFlags()
	pf.StringVar(&cfg.addr, "addr", "http://localhost:2019", "Caddy admin API address")
	pf.DurationVar(&cfg.interval, "interval", 1*time.Second, "Polling interval")
	pf.IntVar(&cfg.frankenphpPID, "frankenphp-pid", 0, "FrankenPHP PID (auto-detected if not set)")

	f := cmd.Flags()
	f.IntVar(&cfg.slowThreshold, "slow-threshold", 500, "Slow request threshold in ms")
	f.BoolVar(&cfg.noColor, "no-color", false, "Disable colors")
	f.BoolVar(&cfg.jsonMode, "json", false, "JSON output mode (streaming JSONL)")
	f.BoolVar(&cfg.once, "once", false, "Output a single snapshot and exit (requires --json)")
	f.StringVar(&cfg.expose, "expose", "", "Expose Prometheus metrics (e.g. :9191)")
	f.BoolVar(&cfg.daemon, "daemon", false, "Headless mode (requires --expose)")
	f.StringVar(&cfg.metricsPrefix, "metrics-prefix", "", "Prefix for exported Prometheus metric names")

	cmd.AddCommand(newStatusCmd(&cfg))
	cmd.AddCommand(newWaitCmd(&cfg))
	cmd.SetVersionTemplate("ember {{.Version}}\n")

	return cmd
}

func Run(args []string, version string) error {
	cmd := newRootCmd(version)
	cmd.SetArgs(args)
	return cmd.Execute()
}

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
	return nil
}
