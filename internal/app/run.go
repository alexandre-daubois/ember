package app

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/alexandredaubois/ember/internal/fetcher"
)

type config struct {
	addr          string
	interval      time.Duration
	slowThreshold int
	noColor       bool
	jsonMode      bool
	pid           int
	expose        string
	daemon        bool
}

func Run(args []string, version string) error {
	var cfg config

	fs := flag.NewFlagSet("ember", flag.ContinueOnError)
	fs.StringVar(&cfg.addr, "addr", "http://localhost:2019", "Caddy admin API address")
	fs.DurationVar(&cfg.interval, "interval", 1*time.Second, "polling interval")
	fs.IntVar(&cfg.slowThreshold, "slow-threshold", 500, "slow request threshold (ms)")
	fs.BoolVar(&cfg.noColor, "no-color", false, "disable colors")
	fs.BoolVar(&cfg.jsonMode, "json", false, "JSON output mode")
	fs.IntVar(&cfg.pid, "pid", 0, "FrankenPHP process PID (auto-detected if not set)")
	fs.StringVar(&cfg.expose, "expose", "", "expose Prometheus metrics on address (e.g. :9191)")
	fs.BoolVar(&cfg.daemon, "daemon", false, "run in daemon mode (no TUI, requires --expose)")
	showVersion := fs.Bool("version", false, "show version")
	completion := fs.String("completion", "", "generate shell completions (bash, zsh, fish)")

	fs.Usage = func() { printUsage(fs.Output(), version) }

	if err := fs.Parse(args); err != nil {
		return err
	}

	if *showVersion {
		fmt.Printf("ember %s\n", version)
		return nil
	}

	if *completion != "" {
		return printCompletion(os.Stdout, *completion)
	}

	if err := validate(&cfg); err != nil {
		return err
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	pid := int32(cfg.pid)
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
		runJSON(ctx, f, cfg.interval)
	case cfg.daemon:
		runDaemon(ctx, f, &cfg)
	default:
		return runTUI(ctx, f, &cfg, hasFrankenPHP, version)
	}
	return nil
}

func validate(cfg *config) error {
	if cfg.daemon && cfg.expose == "" {
		return fmt.Errorf("--daemon requires --expose")
	}
	return nil
}
