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

	"github.com/alexandredaubois/ember/internal/fetcher"
	"github.com/alexandredaubois/ember/internal/model"
	"github.com/alexandredaubois/ember/internal/ui"
	tea "github.com/charmbracelet/bubbletea"
)

type Config struct {
	Addr          string
	Interval      time.Duration
	SlowThreshold int
	NoColor       bool
	JSONMode      bool
	PID           int
}

var version = "1.0.0-dev"

func main() {
	var cfg Config

	flag.StringVar(&cfg.Addr, "addr", "http://localhost:2019", "Caddy admin API address")
	flag.DurationVar(&cfg.Interval, "interval", 1*time.Second, "polling interval")
	flag.IntVar(&cfg.SlowThreshold, "slow-threshold", 500, "slow request threshold (ms)")
	flag.BoolVar(&cfg.NoColor, "no-color", false, "disable colors")
	flag.BoolVar(&cfg.JSONMode, "json", false, "JSON output mode")
	flag.IntVar(&cfg.PID, "pid", 0, "FrankenPHP process PID (auto-detected if not set)")
	showVersion := flag.Bool("version", false, "show version")
	flag.Parse()

	if *showVersion {
		fmt.Printf("ember %s\n", version)
		return
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	pid := int32(cfg.PID)
	if pid == 0 {
		detected, err := fetcher.FindFrankenPHPProcess(ctx)
		if err != nil {
			detected, err = fetcher.FindCaddyProcess(ctx)
			if err != nil && cfg.JSONMode {
				fmt.Fprintf(os.Stderr, "warning: no frankenphp or caddy process found\n")
			}
		}
		if err == nil {
			pid = detected
		}
	}

	f := fetcher.NewHTTPFetcher(cfg.Addr, pid)
	hasFrankenPHP := f.DetectFrankenPHP(ctx)
	f.FetchServerNames(ctx)

	if cfg.JSONMode {
		runJSON(ctx, f, cfg.Interval)
		return
	}

	app := ui.NewApp(f, ui.Config{
		Interval:      cfg.Interval,
		SlowThreshold: time.Duration(cfg.SlowThreshold) * time.Millisecond,
		NoColor:       cfg.NoColor,
		Version:       version,
		HasFrankenPHP: hasFrankenPHP,
	})
	p := tea.NewProgram(app, tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

type jsonOutput struct {
	Threads   fetcher.ThreadsResponse `json:"threads"`
	Metrics   fetcher.MetricsSnapshot `json:"metrics"`
	Process   fetcher.ProcessMetrics  `json:"process"`
	FetchedAt time.Time               `json:"fetchedAt"`
	Errors    []string                `json:"errors,omitempty"`
	Derived   *jsonDerived            `json:"derived,omitempty"`
}

type jsonDerived struct {
	RPS     float64  `json:"rps"`
	AvgTime float64  `json:"avgTime"`
	P50     *float64 `json:"p50,omitempty"`
	P95     *float64 `json:"p95,omitempty"`
	P99     *float64 `json:"p99,omitempty"`
}

func runJSON(ctx context.Context, f *fetcher.HTTPFetcher, interval time.Duration) {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	var state model.State

	poll := func() {
		snap, err := f.Fetch(ctx)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			return
		}
		state.Update(snap)

		out := jsonOutput{
			Threads:   snap.Threads,
			Metrics:   snap.Metrics,
			Process:   snap.Process,
			FetchedAt: snap.FetchedAt,
			Errors:    snap.Errors,
			Derived: &jsonDerived{
				RPS:     state.Derived.RPS,
				AvgTime: state.Derived.AvgTime,
			},
		}
		if state.Derived.HasPercentiles {
			out.Derived.P50 = &state.Derived.P50
			out.Derived.P95 = &state.Derived.P95
			out.Derived.P99 = &state.Derived.P99
		}
		enc.Encode(out)
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
