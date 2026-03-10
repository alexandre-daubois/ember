package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/alexandredaubois/frankentop/internal/fetcher"
	"github.com/alexandredaubois/frankentop/internal/ui"
	tea "github.com/charmbracelet/bubbletea"
)

type Config struct {
	Addr          string
	Interval      time.Duration
	SlowThreshold int
	LeakThreshold int
	LeakWindow    int
	NoColor       bool
	JSONMode      bool
	PID           int
}

var version = "0.1.0-dev"

func main() {
	var cfg Config

	flag.StringVar(&cfg.Addr, "addr", "http://localhost:2019", "Caddy admin API address")
	flag.DurationVar(&cfg.Interval, "interval", 1*time.Second, "polling interval")
	flag.IntVar(&cfg.SlowThreshold, "slow-threshold", 500, "slow request threshold (ms)")
	flag.IntVar(&cfg.LeakThreshold, "leak-threshold", 5, "leak detection threshold (MB)")
	flag.IntVar(&cfg.LeakWindow, "leak-window", 20, "leak watcher sample window")
	flag.BoolVar(&cfg.NoColor, "no-color", false, "disable colors")
	flag.BoolVar(&cfg.JSONMode, "json", false, "JSON output mode")
	flag.IntVar(&cfg.PID, "pid", 0, "FrankenPHP process PID (auto-detected if not set)")
	showVersion := flag.Bool("version", false, "show version")
	flag.Parse()

	if *showVersion {
		fmt.Printf("ftop %s\n", version)
		return
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	pid := int32(cfg.PID)
	if pid == 0 {
		detected, err := fetcher.FindFrankenPHPProcess(ctx)
		if err != nil {
			fmt.Fprintf(os.Stderr, "warning: %v\n", err)
		} else {
			pid = detected
		}
	}

	f := fetcher.NewHTTPFetcher(cfg.Addr, pid)

	if cfg.JSONMode {
		runJSON(ctx, f, cfg.Interval)
		return
	}

	app := ui.NewApp(f, ui.Config{
		Interval:      cfg.Interval,
		SlowThreshold: time.Duration(cfg.SlowThreshold) * time.Millisecond,
		LeakThreshold: cfg.LeakThreshold,
		LeakWindow:    cfg.LeakWindow,
		NoColor:       cfg.NoColor,
	})
	p := tea.NewProgram(app, tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func runJSON(ctx context.Context, f *fetcher.HTTPFetcher, interval time.Duration) {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")

	poll := func() {
		snap, err := f.Fetch(ctx)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			return
		}
		enc.Encode(snap)
	}

	poll()

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			poll()
		}
	}
}
